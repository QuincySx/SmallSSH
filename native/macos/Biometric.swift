import Foundation
import CryptoKit
import Security
import LocalAuthentication

struct Input: Decodable { let action: String; let key: Data?; let blob: Data?; let reason: String? }
struct Envelope: Codable { let version: Int; let privateKey: Data; let ephemeral: Data; let ciphertext: Data }
struct Output: Encodable { var supported: Bool?; var touchID: Bool?; var blob: Data?; var key: Data?; var error: String? }
let domain = Data("ssh-workbench/device-unlock/v1".utf8)

func handle(_ input: Input) throws -> Output {
    let context = LAContext()
    context.touchIDAuthenticationAllowableReuseDuration = 0
    context.localizedReason = input.reason ?? "解锁 SmallSSH"
    defer { context.invalidate() }
    guard SecureEnclave.isAvailable else { throw NSError(domain:"设备未提供 Secure Enclave",code:1) }
    var availabilityError: NSError?
    guard context.canEvaluatePolicy(.deviceOwnerAuthentication, error: &availabilityError) else {
        throw availabilityError ?? NSError(domain:"请先设置系统登录认证",code:2)
    }
    if input.action == "probe" {
        let biometricsAvailable = context.canEvaluatePolicy(.deviceOwnerAuthenticationWithBiometrics, error: nil)
        return Output(supported:true, touchID:biometricsAvailable && context.biometryType == .touchID)
    }
    if input.action == "wrap" {
        guard let data = input.key, data.count == 32 else { throw NSError(domain:"密钥长度无效",code:3) }
        var error: Unmanaged<CFError>?
        guard let control = SecAccessControlCreateWithFlags(nil, kSecAttrAccessibleWhenUnlockedThisDeviceOnly,
                    [.privateKeyUsage, .userPresence], &error) else {
            throw error!.takeRetainedValue() as Error
        }
        let protected = try SecureEnclave.P256.KeyAgreement.PrivateKey(accessControl:control)
        let ephemeral = P256.KeyAgreement.PrivateKey()
        let shared = try ephemeral.sharedSecretFromKeyAgreement(with:protected.publicKey)
        let key = shared.hkdfDerivedSymmetricKey(using:SHA256.self,salt:domain,sharedInfo:domain,outputByteCount:32)
        let box = try AES.GCM.seal(data,using:key,authenticating:domain)
        let envelope = Envelope(version:1,privateKey:protected.dataRepresentation,ephemeral:ephemeral.publicKey.x963Representation,ciphertext:box.combined!)
        return Output(blob:try JSONEncoder().encode(envelope))
    }
    if input.action == "unwrap" {
        guard let blob = input.blob else { throw NSError(domain:"缺少设备密钥",code:4) }
        let envelope = try JSONDecoder().decode(Envelope.self,from:blob)
        guard envelope.version == 1 else { throw NSError(domain:"版本不支持",code:5) }
        let protected = try SecureEnclave.P256.KeyAgreement.PrivateKey(dataRepresentation:envelope.privateKey,authenticationContext:context)
        let publicKey = try P256.KeyAgreement.PublicKey(x963Representation:envelope.ephemeral)
        let shared = try protected.sharedSecretFromKeyAgreement(with:publicKey)
        let key = shared.hkdfDerivedSymmetricKey(using:SHA256.self,salt:domain,sharedInfo:domain,outputByteCount:32)
        let data = try AES.GCM.open(AES.GCM.SealedBox(combined:envelope.ciphertext),using:key,authenticating:domain)
        return Output(key:data)
    }
    throw NSError(domain:"不支持的认证操作",code:6)
}
do {
    let data = FileHandle.standardInput.readDataToEndOfFile()
    guard data.count < 65536 else { throw NSError(domain:"请求过大",code:7) }
    let input = try JSONDecoder().decode(Input.self,from:data)
    let output = try handle(input)
    FileHandle.standardOutput.write(try JSONEncoder().encode(output))
} catch {
    let output = Output(error:"系统认证未完成：\(error.localizedDescription)")
    FileHandle.standardOutput.write(try! JSONEncoder().encode(output))
    exit(1)
}
