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

## 2026-11-18 · 连接侧栏首帧加载与落点

解锁后立即唤醒本机后台查询。连接列表结果独立于待审批查询更新。展开和紧凑侧栏在首次读取时显示轻微呼吸的骨架行，系统减少动态效果时使用静态骨架。读取失败提示重试，成功读取空保险库后才显示新增服务器空态。

松手时立即在侧栏显示新顺序。加密保存完成前保持落点，丢弃保存前或保存期间采集的旧轮询快照。失败时恢复已确认顺序并显示错误。移动整个组保持该组的展开状态。

- 无窗口回归覆盖首帧、读取失败与恢复、真实空列表、落点先于保存返回、旧快照在保存前后到达、冲突回退及保险库持久化。全量检查通过，见 `build/logs/check-20261118-154347-2705691149.log`。
- 明暗及宽窄侧栏快照见 `build/snapshots/sidebar-loading/`，测试见 `build/logs/test-20261118-154402-1970027262.log`。已目视复核。SmallPen 修订 `5c20622d55e2198055b9c1b93e6c9b8e689ce588bc2d7916a006dbf6db142a63`，结构零错误和警告，新页明暗渲染零报告问题，五处图标自动对比检查跳过。
- 本机 macOS arm64 已打包并通过深层严格签名检查，见 `build/logs/sidebar-loading-build-20261118.log`。本轮验证使用合成数据和无窗口渲染。正式安装仍是上一版。

## 2026-11-18 · 安装连接侧栏修复版

旧正式后台经只读握手确认为锁定，未发现已建立的 SSH 连接。正常停止旧后台后，用先前通过全量检查的 macOS arm64 包替换 `/Applications/SmallSSH.app`。安装后的主程序、CLI、后台与候选包逐一比对，深层严格签名通过。新后台在正式数据目录完成身份与 TLS 握手，状态为锁定。桌面窗口留待用户打开。

安装日志为 `build/logs/install-20261118-162534-3841124705.log`，替换前后握手分别见 `build/logs/service-20261118-162446-29909541.log` 和 `build/logs/service-20261118-163221-333509261.log`。安装和握手验证仅覆盖本机。

## 2026-11-18 · 转发入口图标与面板线稿

右上角「中转与服务」换成细线双向箭头。保留运行计数及原有菜单行为。核对到「加速与代理」可能在首次读取前显示空态，转发表单展开高级选项会从 480 DIP 变为 760 DIP。

- [竞品对照](archive/SSH-TUNNEL-GUI-REFERENCE.md) 记录 VS Code 的单端口流程、Termius 的转发和线路设置、RSSH 的规则列表及 SSHDeck 的主机分组。面板线稿位于 `build/review/tunnel-panel/转发面板线稿.png`，只用于评审。
- 全量检查和离屏入口测试通过，见 `build/logs/check-20261118-164117-3914349090.log` 与 `build/logs/test-20261118-164247-2875761389.log`。目视核对窄窗图标。SmallPen 修订 `ae95f14ec1a756c5994c155eb0b921204e527194d738ac2e16b1ba43d10ab409`，结构、组件和草稿渲染零报告问题。组件一处自动对比度检查跳过。
- 已打包并通过本机 macOS arm64 深层签名检查，打包日志为 `build/logs/tunnel-entry-build-20261118.log`。正式安装仍是上一版。

## 2026-11-18 · 安装双向箭头入口版

将签名通过的 macOS arm64 候选包替换到 `/Applications/SmallSSH.app`。安装后复核了深层签名以及主程序、CLI 和后台与候选包的一致性。CLI 和后台二进制内容相同，安装保留原文件节点。后台 PID 54425 持续运行，替换前后均完成身份验证与 TLS 握手，状态为锁定。

安装记录为 `build/logs/install-20261118-202121-1783811468.log`，安装后的服务检查在 `build/logs/service-20261118-202130-2948286133.log`。验证覆盖本机安装与后台握手。聊天中的可点菜单和转发图仍是原型。

## 2026-11-18 · 原生图形转发与本机安装

右上角「端口转发」打开运行列表，右键保留快捷停用。新建页通过三张图选择本地、远程或 SOCKS5，服务器与端口直接在节点上编辑。切换场景保留本次端口草稿。沿用目标服务器的默认线路、当前会话或连线上明确选择的可用线路。远程转发标明入口所在服务器，启动成功后列表展示实际入口并提供复制与停止。「启动后打开网页」为本地转发的可选操作。经已连接的 SSH 或 SOCKS5 登录仍从目标服务器的右键菜单选择。

- 完整检查通过，见 `build/logs/check-20261118-204231-1873919823.log`。合成 SSH、远程转发回环收发、SOCKS5 作为连接线路、逐跳指纹核验、取消和浏览器选项都有回归。离屏明暗、中英文与两档宽度证据在 `build/review/tunnel-native-20261118/` 和 `build/logs/test-20261118-204252-1193106759.log`。
- SmallPen 修订 `e97bdb49abca06cf19b7a6991242a2d08c7c27822c3962ef325229e70a36ba05`，结构检查零错误与警告。三种方向、上游代理及服务列表的明暗渲染零报告问题。结构与各页报告位于 `build/logs/tunnel-diagram-*-20261118.json`。
- macOS arm64 已替换 `/Applications/SmallSSH.app`。替换前确认未发现后台已建立的 TCP 连接，正式 App 正常退出。安装包深层签名与三个二进制一致性均通过。CLI 和后台内容及文件节点相同，保留原后台进程。安装与身份握手分别见 `build/logs/install-20261118-204511-655831749.log`、`build/logs/service-20261118-204526-1716043339.log`。桌面留待用户打开，安装及视图验收覆盖本机。

## 2026-11-18 · 右键新建 SSH 中转

目标服务器右键「终端连接」用分隔线划分默认与直连、已就绪通道、新建中转与已保存线路。选「新建 SSH 中转」后可选另一台已保存服务器，通过它连接目标。端口转发图的连线菜单也提供该入口。临时中转只用于本次操作，按原流程核对各跳指纹。

- 离屏回归使用合成 SSH 服务，经实际菜单选择没有现成会话的中转，依次核对中转和目标指纹，验证只打开目标终端，原网络配置和线路列表保持原样。见 `build/logs/test-20261118-205851-362444516.log`。全量检查通过，见 `build/logs/check-20261118-205949-2035949833.log`。
- SmallPen 修订 `456e33a481ebf0aa6b63075f3512f9369c3bd349f3a89f42f5976c7bcadd5042`，结构零错误和警告，两页明暗渲染零报告问题。已目视核对浅色菜单 PNG，见 `build/review/ssh-relay-menu-20261118/`。
- 本机 macOS arm64 候选包通过深层严格签名检查，见 `build/logs/ssh-relay-menu-build-20261118.log`。两个生成文件均为最新。正式安装仍是上一版。

## 2026-11-18 · 安装右键 SSH 中转版

