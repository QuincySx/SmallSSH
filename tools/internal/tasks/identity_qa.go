package tasks

import (
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sshclient/docs/design"
	"sshclient/tools/internal/project"
	"strconv"
	"strings"
)

func identitySwiftSources(dir string) ([]string, error) {
	tokens := filepath.Join(dir, "SSHIdentityTokens.swift")
	var swift strings.Builder
	swift.WriteString("import SwiftUI\nenum IdentityTokens {\n")
	for _, item := range []struct{ name, token string }{
		{"canvas", "color-canvas"}, {"surface", "color-surface"}, {"text", "color-text"}, {"muted", "color-muted"},
		{"border", "color-border"}, {"accent", "color-accent"}, {"accentSoft", "color-accentsoft"},
		{"onAccent", "color-onaccent"}, {"subtle", "color-subtle"}, {"keyBadge", "color-native-tab-selected"},
		{"warning", "color-warning"},
	} {
		fmt.Fprintf(&swift, "    static func %s(_ scheme: ColorScheme) -> Color { color(scheme == .dark ? 0x%s : 0x%s) }\n",
			item.name, strings.TrimPrefix(design.Color(true, item.token), "#"), strings.TrimPrefix(design.Color(false, item.token), "#"))
	}
	for _, item := range []struct{ name, token string }{
		{"fieldGap", "spacing-native-field"}, {"small", "spacing-space-sm"}, {"medium", "spacing-space-md"},
		{"large", "spacing-space-lg"}, {"control", "size-control"}, {"authAction", "size-authentication-action"},
		{"controlRadius", "radius-control"}, {"panelRadius", "radius-window-panel"},
		{"divider", "size-review-divider"}, {"promptWidth", "size-dialog-small"},
	} {
		fmt.Fprintf(&swift, "    static let %s: CGFloat = %s\n", item.name, strconv.FormatFloat(float64(design.Length(item.token)), 'f', -1, 32))
	}
	for _, item := range []struct {
		name, token, font string
	}{
		{"title", "type-native-dialog", "bold"}, {"hint", "type-native-hint", "regular"},
		{"copy", "type-native-copy", "regular"}, {"keyName", "type-native-label", "bold"},
		{"button", "type-body", "regular"}, {"path", "type-path", "mono"},
		{"fingerprintIcon", "type-native-icon-large", "icons"}, {"keyIcon", "type-native-action-icon", "icons"},
	} {
		fmt.Fprintf(&swift, "    static let %s: Font = identityFont(.%s, size: %s)\n", item.name, item.font,
			strconv.FormatFloat(float64(design.TypeSize(item.token)), 'f', -1, 32))
		fmt.Fprintf(&swift, "    static let %sHeight: CGFloat = %.0f\n", item.name, math.Round(float64(design.TypeLineHeight(item.token))))
	}
	swift.WriteString("    private static func color(_ rgb: UInt32) -> Color { Color(red: Double(rgb >> 16) / 255, green: Double((rgb >> 8) & 255) / 255, blue: Double(rgb & 255) / 255) }\n}\n")
	swift.WriteString("enum IdentityFontSource: String, CaseIterable {\n")
	fonts := []struct{ name, file string }{
		{"regular", "SmallSSHUI-400.woff"}, {"bold", "SmallSSHUI-700.woff"},
		{"mono", "SmallSSHMono-400.woff"}, {"icons", "SmallSSHIcons-400.woff"},
	}
	for _, item := range fonts {
		fmt.Fprintf(&swift, "    case %s\n", item.name)
	}
	swift.WriteString("    var base64: String {\n        switch self {\n")
	for _, item := range fonts {
		woff, err := os.ReadFile(filepath.Join("docs", "design", "smallssh.smallpen", "assets", "fonts", item.file))
		if err != nil {
			return nil, err
		}
		font, err := unwrapWOFF(woff)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", item.file, err)
		}
		fmt.Fprintf(&swift, "        case .%s: return %q\n", item.name, base64.StdEncoding.EncodeToString(font))
	}
	swift.WriteString("        }\n    }\n}\n")
	if err := os.WriteFile(tokens, []byte(swift.String()), 0644); err != nil {
		return nil, err
	}
	return []string{tokens, "native/macos/SSHIdentityAuthorization.swift", "native/macos/SSHIdentityStore.swift", "native/macos/SSHIdentityViews.swift", "native/macos/SSHIdentity.swift"}, nil
}

