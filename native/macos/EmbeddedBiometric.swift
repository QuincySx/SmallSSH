import AppKit
import CryptoKit
import Foundation
import LocalAuthentication
import LocalAuthenticationEmbeddedUI
import Security

// C ABI: start is called on the application's main thread while window is live.
// Rectangles are window-content points with a top-left origin. poll is thread-safe;
// its positive result is JSON bytes, zero is pending, and -N requests N bytes.
// A result remains owned by the session until cancel/release, which erase it.
private struct EmbeddedInput: Decodable {
    let action: String
    let blob: Data?
    let reason: String?
}
private struct EmbeddedEnvelope: Decodable {
    let version: Int
    let privateKey: Data
    let ephemeral: Data
    let ciphertext: Data
}
private struct EmbeddedOutput: Encodable {
    var key: Data?
    var error: String?
    var errorCode: String?
    var nativeDomain: String?
    var nativeCode: Int?
}
enum EmbeddedAuthenticationErrorCode: String {
    case cancelled, notReady, authenticationFailed, biometryUnavailable, biometryLocked, keyUnavailable, invalidData

    var message: String {
        switch self {
        case .cancelled: return "Touch ID 验证已取消，可点击指纹重试或使用密码解锁"
        case .notReady: return "Touch ID 尚未就绪，可点击指纹重试或使用密码解锁"
        case .authenticationFailed: return "Touch ID 验证未完成，可点击指纹重试或使用密码解锁"
        case .biometryUnavailable: return "Touch ID 不可用，请使用密码解锁"
        case .biometryLocked: return "Touch ID 已锁定，请使用密码解锁"
        case .keyUnavailable: return "设备密钥解锁失败，请使用密码解锁"
        case .invalidData: return "设备密钥数据无效，请使用密码解锁"
        }
    }

    static func classify(_ error: Error?, fallback: Self = .authenticationFailed) -> Self {
        guard let error = error as NSError? else { return fallback }
        if error.domain == LAErrorDomain {
            switch LAError.Code(rawValue: error.code) {
            case .userCancel, .userFallback: return .cancelled
            case .appCancel, .systemCancel, .notInteractive, .invalidContext, .invalidDimensions: return .notReady
            case .biometryNotAvailable, .biometryNotEnrolled, .biometryNotPaired, .biometryDisconnected, .passcodeNotSet: return .biometryUnavailable
            case .biometryLockout: return .biometryLocked
            case .authenticationFailed: return .authenticationFailed
            default: return fallback
            }
        }
        if error.domain == NSOSStatusErrorDomain {
            switch OSStatus(error.code) {
            case errSecUserCanceled: return .cancelled
            case errSecInteractionNotAllowed: return .notReady
            default: break
            }
        }
        return fallback
    }
}

// Wait for presentation signals, never retry a policy evaluation. Keeping this
// scheduler independent of LAContext also lets offline QA exercise it safely.
final class EmbeddedAuthenticationStartGate {
    private let isReady: () -> Bool
    private var evaluation: (() -> Void)?
    private var timeout: DispatchWorkItem?
    private var checkScheduled = false

    init(isReady: @escaping () -> Bool, evaluate: @escaping () -> Void, timedOut: @escaping () -> Void) {
        self.isReady = isReady
        self.evaluation = evaluate
        let timeout = DispatchWorkItem { [weak self] in
            guard let self = self, self.evaluation != nil else { return }
            self.cancel()
            timedOut()
        }
        self.timeout = timeout
        DispatchQueue.main.asyncAfter(deadline: .now() + 3, execute: timeout)
        requestCheck()
    }

    func requestCheck() {
        precondition(Thread.isMainThread)
        guard evaluation != nil, !checkScheduled else { return }
        checkScheduled = true
        // Apple documents evaluation after the authentication view appears.
        // Even an already-ready host gets a turn to attach and lay out the view.
        DispatchQueue.main.async { [weak self] in
            guard let self = self else { return }
            self.checkScheduled = false
            guard let evaluation = self.evaluation, self.isReady() else { return }
            self.cancel()
            evaluation()
        }
    }

