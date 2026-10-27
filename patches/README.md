# Vendored 安全修复

MyGo 固定在 v0.7.1，完整依赖与原许可证位于 `vendor/`。

`mygo-macos-password-accessibility.patch` 仅修复 macOS 密码框的辅助功能值缓存：外部 AX setter 仍然向实际输入控件传递内容，但向 AppKit 基类保存空值；现代 getter 与旧 AXValue 路径也对安全字段返回空值。普通输入框不受影响。覆盖三条路径：

1. `accessibilityAttributeValue:` 的 `AXValue`（旧式客户端读取）。
2. `accessibilityValue` getter（现代客户端读取）。
3. `setAccessibilityValue:` 与 `setAccessibilitySelectedText:`（现代客户端写入；AppKit 基类不得缓存明文，控件本身仍收到内容）。

`AXStringForRange` / `AXAttributedStringForRange` 不需补丁：MyGo 在 `ui/access.go` 里对密码框不填 `node.Value`，范围读取本身返回空。

实测前：通过 AX setter 填入错误测试密码后，AX 查询会返回缓存明文。修复后：相同输入后查询仍只返回安全字段描述，正常解锁有效。第 3 条中的 `setAccessibilitySelectedText:` 是 v0.6.4 新增的 AX 写入路径，按同一规则加固，尚未由本人做真实 AX 回归。v0.7.1 的 `internal/darwin/access.go` 与 v0.6.4 逐字节相同，补丁因此仍落在同一位置、同一哈希。

`vendor-integrity.json` 记录该修改文件的预期 SHA256。打包入口会验证，防止更新 vendor 时意外丢失修复。更新依赖后需重新核对补丁和原生密码输入回归；不能直接更新哈希跳过审核。

该修改尚未向第三方提交或发布。
