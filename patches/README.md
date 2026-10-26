# Vendored 安全修复

MyGo 固定在 v0.5.5，完整依赖与原许可证位于 `vendor/`。

`mygo-macos-password-accessibility.patch` 仅修复 macOS 密码框的辅助功能值缓存：外部 AX setter 仍然向实际输入控件传递内容，但向 AppKit 基类保存空值；现代 getter 与旧 AXValue 路径也对安全字段返回空值。普通输入框不受影响。

实测前：通过 AX setter 填入错误测试密码后，AX 查询会返回缓存明文。修复后：相同输入后查询仍只返回安全字段描述，正常解锁有效。

`vendor-integrity.json` 记录该修改文件的预期 SHA256。打包入口会验证，防止更新 vendor 时意外丢失修复。更新依赖后需重新核对补丁和原生密码输入回归；不能直接更新哈希跳过审核。

该修改尚未向第三方提交或发布。
