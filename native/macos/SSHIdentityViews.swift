import AppKit
import CoreText
import LocalAuthentication
import LocalAuthenticationEmbeddedUI
import SwiftUI

private let identityFontNames: [IdentityFontSource: String] = Dictionary(uniqueKeysWithValues: IdentityFontSource.allCases.compactMap { source in
    guard let data = Data(base64Encoded: source.base64), let provider = CGDataProvider(data: data as CFData),
          let font = CGFont(provider), let name = font.postScriptName else { return nil }
    CTFontManagerRegisterGraphicsFont(font, nil)
    return (source, name as String)
})

func identityFont(_ source: IdentityFontSource, size: CGFloat) -> Font {
    if let name = identityFontNames[source] { return .custom(name, fixedSize: size) }
    return .system(size: size, weight: source == .bold ? .bold : .regular, design: source == .mono ? .monospaced : .default)
}

struct PendingRow: Identifiable {
    let id: UInt64
    let name: String
    let origin: String
    let group: String
    let fingerprint: String
    let authentication: String
    let kind: String
    let detail: String
    let host: String

    var isLogin: Bool { kind == "login" }
    var purpose: String {
        switch kind {
        case "login": return "SSH 登录"
        case "signature": return "签名"
        default: return "未知签名用途"
        }
    }
}

private struct ReviewHeight: PreferenceKey {
    static let defaultValue: CGFloat = 0
    static func reduce(value: inout CGFloat, nextValue: () -> CGFloat) { value = max(value, nextValue()) }
}

struct IdentityAuthorizationView: View {
    let rows: [PendingRow]
    let reviewIDs: Set<UInt64>
    let context: LAContext?
    let review: (Set<UInt64>) -> Void
    let deny: () -> Void
    let authenticate: (Bool) -> Void
    let evaluate: (LAContext) -> Void
    var fit: (CGFloat) -> Void = { _ in }
    var scrolls = true
    #if IDENTITY_QA
    var detailsOnly = false
    #endif
    @Environment(\.colorScheme) private var scheme
    @State private var fingerprintsFor: PendingRow?
    private typealias T = IdentityTokens

    private var selection: [PendingRow] { rows.filter { reviewIDs.contains($0.id) } }
    private var groups: [(key: String, value: [PendingRow])] {
        Dictionary(grouping: rows, by: \.group).sorted { ($0.value.first?.id ?? 0) < ($1.value.first?.id ?? 0) }
    }

    var body: some View {
        #if IDENTITY_QA
        if detailsOnly, let first = rows.first { fingerprintDetails(first) {} }
        else { authorization }
        #else
        authorization
        #endif
    }

    private var authorization: some View {
        Group {
            if scrolls { ScrollView { content } }
            else { content }
        }
        .onPreferenceChange(ReviewHeight.self, perform: fit)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .background(T.canvas(scheme))
        .accessibilityHidden(fingerprintsFor != nil)
        .disabled(fingerprintsFor != nil)
        .overlay {
            if let row = fingerprintsFor {
                ZStack {
                    Color.black.opacity(0.24).onTapGesture { fingerprintsFor = nil }.accessibilityHidden(true)
                    fingerprintDetails(row) { fingerprintsFor = nil }
                        .padding(T.large)
                }
            }
        }
        .onChange(of: reviewIDs) { _ in fingerprintsFor = nil }
        .onChange(of: rows.map(\.id)) { ids in
            if let row = fingerprintsFor, !ids.contains(row.id) { fingerprintsFor = nil }
        }
    }

    private var content: some View {
        VStack(alignment: .leading, spacing: T.medium) {
            if let first = selection.first {
                approval(first, count: selection.count)
            } else {
                heading("SSH 密钥使用授权", queueDescription)
                ForEach(groups, id: \.key) { _, group in
                    if let first = group.first { requestGroup(first, rows: group) }
                }
            }
        }
        .padding(T.large)
        .fixedSize(horizontal: false, vertical: true)
        .frame(maxWidth: .infinity, alignment: .topLeading)
        .background {
            GeometryReader { proxy in Color.clear.preference(key: ReviewHeight.self, value: proxy.size.height) }
        }
    }

    private var queueDescription: String {
        if groups.count == 1, let first = rows.first { return "\(first.origin)有 \(rows.count) 个请求等待确认。" }
        return "有 \(rows.count) 个请求等待确认。"
    }