    func cancel() {
        precondition(Thread.isMainThread)
        evaluation = nil
        timeout?.cancel()
        timeout = nil
    }
}
private let embeddedDomain = Data("ssh-workbench/device-unlock/v1".utf8)
private let embeddedLimit = 65536

// The icon only presents authentication state. Mouse and keyboard input belongs
// to the MyGo controls underneath (password by default, explicit retry at right).
final class EmbeddedAuthenticationIcon: LAAuthenticationView {
    override func hitTest(_ point: NSPoint) -> NSView? { nil }
    override var acceptsFirstResponder: Bool { false }
}

private func erase(_ data: inout Data) {
    data.resetBytes(in: data.startIndex..<data.endIndex)
    data.removeAll(keepingCapacity: false)
}
private func failure(_ code: EmbeddedAuthenticationErrorCode, message: String? = nil, nativeError: Error? = nil) -> Data {
    let native = nativeError as NSError?
    // Include only stable classification and numeric diagnostics, never userInfo.
    return try! JSONEncoder().encode(EmbeddedOutput(error: message ?? code.message, errorCode: code.rawValue, nativeDomain: native?.domain, nativeCode: native?.code))
}
private func onMain(_ body: () -> Void) {
    if Thread.isMainThread { body() } else { DispatchQueue.main.sync(execute: body) }
}

final class EmbeddedAuthenticationSession {
    private let lock = NSLock()
    private var response: Data?
    private var cancelled = false
    private weak var window: NSWindow?
    private var icon: EmbeddedAuthenticationIcon?
    private var slotSize: NSSize?
    private var context: LAContext?
    private var observers: [NSObjectProtocol] = []
    private var startGate: EmbeddedAuthenticationStartGate?

    func fail(_ code: EmbeddedAuthenticationErrorCode, message: String? = nil, nativeError: Error? = nil) {
        precondition(Thread.isMainThread)
        rejectBeforePresentation(code, message: message, nativeError: nativeError)
        removePresentation()
    }

    func rejectBeforePresentation(_ code: EmbeddedAuthenticationErrorCode, message: String? = nil, nativeError: Error? = nil) {
        lock.lock()
        if !cancelled && response == nil { response = failure(code, message: message, nativeError: nativeError) }
        lock.unlock()
    }

    func begin(windowPointer: UnsafeMutableRawPointer?, input: Data, frame: NSRect) {
        precondition(Thread.isMainThread)
        do {
            let request = try JSONDecoder().decode(EmbeddedInput.self, from: input)
            guard request.action == "unwrap", let blob = request.blob else {
                fail(.invalidData, message: "内嵌认证仅支持设备密钥解锁")
                return
            }
            let envelope = try JSONDecoder().decode(EmbeddedEnvelope.self, from: blob)
            guard envelope.version == 1 else {
                fail(.invalidData, message: "设备密钥版本不支持")
                return
            }
            // Match registered live windows rather than dereferencing a caller's
            // potentially stale NSWindow pointer. Do not keep the window alive.
            guard let pointer = windowPointer, let app = NSApp,
                  let host = app.windows.first(where: { Unmanaged.passUnretained($0).toOpaque() == pointer }),
                  host.isVisible, host.contentView != nil else {
                fail(.notReady, message: "认证窗口已关闭或隐藏，可点击指纹重试")
                return
            }
            guard SecureEnclave.isAvailable else {
                fail(.biometryUnavailable, message: "设备未提供 Secure Enclave，请使用密码解锁")
                return
            }
            let authentication = LAContext()
            authentication.touchIDAuthenticationAllowableReuseDuration = 0
            authentication.localizedFallbackTitle = ""
            let reason = request.reason?.trimmingCharacters(in: .whitespacesAndNewlines)
            authentication.localizedReason = (reason?.isEmpty == false ? reason : nil) ?? "解锁 SmallSSH"
            installPresentation(in: host, context: authentication, frame: frame)
            startGate = EmbeddedAuthenticationStartGate(isReady: { [weak self] in
                self?.presentationIsReady == true
            }, evaluate: { [weak self] in
                self?.evaluate(envelope, context: authentication)
            }, timedOut: { [weak self] in
                self?.fail(.notReady)
            })
        } catch {
            fail(.invalidData)
        }
    }

