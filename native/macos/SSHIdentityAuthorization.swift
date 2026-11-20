import Foundation

// Deadlines use monotonic time; wall time is display metadata only.
struct SSHIdentityAuthorization {
    struct Lease {
        let id: String
        let key: String
        let start: UInt64
        let expiration: UInt64
        var lastUsed: UInt64
        var idleSeconds: Int
        let wallStart: Int64
        func valid(_ now: UInt64) -> Bool {
            now < expiration && (idleSeconds == 0 || now < lastUsed + UInt64(idleSeconds)*1_000_000_000)
        }
        func milliseconds(_ tick: UInt64) -> Int64 { wallStart + Int64((tick-start)/1_000_000) }
        var publicInfo: [String: Any] {
            ["id": id, "key": key, "started": wallStart, "expires": milliseconds(expiration), "lastUsed": milliseconds(lastUsed), "idleSeconds": idleSeconds]
        }
    }
    enum Scope { case requests(Set<UInt64>); case until(Lease) }
    private var scopes: [String: Scope] = [:]
    static let durations = [60, 300, 3600, 86400]
    mutating func allow(key: String, requests: Set<UInt64>) { scopes[key] = .requests(requests) }
    mutating func allow(key: String, seconds: Int, now: UInt64, wall: Int64 = 0) -> Bool {
        guard Self.durations.contains(seconds) else { return false }
        scopes[key] = .until(Lease(id: UUID().uuidString, key: key, start: now, expiration: now+UInt64(seconds)*1_000_000_000, lastUsed: now, idleSeconds: 900, wallStart: wall))
        return true
    }
    func allows(key: String, request: UInt64, now: UInt64) -> Bool {
        switch scopes[key] {
        case .requests(let ids): return ids.contains(request)
        case .until(let lease): return lease.valid(now)
        case nil: return false
        }
    }
    func mode(key: String) -> String { if case .until = scopes[key] { return "lease" }; return "verified" }
    func leases(now: UInt64) -> [Lease] {
        scopes.values.compactMap { if case .until(let lease) = $0, lease.valid(now) { return lease }; return nil }.sorted { $0.key < $1.key }
    }
    mutating func expired(now: UInt64) -> [Lease] {
        let expired = scopes.values.compactMap { scope -> Lease? in if case .until(let lease) = scope, !lease.valid(now) { return lease }; return nil }
        for lease in expired { scopes.removeValue(forKey: lease.key) }
        return expired
    }
    mutating func adjust(key: String, id: String, idle: Int, now: UInt64) -> Lease? {
        guard idle == 0 || (60...1800).contains(idle), case .until(var lease) = scopes[key], lease.id == id, lease.valid(now) else { return nil }
        let previous = lease; lease.idleSeconds = idle; scopes[key] = .until(lease); return previous
    }
    mutating func revoke(key: String, id: String, now: UInt64) -> Lease? {
        guard case .until(let lease) = scopes[key], lease.id == id, lease.valid(now) else { return nil }
        scopes.removeValue(forKey: key); return lease
    }
    mutating func consume(key: String, request: UInt64, now: UInt64 = 0) {
        switch scopes[key] {
        case .requests(var ids):
            ids.remove(request); if ids.isEmpty { scopes.removeValue(forKey: key) } else { scopes[key] = .requests(ids) }
        case .until(var lease): lease.lastUsed = now; scopes[key] = .until(lease)
        case nil: break
        }
    }
    mutating func remove(key: String) { scopes.removeValue(forKey: key) }
    mutating func clear() { scopes.removeAll() }
}

#if IDENTITY_QA
func sshIdentitySelfTest() {
    var grants = SSHIdentityAuthorization()
    let now: UInt64 = 100_000_000_000
    precondition(!grants.allows(key: "a", request: 1, now: now))
    grants.allow(key: "a", requests: [1, 2])
    precondition(grants.allows(key: "a", request: 1, now: now))
    precondition(!grants.allows(key: "b", request: 1, now: now))
    grants.consume(key: "a", request: 1)
    precondition(!grants.allows(key: "a", request: 1, now: now))
    precondition(grants.allows(key: "a", request: 2, now: now))
    grants.consume(key: "a", request: 2)
    for seconds in SSHIdentityAuthorization.durations {
        precondition(grants.allow(key: "a", seconds: seconds, now: now))
        let lease = grants.leases(now: now)[0]
        precondition(grants.adjust(key: "a", id: lease.id, idle: 0, now: now) != nil)
        precondition(grants.allows(key: "a", request: 999, now: now+UInt64(seconds)*1_000_000_000-1))
        precondition(!grants.allows(key: "a", request: 999, now: now+UInt64(seconds)*1_000_000_000))
    }
    precondition(grants.allow(key: "a", seconds: 3600, now: now, wall: 123000))
    let lease = grants.leases(now: now)[0]
    precondition(lease.publicInfo["started"] as? Int64 == 123000)
    precondition(grants.adjust(key: "a", id: "stale", idle: 0, now: now) == nil)
    grants.consume(key: "a", request: 9, now: now+600_000_000_000)
    precondition(grants.allows(key: "a", request: 10, now: now+1499_000_000_000))
    precondition(!grants.allows(key: "a", request: 10, now: now+1500_000_000_000))
    precondition(grants.expired(now: now+1500_000_000_000).count == 1)
    precondition(grants.adjust(key: "a", id: lease.id, idle: 0, now: now+1500_000_000_000) == nil)
    precondition(!grants.allow(key: "b", seconds: 59, now: now))
    grants.clear()
    print("SSH identity authorization QA passed; no keychain, notifications or authentication UI accessed")
}
#endif