    private func heading(_ title: String, _ detail: String) -> some View {
        VStack(alignment: .leading, spacing: T.fieldGap) {
            line(Text(title), font: T.title, height: T.titleHeight, ink: T.text(scheme)).lineLimit(2).accessibilityAddTraits(.isHeader)
            line(Text(detail), font: T.hint, height: T.hintHeight, ink: T.muted(scheme))
        }
    }

    private func approval(_ first: PendingRow, count: Int) -> some View {
        let scope = count == 1 ? "本次请求" : "这 \(count) 个请求"
        let method = first.authentication == "biometry" ? "仅当前指纹" : "macOS 验证"
        return Group {
            heading("允许\(first.origin)使用 SSH 密钥？", "\(method) · \(scope)")
            requestCard(first, reviewing: true)
            divider
            HStack(spacing: T.small) {
                Spacer(minLength: 0)
                action("拒绝", run: deny)
                    .keyboardShortcut(.cancelAction)
                authenticationAction(first)
            }.frame(height: T.authAction)
        }
    }

    private func authenticationAction(_ row: PendingRow) -> some View {
        let label = row.authentication == "biometry" ? "Touch ID 确认" : "macOS 验证"
        return ZStack {
            RoundedRectangle(cornerRadius: T.controlRadius).fill(T.accentSoft(scheme))
            if let context {
                if row.authentication == "biometry" {
                    IdentityTouchIDView(context: context, evaluate: evaluate)
                        .frame(width: T.authAction, height: T.authAction)
                        .accessibilityLabel("Touch ID 验证中")
                } else {
                    ProgressView().controlSize(.small).accessibilityLabel("macOS 验证中")
                }
            } else {
                Button { authenticate(row.authentication == "biometry") } label: {
                    Text("\u{E045}")
                        .font(T.fingerprintIcon)
                        .foregroundStyle(T.accent(scheme))
                        .frame(width: T.authAction, height: T.authAction)
                        .contentShape(Rectangle())
                }.buttonStyle(.plain).accessibilityLabel(label).help(label)
            }
        }.frame(width: T.authAction, height: T.authAction)
    }

    private func requestGroup(_ first: PendingRow, rows: [PendingRow]) -> some View {
        VStack(alignment: .leading, spacing: T.medium) {
            if groups.count > 1 {
                line(Text(first.origin), font: T.keyName, height: T.keyNameHeight, ink: T.text(scheme)).accessibilityAddTraits(.isHeader)
            }
            requestCard(first, reviewing: false)
            VStack(spacing: T.fieldGap) {
                ForEach(Array(rows.enumerated()), id: \.element.id) { index, row in
                    if index > 0 { divider }
                    HStack(spacing: T.small) {
                        value("第 \(index + 1) 个\(row.isLogin ? "登录" : "签名")请求")
                        Spacer(minLength: 0)
                        action("确认这条", fill: T.subtle(scheme)) { review([row.id]) }
                    }
                }
            }
            .padding(.vertical, T.small).padding(.horizontal, T.medium)
            .modifier(IdentityCard())
            if rows.count > 1 {
                HStack(spacing: T.small) {
                    line(Text("整组确认只涵盖这 \(rows.count) 个请求。"), font: T.hint, height: T.hintHeight, ink: T.muted(scheme))
                    Spacer(minLength: 0)
                    action("确认这 \(rows.count) 个请求", fill: T.accent(scheme), ink: T.onAccent(scheme)) { review(Set(rows.map(\.id))) }
                }
            }
        }
    }