    private func evaluate(_ envelope: EmbeddedEnvelope, context authentication: LAContext) {
        precondition(Thread.isMainThread)
        guard isPending, presentationIsReady else { return }
        guard validateAuthenticationSize() else { return }
        var availability: NSError?
        guard authentication.canEvaluatePolicy(.deviceOwnerAuthenticationWithBiometrics, error: &availability),
              authentication.biometryType == .touchID else {
            fail(EmbeddedAuthenticationErrorCode.classify(availability, fallback: .biometryUnavailable), nativeError: availability)
            return
        }
        // The SDK's LAAuthenticationView contract routes this context's
        // evaluation into the embedded icon. Biometrics-only deliberately
        // excludes the system-password fallback permitted by userPresence.
        authentication.evaluatePolicy(.deviceOwnerAuthenticationWithBiometrics, localizedReason: authentication.localizedReason) { [weak self] success, error in
            DispatchQueue.main.async {
                guard let self = self, self.isPending else { return }
                guard success else {
                    self.fail(EmbeddedAuthenticationErrorCode.classify(error), nativeError: error)
                    return
                }
                // Reuse the SAME authenticated context for the actual key
                // operation. If that credential cannot satisfy userPresence,
                // fail instead of opening another authentication dialog.
                authentication.interactionNotAllowed = true
                DispatchQueue.global(qos: .userInitiated).async { [weak self] in
                    do {
                        var plaintext = try Self.unwrap(envelope, context: authentication)
                        DispatchQueue.main.async {
                            self?.finish(key: &plaintext)
                            erase(&plaintext) // Also destroys a late result after release.
                        }
                    } catch {
                        DispatchQueue.main.async {
                            self?.fail(EmbeddedAuthenticationErrorCode.classify(error, fallback: .keyUnavailable), nativeError: error)
                        }
                    }
                }
            }
        }
    }

    var presentationIsReady: Bool {
        precondition(Thread.isMainThread)
        guard isPending, let host = window, let content = host.contentView, let view = icon,
              host.isVisible, !host.isMiniaturized, host.isKeyWindow,
              NSApp?.isActive == true, NSApp?.isHidden == false,
              view.window === host, view.superview === content, !view.isHiddenOrHasHiddenAncestor else { return false }
        content.layoutSubtreeIfNeeded()
        view.layoutSubtreeIfNeeded()
        return !view.frame.isEmpty && !view.bounds.isEmpty
    }

    // Enforce native dimensions before evaluating. AppKit can restore a scaled
    // view's frame during layout, so the assigned slot must fit it as well.
    func validateAuthenticationSize() -> Bool {
        precondition(Thread.isMainThread)
        guard let view = icon, let slot = slotSize else { return false }
        view.layoutSubtreeIfNeeded()
        let natural = view.fittingSize
        guard natural.width > 0, natural.height > 0,
              slot.width >= natural.width, slot.height >= natural.height,
              view.frame.width >= natural.width, view.frame.height >= natural.height else {
            fail(.notReady, message: "Touch ID 图标空间不足，请调整窗口后点击指纹重试或使用密码解锁")
            return false
        }
        return true
    }