正式 App 正常退出，原后台经校验确认已锁定后正常停止。将 macOS arm64 候选包安装到 `/Applications/SmallSSH.app`，主程序、CLI 和后台均与候选一致，深层严格签名通过。安装后短暂启动新后台，在正式数据目录通过本机进程身份及 TLS 握手并确认锁定。该验证进程随后已退出，打开正式 App 后将按正常流程启动后台。

安装记录见 `build/logs/install-20261118-210726-1060537119.log`，旧后台锁定和新后台握手分别见 `build/logs/service-20261118-210700-993822052.log` 与 `build/logs/service-20261118-210741-1989171005.log`。验证覆盖本机安装与后台启动。

## 2026-11-18 · 转发设置直接显示

移除端口转发里的「高级设置」。入口和目标地址、端口直接在图中编辑，切换方向时保留各自草稿。访问远端服务时可在图下选择启动后打开网页。选择本机已有 SOCKS5 时才显示其地址。SmallPen 合并了对应的重复页面。

- 离屏测试覆盖两类地址字段、模式切换、SOCKS5 地址按需显示，以及网页转发的信任、实际访问、重试和取消流程。记录在 `build/logs/test-20261118-212617-3337002202.log`。48 张离屏快照涵盖明暗、中英文及两档宽度，存于 `build/review/tunnel-no-advanced-20261118/native/`，记录在 `build/logs/test-20261118-212436-47413861.log`。全量检查通过，见 `build/logs/check-20261118-212632-2409257573.log`。
- SmallPen 修订 `6647fdbf97ef32aa9b437029c36fa42b4631c99abae17c1d4c550666b1c71bf7`，结构检查零错误和警告，五个受影响状态明暗渲染在各自最近一次改动后均零报告问题。少量图标对比度检查跳过。预览在 `build/review/tunnel-no-advanced-20261118/smallpen/`。
- macOS arm64 候选包通过深层严格签名检查，见 `build/logs/tunnel-no-advanced-build-20261118.log`。两个生成文件均为最新。正式安装仍是上一版。

## 2026-11-18 · 粘贴 SSH 私钥与移除转发中转

连接表单可从剪贴板导入完整私钥，沿用文件导入的本地解析、口令验证和加密保存。原文留在凭据草稿中，界面仅显示导入状态和公钥指纹。点击转发图中转节点上的关闭按钮后，本次改为直连并显示加号，服务器保存的线路保持原样。

- 合成密钥回归覆盖粘贴普通和加密私钥、错误口令、未拨号的保存、重启后登录、加密文件和审计无原文，以及中转移除。记录在 `build/logs/test-20261118-214521-55058551.log`。离屏快照见 `build/review/private-key-paste-20261118/native/` 和 `build/logs/test-20261118-214728-50013215.log`。全量检查通过，见 `build/logs/check-20261118-214645-1747343211.log`。
- SmallPen 修订 `c86aa5576fb7ac1c9119ec6c722c4908cca6ead883ac90bfc4748b45bf768b6e`。结构和明暗渲染零报告问题。部分图标对比度检查跳过。预览位于 `build/review/private-key-paste-20261118/`。
- macOS arm64 已打包并通过深层严格签名检查，见 `build/logs/private-key-paste-build-20261118.log`。本轮覆盖本机离屏与合成 SSH 测试，正式安装留在上一版。

## 2026-11-18 · 安装私钥粘贴版

确认正式后台没有已建立的 TCP 连接后，正常退出正式 App，将候选包安装到 `/Applications/SmallSSH.app`。安装后三个可执行文件与候选包匹配，深层严格签名通过。CLI 和后台文件内容及文件节点一致，后台 PID 26748 持续运行，安装后通过本机身份与 TLS 握手，状态为锁定。

安装记录在 `build/logs/install-20261118-215029-2064164279.log`，安装前后后台检查分别见 `build/logs/service-20261118-214959-4026060711.log` 和 `build/logs/service-20261118-215036-132048237.log`。桌面留待用户重新打开。

## 2026-11-18 · 智能卡与 SmallSSH 密钥引导

硬件登录改名为「智能卡或安全令牌」。可选已安装的 OpenSC 或用文件选择器选择其他 PKCS#11 驱动，按名称选设备和密钥。选择设备后才提供 PIN，换设备会清掉上一台的 PIN 和密钥选择。SmallSSH 密钥可在连接内新建、选择签名验证方式及复制公钥，复用设置中的生成逻辑。

- 离屏测试覆盖中英文认证切换、PIN 的出现时机、换设备时清理选择、表单内新建入口、系统验证默认值和复制公钥。原生快照见 `build/review/key-guidance-20261118/native/`，测试记录为 `build/logs/test-20261118-222913-3815957869.log`。全量检查通过，见 `build/logs/check-20261118-222913-2613871652.log`。实物智能卡和 Secure Enclave 签名仍需实机验收。
- SmallPen 修订 `518b9526da0653ef12872b9bbebad48c00b0b01dcdea5fc8ae1bb01c9682818a`，结构与两种状态的明暗渲染零报告问题。部分自动图标对比度检查跳过。预览在 `build/review/key-guidance-20261118/`。
- 本机 macOS arm64 已打包并通过深层严格签名检查，见 `build/logs/key-guidance-signed-20261118.log`。本机安装仍是上一版。

## 2026-11-18 · 系统 PIV 智能卡与 YubiKey 驱动

macOS 连接表单的「读取系统智能卡」使用内置的 CryptoTokenKit / Security 助手。读取时关闭交互，只枚举系统 PIV 令牌的可签名 RSA 或 ECDSA 密钥。连接按令牌标识、密钥标识及 SSH 指纹匹配，PIN 交给 macOS 处理。助手按构建摘要校验，每次连接开始后由硬件工作进程处理签名。驱动菜单另列各平台标准安装路径中找到的 YubiKey YKCS11 和 OpenSC。

- Apple 芯片候选包通过深层签名及助手摘要核验，见 `build/logs/mac-smart-card-arm64-final-20261118.log`。打包助手的无卡枚举、无已选密钥时拒绝签名和合成公钥指纹回归通过，见 `build/logs/test-20261118-224818-2134304854.log`。全量检查通过，见 `build/logs/check-20261118-224756-1435076877.log`。实物智能卡的 PIN、触摸与服务器登录待验证。
- Linux amd64 和 Windows amd64 编译通过，见 `build/logs/compile-20261118-224457-2067331148.log`。Intel 助手完成编译和签名，完整 App 两次被现有 Ghostty 运行库下载超时阻塞，见 `build/logs/mac-smart-card-intel-retry-20261118.log`。两架构生成文件校验通过，见 `build/logs/mac-smart-card-generated-check-20261118.log`。
- SmallPen 修订 `066282ffe030bb1b1c08cbd3d0fd471eaa2ffa5ff2b3c675349238d5c31ed1b1`，结构和硬件登录页明暗渲染零报告问题，部分图标对比度检查跳过。预览见 `build/review/mac-smart-card-20261118/hardware.png`。本机正式安装仍是上一版。

## 2026-11-18 · Go 硬件进程读取 macOS 智能卡