    private func requestCard(_ row: PendingRow, reviewing: Bool) -> some View {
        VStack(alignment: .leading, spacing: T.small) {
            HStack(spacing: T.small) {
                Text("\u{E01B}").font(T.keyIcon).foregroundStyle(T.accent(scheme))
                    .frame(width: T.control, height: T.control)
                    .background(T.keyBadge(scheme), in: RoundedRectangle(cornerRadius: T.controlRadius))
                    .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: T.fieldGap) {
                    caption("SSH 密钥")
                    line(Text(row.name), font: reviewing ? T.title : T.keyName,
                         height: reviewing ? T.titleHeight : T.keyNameHeight, ink: T.text(scheme))
                }.frame(maxWidth: .infinity, alignment: .leading)
                if !row.host.isEmpty {
                    action("查看指纹") { fingerprintsFor = row }
                }
            }
            divider
            HStack(spacing: T.small) {
                caption("用途")
                value(row.purpose)
                Spacer(minLength: 0)
                if !row.detail.isEmpty {
                    caption(row.isLogin ? "登录账号" : "命名空间")
                    value(row.detail)
                }
            }
            if row.isLogin {
                HStack(spacing: T.small) {
                    caption("主机")
                    value(row.host.isEmpty ? "请求未提供主机身份" : "可核对主机指纹",
                          ink: row.host.isEmpty ? T.warning(scheme) : T.muted(scheme))
                }
            }
            if row.host.isEmpty {
                divider
                fingerprint("密钥指纹", row.fingerprint)
            }
        }.padding(T.medium).modifier(IdentityCard())
    }

    private func fingerprintDetails(_ row: PendingRow, close: @escaping () -> Void) -> some View {
        VStack(alignment: .leading, spacing: T.medium) {
            line(Text("核对指纹"), font: T.title, height: T.titleHeight, ink: T.text(scheme)).accessibilityAddTraits(.isHeader)
            fingerprint("SSH 密钥指纹 · \(row.name)", row.fingerprint)
            fingerprint("主机指纹 · 登录账号 \(row.detail)", row.host)
            HStack {
                Spacer()
                action("关闭", fill: T.subtle(scheme), run: close).keyboardShortcut(.cancelAction)
            }
        }.padding(T.large).frame(maxWidth: 404).modifier(IdentityCard())
    }

    private func caption(_ text: String) -> some View {
        Text(text).font(T.hint).foregroundStyle(T.muted(scheme))
            .fixedSize(horizontal: true, vertical: false).frame(height: T.hintHeight)
    }

    private func value(_ text: String, ink: Color? = nil) -> some View {
        line(Text(text), font: T.copy, height: T.copyHeight, ink: ink ?? T.text(scheme))
    }

    private func fingerprint(_ title: String, _ value: String) -> some View {
        VStack(alignment: .leading, spacing: T.fieldGap) {
            line(Text(title), font: T.hint, height: T.hintHeight, ink: T.muted(scheme))
            line(Text(value), font: T.path, height: T.pathHeight, ink: T.text(scheme)).textSelection(.enabled)
        }.frame(maxWidth: .infinity, alignment: .leading)
    }

    private var divider: some View { Rectangle().fill(T.border(scheme)).frame(height: T.divider).accessibilityHidden(true) }

    private func line(_ text: Text, font: Font, height: CGFloat, ink: Color) -> some View {
        ViewThatFits(in: .horizontal) {
            text.lineLimit(1).fixedSize(horizontal: true, vertical: false).frame(height: height)
            text.fixedSize(horizontal: false, vertical: true)
        }.font(font).foregroundStyle(ink)
    }

    private func action(_ title: String, fill: Color = .clear, ink: Color? = nil, run: @escaping () -> Void) -> some View {
        Button(action: run) {
            Text(title).font(T.button).foregroundStyle(ink ?? T.text(scheme))
                .padding(.horizontal, T.medium).frame(height: T.control)
                .contentShape(Rectangle())
        }.buttonStyle(.plain)
            .background(fill, in: RoundedRectangle(cornerRadius: T.controlRadius))
            .fixedSize()
    }
}

private struct IdentityCard: ViewModifier {
    @Environment(\.colorScheme) private var scheme
    func body(content: Content) -> some View {
        let shape = RoundedRectangle(cornerRadius: IdentityTokens.panelRadius)
        content.frame(maxWidth: .infinity, alignment: .leading)
            .background(IdentityTokens.surface(scheme), in: shape)
            .overlay { shape.strokeBorder(IdentityTokens.border(scheme), lineWidth: IdentityTokens.divider).allowsHitTesting(false) }
    }
}

private struct IdentityTouchIDView: NSViewRepresentable {
    let context: LAContext
    let evaluate: (LAContext) -> Void

    func makeNSView(context _: Context) -> IdentityTouchIDHost {
        IdentityTouchIDHost(context: context, evaluate: evaluate)
    }

    func updateNSView(_ view: IdentityTouchIDHost, context: Context) {}
}

