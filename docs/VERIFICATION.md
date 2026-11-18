# 验收记录

各轮记录当时的工作范围、测试证据与实机边界。统一任务见 [开发、构建与发布](DEVELOPMENT.md)。

- [初期至 2026-11-07](archive/VERIFICATION-01.md)
- [2026-11-08 至 2026-11-18 较早记录](archive/VERIFICATION-02.md)

## 2026-11-18 · 单次限时维护授权

新增 CLI / SDK 的维护准备会话。一次批准绑定服务器、范围、期限及经系统核对的程序实例，允许固定系统检查、下载或续传指定来源的现有备份，以及 APT 模拟升级。范围外操作被后台拒绝并记录；维护授权到期即结束。原生审批显示签名应用 ID 与具体范围，使用统一的「允许操作」按钮。中风险操作被单独拒绝后，后台先交付拒绝结果，原受控会话仍可继续。

- 最终完整检查通过，证据为 `build/logs/check-20261118-115454-2692550611.log`。真实本机 IPC 和合成 SSH 夹具验证了 1 次人工申请、4 次自动授权、越权拦截、已存在分片的正确续传和落盘一致性。时钟夹具验证维护会话到期后无法继续或申请续期。
- 桌面 UI Tester 和离屏快照通过，记录在 `build/logs/test-20261118-115454-1278301061.log`。已目视核对 `build/snapshots/maintenance/approval-maintenance.png` 的签名 ID、来源、期限和决定按钮。
- SmallPen 修订 `bff29fe8ad8fb5a0f12b4f88c90f08ff2659c913f330d0140f196fa1c5886fa6`，结构零错误与警告，目标页渲染零问题。检查记录为 `build/logs/maintenance-smallpen-check.json` 和 `build/logs/maintenance-smallpen-render.json`。透明指纹图标的自动对比检查跳过。分别生成明暗 PNG，并同步页面映射。

本机合成验证使用预置部分文件模拟断点；升级命令由 SSH 夹具返回，不执行真实升级。当前安装的 App 和后台仍运行原版本。签名 ID 用于识别申请端，会话仍绑定当前运行进程及活连接。

## 2026-11-18 · 中风险文件操作确认开关

受控会话审批新增「确认中风险文件操作」，默认开启。所有者关闭后，后台按已批准的范围自动执行普通 SFTP 文件变更，并保留审计。任意命令及未纳入开关的操作仍逐项确认，高风险及受保护路径继续拦截。开关选择纳入审批证明和审计导出，续期时默认重新开启。

- 完整检查通过，见 `build/logs/check-20261118-123417-1320492569.log`。合成本机 IPC 和 SSH 验证了申请端伪造开关及篡改签名字段被拒绝、新建目录直接执行、符号链接和关键路径被拦截、命令继续等待确认、选择和执行记录可查。新旧消息字段的兼容性有协议测试。
- UI Tester 覆盖默认开启、切换、两审批界面的草稿共享及新授权重置。离屏快照见 `build/snapshots/medium-file-review/`，开、关状态都已目视核对，测试记录为 `build/logs/test-20261118-123107-138653128.log`。
- SmallPen 修订 `cdb374dd9968165a428137cd48ae6c8ea69269a63a917471343860e1ad842724`，结构和目标页渲染零问题。明暗两种预览已目视核对，透明指纹图标的自动对比检查跳过。证据在 `build/logs/medium-review-smallpen-check.json` 和 `build/logs/medium-review-smallpen-render.json`。

上述运行验证仅覆盖本机 macOS 与合成服务器。当前正式 App 和后台仍使用原安装版本。

## 2026-11-18 · 实时切换中风险文件确认

当前授权和关联的审计详情可实时切换中风险文件操作确认。实时选项绑定当前授权世代，原审批证明、期限和闲置设置继续有效。开启时撤销未开始的自动许可，已开始的操作继续。审计详情保留申请时的只读勾选，规则修改记录前后状态，并随 JSONL 和 CSV 导出。

