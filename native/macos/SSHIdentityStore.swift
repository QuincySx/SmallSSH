import Foundation
import Security
import LocalAuthentication

// Password items cannot be queried with ReturnData + MatchLimitAll on macOS.
// Enumerate their public account attributes, then fetch each exact item. This
// preserves the existing opaque-record format and never masks a storage error
// as an empty key list.
struct SSHIdentityStoredRecord {
    let account: String
    let data: Data
}

final class SSHIdentityStore {
    private let service: String
    private let dataProtection: Bool
    private let nonInteractive: Bool
    init(service: String, dataProtection: Bool, nonInteractive: Bool = false) {
        self.service = service; self.dataProtection = dataProtection; self.nonInteractive = nonInteractive
    }
    private func query(_ account: String? = nil) -> [CFString: Any] {
        var q: [CFString: Any] = [kSecClass: kSecClassGenericPassword, kSecAttrService: service]
        if dataProtection { q[kSecUseDataProtectionKeychain] = true }
        if nonInteractive { let context = LAContext(); context.interactionNotAllowed = true; q[kSecUseAuthenticationContext] = context }
        if let account { q[kSecAttrAccount] = account }
        return q
    }
    private func error(_ action: String, _ status: OSStatus) -> NSError {
        NSError(domain: "SmallSSHIdentityStorage", code: Int(status), userInfo: [NSLocalizedDescriptionKey: "\(action)（系统错误 \(status)）"])
    }
    func records() throws -> [SSHIdentityStoredRecord] {
        var q = query(); q[kSecReturnAttributes] = true; q[kSecMatchLimit] = kSecMatchLimitAll
        var result: CFTypeRef?
        let status = SecItemCopyMatching(q as CFDictionary, &result)
        if status == errSecItemNotFound { return [] }
        guard status == errSecSuccess else { throw error("无法读取 SSH 密钥列表", status) }
        guard let attributes = result as? [[String: Any]], attributes.count <= 1024 else { throw error("SSH 密钥列表格式无效", errSecDecode) }
        return try attributes.map { item in
            guard let account = item[kSecAttrAccount as String] as? String, !account.isEmpty else { throw error("SSH 密钥标识无效", errSecDecode) }
            var exact = query(account); exact[kSecReturnData] = true; exact[kSecMatchLimit] = kSecMatchLimitOne
            var value: CFTypeRef?
            let status = SecItemCopyMatching(exact as CFDictionary, &value)
            guard status == errSecSuccess else { throw error("无法读取 SSH 密钥", status) }
            guard let data = value as? Data, data.count <= 64*1024 else { throw error("SSH 密钥记录格式无效", errSecDecode) }
            return SSHIdentityStoredRecord(account: account, data: data)
        }
    }
    func insert(account: String, name: String, data: Data) throws {
        var q = query(account); q[kSecValueData] = data; q[kSecAttrLabel] = name
        if dataProtection { q[kSecAttrAccessible] = kSecAttrAccessibleWhenUnlockedThisDeviceOnly }
        let status = SecItemAdd(q as CFDictionary, nil)
        guard status == errSecSuccess else { throw error("无法保存 SSH 密钥", status) }
    }
    func remove(account: String) throws {
        let status = SecItemDelete(query(account) as CFDictionary)
        guard status == errSecSuccess else { throw error("无法删除 SSH 密钥", status) }
    }
}
