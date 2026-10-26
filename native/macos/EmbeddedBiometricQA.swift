// Offline-only QA. Compile together with EmbeddedBiometric.swift as an executable.
// Creates hidden views only; never evaluates authentication or reads a device key.
import AppKit
import Foundation
import LocalAuthentication
import Security

private final class FlippedQAView: NSView {
    override var isFlipped: Bool { true }
}

@main
struct EmbeddedBiometricQA {
    static func main() throws {
        precondition(Thread.isMainThread)
        let app = NSApplication.shared
        app.setActivationPolicy(.prohibited)
        let activeBefore = app.isActive
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 300, height: 200), styleMask: .borderless, backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        let content = window.contentView!
        precondition(!window.isVisible && !window.isKeyWindow)

        let sizingIcon = EmbeddedAuthenticationIcon(context: LAContext(), controlSize: .small)
        print("unmounted small icon frame=\(sizingIcon.frame), fitting=\(sizingIcon.fittingSize)")
        // The production slot must preserve the native 32-point dimensions.
        let production = EmbeddedAuthenticationSession()
        production.installPresentation(in: window, context: LAContext(), frame: NSRect(x: 20, y: 30, width: 32, height: 32))
        let productionIcon = content.subviews.compactMap { $0 as? EmbeddedAuthenticationIcon }.first!
        precondition(productionIcon.fittingSize == NSSize(width: 32, height: 32))
        precondition(productionIcon.frame.size == NSSize(width: 32, height: 32))
        precondition(productionIcon.bounds.size == NSSize(width: 32, height: 32))
        var productionEvaluations = 0
        let productionGate = EmbeddedAuthenticationStartGate(isReady: { true }, evaluate: {
            if production.validateAuthenticationSize() { productionEvaluations += 1 }
        }, timedOut: { preconditionFailure("production size gate timed out") })
        drainMainQueue()
        precondition(productionEvaluations == 1 && !production.presentationIsReady)
        productionGate.cancel()
        production.destroy()

        let undersized = EmbeddedAuthenticationSession()
        undersized.installPresentation(in: window, context: LAContext(), frame: NSRect(x: 20, y: 30, width: 24, height: 24))
        var undersizedEvaluations = 0
        let undersizedGate = EmbeddedAuthenticationStartGate(isReady: { true }, evaluate: {
            if undersized.validateAuthenticationSize() { undersizedEvaluations += 1 }
        }, timedOut: { preconditionFailure("undersized gate timed out") })
        drainMainQueue()
        precondition(undersizedEvaluations == 0 && content.subviews.isEmpty)
        let undersizedReply = read(undersized)
        precondition(undersizedReply["errorCode"] as? String == "notReady")
        precondition((undersizedReply["error"] as? String)?.contains("图标空间不足") == true)
        precondition(undersizedReply["nativeDomain"] == nil && undersizedReply["nativeCode"] == nil)
        undersizedGate.requestCheck()
        drainMainQueue()
        precondition(undersizedEvaluations == 0)
        undersized.destroy()

