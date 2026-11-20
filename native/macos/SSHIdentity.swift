import AppKit
import CryptoKit
import Darwin
import LocalAuthentication
import Security
import SwiftUI
import UserNotifications

private struct StoredIdentity: Codable {
    let id: String
    var name: String
    let authentication: String
    let publicData: Data
    let representation: Data
    var fingerprint: String { sshIdentityFingerprint(publicData) }
    var publicInfo: [String: Any] {
        ["id": id, "name": name, "authentication": authentication, "public": publicData.base64EncodedString()]
    }
}
private struct SigningWork {
    let id: UInt64
    let identity: StoredIdentity
    let data: Data
    let origin: String
    let originPath: String
    let pid: Int32
    let processInstance: String
    let executableID: String
    let target: String
    let targetKind: String
    let targetDetail: String
    let targetHost: String
    let batch: String
    var group: String { batch.isEmpty ? "request:\(id)" : identity.id + "\0" + originPath + "\0" + batch }
}
private func sshIdentityFingerprint(_ publicData: Data) -> String {
    var blob = Data()
    for field in [Data("ecdsa-sha2-nistp256".utf8), Data("nistp256".utf8), publicData] {
        var size = UInt32(field.count).bigEndian
        withUnsafeBytes(of: &size) { blob.append(contentsOf: $0) }
        blob.append(field)
    }
    return "SHA256:" + Data(SHA256.hash(data: blob)).base64EncodedString().replacingOccurrences(of: "=", with: "")
}

// The inherited pipe is accepted only from the installed sibling daemon's
// current signed image, not merely from a same-user process claiming its name.
private func trustedParent() -> Bool {
    let expected = Bundle.main.bundleURL.deletingLastPathComponent().appendingPathComponent("smallsshd")
    var dynamic: SecCode?
    let attrs = [kSecGuestAttributePid: NSNumber(value: getppid())] as CFDictionary
    guard SecCodeCopyGuestWithAttributes(nil, attrs, [], &dynamic) == errSecSuccess,
          let dynamic, SecCodeCheckValidity(dynamic, [], nil) == errSecSuccess else { return false }
    var staticCode: SecStaticCode?
    guard SecStaticCodeCreateWithPath(expected as CFURL, [], &staticCode) == errSecSuccess,
          let staticCode, SecStaticCodeCheckValidity(staticCode, [], nil) == errSecSuccess else { return false }
    var liveInfo: CFDictionary?, diskInfo: CFDictionary?
    var liveStatic: SecStaticCode?
    guard SecCodeCopyStaticCode(dynamic, [], &liveStatic) == errSecSuccess, let liveStatic,
          SecCodeCopySigningInformation(liveStatic, SecCSFlags(rawValue: kSecCSSigningInformation), &liveInfo) == errSecSuccess,
          SecCodeCopySigningInformation(staticCode, SecCSFlags(rawValue: kSecCSSigningInformation), &diskInfo) == errSecSuccess,
          let live = liveInfo as? [String: Any], let disk = diskInfo as? [String: Any],
          let actualHash = live[kSecCodeInfoUnique as String] as? Data,
          let expectedHash = disk[kSecCodeInfoUnique as String] as? Data,
          let location = live[kSecCodeInfoMainExecutable as String] as? URL else { return false }
    return actualHash == expectedHash && location.resolvingSymlinksInPath() == expected.resolvingSymlinksInPath()
}

@MainActor private final class IdentityRuntime: NSObject, ObservableObject, UNUserNotificationCenterDelegate, NSWindowDelegate {
    @Published var rows: [PendingRow] = []
    @Published private(set) var reviewIDs: Set<UInt64> = []
    @Published private(set) var reviewAuthentication: LAContext?
    private let store: SSHIdentityStore
    private let dataProtectionKeychain: Bool
    private var grants = SSHIdentityAuthorization()
    private var leaseTimer: Timer?
    private var fingerprints: [String:String] = [:]
    private var identityNames: [String:String] = [:]
    private var contexts: [String: LAContext] = [:]
    private var work: [UInt64: SigningWork] = [:]
    private var activeContext: LAContext?
    private var activeKey: String?
    private var promptGeneration: UInt64 = 0
    private var prompting = false
    private var automaticReview = false
    private var evaluatingReview = false
    private var persistenceRequest: UInt64?
    private var persistenceRequests: [UInt64: String] = [:]
    private var offeredPersistence = Set<String>()
    private var batchWindow: NSWindow?
    private var notificationState = "unknown"
    private let output = NSLock()