    // Kept separate from evaluation so isolated QA can inspect a hidden view
    // without asking LocalAuthentication to authenticate or touching any key.
    func installPresentation(in host: NSWindow, context: LAContext, frame: NSRect) {
        precondition(Thread.isMainThread)
        guard let content = host.contentView else { return }
        self.window = host
        self.context = context
        let view = EmbeddedAuthenticationIcon(context: context, controlSize: .small)
        view.setAccessibilityElement(false)
        view.setAccessibilityHidden(true)
        view.wantsLayer = true
        view.layer?.masksToBounds = true
        self.icon = view
        content.addSubview(view, positioned: .above, relativeTo: nil)
        move(frame)
        for name in [NSWindow.willCloseNotification, NSWindow.didMiniaturizeNotification] {
            observers.append(NotificationCenter.default.addObserver(forName: name, object: host, queue: .main) { [weak self] _ in
                self?.cancel()
            })
        }
        observers.append(NotificationCenter.default.addObserver(forName: NSWindow.didChangeOcclusionStateNotification, object: host, queue: .main) { [weak self, weak host] _ in
            if host?.isVisible != true { self?.cancel() } else { self?.startGate?.requestCheck() }
        })
        observers.append(NotificationCenter.default.addObserver(forName: NSApplication.didHideNotification, object: nil, queue: .main) { [weak self] _ in
            self?.cancel()
        })
        for name in [NSWindow.didBecomeKeyNotification, NSWindow.didExposeNotification] {
            observers.append(NotificationCenter.default.addObserver(forName: name, object: host, queue: .main) { [weak self] _ in
                self?.startGate?.requestCheck()
            })
        }
        observers.append(NotificationCenter.default.addObserver(forName: NSApplication.didBecomeActiveNotification, object: nil, queue: .main) { [weak self] _ in
            self?.startGate?.requestCheck()
        })
    }

    private static func unwrap(_ envelope: EmbeddedEnvelope, context: LAContext) throws -> Data {
        let protected = try SecureEnclave.P256.KeyAgreement.PrivateKey(dataRepresentation: envelope.privateKey, authenticationContext: context)
        let publicKey = try P256.KeyAgreement.PublicKey(x963Representation: envelope.ephemeral)
        let shared = try protected.sharedSecretFromKeyAgreement(with: publicKey)
        let key = shared.hkdfDerivedSymmetricKey(using: SHA256.self, salt: embeddedDomain, sharedInfo: embeddedDomain, outputByteCount: 32)
        return try AES.GCM.open(AES.GCM.SealedBox(combined: envelope.ciphertext), using: key, authenticating: embeddedDomain)
    }

    private var isPending: Bool {
        lock.lock()
        defer { lock.unlock() }
        return !cancelled && response == nil
    }

    // Internal for the offline lifecycle QA; no authentication success is exposed
    // through the ABI without first obtaining the actual envelope plaintext.
    func finish(key: inout Data) {
        precondition(Thread.isMainThread)
        defer { erase(&key) }
        lock.lock()
        if !cancelled && response == nil {
            if key.count == 32 {
                response = try? JSONEncoder().encode(EmbeddedOutput(key: key))
            } else {
                response = failure(.invalidData, message: "设备密钥长度错误")
            }
        }
        lock.unlock()
        removePresentation()
    }

    func move(_ frame: NSRect) {
        precondition(Thread.isMainThread)
        guard let content = window?.contentView, let view = icon else { return }
        slotSize = frame.size
        let natural = view.fittingSize
        let naturalWidth = natural.width > 0 ? natural.width : frame.width
        let naturalHeight = natural.height > 0 ? natural.height : frame.height
        let scale = min(1, frame.width / naturalWidth, frame.height / naturalHeight)
        let size = NSSize(width: naturalWidth * scale, height: naturalHeight * scale)
        let x = frame.minX + (frame.width - size.width) / 2
        let top = frame.minY + (frame.height - size.height) / 2
        let y = content.isFlipped ? top : content.bounds.height - top - size.height
        view.frame = NSRect(origin: NSPoint(x: x, y: y), size: size)
        // Retain the native bounds for drawing. Undersized slots may draw a
        // scaled icon, but validateAuthenticationSize prevents authentication.
        view.setBoundsSize(NSSize(width: naturalWidth, height: naturalHeight))
        startGate?.requestCheck()
    }