macOS 智能卡改为现有 Go 硬件工作进程通过 purego 调用 CryptoTokenKit 与 Security，移除单独的 Swift 助手和其构建摘要。沿用原来的智能卡标识、公钥指纹和系统 PIN 流程。Windows 和 Linux 使用原有 PKCS#11 方式。

- Apple 芯片 App 通过深层签名，包内无 `smart-card-helper`，已打包硬件进程能无交互枚举。见 `build/logs/smart-card-go-build-20261118.log` 与 `build/logs/smart-card-go-packaged-worker-20261118.json`。Security 框架的合成 ECDSA 签名、无卡枚举和未选卡拒绝签名测试通过，见 `build/logs/test-20261118-225658-3473335342.log`。全量检查通过，见 `build/logs/check-20261118-225732-3159585893.log`。本轮未接实物智能卡。
- 移除的助手为 141856 字节，三个 Go 程序合计增加 140096 字节，净少 1760 字节。本机磁盘占用由 60520 KiB 变为 60516 KiB。测量见 `build/logs/smart-card-swift-size-20261118.log` 与 `build/logs/smart-card-go-size-20261118.log`。
- Intel Mac、Linux 和 Windows 编译通过，见 `build/logs/compile-20261118-225827-926826152.log` 与 `build/logs/compile-20261118-225732-2192814515.log`。两架构生成文件检查通过，见 `build/logs/smart-card-go-generated-20261118.log`。正式安装仍是上一版。

## 2026-11-18 · 安装 Go 智能卡正式版

确认正式后台没有 TCP 连接或转发监听后，正常退出桌面并确认后台锁定，随后正常停止旧后台。用已核验的 Apple 芯片包替换 `/Applications/SmallSSH.app`。安装后三个可执行文件均与候选包一致，深层严格签名通过。短暂启动新版正式后台并通过身份和 TLS 握手，安装包里的 Go 智能卡进程完成无卡读取。测试后台已正常退出，桌面留待用户重新打开。

安装与验证记录见 `build/logs/install-20261118-232542-747260782.log`、`build/logs/service-20261118-232541-1788301540.log`、`build/logs/service-20261118-232616-2327710788.log` 和 `build/logs/smart-card-go-installed-worker-20261118.json`。

## 2026-11-19 · 系统选择器与外部拖放上传

远程文件窗格的上传工具栏和更多菜单调用系统文件选择器，可多选。选择后直接加入传输队列，目标与线路取自打开选择器时的远程窗格。文件列表、文件夹行和目录树接收外部文件拖放，空白处使用当前目录。普通文件校验沿用现有传输队列及无覆盖提交。

- 离屏测试覆盖多选和取消、选择器打开期间换窗格、落到文件夹行或空目录、保留精确目标和线路，以及 App 内的拖放。见 `build/logs/test-20261119-001740-3746783454.log`。文件面板离屏快照见 `build/review/file-upload-native-20261119/native/` 与 `build/logs/test-20261119-002131-2321646130.log`。全量检查通过，见 `build/logs/check-20261119-001915-777145485.log`。
- SmallPen 修订 `a6b0a6d107fcc72564f6e641f7505b6f4427114f6791aea93e0b5f4436f178ee`，结构零错误和警告。文件标签明暗渲染零报告问题，右键稿的两处重叠报告是菜单覆盖列表，已目视核对。自动图标对比度检查部分跳过。
- macOS arm64 候选包通过深层签名，见 `build/logs/file-upload-native-build-20261119.log`。系统选择器回调和 Finder 式拖放由离屏测试模拟，本机正式安装仍是上一版。

## 2026-11-19 · 文件窗格内拖放移动

同一来源的文件和文件夹支持拖到列表中的文件夹、目录树和工具栏「上一级」。有效落点高亮，底部报告进度和结果。同名文件保留，批量移动的失败项留在原处。远程移动走当前窗格的 SFTP 线路和已有文件操作授权。

- 离屏测试以本机临时目录和隔离 SFTP 服务验证拖入、拖出、多选、同名冲突以及目录刷新后去重，并回归外部上传和跨窗格传输。日志见 `build/logs/test-20261119-010454-4085908630.log`，明暗两种外观和两种窗宽的离屏预览见 `build/review/file-move-20261119/native/`。
- SmallPen 结构检查通过，文件标签在明暗组合下均无布局报告问题。部分图标对比度由渲染器跳过。修订为 `a6b0a6d107fcc72564f6e641f7505b6f4427114f6791aea93e0b5f4436f178ee`，报告见 `build/review/file-move-20261119/smallpen/`。
- 全量检查通过，见 `build/logs/check-20261119-010454-2541590269.log`。macOS arm64 候选包已构建并通过深层签名，见 `build/logs/file-move-build-20261119.log` 与 `build/logs/file-move-sign-20261119.log`。Linux 与 Windows 通过交叉编译，见 `build/logs/compile-20261119-010519-3562734463.log`，本轮的交互验证在 macOS 离屏运行。

## 2026-11-19 · 文件表头排序与上传覆盖确认

名称、大小和修改时间表头可切换升降序，窄窗的修改时间排序位于更多菜单。选中项和右键目标按文件名保留。所有者上传遇到同名普通文件时可跳过或逐项确认覆盖。覆盖绑定原文件摘要、连接身份和传输任务，使用已有原子替换并保留旧副本，需服务器支持相应 SFTP 扩展。

- 离屏测试覆盖表头与窄窗菜单、排序后的选中和右键、双击打开目录，并回归系统选择器和拖放。隔离 SFTP 测试覆盖跳过、确认覆盖、旧副本与确认期间远端变更。日志见 `build/logs/test-20261119-013026-3056423170.log`，明暗截图见 `build/review/file-sort-overwrite-20261119/native/`。
- SmallPen 修订 `af9f92ef6ce0688369bd640471198ca4af850902e0a24b606be1824ce521727e` 结构检查通过。文件标签和覆盖对话框在明暗组合下均无布局报告问题。部分文件标签图标对比度由渲染器跳过。证据见 `build/review/file-sort-overwrite-20261119/smallpen/`。
- 全量检查通过，见 `build/logs/check-20261119-013026-1959550885.log`。macOS arm64 候选包构建和深层签名通过，见 `build/logs/file-sort-overwrite-build-20261119.log` 和 `build/logs/file-sort-overwrite-sign-20261119.log`。Linux 和 Windows 通过交叉编译，见 `build/logs/compile-20261119-013104-1458521811.log`。实机安装尚未替换。

## 2026-11-19 · 连接与授权设计稿对齐 App

用本机 macOS 离屏 UI Tester 和当前桌面源码校对 25 张业务画板。连接采用实际的紧凑模态布局、固定分类和操作栏，补齐窄窗、登录方式及保留凭据等状态。授权中心复现当前授权的两组卡片，以及审计整宽表格与打开详情的两种布局。独立审批按 App 放置验证和决定按钮。同步 SmallPen 页面地图及源码目录。