- 最终全量检查通过，见 `build/logs/check-20261118-130542-469024909.log`。Manager 测试核验世代检查、旧撤销凭证、待确认操作、未使用许可和进行中任务。桌面测试通过本机 IPC 从当前授权关掉确认、从审计详情重新开启，并核验历史状态与旧世代的操作入口。
- 1080 宽明暗、中英文离屏快照见 `build/snapshots/live-medium-review/`，测试记录为 `build/logs/test-20261118-130327-123703039.log`。已目视核对审计详情和当前授权。
- SmallPen 修订 `d87ae7da6b562769be1db27b246825c11c9e5c13ce07765d89dd1dd7df427c99`，结构无错误或警告。两页的明暗渲染共四条既有导航与原生窗口按钮叠放信息。报告为 `build/logs/live-medium-review-smallpen-活动-render.json` 和 `build/logs/live-medium-review-smallpen-历史-render.json`。

本轮验证使用本机 macOS 与合成服务器。当前安装的 App 和后台版本尚未替换。

## 2026-11-18 · 本机安装与 README 截图

使用当前工作区打包 macOS arm64，退出原正式 App 和后台后替换 `/Applications/SmallSSH.app`。安装包由主程序、CLI 和后台三份二进制逐一比对，深层严格签名通过。后台启动后通过 SDK 进程身份与 TLS 握手。

- 构建、生成文件检查和终止证据分别为 `build/logs/local-replacement-build-20261118.log`、`build/logs/local-replacement-generated-check-20261118.log` 和 `build/logs/local-replacement-shutdown-20261118.log`。旧后台安装前身份握手通过，未发现已建立的 TCP 会话。正常退出正式 App 后，以后台支持的 SIGTERM 停止旧服务。
- 安装和二进制复核见 `build/logs/install-20261118-133353-311487162.log` 与 `build/logs/local-replacement-installed-20261118.log`。使用后台方式启动，正式 App PID 26701、后台 PID 26805。新后台报告 unlocked，记录在 `build/logs/service-20261118-133510-1099045720.log`。独立开发后台 PID 23417 仍在运行。旧包备份和安装临时目录均已清理。
- 设计入口整理为现行交互、窗口承载和 SmallPen 维护方式。删除旧格式切换、alpha 兼容复核及临时预览入口记录。可编辑设计结构检查通过，见 `build/logs/local-replacement-smallpen-check-20261118.json`。
- 中英文 README 共用三张应用离屏截图，覆盖工作台、SFTP 和授权审计。已目视核对并验证相对链接，原始测试见 `build/logs/test-20261118-133824-2683840456.log` 和 `build/logs/test-20261118-133510-1829411885.log`。
- 全量检查通过，见 `build/logs/check-20261118-133824-3925495574.log`。上述安装和运行证据仅覆盖本机 macOS arm64。截图使用合成数据，正式应用验证停在本地后台握手。

## 2026-11-18 · 设计字体与许可文件核对

四个 WOFF 文件的哈希均不同，UI 字体内嵌字重分别为 400 和 700。同步三份来源说明中的现行字体文件名和本地图标许可证路径。文本字体的已记录 SHA-256 与文件匹配，四条 CSS 字体引用均可解析。SmallPen 检查见 `build/logs/font-notices-smallpen-check-20261118.json`。这一轮仅调整文档和许可元数据。

## 2026-11-18 · 文档合并与历史归档

`docs/` 根目录由 17 份文档减为 8 份。功能进度、授权委托、服务器管理、服务目录与本地化内容并入对应入口。旧拆分计划、竞品取证、组件参考和较早验收记录移入 `docs/archive/`。中英文 README 使用同一套入口。保存连接、当前授权与 VNC 说明按现行实现核对。

- 文档校验记录在 `build/logs/docs-consolidation-links-20261118.log`，覆盖仓库内链接与锚点、49 个需求编号、旧验收章节和 README 截图。
- 本机全量检查通过，见 `build/logs/check-20261118-145434-2246455859.log`。本轮改动仅涉及文档。
