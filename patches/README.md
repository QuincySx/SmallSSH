# Vendored 安全修复

MyGo 固定在 v0.9.2，完整依赖与原许可证位于 `vendor/`。

`mygo-macos-password-accessibility.patch` 仅修复 macOS 密码框的辅助功能值缓存：外部 AX setter 仍然向实际输入控件传递内容，但向 AppKit 基类保存空值；现代 getter 与旧 AXValue 路径也对安全字段返回空值。普通输入框不受影响。覆盖三条路径：

1. `accessibilityAttributeValue:` 的 `AXValue`（旧式客户端读取）。
2. `accessibilityValue` getter（现代客户端读取）。
3. `setAccessibilityValue:` 与 `setAccessibilitySelectedText:`（现代客户端写入；AppKit 基类不得缓存明文，控件本身仍收到内容）。

`AXStringForRange` / `AXAttributedStringForRange` 不需补丁：MyGo 在 `ui/access.go` 里对密码框不填 `node.Value`，范围读取本身返回空。

实测前：通过 AX setter 填入错误测试密码后，AX 查询会返回缓存明文。修复后：相同输入后查询仍只返回安全字段描述，正常解锁有效。第 3 条中的 `setAccessibilitySelectedText:` 是 v0.6.4 新增的 AX 写入路径，按同一规则加固，尚未由本人做真实 AX 回归。v0.9.2 已在上游修复两个 setter 的基类缓存路径；本地补丁仅保留现代 getter 与旧 AXValue 的额外空值保护。升级时已逐项审核上游 setter、防止旧式与现代读取泄露的本地 getter、以及 `ui/access.go` 安全字段值屏蔽；新的原生文件哈希记录在完整性清单。

`vendor-integrity.json` 记录该修改文件的预期 SHA256。打包入口会验证，防止更新 vendor 时意外丢失修复。更新依赖后需重新核对补丁和原生密码输入回归；不能直接更新哈希跳过审核。

该修改尚未向第三方提交或发布。

## sing-vmess Vision 指针与双向过滤状态

`sing-vmess-vision-pointer.patch` 修复本轮 REALITY/Vision 合成 SSH 回归中发现的两项上游问题：TLS 对象地址经过 `uintptr` 跨函数传递会触发 Go 的 checkptr 检查；TLS 检测计数和状态在读写 goroutine 间无同步访问会触发 race。

补丁让注册器保留 `unsafe.Pointer` 并用 `unsafe.Add` 获取字段地址，先核对 `bytes.Reader` / `bytes.Buffer` 字段类型；TLS 过滤器以独立锁串行更新内部解析状态，对跨方向读写的状态使用原子访问，读方向也有独立锁。锁不跨越双向网络 I/O，不把整个连接读写串行化。依然依赖 Go TLS/uTLS 的内部缓冲字段，因此升级时需要重跑实际 REALITY/Vision SSH、SFTP、主机身份与竞态回归，不代表未知 ABI 永远兼容。

两个修改文件的预期 SHA256 同样保存在 `vendor-integrity.json`。`tools deps -update` 重建 vendor 后重新应用这两份补丁，再校验原哈希；任何上游或补丁变化必须重新审核。没有关闭 checkptr 或 race 来跳过失败。以上修复目前只保留在本仓库，未向上游提交。