- 离屏测试生成 183 张合成数据快照，涵盖连接、设置、线路与授权，记录在 `build/logs/test-20261119-133725-3063166218.log`。全量检查通过，见 `build/logs/check-20261119-133725-3529255996.log`。
- SmallPen 修订 `6716dd49753488650ee5e8f2b8def8c5996b221666ed1e2dd853180fd5c70526`，结构检查零错误和警告。25 张业务画板完成明暗渲染，并目视核对代表状态。组件化后完成各页渲染，最后一轮收窄私钥标签并更新页面地图，随后重查这四页。证据在 `build/review/connection-authorization-20261119/final/`。
- 复审保留了与 App 一致的模态覆盖及长表单首屏裁切。浅色连接分类选中文字约 4.0:1，选中审计记录的辅助文字约 4.18:1，低于 SmallPen 的 4.5:1 检查线。未选复选框和禁用测量控件也有对比度报告。
- [并排对照页](../build/review/connection-authorization-20261119/index.html) 已用无界面 Chrome 加载并目视核对。待确认设置页以独立授权页的离屏截图对照审批内容，设置外框依据当前源码和同窗体的其他实测状态。覆盖范围为本机的离屏界面和 SmallPen 明暗渲染，系统窗口装饰及真实审批交互需实机验证。

## 2026-11-19 · 连接分组箭头与密码图标对齐

服务器密码、私钥口令和连接专用代理密码稿使用 App 的闭眼斜线轮廓，固定 17 px 图标视口和 30 px 右侧按钮宽度。原稿的内联路径实际绘制宽约 22 px，App 离屏可见宽约 15 px。分组按钮的 `⌄` 是原生文本字形，其可见中心比字段中心低约 5.5 px，SmallPen 分组稿按这个实际位置绘制。

- 8 张相关画板完成明暗渲染，结构检查零错误和警告。明色连接宽窗使用既有合成数据离屏图逐像素对照，细部可见边界仍有约 1–2 px 的渲染差异。修订 `db698446c4f6081f620b8dc5a6ac4947789b86d391d0deb5a168511bd2eb8134`，报告和放大对照存于 `build/review/connection-field-icons-20261119/`，并排预览的对应图片已更新。
- 全量检查通过，记录在 `build/logs/check-20261119-142703-2434885384.log`。结论基于本机离屏图和 SmallPen 渲染。

## 2026-11-19 · 复核连接及授权控件细节

按 App 源码和本机离屏截图校对 24 张画板。统一连接分类的内边距、圆角与选中底色，设置侧栏间距、标签和按钮底色，以及独立审批摘要卡的留白。审计详情采用固定结果列、独立详情视口及与 App 一致的命令块和字段对齐。页面目录补记详情的滚动行为。

- 浅色对照测量中，连接分类的选中底色和纵向范围与 App 一致。设置侧栏选中项的起点从第 200 行调整到第 204 行。只读审批摘要卡的白色填充从 76 px 调整到 App 的 86 px。[测量记录](../build/review/connection-match-details-20261119/图层测量.json)
- SmallPen 修订 `0abc6be159d8b9b4cd98157ad9fa4842823c38eff4758660d2f57f2f58396e7f`，结构检查零错误和警告。24 张画板完成明暗渲染，最后一轮仅调整审计详情的行间距，并重新核对该页。保留两张长连接表单的滚动首屏裁切与原 App 配色对应的对比度报告。[逐页报告](../build/review/connection-match-details-20261119/)
- 覆盖范围为本机离屏图及 SmallPen 渲染。App 使用系统界面字体，设计包使用 Noto Sans SC 子集，字形、字宽及个别图标仍可能有细微差异。[更新后的对照页](../build/review/connection-authorization-20261119/index.html)
- 全量检查通过，见 `build/logs/check-20261119-145150-1609799261.log`。

## 2026-11-19 · 正式 SmallPen 稿作为 App 实现基准

本轮以仓库当前 SmallPen 正式包为工作基准，撤掉先前为了模仿旧 App 而加给分组箭头的偏移。App 的分组、线路和终端选项改用已有居中 SVG。App 通过 Go embed 读取正式包的 Token，连接窗和字段尺寸、基础配色、授权设置选中态、独立审批的主要尺寸及审计结果列直接使用对应值。开发规则补充修订、Spec 与同状态离屏验收要求。

- 分组箭头回归检查包含明暗两种 App 的实际笔画。浅色宽窗对照中，旧 App 的可见箭头偏低 6 px，更新 App 和 SmallPen 的箭头中心均与各自输入框中心重合。[测量记录](../build/review/design-to-app-20261119/分组箭头测量.json)
- SmallPen 修订 `0c74ef5341d04dcf7f257189f61c55521dc1f2986277e66248b20bf0fd685aad`，结构检查零错误和警告。12 张相关画板完成明暗渲染，11 个场景保存 App 与正式稿的并排预览。渲染报告仍记录已有的长表单滚动裁切及配色对比度。[并排验收页](../build/review/design-to-app-20261119/index.html)
- 本机离屏测试和快照记录见 `build/logs/test-20261119-152143-1625153003.log`。全量检查通过，见 `build/logs/check-20261119-152143-238701240.log`。对照用合成数据，App 系统字体与 SmallPen 的 Noto Sans SC 子集仍有字形差异。

## 2026-11-19 · 连接与授权高保真优化评审

按 Filipiuk 风格 Skill 在正式包新增「连接与授权 · 优化评审」画布。12 张画板包含连接与私钥、线路、终端、访问策略、授权待确认与空态、当前授权及审计。连接的字段按任务分组，审批突出服务器、权限和配对码，审计详情区分历史决策与当前规则。候选稿的状态、操作及 App 源码映射已加入页面目录。

- 对照基线为 `0c74ef5341d04dcf7f257189f61c55521dc1f2986277e66248b20bf0fd685aad`。SmallPen 差异为新增画板及链接，以及候选稿使用的分隔线和备注框两个尺寸 Token。候选修订为 `bb9f920625f4d2f0ce4e23a5a51ce693504d0fdc6136d31d2e197f6119dfa644`。
- 结构零错误和警告。12 张画板逐页检查明暗渲染，最后一轮调整登录菜单锚点后重查三个受影响状态。模态背景和遮罩的重叠为有意叠放，其他布局及对比度检查没有报告问题。目视核对了连接宽窄窗、已有私钥、授权空态、待确认、当前授权及审计详情。
- 两套离线预览各解析出 48 条链接。无界面 Chrome 在明暗两版实测连接分类、登录菜单锚点和私钥切换、授权标签及审计详情开关。原型数据为合成示例。[浅色预览](../build/review/filipiuk-connection-authorization/light-final/index.html) · [深色预览](../build/review/filipiuk-connection-authorization/dark-final/index.html)
- Go 全量检查通过，记录在 `build/logs/check-20261119-154441-2380409010.log`。本轮验收覆盖 SmallPen 渲染和离线原型导航。

## 2026-11-19 · 登录方式与加速和代理分支补审

对照 App 的连接编辑器和线路管理实现，补齐连接与授权评审画布。本轮设计覆盖五种 SSH 登录方式、SmallSSH 密钥签名策略、代理身份、macOS 系统智能卡，以及 OpenSC 和 YKCS11 从选择驱动、读取设备到输入 PIN 的状态。加速与代理重画连接内的直连、三种专用代理、SSH 跳板与握手结果，同时补共用线路库、手输代理、保留或替换密码及导入节点编辑。已同步页面目录与 App 源码映射。