func stageIdentityAgent(dir, arch string, log *tee) (string, error) {
	runner := run
	if log != nil {
		runner = func(name string, args ...string) error { return step(log, name, args...) }
	}
	bundle := filepath.Join(dir, "SmallSSHIdentityAgent.app", "Contents")
	executable := filepath.Join(bundle, "MacOS", "SmallSSHIdentityAgent")
	if err := os.MkdirAll(filepath.Dir(executable), 0755); err != nil {
		return "", err
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict><key>CFBundleIdentifier</key><string>dev.quincy.sshworkbench.identity-agent</string><key>CFBundleName</key><string>SmallSSH 密钥</string><key>CFBundleExecutable</key><string>SmallSSHIdentityAgent</string><key>CFBundlePackageType</key><string>APPL</string><key>CFBundleVersion</key><string>1</string><key>LSUIElement</key><true/><key>LSMinimumSystemVersion</key><string>13.0</string></dict></plist>`
	if err := os.WriteFile(filepath.Join(bundle, "Info.plist"), []byte(plist), 0644); err != nil {
		return "", err
	}
	sourceDir, err := os.MkdirTemp("build", "identity-sources-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(sourceDir)
	sources, err := identitySwiftSources(sourceDir)
	if err != nil {
		return "", err
	}
	args := append([]string{"-O", "-module-name", "SmallSSHIdentityAgent", "-target", arch + "-apple-macosx13.0"}, sources...)
	if err := runner("/usr/bin/swiftc", append(args, "-o", executable)...); err != nil {
		return "", err
	}
	if err := runner("/usr/bin/codesign", "--force", "--sign", "-", "--options", "runtime", "--timestamp=none", filepath.Dir(bundle)); err != nil {
		return "", err
	}
	return executable, nil
}

func runIdentityQA(args []string) error {
	fs := flag.NewFlagSet("identity-qa", flag.ContinueOnError)
	keychain := fs.Bool("keychain", false, "额外执行不弹窗的系统钥匙串回归，仅创建并清理随机QA命名空间中的合成记录")
	embedded := fs.Bool("embedded", false, "额外检查隐藏的 macOS 指纹控件尺寸和生命周期，不执行系统验证")
	snapshots := fs.Bool("snapshots", false, "离屏渲染合成 SSH 密钥审批请求，不执行系统验证")
	if err := parse(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 || runtime.GOOS != "darwin" {
		return errors.New("identity-qa 需要 macOS 且不接受参数")
	}
	if _, err := project.Chdir(); err != nil {
		return err
	}
	out, path, err := openLog("identity-qa", false)
	if err != nil {
		return err
	}
	defer out.Close()
	dir := filepath.Join("build", "identity-qa")
	if err = os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x86_64"
	}
	if _, err = stageIdentityAgent(dir, arch, out); err != nil {
		return err
	}
	executable := filepath.Join(dir, "authorization-qa")
	sources, err := identitySwiftSources(dir)
	if err != nil {
		return err
	}
	qaArgs := append([]string{"-D", "IDENTITY_QA", "-O", "-module-name", "SmallSSHIdentityQA", "-target", arch + "-apple-macosx13.0"}, sources...)
	if err = step(out, "/usr/bin/swiftc", append(qaArgs, "-o", executable)...); err != nil {
		return err
	}
	if err = step(out, executable); err != nil {
		return err
	}
	if *snapshots {
		output := filepath.Join("build", "snapshots", "ssh-key-authorization")
		if err = os.MkdirAll(output, 0755); err != nil {
			return err
		}
		if err = step(out, executable, output); err != nil {
			return err
		}
	}
	if *keychain {
		storage := filepath.Join(dir, "storage-qa")
		if err = step(out, "/usr/bin/swiftc", "-O", "-module-name", "SmallSSHIdentityStorageQA", "-target", arch+"-apple-macosx13.0", "native/macos/SSHIdentityStore.swift", "native/macos/SSHIdentityStoreQA.swift", "-o", storage); err != nil {
			return err
		}
		if err = step(out, storage); err != nil {
			return err
		}
	}
	if *embedded {
		view := filepath.Join(dir, "embedded-qa")
		if err = step(out, "/usr/bin/swiftc", "-O", "-module-name", "SmallSSHEmbeddedQA", "-target", arch+"-apple-macosx13.0", "native/macos/EmbeddedBiometric.swift", "native/macos/EmbeddedBiometricQA.swift", "-o", view); err != nil {
			return err
		}
		if err = step(out, view); err != nil {
			return err
		}
	}
	fmt.Fprintln(out, "原生签名模块构建与纯授权 QA 通过："+path)
	return nil
}