    init(namespace: String) {
        // Data Protection Keychain requires provisioned access-group
        // entitlements. Local ad-hoc previews keep the opaque SEP wrapper in
        // the login Keychain with its normal per-application access controls.
        // The private key itself remains in Secure Enclave in both cases.
        var code: SecCode?
        var staticCode: SecStaticCode?
        var info: CFDictionary?
        if SecCodeCopySelf([], &code) == errSecSuccess, let code,
           SecCodeCopyStaticCode(code, [], &staticCode) == errSecSuccess, let staticCode,
           SecCodeCopySigningInformation(staticCode, SecCSFlags(rawValue: kSecCSSigningInformation), &info) == errSecSuccess,
           let attributes = info as? [String: Any],
           let entitlements = attributes[kSecCodeInfoEntitlementsDict as String] as? [String: Any],
           let applicationID = entitlements["com.apple.application-identifier"] as? String, !applicationID.isEmpty {
            dataProtectionKeychain = true
        } else { dataProtectionKeychain = false }
        store = SSHIdentityStore(service: "dev.quincy.smallssh.ssh-identities." + namespace, dataProtection: dataProtectionKeychain)
        super.init()
        let center = UNUserNotificationCenter.current()
        center.delegate = self
        let durations = SSHIdentityAuthorization.durations.map { seconds in
            UNNotificationAction(identifier: "duration.\(seconds)", title: durationLabel(seconds), options: [])
        }
        let category = UNNotificationCategory(identifier: "ssh-signature", actions: durations, intentIdentifiers: [], options: [])
        center.setNotificationCategories([category])
        center.requestAuthorization(options: [.alert]) { _, _ in }
        refreshNotificationState()
    }
    private func durationLabel(_ seconds: Int) -> String {
        switch seconds { case 60: return "1 分钟"; case 300: return "5 分钟"; case 3600: return "1 小时"; default: return "24 小时" }
    }
    private func identities() throws -> [StoredIdentity] {
        return try store.records().map { item in
            let record = try JSONDecoder().decode(StoredIdentity.self, from: item.data)
            guard record.id == item.account else { throw failure("SSH 密钥记录与标识不一致") }
            fingerprints[record.id] = record.fingerprint; identityNames[record.id] = record.name
            return record
        }.sorted { $0.name < $1.name }
    }
    private func key(_ id: String) throws -> StoredIdentity {
        guard let record = try identities().first(where: { $0.id == id }) else { throw failure("找不到指定的 SSH 密钥") }
        return record
    }
    private func create(name: String, authentication: String) throws {
        guard !name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty, name.utf8.count <= 128,
              name.rangeOfCharacter(from: .controlCharacters) == nil,
              ["presence", "biometry", "none"].contains(authentication), try identities().count < 1024 else { throw failure("密钥参数无效") }
        var flags: SecAccessControlCreateFlags = [.privateKeyUsage]
        if authentication == "presence" { flags.insert(.userPresence) }
        if authentication == "biometry" { flags.insert(.biometryCurrentSet) }
        var error: Unmanaged<CFError>?
        guard let control = SecAccessControlCreateWithFlags(nil, kSecAttrAccessibleWhenUnlockedThisDeviceOnly, flags, &error) else { throw failure("无法创建密钥访问策略") }
        let privateKey = try SecureEnclave.P256.Signing.PrivateKey(accessControl: control)
        let record = StoredIdentity(id: UUID().uuidString, name: name, authentication: authentication, publicData: privateKey.publicKey.x963Representation, representation: privateKey.dataRepresentation)
        try store.insert(account: record.id, name: name, data: JSONEncoder().encode(record))
        fingerprints[record.id] = record.fingerprint; identityNames[record.id] = record.name
        emitAudit("ssh_key_created", key: record.id, authorization: record.authentication)
    }
    private func erase(_ id: String) throws {
        _ = try key(id)
        if activeKey == id { cancelPrompt() }
        for request in persistenceRequests.filter({ $0.value == id }).map(\.key) { persistenceRequests.removeValue(forKey: request) }
        for request in work.values.filter({ $0.identity.id == id }).map(\.id) { finish(request, error: "密钥已删除") }
        for lease in grants.leases(now: DispatchTime.now().uptimeNanoseconds) where lease.key == id { emitAudit("ssh_revoked", key: id, lease: lease, reason: "key_deleted") }
        grants.remove(key: id); contexts.removeValue(forKey: id)?.invalidate(); offeredPersistence.remove(id)
        try store.remove(account: id)
        emitAudit("ssh_key_deleted", key: id)
    }
    private func failure(_ message: String) -> NSError { NSError(domain: "SmallSSHIdentity", code: 1, userInfo: [NSLocalizedDescriptionKey: message]) }
    private func respond(_ id: UInt64, _ fields: [String: Any] = [:]) {
        var response = fields; response["request"] = id
        guard let data = try? JSONSerialization.data(withJSONObject: response) else { return }
        output.lock(); defer { output.unlock() }
        try? FileHandle.standardOutput.write(contentsOf: data + Data([10]))
    }
    func handle(_ message: [String: Any]) {
        guard trustedParent(), let number = message["request"] as? NSNumber, let action = message["action"] as? String else { NSApp.terminate(nil); return }
        let id = number.uint64Value
        if leaseTimer == nil { leaseTimer = Timer.scheduledTimer(withTimeInterval: 5, repeats: true) { [weak self] _ in Task { @MainActor in self?.expireLeases() } } }
        expireLeases()
        do {
            switch action {
            case "list":
                respond(id, ["supported": SecureEnclave.isAvailable, "keys": try identities().map(\.publicInfo), "pending": work.count, "notifications": notificationState, "leases": grants.leases(now: DispatchTime.now().uptimeNanoseconds).map(\.publicInfo)])
            case "create":
                try create(name: message["name"] as? String ?? "", authentication: message["authentication"] as? String ?? "")
                respond(id)
            case "delete": try erase(message["key"] as? String ?? ""); respond(id)
            case "sign":
                guard let encoded = message["data"] as? String, let data = Data(base64Encoded: encoded), data.count <= 256*1024,
                      let keyID = message["key"] as? String, let pid = message["pid"] as? Int32, pid > 0, work.count < 64 else { throw failure("签名请求无效") }
                let origin = provenance(pid)
                let identity = try key(keyID)
                fingerprints[keyID] = identity.fingerprint
                let item = SigningWork(id: id, identity: identity, data: data, origin: origin.0, originPath: origin.1, pid: pid,
                                       processInstance: message["processInstance"] as? String ?? "", executableID: message["executableId"] as? String ?? "",
                                       target: message["target"] as? String ?? "未知签名用途",
                                       targetKind: message["targetKind"] as? String ?? "", targetDetail: message["targetDetail"] as? String ?? "",
                                       targetHost: message["targetHost"] as? String ?? "", batch: message["batch"] as? String ?? "")
                work[id] = item
                if item.identity.authentication == "none" || grants.allows(key: keyID, request: id, now: DispatchTime.now().uptimeNanoseconds) { sign(item) }
                else { publish(); showPending() }
            case "cancel":
                if work[id] != nil { finish(id, error: "请求已取消"); kick() }
                else if persistenceRequests.removeValue(forKey: id) != nil, persistenceRequest == id { cancelPrompt(); kick() }
            case "persist":
                let keyID = message["key"] as? String ?? ""
                let seconds = message["seconds"] as? Int ?? 0
                guard SSHIdentityAuthorization.durations.contains(seconds) else { throw failure("授权时长无效") }
                persistenceRequests[id] = keyID
                Task { await persist(keyID, seconds: seconds, response: id) }
            case "lease-policy", "revoke":
                let keyID = message["key"] as? String ?? ""
                let leaseID = message["leaseId"] as? String ?? ""
                let now = DispatchTime.now().uptimeNanoseconds
                if action == "revoke" {
                    guard let lease = grants.revoke(key: keyID, id: leaseID, now: now) else { throw failure("密钥授权已更新或已到期") }
                    emitAudit("ssh_revoked", key: keyID, lease: lease)
                    contexts.removeValue(forKey: keyID)?.invalidate()
                } else {
                    let idle = message["idleSeconds"] as? Int ?? -1
                    guard let previous = grants.adjust(key: keyID, id: leaseID, idle: idle, now: now) else { throw failure("密钥授权已更新或闲置期限无效") }
                    emitAudit("ssh_policy_changed", key: keyID, lease: previous, idle: idle, previous: previous.idleSeconds)
                    expireLeases()
                }
                respond(id)
            case "pending": publish(); showPending(); respond(id)
            case "notifications":
                UNUserNotificationCenter.current().requestAuthorization(options: [.alert]) { _, _ in }
                refreshNotificationState(); respond(id)
            case "clear":
                for lease in grants.leases(now: DispatchTime.now().uptimeNanoseconds) { emitAudit("ssh_revoked", key: lease.key, lease: lease, reason: "cleared") }
                cancelPrompt(); grants.clear(); contexts.values.forEach { $0.invalidate() }; contexts.removeAll(); offeredPersistence.removeAll()
                persistenceRequests.removeAll()
                for request in Array(work.keys) { finish(request, error: "密钥代理已锁定") }; respond(id)
            default: throw failure("未知的密钥代理操作")
            }
        } catch { respond(id, ["error": error.localizedDescription]) }
    }
    private func emitAudit(_ event: String, key: String, lease: SSHIdentityAuthorization.Lease? = nil, authorization: String = "", idle: Int? = nil, previous: Int? = nil, reason: String = "", work: SigningWork? = nil) {
        var value: [String:Any] = ["id": UUID().uuidString, "time": Int64(Date().timeIntervalSince1970*1000), "event": event, "key": key, "fingerprint": fingerprints[key] ?? "", "name": identityNames[key] ?? "", "authorization": authorization, "reason": reason]
        if let lease { value["leaseId"] = lease.id; value["expires"] = lease.milliseconds(lease.expiration); value["idleSeconds"] = lease.idleSeconds }
        if let idle { value["idleSeconds"] = idle }
        if let previous { value["previousIdle"] = previous }
        if let work { value["peerPid"]=work.pid; value["executable"]=work.originPath; value["processInstance"]=work.processInstance; value["executableId"]=work.executableID }
        respond(0, ["audit": value])
    }
    private func expireLeases() {
        let now = DispatchTime.now().uptimeNanoseconds
        for lease in grants.expired(now: now) {
            emitAudit("ssh_expired", key: lease.key, lease: lease, reason: now >= lease.expiration ? "duration_expired" : "idle_timeout")
            contexts.removeValue(forKey: lease.key)?.invalidate()
        }
    }
    private func cancelPrompt() {
        promptGeneration += 1; activeContext?.invalidate(); activeContext = nil; activeKey = nil; prompting = false; persistenceRequest = nil
        reviewAuthentication = nil; evaluatingReview = false
    }
    private func evaluate(_ context: LAContext, identity: StoredIdentity, reason: String) async -> Bool {
        let biometricsOnly = identity.authentication == "biometry"
        context.localizedFallbackTitle = biometricsOnly ? "" : "使用 Mac 登录密码"
        let policy: LAPolicy = biometricsOnly ? .deviceOwnerAuthenticationWithBiometrics : .deviceOwnerAuthentication
        return (try? await context.evaluatePolicy(policy, localizedReason: reason)) ?? false
    }
    func authenticateReview(embedded: Bool) {
        let ids = reviewIDs
        guard !prompting, let first = reviewedWork(ids) else { return }
        let context = LAContext(); activeContext = context; activeKey = first.identity.id; prompting = true
        reviewAuthentication = context
        automaticReview = false
        context.localizedCancelTitle = "拒绝"
        if embedded && first.identity.authentication == "biometry" { return }
        evaluateReview(context)
    }
    func evaluateReview(_ context: LAContext) {
        let ids = reviewIDs
        guard !evaluatingReview, context === reviewAuthentication, let first = reviewedWork(ids) else { return }
        evaluatingReview = true
        let generation = promptGeneration
        let reason = "允许 \(first.origin) 使用 SSH 密钥“\(first.identity.name)”\(ids.count > 1 ? "完成这组签名" : "签名")\n\(first.identity.fingerprint)\n\(first.target)"
        Task {
            let success = await evaluate(context, identity: first.identity, reason: reason)
            guard generation == promptGeneration else { return }
            activeContext = nil; activeKey = nil; prompting = false; reviewAuthentication = nil; evaluatingReview = false
            reviewIDs = []
            if success {
                emitAudit("ssh_authorized", key: first.identity.id, authorization: "verified", work: first)
                contexts[first.identity.id] = context; grants.allow(key: first.identity.id, requests: ids)
                for id in ids { if let item = work[id] { sign(item) } }
            } else { deny(ids, first: first) }
            kick()
        }
    }
    private func deny(_ ids: Set<UInt64>, first: SigningWork) {
        emitAudit("ssh_denied", key: first.identity.id, authorization: "verified", work: first)
        for id in ids { finish(id, error: "用户拒绝了 SSH 密钥签名") }
    }
    func denyReview() {
        let ids = reviewIDs
        guard let first = reviewedWork(ids) else { return }
        if reviewAuthentication != nil { cancelPrompt() }
        reviewIDs = []
        deny(ids, first: first)
        kick()
    }
    private func sign(_ item: SigningWork) {
        guard work[item.id] != nil else { return }
        do {
            let privateKey = try SecureEnclave.P256.Signing.PrivateKey(dataRepresentation: item.identity.representation, authenticationContext: contexts[item.identity.id])
            let signature = try privateKey.signature(for: item.data).rawRepresentation
            let mode = item.identity.authentication == "none" ? "unprotected" : grants.mode(key: item.identity.id)
            grants.consume(key: item.identity.id, request: item.id, now: DispatchTime.now().uptimeNanoseconds)
            work.removeValue(forKey: item.id); publish()
            respond(item.id, ["signature": signature.base64EncodedString(), "authorization": mode])
            notify(item)
        } catch { finish(item.id, error: "SSH 密钥签名未完成") }
    }
    private func finish(_ id: UInt64, error: String) {
        if work.removeValue(forKey: id) != nil { respond(id, ["error": error]); publish() }
    }
    private func kick() {
        for item in Array(work.values) where grants.allows(key: item.identity.id, request: item.id, now: DispatchTime.now().uptimeNanoseconds) { sign(item) }
        if !work.isEmpty { showPending() }
    }
    private func reviewedWork(_ ids: Set<UInt64>) -> SigningWork? {
        guard let id = ids.min(), let first = work[id],
              ids.allSatisfy({ work[$0]?.group == first.group }) else { return nil }
        return first
    }
    func review(_ ids: Set<UInt64>) {
        guard !prompting, reviewedWork(ids) != nil else { return }
        automaticReview = false
        reviewIDs = ids
        sizePendingWindow()
    }
    private func persist(_ id: String, seconds: Int, response: UInt64? = nil) async {
        if let response, persistenceRequests[response] != id { respond(response, ["error": "授权已取消"]); return }
        defer { if let response { persistenceRequests.removeValue(forKey: response) } }
        do {
            let identity = try key(id)
            cancelPrompt(); let context = LAContext(); activeContext = context; activeKey = id; prompting = true
            context.localizedCancelTitle = "取消"
            persistenceRequest = response
            let generation = promptGeneration
            let success = await evaluate(context, identity: identity, reason: "允许 SSH 密钥“\(identity.name)”在 \(durationLabel(seconds))内使用\n\(identity.fingerprint)")
            guard generation == promptGeneration else { if let response { respond(response, ["error": "授权已取消"]) }; return }
            activeContext = nil; activeKey = nil; prompting = false; persistenceRequest = nil
            if success {
                fingerprints[id] = identity.fingerprint
                contexts[id] = context
                _ = grants.allow(key: id, seconds: seconds, now: DispatchTime.now().uptimeNanoseconds, wall: Int64(Date().timeIntervalSince1970*1000))
                emitAudit("ssh_authorized", key: id, lease: grants.leases(now: DispatchTime.now().uptimeNanoseconds).first(where: { $0.key == id }), authorization: "lease")
                if let response { respond(response) }
            } else if let response { respond(response, ["error": "用户取消了密钥限时授权"]) }
            kick()
        } catch { if let response { respond(response, ["error": error.localizedDescription]) } }
    }
    private func notify(_ item: SigningWork) {
        let content = UNMutableNotificationContent()
        content.title = "\(item.origin) 使用了 SSH 密钥"
        content.subtitle = item.identity.name
        content.body = item.target + "\n" + item.identity.fingerprint
        content.interruptionLevel = .timeSensitive
        if item.identity.authentication != "none" {
            offeredPersistence.insert(item.identity.id)
            content.categoryIdentifier = "ssh-signature"
            content.userInfo["identity"] = item.identity.id
        }
        UNUserNotificationCenter.current().add(UNNotificationRequest(identifier: UUID().uuidString, content: content, trigger: nil))
    }
    private func refreshNotificationState() {
        UNUserNotificationCenter.current().getNotificationSettings { settings in
            Task { @MainActor in
                switch settings.authorizationStatus { case .authorized, .provisional: self.notificationState = "enabled"; case .denied: self.notificationState = "denied"; default: self.notificationState = "notDetermined" }
            }
        }
    }
    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification) async -> UNNotificationPresentationOptions { [.banner, .list] }
    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse) async {
        let action = response.actionIdentifier
        guard action.hasPrefix("duration."), let seconds = Int(action.dropFirst(9)), SSHIdentityAuthorization.durations.contains(seconds),
              let id = response.notification.request.content.userInfo["identity"] as? String else { return }
        await acceptNotification(id, seconds: seconds)
    }
    private func acceptNotification(_ id: String, seconds: Int) async {
        guard offeredPersistence.remove(id) != nil else { return }
        await persist(id, seconds: seconds)
    }
    private func publish() {
        if !reviewIDs.isEmpty && reviewedWork(reviewIDs) == nil {
            if reviewAuthentication != nil { cancelPrompt() }
            reviewIDs = []
            automaticReview = false
            sizePendingWindow()
        }
        rows = work.values.sorted { $0.id < $1.id }.map {
            PendingRow(id: $0.id, name: $0.identity.name, origin: $0.origin, group: $0.group, fingerprint: $0.identity.fingerprint,
                       authentication: $0.identity.authentication, kind: $0.targetKind, detail: $0.targetDetail, host: $0.targetHost)
        }
        if rows.isEmpty { batchWindow?.close() }
        NSApp.dockTile.badgeLabel = rows.isEmpty ? nil : String(rows.count)
    }
    private func showPending() {
        guard !rows.isEmpty else { return }
        if batchWindow == nil {
            let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 520, height: 400), styleMask: [.titled, .closable, .resizable], backing: .buffered, defer: false)
            window.title = "SSH 密钥使用授权"
            window.contentView = NSHostingView(rootView: PendingIdentityView(runtime: self))
            window.contentMinSize = NSSize(width: 420, height: 240); window.isReleasedWhenClosed = false; window.delegate = self
            window.center(); batchWindow = window
        }
        if rows.count > 1 && automaticReview {
            reviewIDs = []
            automaticReview = false
            sizePendingWindow()
        }
        if reviewIDs.isEmpty && rows.count == 1, let only = rows.first, !prompting {
            review([only.id])
            automaticReview = true
        }
        NSApp.setActivationPolicy(.regular); batchWindow?.makeKeyAndOrderFront(nil); NSApp.requestUserAttention(.criticalRequest)
    }
    private func sizePendingWindow() {
        guard let window = batchWindow else { return }
        let size = reviewIDs.isEmpty ? NSSize(width: 520, height: 400) : NSSize(width: IdentityTokens.promptWidth, height: 316)
        if window.contentView?.bounds.width != size.width { window.setContentSize(size) }
    }
    func fitReview(_ height: CGFloat) {
        guard !reviewIDs.isEmpty, height > 0, let window = batchWindow, let content = window.contentView,
              let screen = window.screen ?? NSScreen.main else { return }
        let target = min(ceil(height), screen.visibleFrame.height - 72)
        if abs(content.bounds.height - target) > 1 { window.setContentSize(NSSize(width: content.bounds.width, height: target)) }
    }
    func windowWillClose(_ notification: Notification) {
        if reviewAuthentication != nil { cancelPrompt() }
        reviewIDs = []
        automaticReview = false
        NSApp.setActivationPolicy(.accessory)
    }
}