private final class IdentityTouchIDHost: NSView {
    let icon: LAAuthenticationView
    private var start: (() -> Void)?
    private var startScheduled = false

    init(context: LAContext, evaluate: @escaping (LAContext) -> Void) {
        icon = LAAuthenticationView(context: context, controlSize: .small)
        start = { evaluate(context) }
        super.init(frame: .zero)
        icon.translatesAutoresizingMaskIntoConstraints = false
        addSubview(icon)
        NSLayoutConstraint.activate([
            icon.centerXAnchor.constraint(equalTo: centerXAnchor),
            icon.centerYAnchor.constraint(equalTo: centerYAnchor)
        ])
    }

    required init?(coder: NSCoder) { nil }

    override func viewDidMoveToWindow() { scheduleStart() }

    override func layout() {
        super.layout()
        scheduleStart()
    }

    private func scheduleStart() {
        guard start != nil, !startScheduled, window != nil else { return }
        startScheduled = true
        DispatchQueue.main.async { [weak self] in
            guard let self else { return }
            self.startScheduled = false
            guard let run = self.start, self.window?.isVisible == true else { return }
            self.start = nil
            self.layoutSubtreeIfNeeded()
            let iconFrame = self.icon.frame
            // If the native view cannot fit, let the OS present the same policy.
            if iconFrame.isEmpty || !self.bounds.contains(iconFrame) {
                self.icon.removeFromSuperview()
            }
            run()
        }
    }
}

#if IDENTITY_QA
@MainActor func identitySnapshots(_ directory: String) {
    precondition(identityFontNames.count == IdentityFontSource.allCases.count, "SmallPen fonts did not load")
    let touchID = IdentityTouchIDHost(context: LAContext()) { _ in preconditionFailure("Offline QA must not authenticate") }
    touchID.setFrameSize(NSSize(width: IdentityTokens.authAction, height: IdentityTokens.authAction))
    touchID.layoutSubtreeIfNeeded()
    precondition(touchID.icon.frame.width > 0 && touchID.bounds.contains(touchID.icon.frame))
    print("SSH Touch ID host: \(touchID.bounds.size); system view: \(touchID.icon.frame)")
    let key = "SHA256:WqnVNwRskJkGXMIcKggnk03x80mz8q2iGodqHGXZ/xE"
    let host = "SHA256:bNNH1qSZny3dC4S7+ccPO+DNyoX8w6/riA2aFs4co3U"
    func request(_ id: UInt64, host: String) -> PendingRow {
        PendingRow(id: id, name: "工作 Mac", origin: "终端", group: "example", fingerprint: key,
                   authentication: "presence", kind: "login", detail: "deploy", host: host)
    }
    let pair = [request(1, host: host), request(2, host: host)]
    for scheme in [ColorScheme.light, .dark] {
        func snapshot<V: View>(_ view: V, _ state: String, _ width: Double, _ height: Double) {
            let renderer = ImageRenderer(content: view.frame(width: width, height: height).environment(\.colorScheme, scheme))
            guard let image = renderer.cgImage else { fatalError("无法离屏绘制 SSH 审批 \(state)") }
            let data = NSBitmapImageRep(cgImage: image).representation(using: .png, properties: [:])!
            let name = "\(state)-\(scheme == .dark ? "dark" : "light").png"
            let path = URL(fileURLWithPath: directory, isDirectory: true).appendingPathComponent(name)
            try! data.write(to: path)
            print("SSH 授权离屏截图 \(path.path) \(image.width)×\(image.height)")
        }
        for (state, rows, ids, width, height) in [
            ("queue", pair, Set<UInt64>(), 520.0, 400.0),
            ("group", pair, [1, 2], 480.0, 266.0),
            ("single", [pair[0]], [1], 480.0, 266.0),
            ("unknown", [request(1, host: "")], [1], 480.0, 316.0)
        ] {
            var screen = IdentityAuthorizationView(rows: rows, reviewIDs: ids, context: nil, review: { _ in },
                                                   deny: {}, authenticate: { _ in }, evaluate: { _ in }, scrolls: false)
            snapshot(screen, state, width, height)
            if state == "group" {
                screen.detailsOnly = true
                snapshot(screen, "fingerprints", 404, 186)
            }
        }
    }
}
#endif