- SmallPen 修订 `a1afde3d13dd1f4f34bcf715ef2c1707fc0a186f7f512c45ba3e06cec545cb8e`。57 张画板完成 114 次明暗渲染，无错误和警告。模态背景与遮罩有重叠提示，部分透明背景节点跳过自动对比度检测。已目视 SmallSSH 新建密钥、设备 PIN、握手结果和共用线路库。[逐页检查](../build/review/filipiuk-auth-routes/checks/) · [设计差异](../build/review/filipiuk-auth-routes/design-changes.json)
- 两套预览各解析 318 条链接，无未解析链接。无界面 Chrome 在明暗两版走查五种登录方式、设备 PIN、三种代理、跳板实测及共用线路导入和密码处理。原型使用合成设备、节点和测量结果。[浅色预览](../build/review/filipiuk-auth-routes/light/index.html) · [深色预览](../build/review/filipiuk-auth-routes/dark/index.html) · [点击验证](../build/review/filipiuk-auth-routes/browser-review.json)
- 本机 App 离屏检查通过并保存 55 张合成场景快照，记录在 `build/logs/test-20261119-155608-4227104311.log`。全量检查通过，记录在 `build/logs/check-20261119-155608-4226666356.log`。本轮交付是设计候选与本机离屏核对，硬件、系统认证和真实线路连接以 App 运行验收为准。[App 离屏快照](../build/review/filipiuk-auth-routes/app-snapshots/)

## 2026-11-19 · 线路导入协议悬停提示

在「新建线路 · 导入」候选稿的格式说明旁加圆形叹号。悬停后分别显示分享链接和节点 JSON 的支持范围，Snell 放在节点 JSON 下。页面目录映射到导入解析器。

- SmallPen 修订 `2893a70393fb67e49f6faf6c55532963dc42341698088f4f78f650ab407252eb`。结构零错误和警告。导入页和提示卡的四组明暗渲染完成，目视检查了两种主题下的浮层。无界面 Chrome 验证悬停打开、移开关闭，两套离线预览各解析 319 条链接。本轮核验覆盖设计稿与网页预览。[明暗交互记录](../build/review/filipiuk-import-protocols/browser-hover.json)
- Go 全量检查通过，见 `build/logs/check-20261119-162354-1578574227.log`。

## 2026-11-19 · 统一跳板机示例名

原稿、候选稿的 10 张画板及 App 离屏测试样例统一使用「跳板机」。路径图用「途经」标注中间节点，线路库去除重复名称。

- SmallPen 修订 `ec19d2631009f2ba53ed9d8b9058c0700ce0440f4e102315187a73b1382633ab`，结构零错误和警告。改动的画板完成 20 组明暗渲染，原稿「加速与代理」的对比度提示与改名前一致。两版离线预览均实际点击「跳板机」并跳到专用跳板状态。[浅色预览](../build/review/relay-label-20261119/light/index.html) · [深色预览](../build/review/relay-label-20261119/dark/index.html)
- App 合成场景的离屏测试通过，生成 21 张快照，见 `build/logs/test-20261119-162841-3172454795.log`。Go 全量检查通过，见 `build/logs/check-20261119-162841-1065810478.log`。

## 2026-11-19 · 硬件密钥登录名称

App 和 SmallPen 的登录方式统一为「硬件密钥」，英文为「Hardware key」。沿用原有的认证类型 `pkcs11`，已保存连接继续使用原配置。更新登录下拉、选中态、设计页面目录、原型链接和使用说明。

- SmallPen 修订 `2a0b05a70f99a2a69635d99090760ecc4758c1508a24a21c9e2baa8012195aa5`，结构零错误和警告。14 张改动画板完成 28 组明暗渲染，原稿「硬件认证」的两项既有对比度提示与改名前相同。Chrome 在明暗两版点击登录菜单、读取系统智能卡和更换设备返回。网页原型采用合成状态。[交互记录](../build/review/hardware-key-label-20261119/browser-review.json)
- App 在本机离屏生成了宽窗、560 px 窄窗的明暗快照，见 `build/logs/test-20261119-163634-2512635696.log`。Go 全量检查通过，包含中英文登录方式切换与 PIN 流程回归，见 `build/logs/check-20261119-163634-4283918157.log`。[窄窗快照](../build/snapshots/hardware-key-label-20261119/connection-hardware-narrow-light.png)

## 2026-11-19 · 连接主流程、审批边界与保活补审

在「连接与授权 · 优化评审」补齐连接保存、编辑凭据、首次核对服务器身份、指纹变化、终端连接失败和重试。独立审批按授权范围与验证方式展示服务器操作、维护准备和单次操作，增加验证失败后的主密码入口、无有效授权与到期记录。SSH 密钥签名展示来源、目标、指纹以及单次和批量审阅。保活默认关闭，开启后分别写明探测频率与单次等待时长。

- SmallPen 修订 `677c3163d94959eae48fd303552c6c3e831e60675f5a2541b856106e1290d0fb`。新增 29 张画板并调整 2 张，31 张完成明暗渲染。初稿主密码回退弹窗的底部操作裁切和未选复选框对比度已修复，按最终修订重查这两页。结构零错误和警告。其余报告记录模态背景的故意重叠和部分节点的对比度跳过。[逐页检查](../build/review/filipiuk-flow-review-20261119/checks/)
- 两个离线预览各解析 436 条链接，无未解析链接。无界面 Chrome 在明暗两版点验保存与编辑、首次核对和取消、进终端、失败重试、指纹变化、独立审批主密码回退、授权到期记录、签名审阅和保活切换。网页使用单击代表从服务器列表双击打开，连接、系统验证和授权结果均为合成状态。[浅色交互预览](../build/review/filipiuk-flow-review-20261119/light/index.html) · [深色交互预览](../build/review/filipiuk-flow-review-20261119/dark/index.html) · [点击记录](../build/review/filipiuk-flow-review-20261119/browser-review.json)
- App 本机离屏参考包括连接表单、首次指纹核对、保活和独立审批，见 `build/logs/test-20261119-165513-2196251705.log`。Go 全量检查通过，见 `build/logs/check-20261119-165459-1460860585.log`。[App 离屏参考](../build/snapshots/filipiuk-flow-review-20261119/)

## 2026-11-19 · macOS 审批保留 Touch ID 指纹入口

对照 App 的 40 × 40 按钮和原生验证视图，候选稿把独立审批的文字入口换回可见指纹。普通审批、维护准备和单次操作均有等待验证的状态。临时失败时展开主密码，保留系统验证重试入口。另画系统验证未启用或不可用时的主密码状态。原生窗口可见性、聚焦和空间的检查来自 App 的 macOS 实现。