        // Legacy undersized-slot coverage tests drawing geometry only. A
        // scaled icon must never be treated as authentication-ready.
        let item = EmbeddedAuthenticationSession()
        item.installPresentation(in: window, context: LAContext(), frame: NSRect(x: 20, y: 30, width: 24, height: 24))
        precondition(!item.presentationIsReady)
        let icon = content.subviews.compactMap { $0 as? EmbeddedAuthenticationIcon }.first!
        precondition(icon.hitTest(NSPoint(x: 12, y: 12)) == nil)
        precondition(!icon.acceptsFirstResponder)
        let natural = icon.fittingSize
        precondition(NSRect(x: 20, y: 146, width: 24, height: 24).contains(icon.frame))
        precondition(icon.frame.width <= 24 && icon.frame.height <= 24)
        precondition(icon.bounds.width == natural.width && icon.bounds.height == natural.height)
        precondition(icon.frame.width <= icon.bounds.width && icon.frame.height <= icon.bounds.height)
        print("small icon fitting=\(natural), frame=\(icon.frame), bounds=\(icon.bounds)")
        if let bitmap = icon.bitmapImageRepForCachingDisplay(in: icon.bounds) {
            icon.cacheDisplay(in: icon.bounds, to: bitmap)
            try bitmap.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: "/private/tmp/smallssh-embedded-icon.png"))
        }
        item.move(NSRect(x: 40, y: 50, width: 36, height: 34))
        precondition(NSRect(x: 40, y: 116, width: 36, height: 34).contains(icon.frame))
        window.close()
        precondition(content.subviews.isEmpty)
        var lateKey = Data(repeating: 0xA5, count: 32)
        item.finish(key: &lateKey)
        precondition(lateKey.isEmpty)
        let cancelled = read(item)
        precondition(cancelled["key"] == nil && cancelled["errorCode"] as? String == "cancelled")
        item.destroy()

        let flippedWindow = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 300, height: 200), styleMask: .borderless, backing: .buffered, defer: false)
        flippedWindow.isReleasedWhenClosed = false
        let flipped = FlippedQAView(frame: NSRect(x: 0, y: 0, width: 300, height: 200))
        flippedWindow.contentView = flipped
        let hiddenItem = EmbeddedAuthenticationSession()
        hiddenItem.installPresentation(in: flippedWindow, context: LAContext(), frame: NSRect(x: 20, y: 30, width: 24, height: 24))
        precondition(NSRect(x: 20, y: 30, width: 24, height: 24).contains(flipped.subviews.first!.frame))
        NotificationCenter.default.post(name: NSWindow.didChangeOcclusionStateNotification, object: flippedWindow)
        precondition(flipped.subviews.isEmpty && read(hiddenItem)["key"] == nil)
        hiddenItem.destroy()
        flippedWindow.close()

        // Valid envelope shape with deliberately unusable fixture bytes. A
        // bogus/stale pointer is rejected without ever dereferencing it.
        let fixture: [String: Any] = ["version": 1, "privateKey": "", "ephemeral": "", "ciphertext": ""]
        let blob = try JSONSerialization.data(withJSONObject: fixture)
        let request = try JSONSerialization.data(withJSONObject: ["action": "unwrap", "blob": blob.base64EncodedString(), "reason": "离线 QA"])
        let token = request.withUnsafeBytes { bytes in
            smallsshAuthStart(UnsafeMutableRawPointer(bitPattern: 1), bytes.bindMemory(to: UInt8.self).baseAddress, Int32(request.count), 0, 0, 24, 24)
        }
        let needed = smallsshAuthPoll(token, nil, 0)
        precondition(needed < -1)
        var buffer = [UInt8](repeating: 0, count: Int(-needed))
        let count = buffer.withUnsafeMutableBufferPointer { smallsshAuthPoll(token, $0.baseAddress, Int32($0.count)) }
        precondition(count == -needed)
        let reply = try JSONSerialization.jsonObject(with: Data(buffer)) as! [String: Any]
        precondition(reply["key"] == nil && reply["errorCode"] as? String == "notReady")
        let repeated = buffer.withUnsafeMutableBufferPointer { smallsshAuthPoll(token, $0.baseAddress, Int32($0.count)) }
        precondition(repeated == count)
        smallsshAuthMove(token, .nan, 0, 24, 24)
        smallsshAuthCancel(token)
        smallsshAuthRelease(token)
        precondition(smallsshAuthPoll(token, nil, 0) == -1)
        let invalid = smallsshAuthStart(nil, nil, 0, 0, 0, 24, 24)
        precondition(smallsshAuthPoll(invalid, nil, 0) < -1)
        smallsshAuthRelease(invalid)
        verifyDeferredEvaluation()
        verifyErrorMapping()
        precondition(!window.isVisible && !window.isKeyWindow && app.keyWindow == nil)
        precondition(app.isActive == activeBefore)
        print("PASS hidden view, production 32-point size, undersized authentication rejection, legacy drawing geometry, click/keyboard pass-through, close cleanup, late key destruction, ABI polling, stale-window rejection, deferred single evaluation, cancellation, readiness timeout and structured errors; no authentication evaluated")
    }

    static func verifyDeferredEvaluation() {
        var ready = false
        var evaluations = 0
        var timeouts = 0
        let gate = EmbeddedAuthenticationStartGate(isReady: { ready }, evaluate: { evaluations += 1 }, timedOut: { timeouts += 1 })
        precondition(evaluations == 0)
        drainMainQueue()
        precondition(evaluations == 0)
        ready = true
        gate.requestCheck()
        gate.requestCheck()
        precondition(evaluations == 0) // Readiness must still defer to the next turn.
        drainMainQueue()
        gate.requestCheck()
        drainMainQueue()
        precondition(evaluations == 1 && timeouts == 0)

        var cancelledEvaluations = 0
        ready = false
        let cancelled = EmbeddedAuthenticationStartGate(isReady: { ready }, evaluate: { cancelledEvaluations += 1 }, timedOut: { preconditionFailure("cancelled gate timed out") })
        drainMainQueue()
        cancelled.cancel()
        ready = true
        cancelled.requestCheck()
        drainMainQueue()
        precondition(cancelledEvaluations == 0)

        var expiredEvaluations = 0
        var expired = false
        ready = false
        let expiring = EmbeddedAuthenticationStartGate(isReady: { ready }, evaluate: { expiredEvaluations += 1 }, timedOut: { expired = true })
        let deadline = Date(timeIntervalSinceNow: 4)
        while !expired && Date() < deadline {
            _ = RunLoop.main.run(mode: .default, before: Date(timeIntervalSinceNow: 0.01))
        }
        precondition(expired && expiredEvaluations == 0)
        ready = true
        expiring.requestCheck()
        drainMainQueue()
        precondition(expiredEvaluations == 0 && evaluations == 1 && timeouts == 0)
    }

    static func verifyErrorMapping() {
        let cases: [(LAError.Code, EmbeddedAuthenticationErrorCode)] = [
            (.userCancel, .cancelled), (.userFallback, .cancelled),
            (.appCancel, .notReady), (.systemCancel, .notReady),
            (.notInteractive, .notReady), (.invalidContext, .notReady), (.invalidDimensions, .notReady),
            (.authenticationFailed, .authenticationFailed), (.biometryLockout, .biometryLocked),
            (.biometryNotAvailable, .biometryUnavailable), (.biometryNotEnrolled, .biometryUnavailable),
            (.biometryNotPaired, .biometryUnavailable), (.biometryDisconnected, .biometryUnavailable),
            (.passcodeNotSet, .biometryUnavailable),
        ]
        for (nativeCode, expected) in cases {
            let error = NSError(domain: LAErrorDomain, code: nativeCode.rawValue, userInfo: [NSLocalizedDescriptionKey: "PRIVATE-QA-MARKER", "blob": "PRIVATE-QA-BLOB"])
            let mapped = EmbeddedAuthenticationErrorCode.classify(error)
            precondition(mapped == expected)
            let item = EmbeddedAuthenticationSession()
            item.fail(mapped, nativeError: error)
            let reply = read(item)
            precondition(reply["errorCode"] as? String == expected.rawValue)
            precondition(reply["nativeDomain"] as? String == LAErrorDomain)
            precondition(reply["nativeCode"] as? Int == nativeCode.rawValue)
            precondition(Set(reply.keys) == ["error", "errorCode", "nativeDomain", "nativeCode"])
            precondition(!(reply["error"] as! String).contains("PRIVATE-QA"))
            item.destroy()
        }
        precondition(EmbeddedAuthenticationErrorCode.classify(nil) == .authenticationFailed)
        precondition(EmbeddedAuthenticationErrorCode.classify(NSError(domain: LAErrorDomain, code: -99999)) == .authenticationFailed)
        precondition(EmbeddedAuthenticationErrorCode.classify(NSError(domain: "qa", code: 1), fallback: .keyUnavailable) == .keyUnavailable)
        precondition(EmbeddedAuthenticationErrorCode.classify(NSError(domain: NSOSStatusErrorDomain, code: Int(errSecInteractionNotAllowed))) == .notReady)
        precondition(EmbeddedAuthenticationErrorCode.classify(NSError(domain: NSOSStatusErrorDomain, code: Int(errSecUserCanceled))) == .cancelled)
        let completed = EmbeddedAuthenticationSession()
        var fixtureKey = Data(repeating: 0xA5, count: 32)
        completed.finish(key: &fixtureKey)
        precondition(fixtureKey.isEmpty && Set(read(completed).keys) == ["key"])
        completed.destroy()
    }

    static func drainMainQueue() {
        var drained = false
        DispatchQueue.main.async { drained = true }
        let deadline = Date(timeIntervalSinceNow: 1)
        while !drained && Date() < deadline {
            _ = RunLoop.main.run(mode: .default, before: Date(timeIntervalSinceNow: 0.01))
        }
        precondition(drained)
    }

    static func read(_ item: EmbeddedAuthenticationSession) -> [String: Any] {
        let needed = item.poll(nil, capacity: 0)
        precondition(needed < 0)
        var bytes = [UInt8](repeating: 0, count: Int(-needed))
        let count = bytes.withUnsafeMutableBufferPointer { item.poll($0.baseAddress, capacity: Int32($0.count)) }
        precondition(count == -needed)
        return try! JSONSerialization.jsonObject(with: Data(bytes)) as! [String: Any]
    }
}