private struct PendingIdentityView: View {
    @ObservedObject var runtime: IdentityRuntime
    var body: some View {
        IdentityAuthorizationView(rows: runtime.rows, reviewIDs: runtime.reviewIDs, context: runtime.reviewAuthentication,
                                  review: runtime.review, deny: runtime.denyReview, authenticate: runtime.authenticateReview,
                                  evaluate: runtime.evaluateReview, fit: runtime.fitReview)
    }
}

// Labels come from the kernel-derived caller PID, never SSH agent input.
private func provenance(_ pid: Int32) -> (String, String) {
    var current = pid
    var visited = Set<Int32>()
    var paths: [String] = []
    var fallback = "SSH 客户端 (\(pid))", firstPath = "pid:\(pid)"
    while current > 1, visited.insert(current).inserted, visited.count < 32 {
        var bytes = [UInt8](repeating: 0, count: 4 * Int(MAXPATHLEN))
        if proc_pidpath(current, &bytes, UInt32(bytes.count)) > 0 {
            let path = String(cString: bytes)
            paths.append(path)
            if current == pid { firstPath = path; fallback = URL(fileURLWithPath: path).lastPathComponent }
        }
        if let app = NSRunningApplication(processIdentifier: current), let name = app.localizedName { return (name, paths.joined(separator: "\0")) }
        var info = kinfo_proc(); var length = MemoryLayout<kinfo_proc>.size
        var mib: [Int32] = [CTL_KERN, KERN_PROC, KERN_PROC_PID, current]
        guard sysctl(&mib, UInt32(mib.count), &info, &length, nil, 0) == 0, length == MemoryLayout<kinfo_proc>.size else { break }
        current = info.kp_eproc.e_ppid
    }
    return (fallback, paths.isEmpty ? firstPath : paths.joined(separator: "\0"))
}

@main struct SSHIdentityMain {
    static func main() {
        #if IDENTITY_QA
        if CommandLine.arguments.count == 2 { identitySnapshots(CommandLine.arguments[1]) }
        else { sshIdentitySelfTest() }
        #else
        guard CommandLine.arguments.count == 3, CommandLine.arguments[1] == "--namespace",
              CommandLine.arguments[2].count == 64, CommandLine.arguments[2].allSatisfy({ $0.isHexDigit }), trustedParent() else { exit(1) }
        let application = NSApplication.shared
        application.setActivationPolicy(.accessory)
        let runtime = IdentityRuntime(namespace: CommandLine.arguments[2])
        DispatchQueue.global().async {
            while let line = readLine(strippingNewline: true) {
                guard line.utf8.count <= 1<<20, let data = line.data(using: .utf8), let message = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any] else { exit(1) }
                DispatchQueue.main.async { runtime.handle(message) }
            }
            DispatchQueue.main.async { NSApp.terminate(nil) }
        }
        application.run()
        #endif
    }
}