    func poll(_ output: UnsafeMutablePointer<UInt8>?, capacity: Int32) -> Int32 {
        lock.lock()
        defer { lock.unlock() }
        guard let bytes = response else { return 0 }
        guard capacity >= bytes.count, let output = output else { return -Int32(bytes.count) }
        bytes.copyBytes(to: output, count: bytes.count)
        return Int32(bytes.count)
    }

    func cancel() {
        precondition(Thread.isMainThread)
        lock.lock()
        cancelled = true
        if response != nil { erase(&response!) }
        response = failure(.cancelled)
        lock.unlock()
        removePresentation()
    }

    func destroy() {
        precondition(Thread.isMainThread)
        cancel()
        lock.lock()
        if response != nil { erase(&response!) }
        response = nil
        lock.unlock()
    }

    private func removePresentation() {
        precondition(Thread.isMainThread)
        startGate?.cancel()
        startGate = nil
        context?.invalidate()
        context = nil
        icon?.removeFromSuperview()
        icon = nil
        slotSize = nil
        window = nil
        for observer in observers { NotificationCenter.default.removeObserver(observer) }
        observers.removeAll()
    }
}

private let sessionsLock = NSLock()
private var sessions: [UInt64: EmbeddedAuthenticationSession] = [:]
private var nextToken: UInt64 = 0
private func session(_ token: UInt64) -> EmbeddedAuthenticationSession? {
    sessionsLock.lock()
    defer { sessionsLock.unlock() }
    return sessions[token]
}
private func validFrame(_ x: Double, _ y: Double, _ width: Double, _ height: Double) -> Bool {
    [x, y, width, height].allSatisfy { $0.isFinite } && width > 0 && height > 0
}

@_cdecl("smallssh_auth_start")
public func smallsshAuthStart(_ window: UnsafeMutableRawPointer?, _ input: UnsafePointer<UInt8>?, _ length: Int32, _ x: Double, _ y: Double, _ width: Double, _ height: Double) -> UInt64 {
    let item = EmbeddedAuthenticationSession()
    sessionsLock.lock()
    repeat { nextToken &+= 1 } while nextToken == 0 || sessions[nextToken] != nil
    let token = nextToken
    sessions[token] = item
    sessionsLock.unlock()
    // Do not dispatch a raw window pointer for later use: its owner must enter
    // start on main while the window is live. No pointer is touched on failure.
    guard Thread.isMainThread else {
        item.rejectBeforePresentation(.notReady, message: "认证必须从窗口主线程启动")
        return token
    }
    guard length > 0 && length < embeddedLimit, let input = input,
          validFrame(x, y, width, height) else {
        item.fail(.invalidData, message: "认证请求无效")
        return token
    }
    var data = Data(bytes: input, count: Int(length))
    defer { erase(&data) }
    item.begin(windowPointer: window, input: data, frame: NSRect(x: x, y: y, width: width, height: height))
    return token
}

@_cdecl("smallssh_auth_move")
public func smallsshAuthMove(_ token: UInt64, _ x: Double, _ y: Double, _ width: Double, _ height: Double) {
    guard validFrame(x, y, width, height) else { return }
    onMain { session(token)?.move(NSRect(x: x, y: y, width: width, height: height)) }
}

@_cdecl("smallssh_auth_cancel")
public func smallsshAuthCancel(_ token: UInt64) {
    onMain { session(token)?.cancel() }
}

@_cdecl("smallssh_auth_poll")
public func smallsshAuthPoll(_ token: UInt64, _ output: UnsafeMutablePointer<UInt8>?, _ capacity: Int32) -> Int32 {
    session(token)?.poll(output, capacity: capacity) ?? -1
}

@_cdecl("smallssh_auth_release")
public func smallsshAuthRelease(_ token: UInt64) {
    onMain {
        sessionsLock.lock()
        let item = sessions.removeValue(forKey: token)
        sessionsLock.unlock()
        item?.destroy()
    }
}
