import Foundation
import Security
import LocalAuthentication

// Opt-in native API contract test. It creates only two synthetic password
// records under a new random QA service, disallows every authentication UI,
// and removes those exact records. No existing keys are enumerated or read.
@main struct SSHIdentityStoreQA {
    static func main() throws {
        guard SecKeychainSetUserInteractionAllowed(false) == errSecSuccess else { fatalError("Cannot disable authentication UI") }
        let service = "dev.quincy.smallssh.identity-qa." + UUID().uuidString
        let store = SSHIdentityStore(service: service, dataProtection: false, nonInteractive: true)
        let context = LAContext(); context.interactionNotAllowed = true
        var old: [CFString: Any] = [kSecClass: kSecClassGenericPassword, kSecAttrService: service, kSecReturnData: true, kSecMatchLimit: kSecMatchLimitAll, kSecUseAuthenticationContext: context]
        var result: CFTypeRef?
        let previousStatus = SecItemCopyMatching(old as CFDictionary, &result)
        old.removeAll(); result = nil
        print("Previous bulk-data query status: \(previousStatus)")
        let empty = try store.records(); precondition(empty.isEmpty)
        var inserted = Set<String>()
        defer { for id in inserted { try? store.remove(account: id) } }
        let first = Data("synthetic opaque identity record one".utf8)
        let second = Data("synthetic opaque identity record two".utf8)
        try store.insert(account: "one", name: "SmallSSH isolated QA one", data: first); inserted.insert("one")
        let one = try store.records()
        precondition(one.count == 1 && one[0].account == "one" && one[0].data == first)
        try store.insert(account: "two", name: "SmallSSH isolated QA two", data: second); inserted.insert("two")
        let two = try store.records()
        precondition(Dictionary(uniqueKeysWithValues: two.map { ($0.account, $0.data) }) == ["one": first, "two": second])
        try store.remove(account: "one"); inserted.remove("one")
        let remaining = try store.records()
        precondition(remaining.count == 1 && remaining[0].account == "two")
        try store.remove(account: "two"); inserted.remove("two")
        let cleared = try store.records(); precondition(cleared.isEmpty)
        print("Native storage QA passed: empty / one / two / delete; authentication UI disabled; synthetic records removed")
    }
}