- SmallPen 修订 `4ee4b2b93cc57e7b341844ddfd8eef2aa49c67b384c4e4c65420317c9836df3f`。10 张画板和指纹组件完成 22 组明暗渲染。结构零错误、零警告，最终渲染无警告。主密码回退页初稿出现底部裁切，调整后复查。指纹区为 40 × 40，独立审批切换时横向坐标相同，竖向差最大约 1 px。[渲染及尺寸记录](../build/review/filipiuk-touch-id-20261119/final-checks/)
- 无界面 Chrome 走查明暗两版的验证中、失败回退、仅主密码、维护准备和本次操作。每版 447 条链接均解析。[浅色预览](../build/review/filipiuk-touch-id-20261119/light-final/index.html) · [深色预览](../build/review/filipiuk-touch-id-20261119/dark-final/index.html) · [交互记录](../build/review/filipiuk-touch-id-20261119/browser-review.json)
- App 无窗口测试核验自动验证等待可用位置、切换和输入主密码时取消正在进行的验证，见 `build/logs/test-20261119-171937-1966475870.log`。Go 全量检查通过，见 `build/logs/check-20261119-171937-3994816301.log`。本轮证据覆盖设计原型和模拟验证流程。

## 2026-11-19 · SSH 密钥使用授权

连接与授权评审稿借鉴 1Password SSH Agent 先展示调用应用和密钥的做法。示例写明是 SSH 登录、使用哪个账号，以及允许当前一条还是同组两条请求。请求附带主机指纹时可展开核对。主机未知时明确说明，并显示本机密钥指纹。按应用记住许可属于 1Password 的授权方式，SmallSSH 的限时许可作用于整把密钥。[1Password SSH Agent 授权说明](https://www.1password.dev/ssh/agent/authorization)

- SmallPen 修订 `7d763fbfdf40b0ef2e37e4e8097762cca21fbfb7e81371aa565b53a91de3999d`。七张画板通过结构和 14 组明暗渲染，零错误、零警告。逐页记录保留了个别透明按钮文字的自动对比度跳过项，已在明暗 PNG 中核对。[渲染检查](../build/review/ssh-key-use-20261119/checks/)
- 无界面 Chrome 在两套主题验证了指纹详情、单条与整组批准和拒绝、主机未知分支。每版 95 张画板，455 条链接均可解析。Touch ID 热区为 40 × 40。网页使用合成请求和结果，真实批准由 macOS 系统验证执行，拒绝结果返回调用方，成功通知由系统通知设置决定。[浅色预览](../build/review/ssh-key-use-20261119/light-final/index.html#page-10) · [深色预览](../build/review/ssh-key-use-20261119/dark-final/index.html#page-10) · [交互记录](../build/review/ssh-key-use-20261119/browser-review.json)
- 本轮交付 SmallPen 评审稿。全量 Go 检查通过，包含 SSH Agent 测试。[检查日志](../build/logs/check-20261119-173511-3616632768.log)

## 2026-11-19 · 共用认证底部操作

App 和 SmallPen 的保险库解锁、独立审批、授权中心共用「使用主密码」和系统验证入口。点指纹启动或重试系统验证；识别失败保留选项；系统不可用、锁定或验证视图未能就绪时展开保险库主密码输入。主动选密码可中止正在进行的验证并开始输入。SSH 密钥的「系统验证」允许由 macOS 接收登录密码，「仅当前指纹」仍走仅生物识别策略。参考 [1Password 的 Touch ID 解锁行为](https://support.1password.com/touch-id-mac/) 和 [Apple 的系统密码回退说明](https://developer.apple.com/documentation/localauthentication/LAPolicy/deviceOwnerAuthentication)。

- macOS 隐藏窗口测得小尺寸原生视图为 32 × 32 pt。验证了 40 × 40 pt 宿主空间内的居中、24 pt 区域和裁切区域的提前拒绝，QA 未执行系统验证。两种 macOS 架构的原生组件哈希已通过生成文件校验。[原生 QA](../build/logs/identity-qa-20261119-175711-398642226.log)
- App 离屏测试覆盖了点指纹保持系统验证路径、点主密码展开并聚焦、取消验证、输入主密码及失败回退。380 pt 宽的英文审批窗口用「Use Password」，认证和决定按钮在同一行，指纹区为 40 × 40 pt。[离屏快照](../build/snapshots/shared-auth-footer/) · [UI 测试日志](../build/logs/test-20261119-180419-1657108844.log)
- SmallPen 修订 `984f6201e163224b93693e7ecdb369328e5b27cdc65a3f74323644b7490a59ee`。21 张画板与 5 个组件变体共 52 组明暗渲染，结构零错误和警告，渲染零警告。无界面 Chrome 验证两个主题里的主密码切换、返回 Touch ID 和系统登录密码示意，每版 460 条链接均解析。系统密码窗口使用合成原型，实际输入和校验由 macOS 接管。[浅色预览](../build/review/shared-auth-footer/light/index.html) · [深色预览](../build/review/shared-auth-footer/dark/index.html) · [交互记录](../build/review/shared-auth-footer/browser-review.json)
- Go 全量检查通过。[检查日志](../build/logs/check-20261119-180504-4030356089.log)

## 2026-11-19 · 跨平台认证入口

保险库解锁与审批共用「使用主密码」和当前平台的系统入口。macOS 保留原生 Touch ID 位置。Windows 明示「Windows Hello」，由系统选择可用的 PIN、人脸或指纹；取消系统弹窗回到原来的选择。Linux 明示「指纹验证」，读取时显示操作提示，指纹未匹配可以重试。系统验证未配置或不可用时直接显示保险库主密码。电脑登录密码交由操作系统，Mac 系统密码示例来自 SSH 密钥的系统验证策略。[Windows Hello](https://learn.microsoft.com/en-us/windows/apps/develop/security/windows-hello) · [fprintd](https://fprint.freedesktop.org/fprintd-dev/Device.html)

- SmallPen 修订 `25ffcca802f52acb22bf095bb8542e3dab4b5401305b68ac61254b710e2fec55`。共用系统入口有三种变体，单张「跨平台认证」对照稿展示各平台入口和主密码回退。19 张画板与七个组件变体完成 52 组明暗渲染，零错误、零警告。无界面 Chrome 走查 Mac 系统密码示例和保险库主密码路径，每版 461 条链接均解析。[跨平台对照稿](../build/review/platform-authentication/images/platforms-light.png) · [浅色预览](../build/review/platform-authentication/light/index.html#page-72) · [深色预览](../build/review/platform-authentication/dark/index.html#page-72) · [交互记录](../build/review/platform-authentication/browser-review.json)
- App 离屏测试覆盖 Windows、Linux 状态下的窄窗审批、保险库解锁和主密码切换，以及 Linux 指纹未匹配后重试。Windows amd64、Linux amd64 完整桌面目标跨编译通过。Windows Hello 和 Linux 指纹硬件验证使用对应平台和设备，本轮离屏测试只使用合成状态。[UI 日志](../build/logs/test-20261119-181414-960176196.log) · [跨编译日志](../build/logs/compile-20261119-181153-3209080237.log)
- Go 全量检查通过。[检查日志](../build/logs/check-20261119-181446-2991532326.log)

## 2026-11-19 · 认证底栏的取消与拒绝

保险库验证中提供「取消」，停止验证并留在锁定页。审批验证中并列「取消」与「拒绝」。取消保留待处理申请并阻止当前尝试重新自动发起，拒绝提交明确决定。macOS 的 SSH 密钥使用请求使用「拒绝」，用户主动开启密钥限时许可时使用「取消」。

- SmallPen 修订 `fd83a0197de4b008409875d47fb71e1087da1acbc4949e8f5c1206ccbaf61f79`。六张画板完成 12 组明暗渲染，结构与渲染均零错误、零警告。无界面 Chrome 走查服务器操作、维护准备及单次操作，取消都回到对应待处理页面，拒绝进入无待处理请求状态。每版 465 条链接均解析。[审批预览](../build/review/auth-cancel-deny/light/index.html) · [交互记录](../build/review/auth-cancel-deny/browser-review.json)
- App 离屏测试确认取消后保险库保持锁定，审批未提交决定，延迟返回的成功结果无法批准请求。另测了仍可一键拒绝。macOS 原生 SSH 授权 QA 和生成文件检查通过，Windows、Linux 桌面目标通过跨编译。本轮使用离屏测试与合成输入，未调用系统验证弹窗。[UI 日志](../build/logs/test-20261119-181955-1844344228.log) · [原生 QA](../build/logs/identity-qa-20261119-181955-1226193776.log) · [跨编译](../build/logs/compile-20261119-182236-351198145.log)
- Go 全量检查通过。[检查日志](../build/logs/check-20261119-182105-3301057476.log)

## 2026-11-19 · 收紧认证底栏并恢复配对码强调

审批的系统验证入口承担批准动作，右侧只与「拒绝」配对，左侧留「使用主密码」。验证中沿用这个位置，由 Touch ID 控件自身显示状态。解锁中止使用「取消」。主密码展开后提供对应的提交按钮。SSH 密钥的网页示意沿用同一布局，密码由 macOS 系统窗口接收。[1Password SSH Agent 的批准方式](https://www.1password.dev/ssh/agent/authorization)

根据早期授权中心评审图恢复配对码的独立淡紫底色、细边框和加粗数值。授权中心在审批操作前固定显示一次，独立弹窗用紧凑排列保留核对区。App 使用设计稿的颜色与数字字号 Token。[早期评审图](../build/review/filipiuk-connection-authorization/待确认授权.png) · [当前授权中心](../build/review/compact-authorization/final-images/pending-center-light.png) · [当前独立弹窗](../build/snapshots/compact-authorization/approval-full.png)

- SmallPen 修订 `81ae74b19fd13e8f82d5a9e1c561975317b389d5c8bc8585a2ee5be5a60d63b0`。23 张画板、八个组件变体完成 62 组明暗渲染，最终范围零错误和警告。维护准备主密码页初稿底栏超出 400 pt 窗口，压紧授权范围的内容间距后复查通过。[明暗渲染检查](../build/review/compact-authorization/final-checks/)
- 两套交互预览各有 99 张画板，469 条链接均解析。无界面 Chrome 验证独立审批、维护准备和单次操作进入主密码、返回指纹、拒绝，以及 SSH 密钥的原生密码示例。[浅色预览](../build/review/compact-authorization/light-final/index.html) · [深色预览](../build/review/compact-authorization/dark-final/index.html) · [交互记录](../build/review/compact-authorization/browser-review.json)
- App 离屏测试覆盖系统模式仅显示一个明显的退出操作、切换主密码、Esc 停止审批验证、配对码在 380 pt 窗口的可见性及原生指纹区的 40 × 40 pt 尺寸。Linux 和 Windows 桌面目标完成跨编译。本轮验证使用合成申请与离屏控件，未发起实际系统验证。[UI 测试日志](../build/logs/test-20261119-190132-3266814948.log) · [跨编译日志](../build/logs/compile-20261119-190248-2636217604.log)
- Go 全量检查通过。[检查日志](../build/logs/check-20261119-190725-2885603064.log)

## 2026-11-19 · 关闭中风险文件确认的提示

待确认审批和当前授权共用开关及提示。关闭时，在开关正下方说明符合条件的文件操作可在授权范围内跳过逐项确认，未知命令仍需确认，高风险操作仍拒绝。开启后收起提示。当前授权按后台回传的规则显示；审计中的历史记录保持只读。

- SmallPen 修订 `4c4b997885d8a813ec544075e0f121c355c88db66829820425a6e119cb66e99e`。六张画板和共用提示组件完成 14 组明暗渲染，结构及渲染零错误、零警告。目视核对弹窗和当前授权的提示位置。[审批弹窗](../build/review/medium-review-notice/images/dialog-off-light.png) · [当前授权](../build/review/medium-review-notice/images/active-off-dark.png)
- 两套交互预览各有 101 张画板和 478 条已解析链接。无界面 Chrome 验证独立审批与待确认授权的开关往返，并核对关闭画板仍显示配对码。网页展示开关状态，已生效授权的保存和回传由 App 测试验证。[浅色预览](../build/review/medium-review-notice/light/index.html) · [深色预览](../build/review/medium-review-notice/dark/index.html) · [交互记录](../build/review/medium-review-notice/browser-review.json)
- App 离屏测试验证 380 pt 窗口内的提示位置、重新开启时收起提示，以及切换授权时的草稿隔离。当前授权与审计详情的开关测试通过本机隔离后台完成，使用合成授权及操作。[UI 测试](../build/logs/test-20261119-191247-2604775791.log) · [后台联动测试](../build/logs/test-20261119-191348-3591886493.log) · [全量检查](../build/logs/check-20261119-191247-3468203020.log)

## 2026-11-19 · 区分保险库主密码和 macOS 密钥验证

保险库解锁和服务器操作审批的「使用主密码」指 SmallSSH 保险库主密码。SSH 密钥的验证选项同步写成「macOS 验证（指纹或 Mac 登录密码）」，登录密码由 macOS 系统窗口接收。设计稿移除了模拟登录密码入口和输入画板。[Apple 系统验证说明](https://developer.apple.com/documentation/localauthentication/lapolicy/deviceownerauthentication)

- SmallPen 修订 `61814ba5740569c7121a08fba374ead85b9f4aac22f5332b4f8ee356293b8df1`，结构零错误、零警告。八张画板完成 16 组明暗渲染。正式连接表单仍有此前记录的两项滚动视口溢出和两项浅色对比度警告，其余画板无警告。[检查报告](../build/review/ssh-password-wording/checks/) · [密钥审批稿](../build/review/ssh-password-wording/images/key-group-light-final.png) · [密钥选项](../build/review/ssh-password-wording/images/signing-choice-dark-final.png)
- 浅色离线预览的 472 条链接均解析。无界面 Chrome 验证同组密钥拒绝、单次密钥指纹确认和独立审批进入主密码。离屏测试核对 App 的密钥选项文案及保存的验证策略，未打开真实系统验证窗口。Go 全量检查通过。[交互预览](../build/review/ssh-password-wording/light-final/index.html#page-10) · [交互记录](../build/review/ssh-password-wording/browser-review.json) · [UI 测试](../build/logs/test-20261119-193428-2177514277.log) · [全量检查](../build/logs/check-20261119-193428-931403800.log)

## 2026-11-19 · 在 App 中实现 SSH 密钥申请核对

按已确认的 SmallPen 修订 `61814ba5740569c7121a08fba374ead85b9f4aac22f5332b4f8ee356293b8df1` 实现请求列表、单条及整组的确认窗口、主机未知状态和指纹详情。App 显示调用应用及签名数据中的用途、登录账号和主机指纹，拒绝结果返回调用方。签名验证菜单使用稿件的行高、内边距和宽度，并保留键盘高亮。代理构建复用设计包的字体、图标和尺寸配色 Token。保险库解锁和服务器操作审批的密码入口使用 SmallSSH 主密码。

- 六张设计画板完成明暗两套渲染，无检查问题。App 用相同状态和逻辑尺寸比对了 12 张离屏图。指纹详情的 SmallPen PNG 向上取整时多出一行像素，逻辑尺寸仍为 404 × 186。[设计 Spec 与对照图](../build/review/ssh-authorization-implementation/) · [App 离屏图](../build/snapshots/ssh-key-authorization/) · [App UI 测试](../build/logs/test-20261119-202855-3686490570.log)
- 本机离屏测得 32 × 32 pt 的原生指纹控件在 40 × 40 pt 宿主内居中。验证使用合成请求，未调用真实指纹、Mac 登录密码或系统通知。Go 全量检查通过，macOS arm64 与 amd64 的原生组件指纹及许可证生成文件校验通过。[原生 QA](../build/logs/identity-qa-20261119-203319-2482615382.log) · [全量检查](../build/logs/check-20261119-203054-360053577.log)

## 2026-11-19 · 统一授权验证样式

在 SmallPen 中建立共用的「授权验证底栏」。SSH 密钥、服务器操作、维护准备、会话内操作和授权中心按同一顺序显示拒绝与确认。指纹等待时保留 40 × 40 pt 的系统入口，支持切换的请求可以从左侧改用主密码。主密码在底栏上方输入，未填写时「允许」不可用。对照页并排展示上述状态和 macOS 登录密码的系统弹窗示意。

- SmallPen 修订 `d7e1361e2211107d909fc6562a71672e1c3efb3d15287191cd88e1d84a65ba20`。26 张画板和 24 个组件变体完成明暗共 100 组渲染，结构和渲染均零错误、零警告。核对了拒绝按钮在待验证与等待指纹间的位置，以及 Windows Hello、Linux 指纹和仅主密码状态。[明暗图](../build/review/authorization-footer/images/) · [逐项检查](../build/review/authorization-footer/checks/)
- 浅色可点稿共 104 页，491 条链接全部解析。无界面 Chrome 核对了 SSH 指纹等待与拒绝、服务器审批切换验证方式、空密码到模拟填写后允许，以及中风险开关往返。本轮验证范围是设计原型及合成状态。Go 全量检查通过。[可点稿](../build/review/authorization-footer/light-final/index.html#page-36) · [交互记录](../build/review/authorization-footer/browser-review.json) · [全量检查](../build/logs/check-20261119-213152-1293258497.log)

## 2026-11-19 · 精简授权指纹等待状态

共用底栏的 macOS 等待状态由最右侧的指纹图标呈现。服务器操作授权点击图标即发起认证。「拒绝」和指纹区域在待验证与等待指纹之间保持原位。Windows 和 Linux 的等待文案随对应变体显示。

- SmallPen 修订 `1ebb191d928abc64b6502240d62270941b2952bd83babe755feb3569099cde9f`。五张画板和 24 个组件变体完成明暗共 58 组渲染，结构及渲染无错误和警告。无界面 Chrome 核对从最右侧指纹一次进入等待、等待中直接拒绝，491 条原型链接全部解析。[对照图](../build/review/authorization-touchid/overview-light.png) · [可点稿](../build/review/authorization-touchid/preview/index.html#page-48) · [交互记录](../build/review/authorization-touchid/browser-review.json)
- App 离屏复核覆盖已有系统验证入口和切换行为。快照使用合成状态，真实 Touch ID 由系统控件呈现。Go 全量检查通过。[App 离屏测试](../build/logs/test-20261119-220632-3764697231.log) · [全量检查](../build/logs/check-20261119-220632-415138087.log)

## 2026-11-19 · 在主密码框右端直接切换验证

App 和 SmallPen 的共享审批底栏把系统验证按钮放到主密码框右端，显隐按钮在它左侧。点击后清空当前密码并原位等待系统验证，用户仍可拒绝。在 macOS 等待指纹时重新输入密码会结束当前指纹验证。仅支持主密码的状态只显示密码框及决定按钮。

- SmallPen 修订 `2fd922f902e0dcb6e807d8145eeddcf1f8580efcd3bf3878117538671ee76028`。11 张画板和 30 个变体完成明暗共 82 组渲染，结构及渲染零错误、零警告。无界面 Chrome 核对空密码和已输入密码时直接切到指纹，及等待后拒绝或改回主密码的流程。495 条原型链接全部解析。[可点稿](../build/review/approval-password-fingerprint/preview/index.html#page-59) · [交互记录](../build/review/approval-password-fingerprint/browser-review.json)
- App 离屏核对了密码框内的眼睛和 40 × 40 pt 的指纹入口、切换时清空输入、Esc 结束验证以及仅主密码状态。420 × 400 同主题同状态的截图用于对照；快照使用合成状态，未调用真实 Touch ID。Go 全量检查通过。[App 离屏截图](../build/snapshots/approval-password-fingerprint/) · [针对性测试](../build/logs/test-20261119-222131-1255200592.log) · [全量检查](../build/logs/check-20261119-222303-2603325390.log)

## 2026-11-20 · 外部请求唤起保险库解锁

SmallSSH 桌面驻留时，遇到服务器操作或会话申请撞到锁定保险库，已验证的本地后台会留下短时提示，桌面恢复现有解锁页。提示按锁定代次清除并合并短时间内的重复请求。调用方解锁后重新申请。收到授权决定成功的回复后，桌面立即从弹窗队列略过已处理的请求，即使轮询仍返回旧的待处理快照。

- 本机 macOS arm64 签名候选包和合成数据目录验证了包内 CLI → `smallsshd` 的锁定状态和提示。离屏验证覆盖恢复窗口一次、忽略过期提示、隐藏开发实例和已处理的请求。系统验证的真实交互和其它平台的窗口行为未在本轮运行。[候选包](../build/darwin-arm64/SmallSSH.app) · [CLI 集成测试](../build/logs/test-20261120-000153-3220151287.log) · [离屏测试](../build/logs/test-20261120-000021-3733499099.log) · [解锁页截图](../build/snapshots/locked-request-prompt/)
- Go 全量检查通过，服务协议 1.3 的解锁提示字段完成序列化回归验证。[全量检查](../build/logs/check-20261120-000237-971292682.log)
