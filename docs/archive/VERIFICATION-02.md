# 验收记录 · 2026-11-08 至 2026-11-18

记录保留各轮当时的环境、结果与证据路径。最新验收见 [验收记录](../VERIFICATION.md)。

## 2026-11-08 — 文件本机并排、打开反馈与连续右键

文件工具栏新增显示器图标“显示本机文件”，一次在同一文件区并排本机与服务器；已有本机栏时复用并把键盘焦点交给其列表，保留远端及本机目录、缓存、选中项。现有画布、标签、分栏和传输入口继续使用原模型。文件打开在名称旁及底部展示转圈、成功/失败及显示文件窗口入口；进入未缓存目录时先切换目标位置并显示加载，旧目录不再可被误操作。目录尚未读出时不显示虚构的零计数。

文件列表使用非模态菜单解决原生菜单追踪时的外部右键吞事件，并在行渲染前同步选中项；连续右键可换目标，选中组内保持多选。打开、下载、跨栏传输、权限、删除、目录操作和跟随均共用原动作定义。菜单捕获并复核来源、选中项、目录序列、工作区身份及传输目标；过期菜单不执行。原生树和工具栏菜单保留，新增菜单支持方向键、Tab/Enter、Esc 和菜单外点击直达。

- 必需 gate：`build/logs/check-20261108-002544.log` 全部通过，含 vendor 补丁、575 个 Go 文件格式、vet 与全仓库测试。前一轮发现并修正了新按钮焦点回跳、目录树测试误点同名列表行、缺失翻译；最终树测试使用实际目录树与原生菜单。
- 集成：`build/logs/test-20261108-002544.log` 通过，覆盖画布/标签、工作区来源绑定、菜单、加载、本机栏复用和批量删除。针对性回归为 `test-20261108-001624.log`、`test-20261108-002523.log`；新增文本中英文齐备，`test-20261108-002422.log` 通过。加载与菜单来源约束分别经过独立只读 code review，无待修正高风险发现。
- 真实 Go UI 的离屏快照：`build/logs/test-20261108-002138.log` 通过；560/1100 DIP、明暗、并排/打开/失败/右键/目录读取共 20 张图位于 `build/snapshots/file-interactions-20261108/`，已抽查窄窗、本机显示器图标、加载与深色菜单。最后焦点修复仅改变交互状态，随后由 UI 点击测试验证。
- SmallPen 使用 CLI 保存并同步 pages.json：修订 `29e7d942dd8685781208ff998dc2cc9bc66fb46260d38b7fb7fec0e5d5dfff59`，66 页、11 个组件、23 个变体、39 个 Token；明暗各 89/89 目标完成，数据有效，无 warning；7 条有意叠放 info（6 条既有设置控制、1 条菜单覆盖列表），659 项跳过，coverage=false。完整报告：`build/logs/file-interactions-design-light-20261108.json` 及 dark 文件。
- 打包：`build/logs/build-file-interactions-20261108.log`；生成文件检查：`build/logs/build-file-interactions-check-20261108.log`，通过。已静默安装 macOS arm64 0.0.1 到 `/Applications/SmallSSH.app`，签名有效，安装日志 `build/logs/install-20261108-002739.log`；旧版保留在 `/Users/smallraw/.Trash/SmallSSH-before-20261108-002739.519607000.app`。

外部配置节点按要求调用后长时间停在探索，两次调用均未落码并已终止；过滤日志为 `build/logs/delegate-local-files-20261108.log` 与 `delegate-local-files-retry-20261108.log`。本机按钮改由已授权的 GPT 6.1 Terra worker 落码，主 agent 补齐焦点、翻译并统一验收；菜单实现单独委派，未读取或更改节点配置。

限制：本轮行为与截图证据是本机 headless UI Tester、合成服务器/文件夹具；没有真实服务器凭据或原生鼠标交互验收，没有自动启动、退出或激活正在使用的应用。菜单自己的区域仍接收自己的点击；已打开菜单外的可见行可以直接再右键。SmallPen 静态页面不等同于运行时交互或逐像素一致。构建/安装二进制指纹和本地提交记录于 `build/logs/file-interactions-20261108-summary.json`。


## 2026-11-08 — code review 第 1–6 项修复

针对 `origin/main..HEAD` 的 code review，修复六处行为问题：

1. 画布终端标题的“在下方打开文件”只改服务器窗格：新增 `serverFilePane`，固定的本机窗格即使最近用过也保持本机；只有当文件区仅剩一个固定本机窗格时，这一显式动作才会改它。
2. 加载未缓存目录时，占位区可获得焦点，并保留 Alt+Up 返回上一级。核实发现：MyGo 元素 id 按位置与类型分配，占位区与表格同位同类，焦点本来就会保留，审查所说“加载完成后列表失焦”并不成立；实际缺陷是加载期间按键无效。新增测试固定了“焦点跨加载保留，用户移走后不抢回”这一前提。
3. 已有画布“任务”卡片时，顶栏传输按钮从非工作区页面回到工作台；`showTool` 已有画布工具时同样漏设 `page = 1`，一并修正。
4. 恢复默认布局按当前呈现方式恢复：标签聚合保持原行为（合并标签、工具区在底部）；画布重新平铺全部卡片并隐藏底部 tab 工具区，与首次启动默认一致。“主工作区”的选择是单独设置，不被重置。
5. 保险库锁定或会话失效时，失败卡片重试先拒绝，保留错误原因和重试按钮，不再进入永远不会结束的“正在连接”。普通 shell 卡片原来也有同样问题，在 `retryPendingTerminal` 一处同时修复。
6. 画布内拆分容器终端时，占位卡直接使用最终的“容器 · 名称 (n)”唯一标题（新 helper `containerTerminalName`，与 `openContainerTerminal` 共用），连续拆分及连接后都不再重名。

- 必需 gate：`build/logs/check-20261108-094324.log`（`-serial`）全部通过。前一次并行运行 `build/logs/check-20261108-094147.log` 中 desktop 包已通过，唯一失败是未改动的 `internal/broker` `TestDelegatedClientEndToEndSSHOneApprovalAndCascadeRevoke`（EOF）；单独重跑通过（`build/logs/test-20261108-094316.log`），按负载下的时序偶发记录，未修改。
- 针对性回归：`build/logs/test-20261108-093922.log`；带 race 的终端集成（含新增容器重复拆分标题测试与既有容器拆分用例）：`build/logs/test-20261108-094118-race.log`。
- 反向验证：新增测试在未修改的 HEAD 副本（会话 scratchpad，不在仓库内）上运行，第 1、3、4、5、6 项及第 2 项的 Alt+Up 用例均失败，第 2 项的焦点用例按上述核实结果在 HEAD 上通过。

本轮未改变页面入口、状态或操作结构，SmallPen 与 `pages.json` 无需同步。证据均为本机 headless UI Tester 与合成夹具；未打开或激活应用窗口，未使用真实服务器凭据。


## 2026-11-11 — 首页留白、工具标签与文件管理布局修正

按用户本轮对首页的评审，对照 SmallPen 和正式 Go 界面检查顶栏、右侧功能开关、终端、底部工具区及文件管理器。确认设计稿中右侧32宽按钮放在36宽栏内且靠左，与顶栏错开4；工具标签和承载条同高，没有上下间距；文件计数放在整个树/列表下方且无安全内边距，目录树提前结束，表头与行未对齐。实际 Go 已有完整的目录树和表格，本轮修正设计表达，并同步实际终端及工具栏/底栏的间距。

- 设计源：更新26页，重整11个文件窗格。顶栏48、常规按钮32×32；右侧功能栏32宽与顶栏同轴。终端内边距左右16、上下6，清除示例命令间多余空行。工具条36、标签28，左右留8、上下留4；文件操作28，宽幅底部工具区336高。目录树填满内容高度，右侧表头与26高数据行对齐，状态栏独立28高并保留文字安全边距。新增工具标签、文件表头和文件状态栏三个共享组件，使用角色 Token，不添加内联 style 或检查屏蔽。
- Go 源码：通过正式 terminal.Padding 选项设置16/6，不改 vendor；工具标签条和文件工具栏左右留8；文件底栏最小高28、上下留4并垂直居中。生产快照和终端分组夹具使用相同终端 Padding。未修改业务行为，保留工作区已有的其他未提交修改。
- 映射与源审计：pages.json 同步选中/计数状态、布局约束及终端/工具条/文件行源码位置，DESIGN.md 更新组件数及当前评审记录。66个页面路径和映射有效，26条原型跳转、1个起点及4个字体SHA-256保持，原有 Token 仅文件行高从28改26，另增9个角色 Token。证据 `build/logs/home-layout-audit-20261111.json`；修改前副本 `build/authoring/home-layout-review-20261111/before.smallpen/`，清单 `build/logs/home-layout-edit-manifest-20261111.json`。

当前修订 `c76e8ac4b88e31191d2740ac1f3702670980be7c02dd653c84b502ce6bdc3776`，66页、14组件、28变体、71 Token。结构检查0 error、0 warning、0 unused Token（`build/logs/home-layout-check-20261111.json`）。明暗共94目标×2主题、188个组合全部完成，无失败、无剩余、无渲染 warning；仍有14条既有 possible_overlap info、669项跳过、coverage.complete=false，不视为完整视觉验收。渲染报告 `build/logs/home-layout-render-20261111.json`。

CLI 原生求解规格实测：首页顶栏按钮上下留8，顶栏设置与右侧文件按钮中心同为x1256；终端内容左右16/上下6；工具标签上下4/左8；树与右列表底边同为y852；底栏28高，计数文字距底边6、右边12；六行数据的大小/修改时间列均与表头一致。完整规格 `build/logs/home-layout-after-spec-20261111.json`，提取坐标 `build/logs/home-layout-geometry-20261111.json`，数值断言见上述 audit。已目视检查临时 CLI PNG：首页明暗、右侧窄栏浅色、六格浅色、本机/服务器并排浅色、文件大标签深色，无本轮目标的裁切或缺字。元数据为 `build/logs/home-layout-{after-light,home-dark,right-light,six-light,paired-light,files-dark}-20261111.json`，没有增加静态导出图库。

本机离屏与仓库验证：

- 使用 opt-in preview_qa / terminal_integration，通过现有 headless UI Tester 生成工作台、文件标签、混合画布、窄画布的明暗8张快照，并通过终端按键、Unicode及粘贴回归：`build/logs/test-20261111-205053.log`。快照位于 `build/snapshots/home-layout-after-20261111/`；修改前对照在 `build/snapshots/home-review-before-20261111/`。已目视检查实际工作台浅色、混合画布浅色和文件标签深色。
- 必需 gate `mise exec go@1.27.3 -- go run ./tools check -serial` 全部通过，含 vendor 完整性、576个Go文件格式、vet及全仓库测试。证据 `build/logs/check-20261111-205111.log`。
- 读取用户现有58766 Web / 58763 Background中的同一SmallSSH会话，已加载上述修订，66页、14组件、ready、readOnly=false、projectionErrors=[]。没有另起服务或切换其他项目，摘要 `build/logs/home-layout-live-summary-20261111.json`。

证据仅针对本机当前 CLI离屏渲染、Go headless夹具和现有服务读取结果；未自动启动、激活、安装或重启桌面App，未使用真实凭据，未完成浏览器点击或已安装App交互验收。本轮汇总 `build/logs/home-layout-summary-20261111.json`。


## 2026-11-11 — 工具区恢复图标停靠选择

用户要求工具区布局像原来的 Chrome DevTools 一样通过图标选择。核实正式 Go `internal/desktop/components/dock.go` 已实现紧凑的图标单选行（系统窗口、底部、右侧），含当前位置高亮、点击关闭菜单、方向键选择、Enter确认、Escape/外部点击退出与焦点处理；SmallPen“小窗布局菜单”仍是文字按钮，故本轮仅同步该设计页，不改 Go 源码。

菜单调整为256宽、内容自适应157高：“停靠侧”左置，三个32×32图标按钮右置、间隔4，底部选中状态高亮；下方按正式源码顺序保留放大工具面板、移到画布、收起面板和⌘⇧J。向量轮廓取自已有正式 Go 图标，按24到16 DIP缩放；不增改字体或引入依赖。新增菜单图标尺寸、菜单行高、停靠按钮圆角、菜单动作间距、菜单字体和向量线宽6个角色 Token。pages.json 同步图标选择/选中状态与 dock.go 源码位置，DESIGN.md 更新当前设计记录及77项 Token 数。

- 数据与保真：修改前修订为 `c76e8ac4b88e31191d2740ac1f3702670980be7c02dd653c84b502ce6bdc3776`，changes确认无外部更新后写入。86个包文件中仅本页及tokens.css改变，其余84个文件SHA-256和全部既有Token值保持，66个页面/源码映射有效，26条跳转及1个起点保留。菜单已有的底部、右侧、移到画布3条跳转保留；没有把设计示意视为系统窗口等真实功能。恢复副本在 `build/authoring/dock-icons-20261111/before.smallpen/`，原始摘要 `build/logs/dock-icons-before-20261111.json`，最终审计 `build/logs/dock-icons-audit-20261111.json`。
- 结构与渲染：最终修订 `73f268fe75212141e11c7bd5f1e32855a3bb1a6f6e4388c7dcd66cd4431b4337`，66页、14组件、28变体、77 Token，0 error、0 warning、0 unused Token。报告 `build/logs/dock-icons-check-final-20261111.json`。本页明暗2个目标组合完成，无失败、无剩余、无渲染问题或跳过项，scoped coverage.complete=true；报告 `build/logs/dock-icons-render-final-20261111.json`。其余未变范围继续引用前轮 `home-layout-render-20261111.json`，不称为当前修订全包重新渲染。初次检查拒绝SVG的fill/stroke-width/linecap/linejoin属性，已改用当前HTML契约的类与Token；初始失败记录仅为 `dock-icons-check-initial-full-20261111.json`，不作通过证据。
- 目视：逐张查看最终明暗CLI临时PNG，图标、选中底色、菜单行和快捷键完整。元数据 `build/logs/dock-icons-{light,dark}-final-20261111.json`；求解规格 `build/logs/dock-icons-spec-final-20261111.json`。另查看实际Go浅色菜单离屏快照；不保留静态导出图库。
- 原生交互：通过已有 headless UI Tester 的菜单紧凑布局、点击、键盘、焦点、关闭及独立窗口状态测试，并生成 light/dark/right/expanded/detached 5张离屏快照。命令为 `SSH_CLIENT_PREVIEW_QA=1 SSH_CLIENT_UI_SNAPSHOT_DIR="$PWD/build/snapshots/dock-icon-review-20261111" mise exec go@1.27.3 -- go run ./tools test -pkg ./internal/desktop/components -run 'TestDockMenu|TestVisualDockSelector|TestDetachedDockSelector'`；通过日志 `build/logs/test-20261111-212614.log`。
- 仓库 gate：`mise exec go@1.27.3 -- go run ./tools check -serial` 全部通过，含vendor完整性、576个Go文件格式、vet及全仓库测试，日志 `build/logs/check-20261111-212900.log`。
- 在线加载：用户原有58766 Web / 58763 Background中的同一SmallSSH会话已读取最终修订，66页、14组件、ready、readOnly=false、projectionErrors=[]。证据 `build/logs/dock-icons-live-summary-20261111.json`；未另起服务或切换其他项目。

本轮未打开、激活、安装或重启桌面App，未使用真实凭据。核验范围为本机当前CLI设计渲染、既有Go的headless测试及在线后端读取，不等于浏览器点击或已安装App交互验收。


## 2026-11-11 — 文件树层级与圆角卡片留白

用户澄清评审对象是文件树布局，并追加圆角卡片左上角的图标、按钮和状态点离边缘太近。对照正式 `filetree.go`、`components/tree.go`、`components/file_item.go`、画布标题及工具条实现，确认原设计树由带空格的p文本构成，HTML空白合并后丢失层级，没有独立展开控件、文件夹和当前目录高亮；卡片与局部标签组也缺少统一留白。

- SmallPen：新增共享“目录树行”，含普通/选中及展开/折叠/叶子6个组合。重整12个树、70行：行高26、每级缩进14、展开控件与文件夹图标各16宽，空目录保留展开占位，当前目录整行高亮。常规树宽180；右侧380宽面板按正式Go的40%上限示意为152。本机并排树显示/Users/demo并选中demo，远端选中/srv/site。右键反馈页底栏移入右侧列表，树贯通到底，表头复用共享组件并补齐2项选中高亮。
- 圆角留白：单标题、局部标签组、工具栏和文件工具栏均40高。单标题左16、右12，24高动作上下8；局部标签组左右12；工具标签28高、上下6，右侧32高按钮上下4。文字标签与操作分别成组，工具/工作区标签及卡片标题用12号样式。全局48高顶栏与终端16/6内容内边距不变。
- 正式Go：增加共享PanelHeaderHeight/PanelInset，修改 `components/panel.go`、`workspace_canvas.go`、`terminal_group_view.go`、`file_toolbar.go` 的头部尺寸/留白；不改变目录树导航、文件操作或终端连接行为。任务前5个源码文件的恢复副本使用 `.go.before` 后缀保存在 `build/authoring/tree-inset-20261111/`，与任务前SHA-256一致；逐行差异见审计报告，保留已有未提交改动。
- 映射与保真：pages.json同步树状态、布局约束及filetree/tree/theme源码映射；DESIGN.md更新设计入口。86个原有包文件中仅16个改变（12页、3组件、tokens.css），新增1组件；26条跳转、1个起点及4个字体SHA-256保持，既有Token仅工具条高度36→40、画布标题左边距10→16，新增14个角色Token。证据 `build/logs/tree-inset-audit-20261111.json`、`tree-inset-before-20261111.json`；设计源恢复副本 `build/authoring/tree-inset-20261111/before.smallpen/`。

最终修订 `9471a97ecf5fa1f1ce6e0c39cea98b19c6a180986f5c38e8a915956f0f9acb63`，66页、15组件、34变体、91 Token。结构检查0 error、0 warning、0 unused Token，见 `build/logs/tree-inset-check-final-20261111.json`。明暗100目标×2主题、200个组合全部完成，无失败、无剩余或渲染warning；保留14条既有possible_overlap info，686项跳过，coverage.complete=false。完整报告 `build/logs/tree-inset-render-final-full-20261111.json`，不将零warning视为完整视觉验收。初次新组件覆盖未按嵌套路径书写，CLI已指出并修正；初始失败日志 `tree-inset-check-initial-full-20261111.json` 不作通过证据。

CLI原生实测：首页卡片标题40高，状态点左距16、上距17，关闭按钮上下各8、右距12；工具标签左距12、上距6；树行步进26、名称逐级14缩进，树与右列表底边相同。完整规格 `build/logs/tree-inset-home-spec-full-20261111.json`，提取坐标 `tree-inset-geometry-20261111.json`，数值核对见audit。已目视检查首页浅色、右侧窄栏浅色、最终混排深色、本机/服务器并排浅色及右键反馈浅色临时PNG。前两张在未改变像素的中间修订fd9683上检查，后续仅改混排标签栏、工作区标签字体及树行圆角Token名；最终修订全包渲染已覆盖这些页面。预览元数据为 `tree-inset-{home-light,right-light,mixed-dark-final,paired-light-final,popup-light-final}-20261111.json`，不增加静态导出图库。

原生headless验证通过：生成工作台、文件标签、混合画布及窄画布明暗8张快照，并运行目录树点击/展开、十万行虚拟化与终端按键/Unicode/粘贴回归。命令通过 `go run ./tools test`，带 `preview_qa,terminal_integration` 标签；日志 `build/logs/test-20261111-214001.log`，快照 `build/snapshots/tree-inset-after-20261111/`。已目视检查实际混合画布浅色及窄画布深色。

初次全仓gate `build/logs/check-20261111-214001.log` 因本轮恢复副本保留`.go`后缀而被`./...`扫描为不完整包，vet中断。恢复文件已改为`.go.before`，未删除恢复内容；随后从同一正式源码重新运行完整门禁，vendor完整性、576个Go文件格式、vet和全仓测试全部通过，最终日志为 `build/logs/check-20261111-214114.log`。

复用用户原有58766 Web / 58763 Background中的SmallSSH会话，已加载最终修订，66页、15组件、ready、readOnly=false、projectionErrors=[]；证据 `build/logs/tree-inset-live-summary-20261111.json`。未另起服务、切换其他项目或打开/激活/安装/重启桌面App，未使用真实凭据。证据只适用于本机CLI、headless夹具及在线后端读取，未进行浏览器点击、已安装App交互或逐像素一致性验收。本轮汇总 `build/logs/tree-inset-summary-20261111.json`。


## 2026-11-11 — 按规格复核桌面全部设计页

按用户要求主动以 REQUIREMENTS.md、49项pages.json功能映射和正式Go源码核对桌面页面，覆盖67页：目录7、访问4、工作台13、连接7、文件9、命令3、状态6、Docker5、设置7、工具3、待实现3。逐模块结论及核验边界写入 `docs/design/DESIGN.md` 的“按规格逐模块复核”。当前地图与全部HTML路径、目录49张记录卡各字段、源码引用均有效；4个字体保持；原有26条跳转完整保留，补齐分类等导航后198条。审计 `build/logs/spec-audit-audit-20261111.json`；任务前完整副本 `build/authoring/spec-audit-20261111/before.smallpen/`，原始哈希 `build/logs/spec-audit-before-20261111.json`。

修正包括：正式Go右侧功能栏36→32以匹配顶栏；12条文件工具栏的来源/路径/响应式动作；72大小列、130时间列、8单元格内边距和原生时间列显示条件；树/表格间1布局间隔及底部状态区；读取与失败互斥并新增读取页；连接端口64；状态六分类、四指标与采样范围；设置文本导航/720内容/语言布局分组/36×20开关；保险库及CLI两种闲置策略、App进程后台语义；Docker只读镜像和项目动作范围、搜索与日志行数；端口筛选及6列；任务承载说明。U17命令“末尾回车选择”确实缺少UI及模型，已在REQUIREMENTS与地图列为待实现，未用稿子伪装交付。其它原有规划项保持待实现。

Go本轮源码差异只涉及workspace_canvas.go的功能栏宽度及5个离屏测试文件。修正过时夹具：文件计数按6文件/2目录；独立文件加载与错误测试使用editorWindowView；工具窗按命令/文件/状态顺序及40/28尺寸；设置测试使用独立settingsWindowView并开启settingsOpen，断言200侧栏中的176宽文本分类，主窗收栏状态不影响设置。生产快照新增canvas-home、安全设置、后台设置明暗场景及控件JSON边界。Go恢复副本使用`.go.before`后缀；保留其它既有未提交内容，不触碰vendor或历史。

离屏与原生测试均通过现有 `go run ./tools test`，使用 `preview_qa,terminal_integration` 和opt-in环境，路径 `build/snapshots/spec-audit-20261111/`。初始24系列宽范围运行 `build/logs/test-20261111-222223.log` 除旧LoadingAndDirectoryTransfer与DetachedTools夹具外均通过；修正后这两系列通过 `test-20261111-222956.log`。新增三类生产场景通过 `test-20261111-223115.log`。单独补跑Settings系列时旧主窗/收栏断言及缺少settingsOpen曾失败（`test-20261111-225153.log`、`test-20261111-225235.log`），修正后8变体通过 `test-20261111-225316.log`。合计25个系列最终有通过证据，350张不同PNG；并非350张全部人工目视。没有忽略失败或为迁就旧夹具改回正式UI。

必需gate `mise exec go@1.27.3 -- go run ./tools check -serial` 通过，含vendor校验、576文件gofmt、vet与全仓测试，最终日志 `build/logs/check-20261111-225235.log`，此前 `check-20261111-223159.log` 亦通过。最终Settings的settingsOpen修正发生在最后一次gate运行期间，仅影响带preview_qa标签的夹具，已由上述最终定向日志覆盖；正式Go代码未再变更。

19项原生/设计数值对照全部通过（`build/logs/spec-audit-geometry-20261111.json`）：功能栏和设置同轴且32宽；工具栏40、路径30、8动作28及水平位置/相对顶部偏移；树180；表格x401；树底852、表格底824/计数区28；设置内容x284/y48、720×376，语言组74/间隔12/布局组290。面板高可拖动，比较工具栏相对位置和底部，不将示例固定高度当作运行时约束。完整CLI求解规格 `spec-audit-{home,settings}-spec-final-20261111-full.json`；原生JSON随生产快照保存。

SmallPen最终修订 `d558913d3377fe5c00e8eed8ac9a535833ace85e54fc66fca2b980a193b3a5e7`，67页、16组件、47变体、122Token。结构0 error/0 warning/0 unused；明暗114目标、228组合全部完成，0失败/0剩余/0渲染warning；14条既有possible_overlap info（菜单及窗口按钮叠放）、721项对比度跳过，coverage.complete=false。最终证据 `build/logs/spec-audit-check-final-20261111.json`、`spec-audit-render-final-20261111-full.json`。期间同时调用check/view曾导致包写锁及scandir错误，未重启服务或手工删除锁，改为串行后最终全包渲染通过；中间失败不作通过证据。

已目视最终CLI临时PNG：首页浅色、工作区设置浅色、安全设置深色、六混排深色及端口浅色，元数据为 `build/logs/spec-audit-{home-light,workspace-light,security-dark,six-dark,ports-light}-final-20261111.json`；另对照原生canvas-home-light与settings-light。没有新增静态HTML/PNG图库。统计与检查全部覆盖不代表逐页点击或逐像素相等；状态趋势为单样本示意，字体和系统装饰/真实数据仍受运行环境影响。

用户已有58766 Web/58763 Background的同一SmallSSH会话读取最终修订、67页/16组件、ready、readOnly=false、projectionErrors=[]（`build/logs/spec-audit-live-summary-20261111.json`）；未另起服务、切换其它项目或将窗口前置。未安装/启动/重启桌面App、输入真实服务器凭据或触发系统认证。证据只适用于本机CLI、headless合成夹具和在线后端读取，不等同于已安装App或跨桌面平台验收。本轮汇总 `build/logs/spec-audit-summary-20261111.json`。

收尾时git diff --check发现Docker三页共3条空白行的行尾空格，已移除并重新检查。清理前目视PNG与求解规格修订为`a1fc41e3cc990c180c0bbb2925f02f5b34ac16a87caf4ed5cd3b2a74b652e003`，本次只改空白行，视觉/几何内容未变；最终修订另有完整结构、明暗渲染与在线加载证据，不将原PNG元数据伪记为新修订。


## 2026-11-11 — 画布与标签聚合恢复顶部选项卡

用户明确指出两种模式都应有窗口最上方那排选项卡。核实workspaceTerminalTabs在画布或多分栏时只画空白；单卡只保留标题，多标签分栏才有内部tab，稿子也照此隐藏，上一轮复核漏掉了这项要求。本轮在现有两种呈现中恢复顶部工作区导航，不扩展为后续待实现的命名临时画布/保存入口体系。

Go变更：workspace_menu.go汇集所有叶子分栏的workspaceTabs并画顶部标签；terminal_group_view.go提取共用tab strip，沿用切换、关闭、右键及拖拽逻辑，按真实owner激活视图。画布点击只改变焦点/选中视图，不合并重排分栏、不重建SSH或改变工具source。局部标签仍存在；顶部与局部控件使用不同可访问标签，选中项或可用宽度变化时揭示溢出项及关闭按钮。app.go保存两项纯显示状态，workspace_tabs.go复用owner查询。en/zh-Hans各补3个新增控件名称，未改变依赖或vendor。

SmallPen更新8个工作台页面：首页、双终端、四/六混合、画布开关、局部混排及底部/右侧停靠。标签聚合页的原有顶部导航保留。新增workspace-tabs=3px间距Token；其它既有Token、全部组件、4字体及198条原型跳转保持。同步REQUIREMENTS.md、pages.json的workspace操作/状态/边界以及目录记录卡；67个页面路径、49项映射与源码位置有效。没有用跨布局的静态跳转冒充“定位卡片”，实际交互由Go验证。原始副本 `build/authoring/top-tabs-20261111/before.smallpen/`，7个Go文件及2个翻译表的恢复副本均以`.before`结尾；哈希与差异清单 `build/logs/top-tabs-before-20261111.json`、`top-tabs-code-manifest-20261111.json`。保留其它已有未提交修改。

最终设计修订 `33b434391c2258fb68b587d0c2bfea32be88fab0d396c1a1e69987e9948a7e40`，67页、16组件、47变体、123Token；结构0 error/0 warning，明暗114目标×2主题=228组合完成，0失败/0剩余/0渲染warning。保留14条菜单/窗口按钮叠放info、751项对比度跳过，coverage.complete=false，不宣称完整视觉验收。证据 `build/logs/top-tabs-check-20261111.json`、`top-tabs-render-full-20261111.json`。首页求解规格 `top-tabs-home-spec-full-20261111.json` 中新顶部标签位于y8、高32，完整落在48高标题栏内。`top-tabs-audit-20261111.json`记录页面、字体、组件、Token及原型保真。

使用headless UI Tester新增两种模式下的顶部可见性/点击/输入路由/工具来源/分栏不变/关闭文件不影响SSH，以及16个跨分栏标签的溢出滚动测试；更新旧Chrome测试为同时断言顶部导航和局部关闭按钮处于各自区域。首轮针对性测试通过 `build/logs/test-20261111-231325.log`。较宽运行发现旧测试仍要求分栏后顶栏无标签，已据新要求修正。最初两个并行tools test同秒写入同名日志 `test-20261111-231421.log`，不将其作为完整通过证据；随后合并为单次运行，工作区/画布/终端/分栏/溢出/拖拽回归及全部ProductionWorkspace快照通过 `test-20261111-231529.log`（preview_qa,terminal_integration）。离屏产物 `build/snapshots/top-tabs-20261111/` 共71张不同PNG，并非全部人工目视。

已目视SmallPen首页浅色、六分栏深色、标签聚合浅色（`top-tabs-{home-light,six-dark,aggregate-light}-20261111.json`临时PNG元数据）；对照原生canvas-home-light与canvas-grouped-dark。未建立静态导出图库。实际原生样例内容与字体受夹具/环境影响，本轮验收为顶部标签存在及正确交互，不作整页逐像素相等承诺。

首次全仓检查 `build/logs/check-20261111-231421.log` 及补翻译前已启动的 `check-20261111-231818.log` 报新增3个tr源词缺失；已在两种语言目录各补3项。翻译模块复测通过 `test-20261111-231904.log`。全部源代码和翻译补齐后重新运行 `mise exec go@1.27.3 -- go run ./tools check`，vendor完整性、576个Go文件gofmt、vet及全仓测试全部通过，最终门禁日志 `build/logs/check-20261111-231927.log`；不以此前失败记录代替最终通过证据。

用户已有58766 Web/58763 Background的同一SmallSSH会话已加载最终修订，67页/16组件，ready/readOnly=false/projectionErrors=[]；读取记录 `build/logs/top-tabs-live-summary-20261111.json`，最终changes=0见 `top-tabs-changes-final-20261111.json`。未另起服务、切换其它项目、启动/前置/安装/重启桌面App，未使用真实SSH凭据或系统认证。证据为本机CLI、headless夹具与在线后端读取，未进行浏览器点击或已安装App验收。汇总 `build/logs/top-tabs-summary-20261111.json`。


## 2026-11-11 — 无会话空态移除示例标签

用户截图显示“未选择服务器”页仍有Web/Staging顶部标签，且Web侧栏高亮。检查确认是设计页残留的静态示例，与无会话/未选择状态矛盾；并非Go临时生成的会话。本轮移除2个顶部工作区标签、取消侧栏选中态，并将中央操作和说明改为正式Go的“选择服务器”/搜索引导。REQUIREMENTS补充顶部标签只对应已打开视图；pages.json和目录1的terminal-empty边界/源码同步。已有视图不会因侧栏选择为空而隐藏，正式Go运行逻辑未改。

Go验证仅改2个测试文件：已有5种空态覆盖两种呈现，共10组状态，断言无伪造会话/关闭标签；反向用例覆盖两种模式在已打开视图而侧栏无选择时保留顶部标签。预览补画布空态明暗2例，加既有6例共8张PNG，使用opt-in preview_qa、headless Tester与合成状态，无真实凭据。通过日志 `build/logs/test-20261111-232404.log`，目录 `build/snapshots/empty-tabs-20261111/`。必需 `mise exec go@1.27.3 -- go run ./tools check` 的vendor、576文件格式、vet与全仓测试全部通过，最终日志 `build/logs/check-20261111-232435.log`。

SmallPen修订 `e53eba61402499cf9b90b2d40a1e33e884cff9a77f2eac46b814d0134f69a659`，67页/16组件/47变体/123Token不变；结构0 error/0 warning（`build/logs/empty-tabs-check-20261111.json`）。仅空态和目录1两页HTML变更；分别按明暗执行scoped check --render，共4组合，无失败、剩余或渲染warning。空态有10项对比度跳过、coverage.complete=false；目录完整覆盖。报告 `empty-tabs-render-20261111-full.json`、`empty-tabs-catalogue-render-20261111-full.json`。未称为全包重新渲染。其余9张已打开视图示例顶部标签数量保持，4字体哈希保持；审计 `build/logs/empty-tabs-audit-20261111.json`，任务前恢复副本及哈希 `build/authoring/empty-tabs-20261111/`、`empty-tabs-before-20261111.json`。

已目视SmallPen空态明暗临时PNG（`empty-tabs-{light,dark}-20261111.json`）及原生workspace-empty-choose-canvas-light；检查无伪会话标签、无侧栏选中，未宣称整页逐像素一致，也未逐张目视全部8张。复用用户已有58766 Web/58763 Background的同一SmallSSH会话，已加载最终修订、ready/readOnly=false/projectionErrors=[]，证据 `empty-tabs-live-summary-20261111.json`。未另起服务、切换其它项目或启动/前置/安装/重启桌面App，未做浏览器点击验收。本轮汇总 `build/logs/empty-tabs-summary-20261111.json`。


## 2026-11-11 — 取消工作区卡片内部选项卡

用户明确表示没有“终端卡片内部选项卡”这种样式。本轮移除稿子混排页内部Web/Staging一排标签，改为复用画布标题栏显示当前Staging标题和操作；顶部4个工作区标签保留。REQUIREMENTS更新为卡片内只显示当前标题与操作、视图切换统一在窗口顶部；pages.json的workspace/terminal承载、动作及边界与目录记录卡同步。设计页文件名保留，避免已有入口失效；此前含内部标签的日期章节仅作为历史，不再作为当前规范。

正式Go：terminal_group_view.go在画布和多分栏中对非空视图统一画普通标题，空分栏保留无标签标题；workspace_canvas.go的标题从单项改为读取当前选中视图，同一分栏的其它视图继续由顶部标签访问。保留会话、分栏、工具来源、关闭/拖拽语义，不执行删除视图或重连。非画布分栏不额外加入画布专用动作。停靠工具区和业务分类的既有导航保留。

新增两种模式回归：内部不存在标签条、顶部切换更新普通标题、切换不搬迁或丢失视图、标题关闭只关闭当前项并显示剩余项。旧Chrome说明同步普通标题；拖拽测试从顶栏实际控件定位。初次宽范围运行 `build/logs/test-20261111-233138.log` 因仍用旧“服务器状态”内部标签定位失败，已改为顶栏控件。修正后通过工作区/画布/终端/分栏/溢出/拖拽及生产快照回归，最终 `build/logs/test-20261111-233254.log`，preview_qa,terminal_integration；73张不同离屏PNG在 `build/snapshots/no-inner-tabs-20261111/`，未逐张目视。必需 `mise exec go@1.27.3 -- go run ./tools check` 全部通过，含vendor、576文件gofmt、vet和全仓测试，门禁日志 `build/logs/check-20261111-233306.log`。

最终SmallPen修订 `6742a3c4fdccd0465ff380bb3c95cf80e38e6b8811cd78e767b90703ef8a8bb7`，67页/16组件/47变体/123Token保持，结构0 error/0 warning（`build/logs/no-inner-tabs-check-20261111.json`）。混排与目录1明暗共4组合渲染完成，无失败/剩余/渲染warning；混排38项对比度跳过、coverage.complete=false，目录完整覆盖。报告 `no-inner-tabs-render-20261111-full.json`、`no-inner-tabs-catalogue-render-20261111-full.json`；不称为全包重新渲染。源码扫描全部67页，“工作区标签”实例均位于窗口标题栏、卡片内部实例为0；页面路径、198条原型跳转和4个字体保持，审计 `no-inner-tabs-audit-20261111.json`。恢复副本 `build/authoring/no-inner-tabs-20261111/`、哈希 `no-inner-tabs-before-20261111.json`；Go恢复文件以`.go.before`结尾，保留其它既有未提交修改。

已目视最终SmallPen混排明暗临时PNG（`no-inner-tabs-{light,dark}-20261111.json`）和原生canvas-grouped-dark，确认内部为普通当前标题、视图标签在顶栏。用户已有58766 Web/58763 Background的同一SmallSSH会话已加载最终修订、ready/readOnly=false/projectionErrors=[]，证据 `no-inner-tabs-live-summary-20261111.json`。未另起服务、切换其它项目、使用真实凭据或启动/前置/安装/重启桌面App；证据为本机CLI与headless夹具，不替代浏览器点击或已安装App验收。汇总 `build/logs/no-inner-tabs-summary-20261111.json`。


## 2026-11-11 — 连接策略内联路径编辑与复选框修正

用户要求纠正新建连接的checkbox画法，并直接利用访问策略页空间显示保护路径，不另开子对话框。此前稿子是“☑”字符，Go虽使用原生ui.Checkbox但路径按钮另开a.ask多行弹窗。本轮保留原生控件逻辑，稿子新增共享复选框：16方框、4圆角、8间隔、13字/16行高，使用既有Lucide Check字形、原生明暗强调/前景及未选边框配色；4个选中/可用组合。访问策略直接显示210高、填满内容宽的多行输入，默认12根路径可读；保留实际“保存后连接”选项。

Go删除editProtectedPaths及其专用prompt.multilineHeight分支。connectionEditor保存原始多行草稿，解析路径进入form.Policy并参与脏状态和connectionDraft的Policy.Validate。错误原处提示、保留输入并阻止保存/身份探测/连接；提交后定位访问策略并聚焦输入。关闭AI或保护不能绕过无效路径校验，错误草稿仍可编辑。切换分类不丢草稿，清空采用默认且不产生无意义改动，新建连接后原控件重绑默认值。关闭关键操作保护的显式风险确认与防陈旧回调检查保留，策略语义与服务器端权限未改。描述文本补齐中英翻译。

设计正常/错误状态都使用完整新建连接表单。旧“保护路径编辑器.html”改名为“访问策略 · 路径校验.html”，不再承载独立路径弹窗；正常编辑按钮和子弹窗返回路径移除。pages.json、目录2及REQUIREMENTS同步入口/状态/动作/边界/源码与路径。49项映射和目录记录字段有效。原198条原型链接中有意去掉上述2条，新增错误状态4条分类导航，现200条；不将状态图的分类跳转等同于真实文本输入或保存。原16组件、123Token和4字体保持，新增复选框及5个角色Token。备份 `build/authoring/inline-policy-20261111/before.smallpen/`，Go/翻译/映射原始哈希 `build/logs/inline-policy-before-20261111.json`；Go恢复文件均为`.go.before`，保留其它既有改动。

原生测试通过 `go run ./tools test`，带preview_qa,terminal_integration及opt-in快照：`build/logs/test-20261111-234231.log`，涵盖原策略风险确认/凭据及元数据保留、内联默认/空值、560与1000宽可读性、错误草稿与跨分类保留、关闭访问不能绕过校验、重置隔离、连接分类及生产页面。后续加强为真实点击“保存并连接”验证拒绝与焦点，并检查旧控件重绑；最终全仓check包含这些无标签限制的测试及翻译校验。两个临时定向运行同秒共用test-20261111-234500.log，不以该混合记录作最终通过证据。路径快照入口改用a.view以应用实际App主题，明暗宽窄4张复测通过 `test-20261111-234819.log`。快照共74张在 `build/snapshots/inline-policy-20261111/`，并非全部人工目视。

清理旧弹窗专用状态后，最终 `mise exec go@1.27.3 -- go run ./tools check` 全部通过，含vendor完整性、576个Go文件gofmt、vet和全仓测试，最终日志 `build/logs/check-20261111-235222.log`。此前 `check-20261111-234531.log` 亦通过，但不替代清理后的最终门禁。未改vendor、引入依赖或启动常规桌面程序。

SmallPen最终修订 `0e1d172ace897a9d8d4021d556b99f0385fc02dd6a1ae0ef1742001ab2bc534c`：67页、17组件、51变体、128Token，结构0 error/0 warning。正常页2、错误页2、复选框4变体×2主题8、目录2合计14组合完成，0失败/0剩余/0渲染warning；前三组对比度共13项跳过，coverage.complete=false，目录完整覆盖。报告 `build/logs/inline-policy-check-20261111.json`、`inline-policy-{render,error-render,checkbox-render,catalogue-render}-20261111-full.json`。不称全包重渲染。中间Footer留白缺少高度曾被CLI拒绝，补充尺寸后重新检查；只引用最终通过报告。求解规格 `inline-policy-spec-full-20261111.json` 确认3个checkbox方框16×16、输入高210。源码/资源/链接及坐标审计 `inline-policy-audit-20261111.json`。

已目视最终SmallPen正常浅色、内联错误深色、复选框明暗4变体图（`inline-policy-{light,error-dark,checkbox-light,checkbox-dark}-20261111.json`临时PNG）；对照实际App主题下原生protected-paths-light与protected-paths-narrow-dark。没有增加静态导出图库，不宣称字体与系统装饰逐像素一致。用户已有58766 Web/58763 Background中同一SmallSSH会话已加载最终修订，67页/17组件、ready/readOnly=false/projectionErrors=[]，证据 `inline-policy-live-summary-20261111.json`。未另起服务、切换其它项目、输入真实凭据或启动/前置/安装/重启桌面App；证据只适用于本机CLI、headless夹具与后端读取，未代替浏览器点击或已安装App验收。汇总 `build/logs/inline-policy-summary-20261111.json`。


## 2026-11-12 — 终端保活说明与启闭状态

用户指出终端设置只写保活术语，缺少用途与启用/关闭解释。对照internal/sshconn/ssh.go的实际keepalive流程，确认0不启动SSH探测；启用后按间隔发送一次探测，并等待间隔×倍数（0采用3），无响应超时关闭连接。该倍数不是连续失败次数，也不是断开后自动重连。本轮不改Go网络算法、配置字段或默认值，只补清楚UI说明及依赖控件状态。

connection_options.go在设置旁常驻说明启用可帮助维持空闲链路、发现无响应，关闭不发探测且外部仍可能因空闲断开。原“无响应断开阈值”改称“无响应等待上限”，关闭时禁用并保留原选择；启用时直接显示当前间隔和最长等待秒数。默认30秒/3间隔显示90秒，改5显示150秒，超时断开需手动重连；关闭状态给出30×3示例。5条中英词条同步。没有为了示例修改用户已保存的值或自动开启保活。

SmallPen终端页按实际默认值显示关闭，等待项置灰；补完整编码说明与保活启闭说明，选择框显示当前值，替代把全部选项写在一起的旧示例，并复用实际保存后连接/固定保存Footer。pages.json、目录2及REQUIREMENTS同步用途/状态/源码与单次探测等待语义。67页/17组件/51变体/128Token保持；除终端和目录2两页，其余包文件、所有字体/组件/Token及200条原型链接保持。审计 `build/logs/keepalive-copy-audit-20261112.json`；恢复副本与原始哈希为跨日轮次开始时的 `build/authoring/keepalive-copy-20261111/`、`build/logs/keepalive-copy-before-20261111.json`。

新增的headless交互回归验证默认关闭说明不改脏状态、启用30秒显示90秒、改等待值显示150秒、关闭后不能展开无效控件且保留5、再启用仍显示150秒。相关连接选项/分类回归及19张opt-in离屏快照通过 `build/logs/test-20261112-000202.log`（preview_qa），含终端关闭及30秒开启、中英、明暗与宽窄窗口；快照目录 `build/snapshots/keepalive-copy-20261111/`。已目视SmallPen终端明暗、原生开启窄幅深色、英文关闭和英文开启，未逐张目视19张。UI测试未触发真实服务器、窗口激活或系统认证。

必需 `mise exec go@1.27.3 -- go run ./tools check` 全部通过，含vendor完整性、576文件gofmt、vet与全仓测试，最终日志 `build/logs/check-20261112-000507.log`。仅改现有3个Go源/测试文件及2个语言表，未引入依赖、修改vendor或网络保活实现。

最终SmallPen修订 `b13d1038b82ced4ac55c93cebda23217fdbc8eef799e52273cacdfda816e236c`，结构0 error/0 warning；终端与目录2明暗共4组合完成，0失败/0剩余/0渲染warning。终端7项对比度跳过、coverage.complete=false，目录完整覆盖；不称全包重渲染。报告 `build/logs/keepalive-copy-check-20261112.json`、`keepalive-copy-{render,catalogue-render}-20261112-full.json`。初次将尺寸Token用于text字号被CLI拒绝，已改用既有icon字号Token；最终报告为修复后的通过结果。临时PNG元数据 `keepalive-copy-{light,dark}-20261112.json`，未新增静态图库。

复用用户已有58766 Web/58763 Background的同一SmallSSH会话，读取最终修订、67页/17组件、ready/readOnly=false/projectionErrors=[]，记录 `keepalive-copy-live-summary-20261112.json`。未另起服务、切换其它项目、安装/启动/前置/重启桌面App或使用真实凭据；证据只针对本机CLI、headless夹具及后端读取，不等同于浏览器点击或已安装App验收。汇总 `build/logs/keepalive-copy-summary-20261112.json`。


## 2026-11-12 — 共享代理/SSH中转线路与统一引用

- 用户确认统一引用：设置新增“加速与代理”分类，提前保存命名线路；连接可选择已有线路、当前窗口中新建并自动选用、编辑所选线路，并保留原连接专用配置。HTTP/HTTPS/SOCKS5或已保存SSH中转均可复用；各连接保活独立。没有实现或宣称U09可靠UDP双端加速。
- 线路及代理密码随保险库加密持久化；所有者专用工作区读写沿用一次性签名/审计，hosts仅向认证所有者带出线路脱敏元数据，公开列表不披露线路。编辑保留密码，不返回明文；保留/替换选择纳入签名。新增加密字段已覆盖保险库各写入副本与锁定清理，旧无线路配置保持兼容。
- 连接只存线路ID。后端每次拨号解析新快照，更新清空空闲池与路径缓存，既有活跃流保持。线路缺失/冲突/循环/超过4跳拒绝操作，不会悄悄直连；在用线路禁删，线路引用的中转服务器亦禁删。桌面晚到回调按解锁epoch、弹窗身份及连接草稿序号丢弃；所有验证都未启动真实桌面窗口或触碰正式凭据。
- 定向持久化/协议/所有者/拨号测试：`build/logs/test-20261112-001819.log`。实际SSH回环用例验证共享中转用于探测和终端，更新成不可达代理后新连接失败、旧终端仍收发。覆盖密文与列表不含密码、其它保险库写入不丢线路、保留密码、已引用禁删、缺失/冲突/循环、失败写不发布及载荷篡改被拒。最初把多个包用逗号放入单个-pkg的命令失败，仅参数错误，记录`test-20261112-001805.log`，不是通过证据。
- 最终桌面真实UI Tester交互与12张线路专用离屏快照：`build/logs/test-20261112-002634.log`通过，覆盖新建自动选用、取消、代理密码遮盖/保留/替换、SSH中转创建、删除取消/确认、缺失线路提示。设置/连接分类回归与其余27张快照见`test-20261112-001933.log`，共39张置于`build/snapshots/saved-routes-20261112/`。仅使用合成信息、回环测试SSH和临时测试保险库。最终全仓`mise exec go@1.27.3 -- go run ./tools check`通过：`build/logs/check-20261112-002657.log`，含固定vendor补丁、582个Go文件格式、vet与所有默认测试。前两轮全仓通过记录002038/002400对应调整前版本，不替代最终门禁。
- SmallPen使用用户现有58766/58763实例及正式HTML包，不启动新服务。最终修订`5fa51b3550bfdbb9df0ee596055529e278ab6afaf85763c20b9b7744187e7e03`，73页/17组件/51变体/128Token；本轮新增6页，组件/Token/字体不变；73页映射、51功能入口、235链接无坏目标。结构检查`saved-routes-check-20261112.json`为0错误/0警告。15页明暗30组合完成，0失败/剩余/渲染warning；14条有意窗口装饰叠放info、54项对比度跳过，coverage.complete=false。最终编辑页05重测清除了最初2条checkbox对比度warning，其余14页在前一修订检查后无变化。逐页报告01–15与汇总`saved-routes-render-summary-20261112.json`、源码审计`saved-routes-audit-20261112.json`、约束`saved-routes-network-spec-full-20261112.json`均在build/logs。
- 已目视SmallPen线路库浅色及新建/最终代理编辑深色；实际原生线路库明暗、英文窄幅选择/新建/最终编辑。未声称39张逐张目视、全包渲染、SmallPen能执行动态保存或整页逐像素一致。现有SmallPen已加载最终修订、ready/readOnly=false/projectionErrors=[]，见`saved-routes-live-summary-20261112.json`。未浏览器点击验收、未安装或启动/前置/重启App，真实网络和其它桌面系统运行仍待人工验收。`git diff --check`通过；所有现有未提交工作保留。


## 2026-11-12 — Dialog分类整块面板、操作统一靠右

- 按用户要求将连接左侧分类框为一整块浅底、1 DIP细边框、8 DIP圆角和8 DIP内边距的面板；最小宽144，随中英文文字扩展。以公共components.DialogSidebar承载原生滚动、纵向焦点组，保留选择/草稿/滚动位置及安全操作行为。通用ActionGroup的End布局使用末端对齐和换行，连接底部取消、保存后连接、返回修改与保存/连接集中到右侧；取消旧的左右分散占位。没有改变代理线路或连接后端。
- `SSH_CLIENT_PREVIEW_QA=1 SSH_CLIENT_UI_SNAPSHOT_DIR="$PWD/build/snapshots/dialog-panels-20261112" mise exec go@1.27.3 -- go run ./tools test -pkg ./internal/desktop -tags preview_qa -run 'TestConnectionDensity|TestConnectionFooter|TestConnectionSection|TestConnectionModal|TestSavedRoute'`通过，日志`build/logs/test-20261112-003223.log`。复用既有头部/分类/草稿/焦点与共享线路回归，扩展边框内留白、操作成组靠右，以及窄窗长中英文连接操作不越界、不重叠并保持主按钮右对齐的验证。31张图片仅为离屏合成状态，没有打开原生窗口或真实认证对话框。
- 最终`mise exec go@1.27.3 -- go run ./tools check`通过：`build/logs/check-20261112-003342.log`，包含固定vendor补丁、582个Go文件格式、vet及全部默认测试。未改vendor及依赖。已目视原生宽幅常规浅色、网络深色、英文窄幅线路选择；未逐张目视全部31张。
- SmallPen正式HTML包更新7张连接分类状态的面板及18张模态页的底部布局；5张目录和pages.json同步状态、边界、公共组件源码。REQUIREMENTS记录公共Dialog布局规则。修订`384f55a95e5bd34533d36f82786a8d22af7d7c138e965e3368c5761105964f36`，73页/17组件/51变体/131Token；新增3个角色Token，原Token值、组件及字体不变。无新增/删除页面；73页映射、235链接无坏目标。23页明暗46组合完成，0失败/剩余/渲染问题；45项对比度跳过、coverage.complete=false。结构0 error/0 warning。报告`dialog-panels-check-20261112.json`、`dialog-panels-render-summary-20261112.json`及编号01–23报告，源码/链接审计`dialog-panels-audit-20261112.json`，均位于build/logs。
- 已目视SmallPen最终网络浅色，约束保存在`build/logs/dialog-panels-network-spec-full-20261112.json`。未声称全包重渲染、完整可访问性覆盖、浏览器点击或逐像素一致。继续复用用户原58766/58763服务，最终修订已在同一包会话ready/readOnly=false/projectionErrors=[]，证据`dialog-panels-live-summary-20261112.json`。没有另起服务、安装、启动/前置/重启App。既有未提交工作保留；`git diff --check`通过。


## 2026-11-12 — 身份确认小号Dialog与单层提示

- 公共Dialog支持Small宽480及Large宽820；原未指定规格的弹窗保留520默认宽，显式Width优先。连接表单使用Large，身份复核与首次/重新核对提示使用Small并随内容收高，移除复核640×380固定框。身份资料为紧凑只读信息，指纹不作可编辑输入；取消、返回修改、信任操作成组靠右。主窗显示首次身份提示时不再重绘后方同类确认框，避免两层重复按钮；取消后原复核流程仍可继续，打开/取消不会信任目标。
- 定向UI/首次连接实际回环/模态回归及16张离屏快照通过`build/logs/test-20261112-004147.log`，命令为`SSH_CLIENT_PREVIEW_QA=1 SSH_CLIENT_UI_SNAPSHOT_DIR="$PWD/build/snapshots/identity-dialog-20261112" mise exec go@1.27.3 -- go run ./tools test -pkg ./internal/desktop -tags preview_qa -run 'TestIdentityConfirmation|TestFirstConnectionPromptsAndRecordsTheHost|TestConnectionFormSnapshotsWithoutWindows|TestConnectionModal'`。覆盖480宽及紧凑高度、中英指纹和动作边界、右侧按钮、无重复首次确认层、打开/取消不写信任及既有首次连接记录指纹流程。初两次新增断言误用了带Label文本的HasText查询及“取消必须位于右侧三分之二”的英文三按钮空间假设，失败日志003933/004114仅作迭代记录，已改为实际标签定位、动作顺序和主按钮右边界检查；不作通过证据。
- 最终`mise exec go@1.27.3 -- go run ./tools check`通过：`build/logs/check-20261112-004241.log`，固定vendor补丁、582个Go文件格式、vet与全部默认测试均通过。认证、目标绑定和信任保存逻辑不变；未改vendor/依赖，未使用真实凭据。
- SmallPen确认页由900×600大容器改为480宽/内容收高；7张大表单宽度绑定公共820 Token，设计高度仍用于展示内容；目录2、pages.json与REQUIREMENTS同步。修订`7c6e1c899e1291d6a5234ae23442653eda658555712bef835b038380ec41e793`，73页/17组件/51变体/133Token，新增2个角色尺寸Token，原Token值、组件、字体和235链接保持，73页映射完整。结构0 error/0 warning，9页明暗18组合完成，0渲染问题/失败/剩余；32项对比度跳过、coverage.complete=false。不称全包重新渲染或完全无障碍验收。报告`identity-dialog-check-20261112.json`、`identity-dialog-render-summary-20261112.json`及编号01–09逐页报告、`identity-dialog-audit-20261112.json`、`identity-dialog-spec-full-20261112.json`均在build/logs。
- 已目视SmallPen确认浅色、原生复核浅色/深色及最终首次提示英文窄幅，未逐张目视16张图，不作字体及系统装饰逐像素保证。继续复用现有SmallPen服务与正式包会话，ready/readOnly=false/projectionErrors=[]，见`build/logs/identity-dialog-live-summary-20261112.json`。没有另起服务、安装或启动/前置/重启App；浏览器点击和其它桌面系统运行仍待使用验收。现有未提交工作保留，`git diff --check`通过。


## 2026-11-12 — 终端选中底色减淡与工具区间距收紧

- 终端/工作区选中标签强调色透明度10%→6%，活动画布标题5.5%→3.5%；文字、状态点及交互保持。工具标签行、文件操作行40→36，水平内边距12→8，状态内容顶部8→4；标签28、路径30及原按钮点击尺寸保留。卡片标题40和顶栏48保持，文件树/表格/计数与状态采样逻辑不变。
- 定向原生回归与22张离屏图通过`build/logs/test-20261112-092841.log`、`test-20261112-092953.log`；使用`SSH_CLIENT_PREVIEW_QA=1 SSH_CLIENT_UI_SNAPSHOT_DIR="$PWD/build/snapshots/workspace-density-20261112" mise exec go@1.27.3 -- go run ./tools test -pkg ./internal/desktop -tags 'preview_qa terminal_integration'`，run筛选画布首页/底部/右侧/标签聚合、文件窗格、独立工具窗及既有标签/窗格/状态测试。图片位于上述build目录，均为合成数据和离屏渲染，没有真实SSH凭据或窗口前置。仅更新既有工具窗选项卡垂直位置断言，不添加纯样式镜像测试。
- 最终`mise exec go@1.27.3 -- go run ./tools check`通过：`build/logs/check-20261112-093125.log`，含固定vendor补丁、582个Go文件格式、vet和所有默认测试。未改vendor、依赖、连接或文件操作后端。`git diff --check`通过；现有未提交工作保留。
- SmallPen更新22张相关内容页及4张目录，工作区标签的选中底色改用专用角色Token，新增工具内边距角色Token；共有73页/17组件/51变体/135Token，原字体和235链接保持，73页映射完整、无新页面。修订`734f74b4161bcdbcc2981e3493de947c7ab11828626e08c93577cb8127e1fd90`。结构0 error/0 warning；29页与工作区标签3变体明暗64组合完成，0失败/剩余/渲染warning；2条已知菜单覆盖列表info、663项对比度跳过，coverage.complete=false。报告`workspace-density-check-20261112.json`、`workspace-density-render-summary-20261112.json`及编号01–30、`workspace-density-audit-20261112.json`、`workspace-density-home-spec-full-20261112.json`均在build/logs。不称全包重新渲染或完整可访问性验收。
- 已目视原生画布首页明暗、独立服务器状态深色、SmallPen首页明暗，未逐张目视22张图，不称整页逐像素一致。现有SmallPen服务与包会话已加载最终修订、ready/readOnly=false/projectionErrors=[]，证据`workspace-density-live-summary-20261112.json`。未另起服务、安装或启动/前置/重启桌面App；浏览器点击与其它桌面系统运行仍待使用验收。


## 2026-11-12 — 按截图修正画布卡片标题到正文的距离

- 用户两张截图定位的是画布文件/服务器状态卡片内部，前一轮调整的普通停靠工具条不是同一承载层。本轮功能卡片标题40→32，工作区文件来源/路径/操作行36→28，文件区上方两行累计少16 DIP；工作区状态分类顶部内边距为0。Go文件来源选择器和路径输入同步缩到28。标题操作24与文件操作28保持，终端标题40和普通停靠/独立工具窗36不变，没有改变来源绑定、文件操作或状态采样。
- `SSH_CLIENT_PREVIEW_QA=1 SSH_CLIENT_UI_SNAPSHOT_DIR="$PWD/build/snapshots/canvas-inner-gap-20261112" mise exec go@1.27.3 -- go run ./tools test -pkg ./internal/desktop -tags 'preview_qa terminal_integration' -run 'TestProductionWorkspaceSnapshotsWithoutWindows/(canvas-mixed|canvas-grouped)'`通过：`build/logs/test-20261112-094851.log`，输出混合/同组布局明暗4张图，合成状态、无真实凭据或原生窗口。最终`mise exec go@1.27.3 -- go run ./tools check`通过：`build/logs/check-20261112-095022.log`，固定vendor补丁、582个Go文件格式、vet及全部默认测试均通过。未改vendor或依赖。
- SmallPen公共画布标题的功能区变体、6张相关内容页及3张目录同步；新增2个角色Token。修订`83558c9f8ad8f629c449cdb90ccb2a7a365fa8c01d7cc8dd4963446cbde52bd0`，73页/17组件/51变体/137Token，旧Token值和字体保持，235条链接无坏目标、73页映射完整。结构0 error/0 warning；9页加2个标题变体明暗22组合完成，0失败/剩余/渲染warning，保留2条菜单覆盖列表info，218项对比度跳过、coverage.complete=false。初轮目录路径仍为30导致超出28行高的warning已修复为28并重测。报告`canvas-inner-gap-check-20261112.json`、`canvas-inner-gap-render-summary-20261112.json`及编号01–10、`canvas-inner-gap-audit-20261112.json`均在build/logs。
- 约束`build/logs/canvas-inner-gap-file-spec-full-20261112.json`确认功能标题32，文件工具行、来源选择及路径28；标题到文件管理、工具行到树/列表的边距均0。已目视原生混合浅色，以及SmallPen单独查看文件/状态卡片浅色；未逐张目视全部4张，不宣称整页字体/系统装饰逐像素相同或全包重渲染。现有SmallPen同一会话已加载最终修订、ready/readOnly=false/projectionErrors=[]，证据`canvas-inner-gap-live-summary-20261112.json`。未另起服务、安装、启动/前置/重启App，实际浏览器点击与其它桌面系统仍待使用验收。既有未提交工作保留，`git diff --check`通过。


## 2026-11-12 — 状态工具栏时间范围左置并与分类合排

- 用户确认“5 分钟”应放到分类左侧，并指出稿子尚未变化。本轮实际更新diagnosticsView与正式SmallPen：概览按时间范围、六个分类、靠右采样反馈排列；中宽卡片合为一行。原生根据面板宽度与当前语言分类测量值回退，窄面板保留分类第一排、时间范围及靠右反馈第二排。原有范围状态、采样、过期、断开和分类业务不变。未新增纯样式镜像测试。
- `SSH_CLIENT_PREVIEW_QA=1 SSH_CLIENT_UI_SNAPSHOT_DIR="$PWD/build/snapshots/status-range-left-20261112" mise exec go@1.27.3 -- go run ./tools test -pkg ./internal/desktop -tags preview_qa,terminal_integration -run 'TestProductionWorkspaceSnapshotsWithoutWindows/(canvas-mixed|canvas-grouped|canvas-bottom|canvas-right|canvas-small)|TestResourceCardsNeverOverlapAndMissingRatesAreUnknown'`通过，日志`build/logs/test-20261112-120010.log`，10张混合/同组/底部/右侧/小尺寸明暗离屏图。该筛选中的斜线使独立资源测试不构成本次定向执行证据；资源交互与导航回归由随后全量check覆盖。已目视原生混合浅色单排和右侧浅色窄布局两排，未逐张目视10张；均使用合成数据，无真实服务器凭据和系统窗口。
- 最终`mise exec go@1.27.3 -- go run ./tools check`通过，`build/logs/check-20261112-120250.log`包含固定vendor完整性、582个Go文件格式、vet与全部默认测试。未改vendor、依赖或网络行为。
- SmallPen同步14张状态/Docker/混合内容页和目录5；工具标签增加紧凑密度，新增采样反馈最小宽度Token，pages.json/REQUIREMENTS同步操作与窄布局规则。最终修订`472e99fd80da221e724c6c01ff5d601d7454b0a2bc052364d5201a8909be84e0`，73页/17组件/53变体/138Token；235链接保持且无坏目标，73页映射完整，无新增页面或字体修改。结构0 error/0 warning；15页及4组件变体明暗38组合完成，0渲染问题/失败/剩余，418项对比度跳过，coverage.complete=false。报告`build/logs/status-range-left-check-full-20261112.json`、`status-range-left-render-summary-20261112.json`及编号01–16、`status-range-left-audit-20261112.json`。初轮实例padding写法不受CLI支持，已改成共享组件密度变体并重测；初始反馈最小120导致582卡片换行，已调整104并确认同排。
- 已目视SmallPen四个混合分栏浅色、六个混合分栏深色；修订8a9a2d44的页面图在最终修订中内容未变（仅目录说明后续更新）。最终工具栏与反馈约束为`build/logs/status-range-left-toolbar-spec-final-20261112.json`、`status-range-left-feedback-spec-final-20261112.json`。未持续导出静态图库，不称全包重渲染、完整无障碍或全尺寸逐像素一致。同一用户现有SmallPen服务已加载最终修订，ready/readOnly=false/projectionErrors=[]，见`build/logs/status-range-left-live-summary-20261112.json`；没有另起实例或抢占窗口，没有安装/启动/前置/重启桌面App，浏览器点击及其它桌面系统运行仍待使用验收。既有未提交工作保留，`git diff --check`通过。


## 2026-11-12 — 将状态时间范围收进右侧⋯菜单

- 按用户纠正移除常驻“5 分钟”选择框，改为状态分类栏最右侧的原生⋯菜单。菜单提供1/5/15分钟并勾选当前项；打开/取消不改值，选择影响图表时间窗口，分类切换保留。实际采样仍每2秒，没有增加刷新频率配置或新Dialog。移除上一轮为常驻框增加的宽度测量与分排代码，增加中英文菜单可访问名称。
- 新增headless功能回归在底部、右侧、独立工具窗验证隐藏常驻框、完整菜单、取消不变、15/1分钟实际窗口生效及分类切换后保留。定向范围/资源导航测试通过`mise exec go@1.27.3 -- go run ./tools test -pkg ./internal/desktop -run 'TestStatusRangeChangesThroughOverflowMenu|TestResourceCardsNeverOverlapAndMissingRatesAreUnknown|TestResourceNavigationPreservesTerminalAndPinsReturn'`，日志`build/logs/test-20261112-121349.log`。
- `SSH_CLIENT_PREVIEW_QA=1 SSH_CLIENT_UI_SNAPSHOT_DIR="$PWD/build/snapshots/status-menu-20261112" mise exec go@1.27.3 -- go run ./tools test -pkg ./internal/desktop -tags preview_qa,terminal_integration -run 'TestProductionWorkspaceSnapshotsWithoutWindows/(canvas-mixed|canvas-right)'`通过：`build/logs/test-20261112-121447.log`，4张明暗合成离屏图。已目视混合浅色卡片，未逐张目视；原生菜单由Tester收集并选择，不将离屏截图当作系统菜单的视觉证据。
- 必需`mise exec go@1.27.3 -- go run ./tools check`通过：`build/logs/check-20261112-121500.log`，含固定vendor补丁、582文件格式、vet与全部默认测试。未改vendor、依赖、采样和网络行为。
- SmallPen移除6处常驻框，14页接入右侧⋯，新增“06 状态/状态更多菜单”覆盖层，并同步目录5、pages.json与REQUIREMENTS；菜单示意默认5分钟，点击选项演示关闭，真实选择由上述UI测试验证。最终修订`8e4f07fbd03fab64858c4ec7eedf35e2c3cc327b3939243fc80f1d948ae04cff`，74页/17组件/53变体/137Token；删除无引用的范围宽度Token，其他组件与字体保持，252链接无坏目标、74页映射完整、无常驻范围选择器。结构0 error/0 warning；16页明暗32组合，0渲染问题/失败/剩余，430项对比度跳过、coverage.complete=false。报告`build/logs/status-menu-check-full-20261112.json`、`status-menu-render-summary-20261112.json`及编号01–16、`status-menu-audit-20261112.json`。已目视同一最终修订SmallPen状态卡片及208×134菜单浅色，不称完整无障碍或全包重渲染。
- 继续复用用户现有58766/58763服务，同一包会话已加载最终修订，ready/readOnly=false/projectionErrors=[]，证据`build/logs/status-menu-live-summary-20261112.json`。没有另起服务、打开新原生窗口、安装或启动/前置/重启App；现有未提交工作保留，`git diff --check`通过。本轮证据仅适用于本机CLI、合成headless UI及后端加载，未代替浏览器点击或其它桌面系统实际运行验收。


## 2026-11-12 — 终端标题与首行内容的视觉留白

- 按用户说明改为核对鼠标未悬浮时的视觉空白。终端标题40→32，正文上下Padding6→2、左右16；首行命令上提12，减少透明标题区与正文上内边距叠加的空白。源码改动限于components/theme.go与terminal_colors.go的既有尺寸；保留顶部工作区选项卡与原生终端输入、选择和拖动路径，无新增样式镜像测试。
- `SSH_CLIENT_PREVIEW_QA=1 SSH_CLIENT_UI_SNAPSHOT_DIR="$PWD/build/snapshots/terminal-optical-20261112" mise exec go@1.27.3 -- go run ./tools test -pkg ./internal/desktop -tags preview_qa,terminal_integration -run 'TestProductionWorkspaceSnapshotsWithoutWindows/(canvas-home|canvas-mixed|canvas-grouped|canvas-tabs|canvas-small|workspace-four)'`通过，`build/logs/test-20261112-140716.log`、12张合成离屏图。已目视混合画布浅色/深色，未逐张目视12张；未创建系统窗口或输入真实服务器凭据。
- 终端分组、窄幅标题关闭、画布保留、顶部标签切换通过`build/logs/test-20261112-140830.log`（terminal_integration）；输入命中、跨组移动及拖动分栏通过`test-20261112-140852.log`，run为`TestSplitTerminalClickRoutesKeysAndSharedToolsToFocusedPane|TestTerminalTabDrag|TestExistingTerminalTabMovesBetweenGroupsWithoutNewConnections`。必需`mise exec go@1.27.3 -- go run ./tools check`通过`build/logs/check-20261112-140920.log`，含固定vendor完整性、582文件格式、vet和默认全仓测试。运行时曾扫描到build中的两份源码留档且均无测试；现已将留档后缀改为.go.txt，后续不会成为Go包。正式源码与依赖不受留档影响。
- SmallPen仅修改2个角色Token值和目录1；8张终端页与共享画布标题复用Token，pages.json/REQUIREMENTS更新当前尺寸及视觉约束。修订`5821b434a5da3a3c4d298887c0fd3b1c7c5bcc0d50c246449f5c3a87e52d3cb0`，74页/17组件/53变体/137Token；252链接无坏目标，74页映射完整，字体及组件定义保持。结构0 error/0 warning；9页加2标题变体明暗22组合完成，0渲染问题/失败/剩余，317项对比度跳过、coverage.complete=false。报告`build/logs/terminal-optical-check-full-20261112.json`、`terminal-optical-render-summary-20261112.json`及编号01–10、`terminal-optical-audit-20261112.json`。已目视SmallPen浅色终端修改前后；修改后图修订629190ab与最终修订仅目录说明不同，不称全包或完整可访问性验收。
- 现有SmallPen58766/58763同一包会话已加载最终修订，ready/readOnly=false/projectionErrors=[]，证据`build/logs/terminal-optical-live-summary-20261112.json`。没有另起服务、安装或启动/前置/重启桌面App；本轮证据限本机CLI与合成headless状态，不代替浏览器点击、用户视觉确认或其它桌面系统运行。既有未提交工作保留，`git diff --check`通过。


## 2026-11-12 — 顶部工作区标签收进内容边界

- 用户截图指出顶部选中标签向下方终端卡片左侧探出。本轮修改workspaceHeader的起点计算：主内容边界内缩8，按当前侧栏宽度重算，单组聚合扣除自身标题内边距；以侧栏开关实际32宽取代30估计。紧凑macOS原生控件区优先。没有新增纯样式镜像测试。
- `SSH_CLIENT_PREVIEW_QA=1 SSH_CLIENT_UI_SNAPSHOT_DIR="$PWD/build/snapshots/top-tabs-inset-20261112" mise exec go@1.27.3 -- go run ./tools test -pkg ./internal/desktop -tags preview_qa,terminal_integration -run 'TestProductionWorkspaceSnapshotsWithoutWindows/(canvas-home|canvas-mixed|canvas-tabs|canvas-small|workspace-four)'`通过`build/logs/test-20261112-142441.log`，10张明暗合成离屏图。标签双模式、溢出自动揭示、侧栏切换、头部拖动与关闭回归通过`test-20261112-142545.log`，run为`TestWorkspaceChrome|TestWorkspaceTopTabs|TestWorkspaceCardsHavePlainHeadersAndOnlyTopTabs|TestSidebarToggleKeepsTabsPinnedFilesAndRestoreButton|TestCanvasHeaderDrag`。已目视原生混合浅色，未逐张目视10张图。
- 必需`mise exec go@1.27.3 -- go run ./tools check`通过`build/logs/check-20261112-142604.log`，包含固定vendor完整性、582文件格式、vet与全部默认测试。未改vendor、依赖或会话操作。
- SmallPen25张主窗页的重复88px留白改用新增角色Token workspace-title-spacer110，目录1、pages.json与REQUIREMENTS同步。最终修订`e98af37829fecd7e4f886321bddedb71da49b42bd44453b8f2143456f2f6f458`，74页/17组件/53变体/138Token，252链接无坏目标、74页映射完整，无新增页面/组件/字体修改。结构0 error/0 warning；26页明暗52组合，0渲染问题/失败/剩余，674项对比度跳过、coverage.complete=false。报告`build/logs/top-tabs-inset-check-full-20261112.json`、`top-tabs-inset-render-summary-20261112.json`及编号01–26、`top-tabs-inset-audit-20261112.json`。
- 已目视最终SmallPen混合浅色；标签起点x206→228，终端卡片x220，实际内缩8。前后约束分别为`build/logs/top-tabs-inset-before-spec-20261112.json`、`top-tabs-inset-after-spec-20261112.json`，最终卡片约束为`top-tabs-inset-content-spec-20261112.json`；后者与最终整页PNG使用最终修订，前后标签规格的页面结构仅有后续目录说明差异。不称全包重渲染、完整无障碍或所有宽度像素一致。现有SmallPen同一包会话已加载最终修订，ready/readOnly=false/projectionErrors=[]，证据`top-tabs-inset-live-summary-20261112.json`。未另起服务、安装或启动/前置/重启App；浏览器点击及其它桌面系统仍待使用验收。现有未提交工作保留，`git diff --check`通过。


## 2026-11-12 — 左侧圆角服务器面板与新建入口上移

- 按用户参考图把服务器区收成整块Surface背景圆角面板，圆角16、内边距6，搜索28、连接行36。移除原侧栏36高新建行；顶部标签右侧＋打开原有新建连接表单，旁边用终端图标保留新建终端。保留既有导航/未保存守卫和保存阶段禁用；没有添加新的连接流程或权限规则。头像使用相同槽位，展开/窄栏保持滚动、选择与分组。
- 新增560宽展开/窄栏下新入口打开及取消恢复回归；相关新建、侧栏搜索/滚动/切换、空态、原子保存与头部对齐测试通过`build/logs/test-20261112-150919.log`。初轮`test-20261112-150816.log`的头像像素断言扫到了圆角遮罩内的部分边缘行，现对完整头像槽继续比较像素，边缘行仍检查位置/高度、虚拟范围和选择；该失败日志不作为通过证据。原有头部对齐断言由已删除的新建行改为侧栏外框。
- 生产主窗画布首页/混合/标签聚合/窄幅/四终端明暗10张合成离屏图通过`build/logs/test-20261112-150954.log`；该命令斜线后的独立侧栏测试不计作执行证据。随后单独执行侧栏9张展开/窄栏/恢复图及顶部标签、溢出、拖动回归，通过`test-20261112-151038.log`（preview_qa,terminal_integration），共19张图位于`build/snapshots/sidebar-card-20261112/`。已目视混合浅色与窄栏深色，未逐张目视19张。所有夹具为合成状态，无真实服务器凭据或原生窗口。
- 必需`mise exec go@1.27.3 -- go run ./tools check`通过`build/logs/check-20261112-151114.log`，含固定vendor完整性、582文件格式、vet与默认全仓测试。未改vendor、依赖、会话和保存后端。
- SmallPen25张主窗页及目录1/2同步，新增2个角色Token，pages.json/REQUIREMENTS更新入口、状态、动作与源码。最终修订`d49ca167ab483b97adbff5ba9c5cf941567b97635e1b916798cc2861b969c819`，74页/17组件/53变体/140Token；276链接无坏目标、74页映射完整，侧栏新建行0、顶栏新建图标25；无新页面、组件或字体变更。结构0 error/0 warning；27页明暗54组合，0渲染问题/失败/剩余，674项对比度跳过、coverage.complete=false。报告`build/logs/sidebar-card-check-full-20261112.json`、`sidebar-card-render-summary-20261112.json`及编号01–27、`sidebar-card-audit-20261112.json`。已目视SmallPen混合浅色和最终深色，浅色图修订4a9b3da3与最终仅目录说明不同；不称完整无障碍、全包重渲染或像素完全一致。
- 继续复用用户现有SmallPen58766/58763同一包会话，已加载最终修订，ready/readOnly=false/projectionErrors=[]，证据`build/logs/sidebar-card-live-summary-20261112.json`。没有另起服务、安装或启动/前置/重启App；浏览器点击、用户视觉确认及其它桌面系统运行仍待使用验收。既有未提交工作保留，`git diff --check`通过。


## 2026-11-12 — 侧栏延伸到顶部并统一8外边距

- 按用户纠正，将上一轮仅覆盖列表区的卡片延伸为包含窗口顶部控制区的整块侧栏背景；四周统一8，与工作区及右侧按钮共用WorkspaceInset。原生根背景绘制圆角Surface，SidebarWidth共用既有宽度夹取规则，背景不创建命中/焦点元素。保留标题栏、搜索、分组、头像滚动及新建入口的原位置和操作。
- 原生最终8张合成离屏图见`build/snapshots/sidebar-fill-20261112/`与`build/logs/test-20261112-152023.log`（preview_qa,terminal_integration；画布首页/混合/标签聚合/窄幅明暗）。最初absolute背景会盖住内容，已在目视时发现并改为根背景绘制，151753快照日志不作最终视觉证据；最终图片覆盖同路径。已目视最终混合明暗，未逐张目视8张。顶栏新建、侧栏切换/搜索/滚动、头部对齐、标签溢出与拖动回归通过`test-20261112-152546.log`。
- 必需`mise exec go@1.27.3 -- go run ./tools check`通过`build/logs/check-20261112-152838.log`，含固定vendor完整性、582文件格式、vet与全部默认测试。未改vendor、依赖或连接后端。未新增样式镜像测试。
- SmallPen25张主窗页改为左右两列自动布局：左列控制区+列表整体Surface，右列标签+工作区；窗口控制新增侧栏位置变体。早期free/重叠grid候选不符合工具约束，未作为交付；最终不使用叠放布局。删除2个旧尺寸Token，新增1个侧栏原生控制预留尺寸Token，pages.json/目录1/REQUIREMENTS同步全高边界和源码。最终修订`c3d5107793aee23dada32a9f0f93940b6b2f39522e164fcd36e8627a15f99594`，74页/17组件/54变体/139Token，276链接无坏目标、74页映射完整，无新页面或字体改动。结构0 error/0 warning；26页加2窗口控制变体明暗56组合完成，0渲染问题/失败/剩余，674项对比度跳过、coverage.complete=false。完整报告`build/logs/sidebar-fill-check-full-20261112.json`、`sidebar-fill-render-summary-20261112.json`、编号01–27及`sidebar-fill-audit-20261112.json`。3张Docker页清理空白后在最终修订补测，其余目标源文件未变，保留434eadec版的同内容检查证据，补测为`sidebar-fill-whitespace-render-01/02/03-20261112.json`。
- 最终1440×960示例测得外框x8/y8、204×944，底边距8，右侧到主内容x220的间隔8，原标签起点x228保持；证据`build/logs/sidebar-fill-final-frame-spec-20261112.json`。已目视SmallPen浅色及最终深色，不称所有窗口尺寸、真实OS标题栏或跨平台像素一致。现有SmallPen58766/58763同一会话已加载最终修订，ready/readOnly=false/projectionErrors=[]，见`build/logs/sidebar-fill-live-summary-20261112.json`；未另起服务、安装或启动/前置/重启App。既有未提交工作保留，`git diff --check`通过。

## 2026-11-14 — sing-box 出站依赖的隔离编译与体积探针

- 按用户“直接依赖、编译一个试试”的要求新增 `go run ./tools proxy-probe -platform <目标>`；入口与其它任务一致，文档见 `docs/DEVELOPMENT.md`。固定版本只声明在独立夹具 `tools/proxy-probe/fixture/go.mod`：sing-box v1.14.4（上游 tag 解析为 `c005f35300ce76427dc285481ec5695f5b179cd2`）。任务复制夹具到 `build/proxy-probe/<时间>/module/`，在那里解析依赖；正式应用的 `go.mod`、`go.sum`、vendor 与 macOS 密码框补丁均未修改。没有启用新的生产代理类型、导入界面或分享链接解析。
- 夹具保留单条 outbound JSON 解码、内嵌 Box 生命周期与 `DialContext` 的可达调用链，只注册 Shadowsocks、Snell、VLESS、AnyTLS，含 Hy2 档再注册 Hysteria 2；不导入全量 `include`。空的入站、端点、服务与证书提供者注册表不提供 TUN、代理监听器或外部控制服务；仅注册本机 DNS 解析客户端。共享运行时仍依赖 DNS 客户端及 sing-tun 的公共类型，不能把本轮说成“所有 DNS/TUN 代码均已裁掉”。四个目标实际包清单中的 Tailscale 与 gVisor 包数均为 0；依赖清单保存在每个产物的 `.deps.txt`。
- 统一 Go 1.27.3、`CGO_ENABLED=0`、`-trimpath`、`-s -w -checklinkname=0`；出站档使用 `with_utls`，Hy2 档再启用 `with_quic`。已核对本版 REALITY 客户端由 `with_utls` 编入，并非旧构建标签留下的空实现。首次命令为 `mise exec go@1.27.3 -- go run ./tools proxy-probe -platform darwin/arm64`，日志 `build/logs/proxy-probe-20261113-235753.log`，报告 `build/proxy-probe/20261113-235753/report.json`；交叉编译命令的目标为 `darwin/amd64,linux/amd64,windows/amd64`，日志 `build/logs/proxy-probe-20261113-235847.log`，报告 `build/proxy-probe/20261113-235847/report.json`。

| 目标 | 直接 TCP 基线 MiB | 出站、不含 Hy2 MiB | 出站、含 Hy2 MiB | 含 Hy2 gzip MiB |
| --- | ---: | ---: | ---: | ---: |
| macOS ARM64 | 2.94 | 15.93 | 18.21 | 7.30 |
| macOS Intel | 3.08 | 17.19 | 19.60 | 7.91 |
| Linux x64 | 2.98 | 17.86 | 20.13 | 8.18 |
| Windows x64 | 3.12 | 17.49 | 19.72 | 8.00 |

- 本机 ARM64 依赖准备 15.491 秒；新建独立编译缓存后，按基线→无 Hy2→含 Hy2 的顺序首次编译分别为 2.575 / 7.631 / 4.001 秒，重复编译分别 0.138 / 0.426 / 0.522 秒。各档共享本轮缓存，含 Hy2 的首次编译已复用前档结果，不是从空缓存单独构建的耗时。交叉编译的含 Hy2 档首次 3.615–3.860 秒、重复 0.256–0.304 秒，亦有相同顺序与缓存限制。这些数字只代表本机当前条件。
- 上述是独立可执行文件，包含 Go runtime、公共依赖与内嵌运行时；gzip 只压缩该文件，不是安装包。不能将其直接加到已有 `.app` 体积，也不能据此推断正式集成时多个可执行文件的总增量。夹具没有执行，没有连接真实服务器或输入真实凭据；尚未验证配置构造、生命周期实际运行、任何协议握手、SSH/SFTP 连通、运行内存、应用依赖合并或其它系统的运行行为。
- 配置方向按本轮讨论记录：优先解析分享链接并转换为单条 outbound，JSON 可复用上游格式；整份配置应先提取节点，让用户选取，再按本产品允许的字段校验。现有加密线路及“引用线路、下次连接用新配置”的语义保留。`ss://` 属于 Shadowsocks，`ssr://` 属于额外的 ShadowsocksR，后者不在本轮编译范围；本轮没有宣称 sing-box 自动提供所有分享链接的解析。导入后的凭据存储和脱敏仍需在正式集成中接入现有保险库。
- 必需 `mise exec go@1.27.3 -- go run ./tools check` 通过，证据 `build/logs/check-20261113-235956.log`，包含固定 vendor 完整性、589 个文件格式检查、vet 与默认全仓测试（独立夹具不作为主模块测试包，已由上述任务编译）。`git diff --check` 通过。所有已有未提交工作保留；没有另起 SmallPen 服务、安装、启动、前置或重启桌面应用，也未提交或推送。


## 2026-11-14 — 正式接入受限代理出站、导入与加密线路

- 按用户确认将 sing-box v1.14.4 接入正式 SSH/SFTP 拨号链。支持 Shadowsocks AEAD/2022、Snell v4（v5 TCP 兼容）、VLESS TLS/REALITY、AnyTLS、Hysteria 2，保留 HTTP/HTTPS CONNECT、SOCKS5 和 SSH 中转。只注册所需出站及本机 DNS 解析客户端，不启动代理监听、DNS 服务、TUN、系统代理或控制接口，不导入全量 include。每条 SSH socket 拥有自己的内核生命周期，成功拨号后不受握手 context 取消影响，关闭 socket 释放内核与会话池；失败不回退直连。Hy2 使用标准 QUIC/TLS，不启用上游浏览器模拟。
- 设置→加速与代理→新建线路默认提供链接/JSON 导入。纯配置模块支持 ss/vless/anytls/hy2/hysteria2 链接、Snell 节点 JSON、整份配置的受支持 outbounds 提取及多节点选择；不下载订阅，不支持 SSR、插件、detour、外部密钥文件或执行全局监听/DNS/路由设置。限制输入长度、节点数、协议字段、凭据和传输选项，拒绝无证书 pin 的 TLS insecure。公开规范化 JSON 与密码/UUID 等凭据分离，凭据经现有保险库加密，预览/编辑不回填；导入原文解析成功后清空。已有连接按线路 ID 引用，修改只影响下次连接，现有会话保留快照。
- 正式依赖更新已重建 vendor、许可证与完整性清单；保留 MyGo v0.7.1，macOS 密码可访问性补丁字节哈希保持不变。新增 sing-vmess Vision 的 GC 指针跟踪和双向读写竞态补丁，具备类型检查与固定哈希，未禁用 checkptr/race。升级 Go 1.27.4、x/crypto v0.59.0、x/net v0.62.0、gRPC v1.83.2、compress v1.20.2、brotli v1.2.7、chi v5.3.0 等，消除本轮扫描发现的已知告警；补丁说明及复用方式见 patches/README.md、docs/DEVELOPMENT.md。GPL 与第三方许可随 vendored 依赖及 THIRD_PARTY_NOTICES 保留，本轮没有发布或改变项目根许可证。
- 最终必需门禁 `mise exec go@1.27.3 -- go run ./tools check -serial` 通过：`build/logs/check-20261114-004650.log`，包含 vendor 补丁完整性、格式、vet 与全仓默认测试；go.mod 自动选用 Go 1.27.4 工具链。合成 loopback 的六种协议组合（SS、Snell、VLESS TLS、REALITY+Vision、AnyTLS、Hy2）实际执行身份探测、固定主机身份的 SSH 命令与 SFTP 列表，并拒绝错误 SSH 主机 pin 和 TLS SPKI pin；最终 race 通过 `build/logs/test-20261114-004234-race.log`。配置拒绝/脱敏、取消/关闭、保险库加密与引用快照、UI 导入/选节点/编辑/取消均由对应测试和最终门禁覆盖。早期 Vision 的 checkptr/竞态失败在补丁后重测通过；并发门禁中既有 remote-forward 取消测试曾失败一次，安静串行的最终门禁通过，未修改该转发路径。
- `security` 使用 go.mod 固定的 govulncheck，并主动解析 JSON finding，扫描失败或解析失败均失败，避免 JSON 模式退出码造成假通过。最终三生产入口源码扫描 `build/logs/security-20261114-004235.log/.json` 为 **0 个已知可达漏洞**；最终打包守护进程扫描 `security-20261114-004744.log/.json` 为 **0 个受影响符号告警**。官方数据库快照 SHA256 为 `3b16a8d3ee29af03d9e8f78650c4286b9630569eb0542a93c4207e714c27ef11`，数据库修改时间 2026-11-11T02:51:11Z。二进制模式没有调用图，符号存在不等于可利用；0 仅覆盖该数据库已知问题，不保证未知漏洞、fork 归属或全部动态调用安全。初轮扫描输出或失败日志不作为最终通过证据。
- 五目标交叉编译通过 `build/logs/compile-20261114-004651.log`：darwin/arm64、darwin/amd64、linux/amd64、linux/arm64、windows/amd64。实际 macOS ARM64 应用在 `build/darwin-arm64/SmallSSH.app`，最终完整构建 `build/logs/proxy-integration-build-final-clean-20261114.log`、生成文件校验 `proxy-integration-generated-final-clean-check-20261114.log` 均通过。CLI/守护进程去除调试符号，旧 CLI 入口改相对链接后重新封装外层 ad-hoc 签名并深层严格校验；原生辅助组件签名和哈希保留。最终应用内普通文件合计 **102,582,609 字节 / 97.83 MiB，未压缩**，含桌面、CLI、守护进程；证据 `proxy-integration-package-final-20261114.json`。MyGo 打印的 125.9 MB 是随后去重前大小，不是最终实测。 当前已安装旧包只读实测为61,813,980字节 / 58.95 MiB，与新包粗比相差40,768,629字节 / 38.88 MiB（约40.77 MB）；旧包不是同工具链、同裁剪参数重新构建的基线，不能将此差额精确归因于代理功能。代理运行时代码当前分别进入桌面、CLI与守护进程，存在跨可执行文件的重复成本。未测安装包压缩体积、运行内存或其它系统的实际协议运行。
- 原生 headless UI 的 18 张明暗/英文合成快照位于 `build/snapshots/proxy-import-final-20261114/`，日志 `build/logs/test-20261114-003006.log`；已目视新建浅色、导入预览深色、节点选择英文，未逐张目视全部图片。SmallPen 新增导入预览、节点选择、编辑导入线路、重新导入四页，更新新建线路、线路库与目录3，并同步 REQUIREMENTS/pages.json。最终修订 `5ca7f0492ed3f58bdd9b225c802b0b856d7fb48b96b46db012ef27f358b009af`，78页/17组件/54变体/140 Token；267条链接无坏目标，78页文件映射完整，见 `build/logs/proxy-integration-audit-20261114.json`。结构检查 `proxy-import-design-check-final-20261114.json` 为0 error/0 warning。新建/选择/重新导入在最终修订完成明暗渲染，预览/编辑/目录保留 a49ec22d 版同源内容的明暗报告，线路库保留 5b30c609 版报告：仅2条既有原生窗口按钮与导航背景重叠 info，无渲染 error/warning；对比度有跳过项，coverage.complete=false，不称全包或完整无障碍验收。最终 SmallPen 新建浅色与选择深色 PNG 已目视，禁用保存与右侧操作完整。
- 复用用户原有 SmallPen 58766/58763 服务，同一包会话 `dbffd4d7-6596-4c43-bfdc-0fdcfbdb4fca` 已载入最终修订，ready、readOnly=false、projectionErrors=[]，见上述 audit 与 `proxy-import-live-final-20261114.json`。没有另起服务、输入真实服务器凭据、安装、启动、前置或重启桌面应用。其它桌面系统仅完成编译，原生密码框未进行前台 AX 实测；现有未提交工作全部保留，无提交或推送。最终 `git diff --check` 通过。


## 2026-11-14 — 三入口收拢为独立 smallsshd 执行后端

- 按用户确认取消 Desktop、CLI、守护进程三份连接能力的重复编入。Desktop 解锁与创建保险库后的接入、CLI agent 均使用固定 smallsshd 的已验证本机 IPC 和既有 prepare/commit 管理协议；实际 broker、SSH/SFTP、隧道、连接池及受限代理运行时只归属守护进程。主密码/系统验证仍在可信本机入口进行，临时根会话在接入后关闭，前端只持有当前进程/活控制连接绑定的管理签名器。管理会话沿用 SDK 最长8小时，到期或断开后重新解锁，不缓存根密钥自动续期。单个管理会话关闭不锁定其他管理端，明确锁定仍为全局动作。
- 将完整消息、确定性证明规则与公开 SSH 数据从执行后端分离：protocol 保存原签名域与 JSON 字节契约，sshdata 保存公开元数据，sshkey 保留本机目标/私钥预检；broker/sshconn 的原导出保留兼容包装或类型别名供后端及既有夹具使用。生产 client/Desktop/CLI 不再导入 broker/sshconn/netroute 或 sing-box。新增 check 的真实 go list 依赖图门禁，防止仅靠链接器裁剪掩盖前端依赖。没有改 go.mod、vendor 或安全补丁；已有未提交工作保留。
- LocalService 复用现有实例，按同一私有数据目录串行启动固定守护进程，并验证端点程序身份；已存在活端点却无法验证时直接失败，不替换或回退内嵌后端。Windows 使用无窗口启动参数，Unix 独立会话不占用户终端，开发任务先暂存本机守护进程。Desktop 锁定先撤销本地证明/取消操作，再异步请求服务锁定，只有服务确认后显示全局撤销；失败通过现有解锁页错误槽显示未确认。服务管理会话失效回到锁定页。保留当前 GUI 退出时锁定语义，未宣称 U04 的后台下载、退出后保留其它调用方、后台方式设置或跨版本守护进程自动更替全部完成；更替需先经已验证管理会话停止旧服务，不强制抢占旧身份。
- 默认全仓门禁 `mise exec go@1.27.3 -- go run ./tools check -serial` 通过：`build/logs/check-20261114-010909.log`，含3个 vendor 哈希、619个Go文件格式、两个生产前端无连接/代理运行时的依赖图、vet 与全部默认测试。随后仅补齐锁定失败/服务失效消息到既有解锁错误槽的同步，并通过最终定向门禁 `check -pkg ./internal/desktop -run 'TestDesktopLock|TestDesktopManagedSession' -race`：`build/logs/check-20261114-011349-race.log`，验证根材料关闭、本地立即撤权、远端锁定/审计锚点及未确认反馈在卡片内可见。旧解锁两次 UI 门禁、取消/迟到/清理链和审计回归的 race 通过 `test-20261114-010625-race.log`。初轮010235门禁因公开类型移入 sshdata 后反射期望仍用旧包名失败，更新 Go 类型名且保留所有 JSON 字段/标签后全仓通过；011216的可见性断言误用被无障碍 Label 覆盖的文本检索，改为校验实际错误槽、内容及边界后最终通过。这些失败日志不作为通过证据。
- 真实进程回归 `SSH_CLIENT_TEST_CLI=<包内CLI> SSH_CLIENT_TEST_DAEMON=<包内smallsshd> ... go run ./tools test -pkg ./internal/client -tags service_integration -run 'TestDaemonProcessSharesOneCoreAcrossFrontendsAndProxyProtocols|TestPackagedCLIAgentUsesDaemonAndLocksOnInterrupt' -race` 通过：`build/logs/test-20261114-010801-race.log`。六种协议组合（Shadowsocks、Snell、VLESS TLS、REALITY+Vision、AnyTLS、Hy2）实际通过同一服务执行 SSH 命令与 SFTP；两个管理端共用相同服务起始时间/内核代次，关闭一个后另一个仍可操作，明确锁定落审计锚点。额外用系统 script 创建私有伪终端，真实包内 CLI agent 经合成主密码解锁、复用服务并在 Ctrl+C 后确认锁定；没有向用户终端输入或打开窗口。测试通过认证 Stop 清理自己的守护进程。仅本机 macOS ARM64 执行，不代替其它 OS 的运行验收。
- 最终五目标编译通过 `build/logs/compile-20261114-011312.log`：darwin/arm64、darwin/amd64、linux/amd64、linux/arm64、windows/amd64。最终 macOS ARM64 打包 `build/logs/daemon-unified-build-final-20261114.log` 通过，包含外层重新封装签名和深层严格校验；生成文件检查 `daemon-unified-generated-check-20261114.log` 通过，原生组件哈希/密码补丁保持。最终产物位于 `build/darwin-arm64/SmallSSH.app`；普通文件合计 **58,497,329字节 / 55.79 MiB，未压缩**，相比本轮前同构建参数的102,582,609字节 / 97.83 MiB减少 **44,085,280字节 / 42.04 MiB**。桌面17,348,256字节、CLI7,302,050字节、守护进程29,123,442字节；兼容CLI入口仍为相对链接。大小与最终 SHA256 在 `build/logs/daemon-unified-package-20261114.json`。MyGo 的62.8MB输出是旧入口去重前，不能作最终包大小；没有安装包压缩大小或运行内存结论。
- 最终源码已知漏洞扫描 `build/logs/security-20261114-011312.log/.json` 为0个可达告警，打包守护进程扫描 `security-20261114-010919.log/.json` 为0个受影响符号告警；该守护进程 SHA256 `b82cdc6ad818ddb166ca1f6492284b27361ff91d066664186161755b8dd9a1b5` 与最终重包相同。官方快照 SHA256 `3b16a8d3ee29af03d9e8f78650c4286b9630569eb0542a93c4207e714c27ef11`，仍只覆盖数据库已知问题；二进制符号检查没有调用图，不证明未知漏洞或所有动态调用安全。
- README、CLI、DEVELOPMENT、ARCHITECTURE、REQUIREMENTS 和解锁页源码映射同步。SmallPen HTML/布局未改，保留用户现有服务及修订5ca7f049；没有重新导出静态图或新建实例。未访问真实保险库/服务器凭据，未安装、启动/前置/重启桌面App，未提交或推送；最终 `git diff --check` 通过。


## 2026-11-14 — 按用户请求启动并显示最新开发版

- 用户明确要求“跑起来我试一下”。先执行统一入口 dev status，确认默认开发数据目录没有运行实例，再执行 dev run；本机 smallsshd 已暂存，MyGo 完成最新 SmallSSH Dev 构建并保持监听。日志为 build/logs/dev-preview-20261114.log。
- 初始就绪状态 ready=true、visible=false、focused=false、activations=0；随后按本次明确请求执行 dev show，复查同一 PID18101/instance，ready=true、visible=true、focused=true、activations=1，证据 build/logs/dev-preview-status-20261114.json。未安装到 Applications，没有解锁、读取正式凭据或连接真实服务器；使用默认独立 development 数据目录。
- 只启动并显示供用户操作，没有修改产品源码，不重跑代码门禁；终端日志保留 MyGo 内置字符串尚无完整 zh-Hans catalog 的提示，不将其作为启动失败。既有工作与 SmallPen 服务保留。


## 2026-11-15 — 画布切换入口与规格位置复核

- 只读核对正式设置实现：设置 → 工作区 → 主工作区的「画布 / 标签聚合」分段选择直接调用布局切换；本轮未调整产品布局或切换行为。
- 用现有无窗口预览夹具运行工作区宽屏子用例，vet 与测试通过，证据为 `build/logs/test-20261115-140516.log`；实际组件离屏截图为 `build/snapshots/workspace-switch-20261115/settings-workspace-wide.png`。夹具为合成状态，不启动原生窗口、不读取真实凭据，不代表实机点击验收。
- 复核 SmallPen「08 设置 / 工作区」明亮主题，修订 `5ca7f0492ed3f58bdd9b225c802b0b856d7fb48b96b46db012ef27f358b009af`，已目视检查主工作区一行；现有在线服务返回同一修订且 projectionErrors 为空，状态证据为 `build/logs/workspace-preview-state-20261115.json`。CLI 部分对比度检查跳过，不宣称完整无障碍验收；未另起 SmallPen 服务。
- 规格入口仍为 `docs/REQUIREMENTS.md`（产品需求）、`docs/design/DESIGN.md`（UI 结构与设计约束）及 `docs/design/pages.json`（页面入口、操作、状态与源码映射）；本轮关于任务重复入口的讨论未写成已交付变更。


## 2026-11-15 — App 任务入口统一并同步规格与 SmallPen

- 正式 App 的任务入口统一保留在主窗顶栏；移除画布右侧任务快捷图标及底部 / 右侧工具区任务标签。画布与标签聚合模式共用显式任务弹层、计数和现有队列；已有任务卡片通过顶栏定位，返回停靠区时恢复为全局弹层。文件 / 命令 / 服务器状态的工具区与画布能力保留。设置 → 工作区 → 主工作区的分段切换原已实现，本轮未改布局模式或默认值。
- 更新现有画布交互测试，覆盖三个独立功能按钮、任务全局弹层开关、已有卡片定位及返回后队列保留。检查门禁 `go run ./tools check -pkg ./internal/desktop -serial` 通过，日志 `build/logs/check-20261115-141245.log`，包含 vendor 补丁、619 个 Go 文件格式、两个前端依赖图、vet 与桌面包全部默认测试。
- 首次显式 terminal_integration 预览在 vet 发现旧 Docker 测试夹具未适配 desktopSession（`build/logs/test-20261115-141335.log`）；仅将该夹具的后台对象包装为现有 testDesktopSession。随后 `check -pkg ./internal/desktop -tags preview_qa,terminal_integration -run '^TestProductionWorkspaceSnapshotsWithoutWindows/(canvas-home|canvas-tabs|canvas-right)-(light|dark)$'` 通过，日志 `build/logs/check-20261115-141413.log`。生成并逐张目视核对 `build/snapshots/task-entry-20261115/` 内 6 张正式组件离屏截图；使用合成状态，不启动系统窗口，不读取真实凭据或连接真实服务器。
- 更新需求总表、DESIGN、pages.json 和 SmallPen 的 25 个业务页 / 3 个目录页。最终结构检查 `build/logs/task-entry-design-check-final-20261115.json` 通过，修订 `94d96023ddfb75d77989cd6567e2fa41e6405b721f2c7d98ef1ecc770ab8417b`。画布、右侧工具区明暗渲染报告 `task-entry-design-canvas-20261115.json` / `task-entry-design-right-20261115.json` 使用 ce49cc33 修订，最终仅更新目录，两个业务页内容未变；首页最终修订报告为 `task-entry-design-home-final-20261115.json`。已目视核对最终修订画布明亮、右侧停靠深色稿；对比度检查有跳过，不宣称完整无障碍或 CLI/App 像素一致。
- 复用现有 SmallPen 在线服务，加载最终修订且 projectionErrors 为空，证据 `build/logs/task-entry-online-state-20261115.json`。未启动新服务、未代替用户点击原型导航。
- 本机 macOS arm64 App 重新打包通过，产物 `build/darwin-arm64/SmallSSH.app`，构建日志 `build/logs/task-entry-build-20261115.log`；包含打包签名及压缩后的深层签名校验。生成文件检查日志 `build/logs/task-entry-generated-check-20261115.log` 通过。未替换 /Applications 中的已安装版本，未启动或唤起桌面；开发实例状态检查未发现可连接实例。其它平台运行未在本轮验收。


## 2026-11-15 — 按用户请求安装并打开正式构建版

- 用户要求查看正式版，执行统一入口 `go run ./tools install`，将本轮 `build/darwin-arm64/SmallSSH.app` 安装到 `/Applications/SmallSSH.app`。安装过程校验应用身份、独立后台程序和深层严格代码签名；日志为 `build/logs/install-20261115-154127.log`。
- 旧版保留在 `/Users/smallraw/.Trash/SmallSSH-before-20261115-154127.992641000.app`，未删除应用数据。安装后的 Desktop、smallsshd、CLI 三个可执行文件 SHA256 与本轮构建逐一一致，证据为 `build/logs/installed-release-20261115.json`。
- 按用户明确的查看请求打开正式 App，并观察到 `/Applications/SmallSSH.app/Contents/MacOS/SmallSSH` 进程（PID 95760）；未启动开发版、未读取或输入凭据，未代替用户解锁或连接服务器。应用版本号仍为现有 `0.0.1`，这里的正式版指本机正式构建与安装，不声称已公开发布或完成全部 0.1 功能验收。


## 2026-11-15 — 内置 noVNC 基础远程桌面并安装正式构建

- 新增服务器菜单「VNC 远程桌面…」、目标 / 只读连接 Dialog、独立系统 WebView 窗口及 noVNC 工具栏。复用当前服务器的已验证所有者 SSH tunnel 操作与代理配置；主机指纹仍由后台验证。支持 VNC 登录提示、额外 RSA 身份确认、只读输入模式、适应窗口、Ctrl Alt Del、重新连接及断开。最多8窗；取消 pending、关闭、锁定、退出取消隧道与本机转接。需要已有 VNC 服务，不自动安装、不包含 RDP / 自动剪贴板 / 主区 Tab 或画布卡片。原生表单进入现有中英文目录，首版网页客户端为简体中文。
- 固定官方 noVNC 1.7.0 commit `63107bd06d9e1f6136ff21aeda8cd62cbf0d433e`；归档 SHA256 `06dac239be3973ed79a00e357a68aab996e6ed2fd496ee2cc6e6a34020344ac9`，来源证据 `build/logs/novnc-source-20261115.json`。63个上游源码 / 许可文件保持不修改，逐文件哈希保存在 `internal/vncview/web/novnc/ORIGIN.json` 并由测试校验；资源总量616227字节。未运行 npm 安装或远端安装脚本，不依赖 CDN 或外部 websockify。Go 沿用已有 ws 模块版本，仅改为直接依赖，未重建 vendor 或改动密码字段补丁；前端依赖守卫对 ws / wsutil 作精确例外，仍拒绝 SSH / 代理内核。正常构建生成完整 noVNC / pako 许可文本。
- 全仓库门禁 `go run ./tools check -serial` 通过，权威日志 `build/logs/check-20261115-173735.log`，副本 `build/logs/novnc-full-check-final-20261115.log`。覆盖 vendor 补丁、格式、前端依赖图、vet 及全部默认测试。新菜单期望、中英文目录与测试中会被就地 mask 的输入副本均已修正。最终网页登录区只另加了可滚动布局，随后用最终浏览器回归及正式构建覆盖；未把此前失败日志记为通过。
- VNC 模块的 Host / Origin / 回环目标限制、二进制与分片帧、ping/pong、owner 取消和上游资源哈希的 race 回归通过：`build/logs/test-20261115-173016-race.log`。Desktop 真正经合成 SSH 服务器完成 TCP/RFB 字节往返、锁定释放与 pending 取消的 race 回归通过：`build/logs/test-20261115-173015-race.log`。只使用合成凭据与 loopback 服务。
- 真正运行内置 noVNC JS 的 opt-in Chrome headless 回归（独立临时配置，不使用个人浏览器配置或系统窗口）通过：`build/logs/test-20261115-174227-race.log`。覆盖 RFB3.8 / VNC密码正确与失败、画布实际RGB像素、Ctrl Alt Del抵达服务器、只读控件及结束转接后断开。不是一张静态网页截图冒充VNC连接；该回归验证Chromium，不等同WKWebView、WebView2及WebKitGTK全平台运行验收。
- 正式组件离屏 Dialog 预览通过：`build/logs/test-20261115-173736.log`。逐张核对 `build/snapshots/novnc-20261115/` 下的 vnc-dialog-light/dark 与 viewer-light/dark 截图；浏览器蓝色帧是合成RFB像素，不是真实用户桌面。
- SmallPen 新增连接 / 登录 / 远程桌面三个页面并更新页面地图7、需求、能力表、DESIGN、pages.json 和开发说明。最终修订 `75a16cda10ad1bf6764a6f5541d0f90daa1248720d5bb556f4fee3301f1b9aab`，81页；结构检查 `build/logs/novnc-design-check-final-20261115.json` 通过。三个页面明暗渲染报告为 `novnc-design-connect-render-current-20261115.json`、`novnc-design-viewer-render-current-20261115.json`、`novnc-design-login-render-final-20261115.json`，均无报告问题，仍有对比度检查跳过。已目视核对连接 / 登录明亮稿及窗口深色稿，复用在线服务加载同一修订且 projectionErrors 为空（`build/logs/novnc-online-state-20261115.json`）；没有另开 SmallPen 服务或声称原型导航已点击验收。
- 五桌面目标编译通过：`build/logs/compile-20261115-173932.log` / `novnc-compile-final-20261115.log`。已知Go可达漏洞扫描为0：`build/logs/security-20261115-173511.json` / `.log`，官方数据库归档SHA256 `714ff02d9533ec6ca269e565b61b27cbd959aa0af89d51f318261322e875861c`；范围是Go已知问题，不包含JS、系统WebView或未知问题的全面保证。
- 最终 macOS arm64 包构建与签名验证通过：`build/logs/novnc-build-shipping-20261115.log`；生成文件检查通过：`build/logs/novnc-generated-check-20261115.log`。包为59684791字节（56.92MiB，未压缩、排除符号链接重复计数），三入口 SHA256 / 安装一致性为 `build/logs/novnc-installed-package-20261115.json`。后台及CLI二进制哈希与上一版相同，不重新编入代理内核。
- 通过 `tools install` 安装到 `/Applications/SmallSSH.app`，证据 `build/logs/install-20261115-174902.log`。旧版保留在 `/Users/smallraw/.Trash/SmallSSH-before-20261115-174902.476032000.app`。未停止当前用户正在运行的旧App和后台、未修改用户数据或解锁 / 连接真实服务器；当前旧窗口需用户正常重启App才显示新入口。实际用户VNC服务与各系统WebView仍待实机验收。
- 最后检查发现上游许可文本原有的行尾空格进入生成的通知文件；保留上游文件及其哈希不变，仅让 noVNC 许可生成采用既有 Go 依赖相同的行尾空白规范化。tools 包门禁通过（`build/logs/novnc-tools-check-final-20261115.log`），并重新构建、签名、安装及复核生成文件；不把格式清理伪装成上游源代码修改。


## 2026-11-15 — 顶部创建连接与终端入口移回左侧

- 用户指出截图中＋与新建终端图标被放到右侧。根因是两个控件位于 Grow(1) 的工作区标签之后。正式Go将它们移到侧栏开关之后、标签之前，并将左侧留白计算计入三个32宽常规操作。顶栏高度、原生窗口控制避让、标签内缩、右侧任务 / 工具 / 设置及已有点击回调保留；侧栏收起时也能访问创建入口。
- 扩展既有创建连接 / 取消状态保留测试至560与1280宽、侧栏展开与收起4种组合，并检查创建入口位于侧栏开关和顶部标签之间。Desktop包门禁通过：`build/logs/check-20261115-181653.log`（副本 `header-create-left-check-20261115.log`），包含vendor补丁、gofmt、前端依赖图、vet和桌面包全部默认测试。
- 现有 opt-in 无窗口预览通过：`build/logs/test-20261115-181852.log`；逐张目视核对 `build/snapshots/header-create-left-20261115/` 内宽窗明暗、560英文窄窗及紧凑侧栏4张图。使用合成缓存，不启动系统窗口或连接真实服务器。
- 同步25张 SmallPen 业务页、pages.json 的入口 / 动作及相应目录，更新需求和 DESIGN。修订 `caf6332dd930785ab963581971997bce0fcfbbfe7831dbbd2e43ed4700966e19`，结构检查 `header-create-left-design-check-20261115.json` 通过；首页与未选服务器页明暗渲染报告为 `header-create-left-design-home-20261115.json` / `header-create-left-design-empty-20261115.json`，无报告问题，部分对比度检查跳过。已目视核对首页明亮稿，未声称全部页面 / 原生与稿子像素一致。现有 SmallPen 服务已加载同一修订且 projectionErrors 为空：`header-create-left-online-20261115.json`，未另起服务。
- 本机 macOS arm64 构建与签名通过：`build/logs/header-create-left-build-20261115.log`；生成文件检查通过：`header-create-left-generated-check-20261115.log`。通过统一 install 任务更新 `/Applications/SmallSSH.app`，安装与严格签名证据为 `build/logs/install-20261115-182045.log`；Desktop / daemon / CLI 与构建逐一哈希相同：`header-create-left-installed-20261115.json`。旧版保留在 `/Users/smallraw/.Trash/SmallSSH-before-20261115-182046.121676000.app`。未重启或前置当前旧进程，未更改用户数据；用户正常重启App后才能在旧窗口中看到此轮变化。其它平台本轮未运行验收。


## 2026-11-15 — 移除顶栏分栏图标与整理任务历史

按用户选择，已完成传输默认隐藏在历史，重开任务中心回到当前任务；暂停 / 失败保留继续 / 重试。任务中心固定使用传输 / 服务器操作分类，当前任务与历史分别展示；完成行支持单条移除与清空记录。关闭按钮、Escape 或点击外部只收起面板，不暂停任务。清理只操作私有传输元数据，不根据任务中的本机 / 远端路径删除文件，不改变运行任务、服务或审计。文件任务删除持久化；服务器操作仍为原有会话内存记录。异步清理有代际检查，并以版本计数阻止旧轮询结果重新显示已移除历史。

顶栏及组头删除独立分栏按钮，标签右键菜单保留画布 / 标签聚合、新建终端到另一组、移组与合并；点击目标标签仍固定原会话 / 工具。现有名称 / 预留宽度断言跟随入口更新。另修正多文件窗离屏拖放夹具：虚拟表的 Find 返回整行，响应式列出现后整行中心可能落进元数据列，改为从文件名列起拖，补充缺失元素断言；没有改变文件拖放生产逻辑。

验证证据：

- 完整门禁 `build/logs/check-20261115-205852.log` 通过：vendor 补丁完整、632个 Go 源文件格式、前端依赖图、全仓 vet / 测试。首次门禁暴露旧入口断言和上述拖放坐标问题，修复后本次全绿；未把失败或跳过计为通过。
- 传输元数据 / race 回归 `build/logs/test-20261115-205210-race.log`：移除、重复移除、重新加载后保持删除、清空保留未完成及被锁记录、目录子元数据和 symlink 不触及下载目标、非法ID / 链接目录拒绝。全仓门禁另跑传输完整测试。
- UI / 真实合成 SSH 回归 `build/logs/test-20261115-205535-race.log`：默认隐藏、单条移除 / 清空及文件保留、服务任务历史、标签右键呈现切换、独立授权 PTY 新分组、Compose 后台任务详情；合并保留连接等回归在 `build/logs/test-20261115-205403-race.log`。无生产凭据、无窗口启动。
- 六张原生组件离屏图在 `build/snapshots/transfer-history-20261115/`，日志 `build/logs/test-20261115-205613.log`。已目视当前任务、明暗历史、英文、空历史及560窄窗；不声称全部尺寸、OS实机或逐像素验收。
- 五目标编译 `build/logs/compile-20261115-205613.log` 通过；Windows / Linux 的执行验收仍未覆盖。
- SmallPen 25张业务页移除图标，重做任务弹层并增加历史页，同步需求、FEATURES、DESIGN、pages.json及页面地图5。修订 `b0a1501f0eb3f52591c6c5252d6bf2b0325f7c5f889854601a75f64da3f62ac7`，82页 / 17组件 / 54变体 / 142 Token。`build/logs/transfer-history-design-check-20261115.json` 和 `transfer-history-design-{current,history}-render-20261115.json` 结构及明暗渲染0问题；中间当前页曾有4px内容溢出，补齐高度后重新检查，最终0问题。部分对比度跳过，coverage.complete=false。已查看最终当前页明亮 / 历史深色 PNG，view元数据为同名前缀的 current-view / history-view JSON。旧在线端口拒绝连接、进程清单无服务，本轮未另起实例，不能宣称在线工作区已加载；记录 `build/logs/transfer-history-online-state-20261115.json`。

正式 macOS 本机构建 `build/logs/transfer-history-build-20261115.log` 与生成文件检查 `build/logs/transfer-history-generated-check-20261115.log` 通过。`build/logs/install-20261115-210118.log` 将签名验证后的包安装到 `/Applications/SmallSSH.app`，旧版在 `/Users/smallraw/.Trash/SmallSSH-before-20261115-210119.112226000.app`。安装包共59,717,975字节（排除符号链接重复），主程序、CLI、smallsshd与构建产物哈希一致，证据 `build/logs/transfer-history-installed-20261115.json`；smallsshd仍为 b82cdc6ad818ddb166ca1f6492284b27361ff91d066664186161755b8dd9a1b5。本轮未启动、前置或重启正式 / 开发应用，当前旧窗口需用户正常退出重开后更新。测试与签名结论只针对本机产物，不代表全平台运行或发行签名验收。


## 2026-11-15 — 按常规使用逻辑复核界面

- 复核方式：先用现有无窗口生产预览渲染58张明暗图（`build/snapshots/ux-review-20261115/before/`，日志 `build/logs/test-20261115-204342.log`），逐项对照桌面应用通用习惯并阅读菜单、标签条、设置、命令与文件列表源码。未启动、前置或安装 App，未读取真实凭据或连接服务器。
- 改动：①快捷键——新建终端标签 macOS Cmd+T / 其他 Ctrl+Shift+T；「视图」新增上一个 / 下一个标签（macOS Cmd+Option+← / →，其他 Ctrl+Shift+Tab / Ctrl+Tab），按顶部顺序循环、只激活不改布局，模态提示或其他窗口聚焦时不动作；「显示 / 收起连接侧栏」（macOS Ctrl+Cmd+S）；macOS 设置移入应用菜单「设置…」（Cmd+,），其他系统留在「安全」菜单且不占 Ctrl 组合。顶栏按钮提示附快捷键。②顶部标签多于一个时按可用宽度均分（最小72、最大210）再滚动，单标签规则不变。③设置分段选择改用强调色浅底 / 文字标出当前项（新增 `components/segmented.go`）。④文件列表修改时间列按列表实测宽度（≥380）显示，取代按容器类型判断；首帧沿用旧规则。⑤命令空列表加「新建命令」，筛选无结果加「清除搜索」。新增中英文目录6项。
- 新增回归 `internal/desktop/workspace_shortcuts_test.go`：平台按键表（含 Windows / Linux 无裸 Ctrl 字母）、菜单快捷键唯一且设置走应用菜单、标签/画布两种模式的循环与环绕且布局不变、标签宽度函数、640宽窄窗至少两枚长标签完整可见、命令空态两个动作、文件列表宽窄两档的修改时间列、分段选中项像素可区分。定向测试 `build/logs/test-20261115-205618.log`、`test-20261115-210204.log` 通过。
- 全仓库门禁 `go run ./tools check -serial` 通过：`build/logs/check-20261115-205657.log`（vendor 补丁、632文件格式、两个前端依赖图、vet、全部默认测试）。此前 `check-20261115-205644.log` 因 `-pkg` 传入含空格的多包参数未匹配任何包而失败，不作为证据。
- 修改后离屏图 `build/snapshots/ux-review-20261115/after/`（`test-20261115-210204.log`）：已目视核对设置明暗分段、底部停靠文件列表出现修改时间、560宽画布顶栏由1枚标签变为2枚、文件大标签下的命令空态按钮。
- 同步 `docs/design/pages.json`（终端、设置、工作区设置、常用命令、文件列表的入口 / 操作 / 状态 / 源码）、`docs/FEATURES.md` 与 DESIGN 同日小节。**未重绘 SmallPen 业务页**，停靠区文件列表、设置分段及命令空态仍为旧稿，留待下一轮按 SmallPen 流程补齐。
- 只在本地成立：原生菜单的按键分派（特别是 macOS 上 Cmd+Option+方向键、Ctrl+Cmd+S 与应用菜单「设置…」的位置）只验证了菜单表与动作逻辑，未在实机菜单栏按键验收；Windows / Linux 的 Ctrl+Tab 未实机验证。
- 本轮期间另有进程并行修改 `internal/desktop`（任务历史、顶栏分栏图标、标签条宽度参数等）及文档；本轮改动按当时磁盘内容做最小替换，`TestMultipleFileWindowsTransferCompletionAndDropUseExactTargets` 曾因文件行新增修改时间列在对方运行中失败，对方已把拖动起点改到文件名列，复跑通过（`build/logs/test-20261115-205838.log`）。未提交、未改 vendor。


## 2026-11-15 — 常规使用逻辑复核：SmallPen 与页面地图同步

- 接续上一节（当时注明未重绘 SmallPen）。按当前安装版 `smallpen-ui-design/SKILL.md`（CLI 0.2.0）直接编辑包文件，每次写入后 `smallpen check`。基线修订 `b0a1501f0eb3f52591c6c5252d6bf2b0325f7c5f889854601a75f64da3f62ac7`，最终修订 `8eff6125b670ac65124df4d349f1670e9f794ea1bd1c58b7a8fd7433354a8136`；83页 / 17组件 / 66变体 / 144 Token，0 error，2 个 warning 均为既有「09 工具」任务页的 typography_overridden，非本轮文件。结构检查证据 `build/logs/ux-review-design-check-20261115.json`。
- 改动：新增 Token `radius-segment`、`spacing-segment-inset`；「设置行」新增「控件: 分段」「选中: 前项 | 后项」，其它设置行仅获得默认维度，外观不变；「08 设置 / 工作区」停靠位置、主工作区改为分段（默认底部、画布）；「02 工作台 / 终端与底部文件」「工具停靠底部」文件表头与6行由紧凑改为完整列，右侧停靠仍紧凑；新增「05 命令 / 命令空态」（工具栏 ＋ 图标、空区说明与「新建命令」，两处均连到「编辑命令模板」）；页面地图 1 / 3 / 4 / 5 的 terminal / settings / settings-workspace / files / commands 条目按 pages.json 逐字段同步，pages.json 增加该页映射。`smallpen changes --since` 基线核对只含上述改动。脚本复核 pages.json 每个 design_pages 都存在，且 83 页中业务页均已映射。
- 渲染：上述 4 张业务页与 4 张页面地图以 `--all-themes` 渲染检查，均 0 个布局 / 文字问题；工作区设置页另有 2 条 info 级 possible_overlap（设置导航与原生窗口按钮，未改动区域）。报告 `build/logs/ux-review-design-render-01…08-20261115.json`，组件「设置行」全部变体 `ux-review-design-render-component-20261115.json`（0 问题）。业务页 text-contrast 仍有跳过项，coverage.complete=false，不宣称完整对比度验收。
- 目视：CLI PNG 已看过工作区设置明暗、终端与底部文件明亮、命令空态明暗，与正式 App 离屏图一致（分段当前项强调色、底部停靠出现修改时间、空态按钮）。命令空态首稿工具栏沿用了「命令列表」的文字主按钮，与空区按钮重复，已改为与 App 一致的 ＋ 图标；「命令列表」页本身的文字按钮与 App 的 ＋ 图标仍有既有差异，未改。
- 在线工作区：本轮检查时没有运行中的 SmallPen 服务（无进程，原 58766/58763 端口无监听）。按 AGENTS 未另起服务，因此**未做在线工作区加载与 projectionErrors 核验**，也未在 App 中点击原型链接。
- 并行进程结束后重跑全仓库门禁通过：`build/logs/check-20261115-211527.log`（vendor 补丁、632 文件格式、两个前端依赖图、vet、全部默认测试，desktop 37.5s）。仍未实机验证原生菜单按键分派；未提交、未改 vendor、未启动或安装 App。


## 2026-11-15 — 侧栏 Enter 打开终端与命令新建入口统一

- 用户确认“全都改”上一节留给用户决定的两项。①服务器侧栏获得焦点并选中服务器时，Enter 与双击相同：经 `navigate` 的未保存连接守卫，`switchHost` 后 `openShellMode(true)` 每次打开新终端；分组标题不响应，方向键 / Home / End 行为不变（`internal/desktop/connection_rail.go`）。原「Enter 不连接」断言改为「Enter 进入新终端授权路径」，并新增脏表单下 Enter 弹出「放弃未保存的连接？」且不建会话的回归。定向测试 `build/logs/test-20261115-212201.log` 通过。②SmallPen「05 命令 / 命令列表」工具栏文字「新建命令」改为与 App 一致的 ＋ 图标按钮，连到「编辑命令模板」；全包仅「命令空态」空区保留文字「新建命令」。
- SmallPen：`changes --since 8eff6125…` 在编辑前确认无外部改动；最终修订 `5c3f799dcfd884e2d2a1c65d923040f5520c3cfb48c4fa16a565627974a11d88`，83页 / 17组件 / 66变体 / 144 Token，0 error（2 warning 仍为既有 09 工具页），证据 `build/logs/ux-review-design-check-final-20261115.json`。命令列表与页面地图 1 明暗渲染 0 问题：`ux-review-design-render-09-20261115.json`、`ux-review-design-render-10-20261115.json`（业务页对比度部分跳过）。已目视命令列表明亮 PNG。pages.json 与页面地图 1 的 server-rail 操作同步 Enter 行为。
- 文档：REQUIREMENTS 记录本次用户确认，FEATURES 快捷键行与 DESIGN 同日小节已更新。
- 全仓库门禁通过：`build/logs/check-20261115-212256.log`（desktop 37.96s）。用户确认设计验收以 SmallPen CLI 的 check / check --render / view 为准，本轮及上一节不再需要在线工作区加载与 projectionErrors 核验。仍未做原生菜单按键实机验收；未提交、未启动或安装 App。


## 2026-11-15 22:16 — 按用户要求重新编译并替换本机 App

用户明确要求编译 App 替换现有版本。本轮未修改业务源码；构建前确认没有 Go 源码晚于最近完整门禁日志 `build/logs/check-20261115-212256.log`（全绿），因此复用该门禁，不重复全量测试。通过统一任务重新构建本机 darwin/arm64：`build/logs/app-replace-build-20261115.log`；生成文件校验通过：`build/logs/app-replace-generated-check-20261115.log`。

安装任务通过深层严格签名校验后替换 `/Applications/SmallSSH.app`，日志 `build/logs/install-20261115-221605.log`。旧版保留在 `/Users/smallraw/.Trash/SmallSSH-before-20261115-221605.940927000.app`；安装后的主程序、CLI、smallsshd 均与本轮构建产物 SHA256 一致，详情 `build/logs/app-replace-installed-20261115.json`（替换前摘要在同名前缀 before JSON）。未启动、前置或重启应用，用户正常退出重开后使用新版；本轮只核验本机打包 / 安装，不据此声称其它系统实机运行通过。


## 2026-11-15 — 本机安装不再保留旧版备份

用户明确表示“不需要备份”。`tools install` 默认不再向废纸篓保留旧 App，也不保留验证失败的候选包。签名与 bundle ID 校验仍在替换前完成；原包只在重命名事务期间临时保留，成功后删除，发布失败则还原；发布与还原同时失败时仍报告原包临时位置以便恢复，避免删除唯一可用应用。同步 DEVELOPMENT 的当前安装说明；不改变产品内配置 / 文件的备份需求。

`go run ./tools check -pkg ./tools/...` 通过，证据 `build/logs/check-20261115-222606.log`：vendor 补丁、633文件格式、前端依赖图、tools 全部 vet / 测试。新增真实临时目录回归覆盖首次安装、替换后只剩新版、候选缺失时还原原包、恢复临时位置被占用时拒绝且不改任何包；未故障注入“发布及还原同时失败”分支。不涉及应用业务源码，未重复全仓应用测试。

用与当前 App 相同的已签名构建产物执行新安装流程，通过严格签名校验并替换到 `/Applications/SmallSSH.app`，日志 `build/logs/install-20261115-222632.log`；安装前后废纸篓旧包集合确认无新增备份。按用户此次要求，仅删除紧接上一轮生成的 `/Users/smallraw/.Trash/SmallSSH-before-20261115-221605.940927000.app`（先核对普通目录、bundle ID）；没有批量清理其它历史备份。上节列出的这份备份路径现已不存在。三份安装可执行文件均与构建产物 SHA256 一致，证据 `build/logs/no-backup-install-verification-20261115.json`，安装前清单为 `no-backup-install-before-20261115.json`。未启动、前置或重启应用；实际安装结论限本机 macOS。


## 2026-11-15 22:33 — 再次直接替换电脑上已安装的 App

按用户“我电脑得替换一下”，确认打包产物覆盖当前应用源文件修改（没有比22:15:58主程序更新的应用源码），通过统一 install 任务重新替换 `/Applications/SmallSSH.app`，不保留备份。严格签名校验及安装通过：`build/logs/install-20261115-223324.log`；主程序、CLI、后台三份 SHA256 与构建产物相同，证据 `build/logs/desktop-app-replace-20261115-223324.json`。未改业务源码，沿用最近应用完整门禁 `build/logs/check-20261115-212256.log` 和安装工具门禁 `build/logs/check-20261115-222606.log`，不重复测试。现有应用进程 PID95760 启动于15:41:49，早于本轮产物，安装文件替换不等于该旧进程已更新；没有终止、重启或前置窗口，用户退出重开后加载新版。结论仅限本机安装与文件一致性。


## 2026-11-15 — 任务中心：进行中可取消、失败可移除

- 用户指出“正在进行的也得有取消，陈年老错得能删除”。原任务中心只能暂停 / 继续，失败项只有「继续」且永久留在当前任务，标题只数进行中。本轮未改授权中心：其待处理可拒绝、活动可撤销，历史属于审计，保持不可删除。
- 传输后端（`internal/transfer/history.go`）：新增 `Discard`（移除任何未运行的顶层任务）、`Cancel`（经暂停标记停止本进程或其他前端中的运行，等待其释放任务锁后再 Discard，超时不删）、`ClearFailed`；`RemoveCompleted` 行为不变。未完成下载只通过 os.Root 删除本任务创建的私有 `.名称.ssh-part-ID` 部分文件及目录任务标记；非私有 / 非普通文件的部分文件不删并保留记录；已完成文件、上传源、服务器内容不动，上传取消不删除服务器上的 `.ssh-part-ID`（无对应安全接口，已在提示中说明）。
- 界面：当前任务行显示状态（百分比 / 已暂停 / 红色失败 / 正在取消…）与下一步——运行中「暂停」+×取消，已暂停「继续」+×取消，失败「重试」+×移除；未传输的失败行不显示 0 B 空进度，已暂停说明改为普通灰字；有失败时出现「清除失败」；标题列出进行中 / 已暂停 / 失败数。服务器操作行内可「停止等待」，失败记录可移除。暂停 / 取消不再把“context canceled”写进状态栏。新增中英文目录 14 项。
- 测试：传输后端 7 项（失败下载只删自身部分文件、非私有部分文件不删、运行中拒绝 Discard、Cancel 等待运行停止、超时不删、目录任务删未完成子部分文件并保留已完成文件、ClearFailed，另有真实合成 SSH + broker 下由另一实例取消等待批准中的下载：运行停止、授权失效、部分文件与记录删除）`build/logs/test-20261115-224918-race.log`、`test-20261115-225728-race.log`；界面 2 项（暂停任务取消、单条移除、清除失败；服务器操作行内停止与移除）及既有任务中心回归 `build/logs/test-20261115-225353.log`；原「继续 / 重试」断言改为新标签。
- 离屏图 `build/snapshots/task-cancel-20261115/`（`build/logs/test-20261115-225440.log`），已目视当前任务宽窗与 560 窄窗。SmallPen「09 工具 / 传输与任务中心」按新行结构重绘，pages.json 与页面地图 5 同步，修订 `6648e3f57010dc14cb75d7bfda1f304b174ed55716a032f7c6fc2051cd2149f0`，0 error；该页与页面地图 5 明暗渲染 0 问题（`task-cancel-design-render-20261115.json`、`task-cancel-design-map5-render-20261115.json`，部分对比度跳过），已目视 CLI PNG。
- 全仓库门禁通过：`build/logs/check-20261115-225700.log`（desktop 58.5s，transfer 5.5s）。真实取消用例在门禁启动后加入，门禁是否编入不确定，以上述单独 race 日志为准。未提交、未启动或安装 App。


## 2026-11-15 — 全应用“进行中可停、失败可清”复核

- 范围：按用户“其他的呢？所有的都过一遍”，在 `internal/desktop` 中枚举全部转圈 / 进度条 / “正在…”状态（约 40 处）及全部红色错误文字（约 25 处），逐个核对是否有停止 / 取消，以及结束后能否关闭或移除。已有出口的保持不变（连接表单读取身份 / 连接时可取消、编辑窗取消读取、Compose 预览与 VNC 取消、运行中隧道停止、全局提示与服务操作结果可关闭、授权中心拒绝 / 撤销；审计历史不删除）。
- 补齐：文件夹读取加「取消读取 / 上一级」；连接中占位卡片按钮改为「取消连接」，卡片内服务器与错误文字居中；服务器操作执行中「停止等待」（只停本机等待，提示服务器可能仍完成；区分用户停止与失败，修正首版把所有失败误报为停止的问题）；已停止 / 失败隧道可移除、失败红色；连接表单路径实测 / 硬件读取中按钮变为「停止测量 / 停止读取」且不关闭表单，主按钮只在读取身份 / 保存 / 连接时显示忙碌；运行与后台页改用本页 `saveError`，不再把全局状态（如“传输完成”）显示成红色错误，且可关闭。
- 测试：`internal/desktop/stuck_states_test.go` 5 项（文件夹取消与上一级、连接中取消 / 失败关闭、隧道移除且运行中不可直接移除、服务操作停止等待、表单两类操作停止不关表单），运行与后台测试补充可关闭与全局状态不串入；`build/logs/test-20261115-232851.log`、`test-20261115-233030.log`、`test-20261115-232723.log`。
- 全仓库门禁 `build/logs/check-20261115-233046.log` 通过；其后仅连接中卡片文字居中，desktop 包门禁 `build/logs/check-20261115-233609.log` 通过。离屏图 `build/snapshots/stuck-states-20261115/`（`test-20261115-233557.log`），已目视连接中 1100、失败 560 与目录下载任务行。
- SmallPen：「09 工具 / SSH 隧道」加入失败记录行（×移除），页面地图 1–5 与 pages.json（files、terminal、tunnels、status-services、settings-presence、connection-general）同步；pages.json 以原有 2 空格格式整体写回，HEAD 版本同格式，未引入格式差异。修订 `5021adccc8d1f46fbad4b0cdec252a85fef8aefaebe736d05948da0b4415e34b`，0 error；隧道页与页面地图 1–5 明暗渲染 0 问题（`build/logs/stuck-states-design-render-*.json`，部分对比度跳过），已目视隧道页 PNG。文件夹读取中未单独出稿。
- 未覆盖：上传取消后服务器 `.ssh-part-ID` 仍需手动清理；服务器操作“停止等待”不能撤回已发出的远端命令；未提交、未启动或安装 App。


## 2026-11-16 — 快速解锁改为后台内存保存（方案 D）与加固运行时

- 起因：安全复核发现旧版 `device-unlock.json` 放在数据目录，任何同用户进程都能读取并让系统以自己写的提示文字弹出 Touch ID，按一次即得保险库密钥；CLI `--biometric approve` 还能在其控制的伪终端里自批申请。用户选择方案 D。
- CLI：`--biometric` 与 `smallssh biometric …` 停用并报错，所有者操作只读终端主密码；`cmd/ssh-client/biometric_retired_test.go`（`build/logs/test-20261116-001158.log`）。
- 本地服务协议 1.2（`internal/service/quick_unlock.go`、`internal/client/service_quick_unlock.go`）：只有所有者控制连接可 `depositQuickUnlock`，记录只在后台内存、绑定存入者的内核 CodeID，`quickUnlockStatus/Record/Clear` 只对同一 CodeID 生效；锁定后保留，48 小时、后台退出或关闭即清除；所有者连接上存入失败按预期动作回复，不结束所有者会话；新字段按秘密清理并纳入客户端“响应不得带秘密”校验。测试：跨锁定保留、取回、清除、超长被拒且会话仍在、非所有者存入被拒（`test-20261116-001703-race.log`）；CodeID 不同 / 进程不可验证 / 过期不释放（`test-20261116-001811-race.log`）；协议版本与字段（`test-20261116-001834.log`）。
- `systemauth`：`Seal / OpenRecord / OpenRecordEmbedded / Discard / RemoveLegacy` 取代基于文件的 `Enroll / Open / OpenEmbedded / Disable`，平台后端不变；Linux 每次封装前清除本应用旧钥匙串条目。原测试意图（取消不放行、每次重新验证、错钥匙拒绝、取消后迟到成功不释放、记录长度界限、嵌入式验证与回退）移植到新接口，另测旧文件删除与钥匙串清理（`test-20261116-002051-race.log`）。
- 桌面：主密码解锁 / 新建保险库后（设置开启且系统验证可用时）封装并经所有者会话存入；系统解锁与审批从后台取回记录再用窗口内系统验证解开，提示写明“AI 请求仍需逐条批准”或具体申请；解锁成功与锁定后刷新状态；启动时删除旧文件；设置“用 Touch ID 快速解锁”默认开，关闭无需密码并立即清除，开启 / 立即启用需主密码；偏好保存失败不再被吞掉。测试 `test-20261116-002851-race.log`（主密码解锁封装并跨锁定保留、关闭清除、封装条件、不可用回落主密码），设置 / 解锁 / 审批 / 审计相关回归 `test-20261116-002618.log`；中英文目录 `test-20261116-002933.log`。
- 构建：`smallsshd`、CLI、`biometric-helper`、认证动态库启用加固运行时；桌面主程序同样启用并仅放宽库校验（`native/macos/Desktop.entitlements`）以加载包内临时签名的终端 / 认证库。macOS arm64 包 `build/logs/quick-unlock-build-20261116.log`：上述各项 `flags=0x10002(adhoc,runtime)`，`libghostty-vt.dylib` 保持其链接签名，`codesign --verify --deep --strict` 通过；生成文件检查 `quick-unlock-generated-check-20261116.log` 通过，`native-hashes.json` 随新签名更新。
- 全仓库门禁 `build/logs/check-20261116-003020.log` 通过；五目标编译 `build/logs/compile-20261116-003444.log` 通过。
- 文档：SECURITY（快速解锁与加固运行时、仍不防 root / 改包 / Windows-Linux 内存）、ARCHITECTURE（协议 1.2）、CLI、USAGE、FEATURES、REQUIREMENTS、DESIGN；SmallPen「08 设置 / 安全与解锁」改为快速解锁已就绪态，pages.json 与页面地图 3 同步，修订 `6c26c81d662fc9a27185df109a9d37a3dd04e837956e8b354d345b91269f3e9b`，0 error；该页明暗渲染仅 2 条既有 info 级导航与窗口按钮重叠，页面地图 3 为 0。
- **未验证（需用户在本机实测）**：按 AGENTS 未启动或安装构建版，因此加固运行时下桌面能否正常启动、加载终端库与 WebView、真实 Touch ID 弹框、“主密码解锁 → 锁定 → 指纹解锁”与“指纹批准 AI 请求”均未实测；Windows Hello / Linux fprintd 只编译未运行。未提交、未安装。


## 2026-11-16 — Repository audit and verified repairs

Reviewed the current working tree, including its substantial pre-existing edits. The review covered first-party CLI/build/CI, vault/routes, service/IPC/authorization/audit, SSH/proxy/transfers, monitoring/native authentication/file attributes, desktop lifecycle/editor/settings, localization, and design-map consistency. Production paths received the main attention; tests were sampled and the default suite was run. This is a broad subsystem review, not an exhaustive line-by-line audit of every test, vendored dependency, native QA fixture, or noVNC implementation. Existing user edits were preserved; this round's isolated delta is in `build/logs/repository-audit-20261116.patch`, with its file inventory in `build/logs/repository-audit-files-20261116.json`. Source snapshots are in hidden `build/.audit-baseline/` so Go package discovery ignores them.

**Repairs made**

| Area | Verified problem and resulting behavior |
|---|---|
| Windows monitoring | Reject negative, zero, malformed, overflowing, or >4096 CPU counts before allocating. Remote sampler output can no longer panic the daemon or request an unbounded core slice. Regression: `TestParseWindowsRejectsInvalidCoreCounts`. |
| Linux quick unlock | Removed service-wide Secret Service deletion from `Wrap`; one vault no longer deletes other live vaults' device keys. Explicit per-record cleanup remains. Linux runtime verification is still required. |
| Tunnel lifecycle | Lock/shutdown cancel and clear tunnel rows and form state. Previous-generation completion callbacks no longer leave permanently running rows that consume the 32-tunnel limit. |
| Idle lock | Start a fresh idle-lock clock at every unlock and clear it on lock. Rapid lock/unlock between watcher ticks cannot inherit an expired clock. |
| Editor rendering | Count saved bytes from buffer length/line metadata, including CRLF expansion, instead of allocating/serializing the whole document every rendered frame. LF/CRLF/Unicode/editing regressions compare the count with actual saved bytes. |
| Relay dependencies | Reject deleting an inline relay still used by another connection. `Put`, `SaveProfile`, and `SaveRoute` validate candidate graphs so edits cannot invalidate previously working indirect routes. Changed profiles/routes must themselves resolve; unrelated already-broken legacy routes can still be repaired one at a time. Rejected changes preserve memory and disk. |
| Audit recovery | Hash JSON separately from a repaired newline separator; skip malformed/oversized crash debris while preserving predecessor checks; allow the exact maximum record size; include separator bytes in rotation limits. Reopening and anchor lookup after recovery work, while malformed linked middle records still fail verification. Tests include rotation and exact retention boundaries. |
| CLI cleanup | Removed unreachable duplicate get/put implementations and their dead file-output cleanup; these commands already return through the resumable transfer manager. `replace` retains its streaming behavior. |
| Release workflow | Added `go run ./tools archive -platform ...` with atomic tar.gz output under `build/dist/`, preserving symlinks/execute bits. Publisher uses the same task and passes platform input through an environment variable instead of interpolating it into shell source. Archive regressions cover bundle contents, symlink preservation, permissions, and failure preserving an existing archive. |
| Documentation | Corrected install help, the current 83-page design summary, the Go-version source of truth, dependency-upgrade task instructions, CI/security/archive descriptions, and stale security text claiming a long-lived desktop `vault.Session` and Linux-only monitoring. Design source structure was not changed. |

Independent code review found and resolved the legacy-route repair deadlock before acceptance. The final implementation, archive behavior, audit chain recovery/rotation, and desktop lifecycle changes had no remaining blocking review findings.

**Validation and evidence**

- Full repository gate: `go run ./tools check -serial` passed, `build/logs/check-20261116-170927.log`: vendor-patch integrity, all 649 Go files formatted, both frontend dependency boundaries, full vet and default tests including headless UI Tester regressions.
- The last audit rotation-budget adjustment landed after the audit package had run in that full pass. Its final gate was rerun with `go run ./tools check -pkg ./internal/auditlog -race`, passing in `build/logs/check-20261116-171517-race.log`. An earlier full auditlog race run also passed in `build/logs/test-20261116-170703-race.log`.
- Five targets compiled: `go run ./tools compile -platform all`, `build/logs/compile-20261116-171528.log` (darwin arm64/amd64, linux arm64/amd64, windows amd64).
- Official vulnerability database snapshot scan: `go run ./tools security -snapshot`, `build/logs/security-20261116-171528.log` and `.json`; zero known reachable findings for desktop/daemon/CLI using that snapshot. Database ZIP SHA256: `714ff02d9533ec6ca269e565b61b27cbd959aa0af89d51f318261322e875861c`. This is limited to the scanner's database, attribution, and reachability analysis.
- Committed generated files: `go run ./tools build -check` passed; its standard output confirmed current `resources/THIRD_PARTY_NOTICES.txt` and `internal/systemauth/native-hashes.json`. No generated source was changed by that check.
- Opt-in offline editor snapshots: `build/logs/test-20261116-171517.log`, images `build/snapshots/repository-audit-20261116/editor-{light,dark}.png`. Both were visually inspected: complete editor surface and footer, with the same `76 B` value. Tests used synthetic data; no desktop window or system dialog was opened.
- Initial machine setup issue: the Go mise shim had no configured version. Commands used the already-installed Go toolchain matching `go.mod` via a process-local PATH, without changing machine settings. The first full post-edit attempt (`build/logs/check-20261116-170803.log`) found review source backups as unintended Go packages; moving those backups into the hidden directory fixed the review setup. That failed attempt is not counted as a pass. The earlier initial gate is `build/logs/check-20261116-165841.log`.
- No app install, publish, regular desktop launch, foregrounding, real credentials, vendor regeneration, or dependency upgrade was performed. Cross compilation does not establish Windows/Linux runtime behavior; physical hardware authentication, packaged WebViews, and optional browser/service/terminal integration matrices were not comprehensively exercised this round.

**Remaining items for owner review — ordered by impact**

| Priority | Finding, evidence, and decision needed |
|---|---|
| P1 | **Long-running daemons lose IPC after 30 days.** [localipc/transport.go](../../internal/localipc/transport.go) creates a certificate valid for 30 days and clients reject expired certificates; [service/host.go](../../internal/service/host.go) retains that configuration for the daemon lifetime. New status/unlock/stop connections then fail. Choose renewable certificates retaining the same trusted SPKI, or an explicit daemon lifetime/restart policy. Clock-controlled handshake and active-session tests are needed. |
| P1 | **Retention can evict a legitimate vault audit anchor.** [auditlog/store.go](../../internal/auditlog/store.go) keeps only current + one archive; [broker/broker.go](../../internal/broker/broker.go) checkpoints the vault anchor at seal/close and refuses unlock if it is absent. Enough rotations during a long unlocked session followed by a crash can remove the last checkpoint's record. Decide checkpoint/retention coordination; simply weakening anchor verification would lose tamper detection. |
| P2 | **Windows metrics imply measurements that were not taken.** [monitor/windows.go](../../internal/monitor/windows.go) copies aggregate CPU into every core and uses the capped top-20 process list length for both total/running process counts. Add actual per-core/process-count probe data or explicitly present these fields as unavailable. |
| P2 | **Local Unix permission repair requires read permission.** [fileattrs/open_unix.go](../../internal/fileattrs/open_unix.go) opens the no-follow handle with `O_RDONLY`, so an owner cannot restore permissions on a mode-000 file through this path. A platform-specific attribute-handle strategy needs native tests while retaining race/symlink safeguards. |
| P2 | **Cleared transfer history keeps accumulating lock directories.** transfer/history.go（`internal/transfer/history.go`） intentionally preserves lock inodes; [transfer/transfer.go](../../internal/transfer/transfer.go) scans every job directory on listing. Listing cost therefore grows with lifetime transfers, even after clearing history. Choose lifecycle-safe compaction or an index; removing active lock inodes is unsafe. |
| P2 | **Linux crash-orphaned keyring records need scoped cleanup.** Per-record discard handles normal shutdown/replacement; a daemon crash can leave its random Secret Service record behind. The former broad clear was unsafe across vaults. Cleanup needs vault ownership/liveness or expiry semantics before implementation. |
| P3 | **Disconnected VNC windows retain one of eight window slots.** desktop/vnc.go（`internal/desktop/vnc.go`） cleans transport resources on tunnel termination but leaves an already-open viewer registered until manually closed. Decide whether disconnect feedback should remain, whether to add reconnect, or whether to close the window automatically. |
| P3 | **Profile count limits differ by save path.** [vault/vault.go](../../internal/vault/vault.go) caps new `SaveProfile` entries at 1024, while offline `Put` has no equivalent count cap. Confirm whether the offline CLI should share the same ceiling before changing existing vault compatibility. |
| P3 | **The idle watcher belongs to the first app in a process.** [desktop/idle.go](../../internal/desktop/idle.go) uses package-global `sync.Once`. Production currently creates one app, so this is only a problem if repeated embedded app instances are intended to be supported. |
| P3 | **Prototype Lucide assets duplicate production assets.** `internal/desktop/testdata/alva-prototype/lucide/` appears to be an intentionally independent visual fixture. Decide whether fixture independence or sharing the production asset set is desired before removing it. |

Already-labeled future work, such as the next default-tabs/saved-canvas design, remote gateway mode, and unimplemented credential management features, was not reclassified as broken or silently implemented.


## 2026-11-16 — 十项审查遗留问题全部落实

用户明确要求把上一节列出的十项全部修复。本节是它们的后续结果；上一节“待评审”清单保留为历史记录，不再表示本轮仍等待决定。采用原有入口和工作流，未增加审批步骤。原有工作树修改保留，本轮独立差异与文件清单位于 `build/logs/audit-ten-fixes-20261116.patch` 和 `build/logs/audit-ten-files-20261116.json`；修改前源码保存在隐藏的 `build/.audit-round2/`。

**十项处理结果**

| 原编号 | 使用上的变化与实现 |
|---|---|
| 1 长期运行后连接失败 | 本机 TLS 在每次新握手检查有效期，提前一天自动续签，保持原 Ed25519 密钥与 SPKI 指纹。使用每次握手都会触发的配置回调，覆盖本机客户端不发送 SNI 的情况；续签失败拒绝握手，过期/错误指纹仍拒绝。真实本机连接回归模拟证书已存在 31 天。 |
| 2 审计轮转误锁保险库 | 丢弃旧归档前，在日志写锁内把当前链头原子保存到加密保险库；失败则不轮转、不追加。关闭检查点也在同一锁下提交，防止延迟的旧头覆盖新检查点。保存保险库内容与锚点共用其原锁；多次轮转后模拟崩溃，重新打开仍能找到提交锚点，伪造整份历史仍不匹配。 |
| 3 Windows 监控假数据 | 采集真实总 CPU、各核心用户态/系统态占用，以及截取展示列表之前的进程总数。Windows 未测的运行进程数、Wait/Guest/Steal、Linux 式负载明确显示未知，不把瞬时 CPU 百分比当作 1 分钟负载；网络首次/计数回退/缺失样本不伪造零速率。补齐此前未接入的独立监控流和共享采样器，固定 PowerShell 探针保留授权、白名单、预算及取消。Windows 10 秒/5 分钟采样周期传到界面和图表，延迟提示按实际周期；识别临时失败可以重试，明确不支持的 POSIX 系统正常报告不支持。 |
| 4 无读取权限时改不回权限 | Linux 使用 O_PATH 固定目标；macOS 可读对象保留原 fd，不可读普通文件优先私有硬链接固定原文件，不移动用户文件；目录/无法固定时使用父目录句柄和 no-follow 操作并核对身份。真实 mode-000 文件、目录、替换、FIFO、符号链接、只读父目录及固定条目清理回归在本机通过。macOS 的精确能力限制见下文。 |
| 5 清空历史仍积累目录 | 新增固定的传输元数据协调锁，覆盖任务锁获取和历史目录回收，任务 IO 仍使用各自的锁。清理完成后删除私有任务目录；列表也会回收旧版纯锁目录。运行中的锁与未知内容保留，旧的 Run/Cancel 不会重建已删目录；完成文件和上传源保持原位。64 次清理后目录数量保持常量，覆盖跨进程互斥与删除竞争。 |
| 6 Linux 崩溃遗留钥匙串 | 新记录先在对应私有数据目录登记随机 ID 空文件，再保存系统条目；无解锁密钥、包装记录、nonce 或密文落盘。后台取得独占服务锁且尚未接受前端时，按 ID 精确清理遗留项，不读秘密、不执行 Prompt。固定清理锁覆盖完整登记+存储、扫描+删除及正常移除，防止旧前端与新后台竞争丢失标记；取消/失败保留标记重试，不阻塞核心服务启动。 |
| 7 VNC 失效窗口占名额 | SSH 通道结束时在 UI 线程关闭失效查看器并释放名额，显示断线原因，保留原目标便于从同一入口重连。重复、旧代次和取消后的回调不能关闭新窗口。 |
| 8 连接数量限制不一致 | Put 和 SaveProfile 统一使用 1024 条新建上限。已经超限的旧保险库仍可编辑、删除原有连接，不删除或截断旧数据。 |
| 9 自动锁定只服务首个实例 | 空闲监视器改为每个 app 实例独立持有，一次启动，随该实例上下文取消而停止；取消后的排队回调被丢弃。无可取消生命周期的离线预览不启动后台时钟。 |
| 10 原型图标重复 | 原型预览调用正式组件的共享 Lucide 入口，删除与正式目录逐字相同的重复 SVG、许可和来源副本。正式资源、许可与来源完整保留；原型离线渲染通过。 |

另修正验证基础设施：任务日志原来仅精确到秒，并行任务可能互相覆盖，改为原子创建带唯一后缀的文件，扫描 JSON/数据库继续使用对应前缀。新增回归确认两个同时启动的任务各自保留完整证据。同步 SECURITY、USAGE、DEVELOPMENT、DESIGN 与 pages.json 的行为、状态及源码映射；没有新增 SmallPen 页面结构或控件。

**验证证据**

- 全仓库门禁 `go run ./tools check -serial` 通过：`build/logs/check-20261116-180939-4036292797.log`，671 个 Go 文件格式、vendor 补丁、两个前端依赖边界、全仓 vet 与默认测试。最后两处空负载占位显示另经 `go run ./tools check -pkg ./internal/desktop -race -run TestWindows` 复核：`build/logs/check-20261116-181150-race-254174759.log`。
- 五目标编译通过：`build/logs/compile-20261116-181429-3620282138.log`，macOS arm64/amd64、Linux arm64/amd64、Windows amd64；包括新增平台锁与属性句柄实现。
- 已提交生成文件校验通过：`build/logs/audit-ten-generated-20261116.log`，THIRD_PARTY_NOTICES 与 native-hashes 无需变化。
- 官方数据库快照漏洞扫描：`build/logs/security-20261116-181429-1035454495.log`、同名前缀 JSON；本机三个入口的扫描中 0 个已知可达告警。数据库 ZIP SHA256 为 `714ff02d9533ec6ca269e565b61b27cbd959aa0af89d51f318261322e875861c`，结论仅限该快照和扫描器可识别的来源/可达性。
- 专项竞态通过：TLS `test-20261116-174636-race-4237868121.log`；最终钥匙串协调 `test-20261116-175537-race-2862146355.log`；本机文件权限 `test-20261116-175437-race-2339816459.log`；传输 `test-20261116-175922-race-3649936998.log`；审计检查点 `test-20261116-175931-race-1251577367.log`；统一连接上限/保险库 `test-20261116-174445-race-3331764216.log`，均位于 build/logs。
- Windows 专项：解析/有效性 `test-20261116-175943-race-2660594275.log`；共享及独立真实合成 SSH 采样、白名单、预算、暂错恢复 `test-20261116-175943-race-453626644.log`；动态周期和未知值 UI `test-20261116-180227-race-3933549364.log`。VNC、独立空闲监视器与快速解锁 UI 竞态：`test-20261116-175147-race-565783053.log`。旧终端/监控共用授权与连接回归经适配后的默认合成服务器通过：`test-20261116-180550-race-4233232052.log`。
- 最终离线图：`build/logs/test-20261116-181455-2998705010.log`，输出 `build/snapshots/audit-ten-fixes-20261116/`。明暗 Windows 监控图已目视：真实 25%/15%、20%/60% 各核心、137 任务、未知运行数、缺失负载占位及实时状态；共享图标的原型按钮图已目视，全部所选原型页面渲染通过。无系统窗口或真实服务器凭据。
- 失败与纠正：最初 Darwin O_EVTONLY 对 mode-000 实测 EACCES，修为当前实现后完整 race 通过；最初 Windows broker 新测试误用 `_windows_test.go` 文件名在 Mac 被跳过，已改为可跨平台执行的 `monitor_platform_test.go` 并实际运行；旧默认合成服务器不识别新探针使首轮全门禁 `check-20261116-180336-3170954965.log` 失败，适配后窄回归和全门禁通过。最初同秒的 audit/transfer 日志发生覆盖，不计为最终证据，已在唯一日志文件中分别重跑。没有把跳过、无测试或失败记作通过。

**能力边界与本地结论**

- macOS 公共接口没有 Linux O_PATH 的等价物。不能建立私有硬链接的路径（目录或父目录不可写等）虽固定父目录、禁止跟随最后一级链接并前后核对 inode，仍无法消除外部进程在核对与修改之间替换普通目录项的竞态。若进程在固定硬链接创建后崩溃，可能遗留 `.smallssh-attributes-*` 临时目录/硬链接；正常关闭会清理，当前无崩溃后自动回收。不声称这些路径具备原子的身份比较与权限修改。
- Linux 旧版未登记归属的随机钥匙串项无法安全判断所属保险库，保留而不做全局清除。新机制对有归属标记的记录自动清理；真实 Linux Secret Service/fprintd 与 Windows CIM/桌面/文件锁仍需要对应系统的运行验收，本轮跨平台编译和合成数据测试不替代实机验证。
- 传输协调保护所有新版获取/删除路径，也会避开旧版已经持有的任务锁；不能保证仍在使用旧协议、绕过固定协调锁的客户端并发创建任务句柄的情况。更新后应避免新旧版同时操作同一任务目录。
- 本轮完成源码与离线验证，未安装或启动桌面应用，未前置窗口、打开系统认证/文件对话框、读取生产凭据、重建 vendor 或升级依赖。


## 2026-11-16 — 恢复失效后台身份并保留同版本可执行文件身份

用户报告“无法验证调用进程实例；拒绝授权”。只读取证发现磁盘 App 深层严格签名有效，但正式 smallsshd PID98658 从11月15日15:46:30一直运行，其实际映像已在旧版废纸篓包中（lsof txt），macOS 对该 PID 动态 codesign 验证报 No such file or directory；新 App PID24717 此时动态签名正常。身份拒绝发生在系统进程身份层，不能通过删信任、放宽签名或仅按路径放行来绕过。核对本用户 UID、精确 PID、启动时刻和命令后，仅 SIGTERM 正式失效后台，未触碰开发后台 PID23417、保险库或凭据。首次恢复旧安装包后台 PID73642 后，SDK 状态握手通过（locked / minor1），证据 `build/logs/stale-service-recovery-20261116.json`、`service-20261116-202352-3564521765.log`。

安装修复：SHA256、大小和模式均相同且为普通文件时，候选包中主程序、CLI、smallsshd 以硬链接复用当前文件 inode；随后重新验证完整候选签名，再替换并删除旧包。不同内容的程序不会复用，符号链接不跟随。仍不保留备份、不开启 / 前置 / 重启 GUI；真正后台代码升级仍须 Stop 后重启，不把同版本修复冒充跨版本会话迁移。新增 `tools service -executable ABS [-data-dir DIR]` 只读诊断，走既有 SDK 的进程 / TLS 校验与状态请求，不启动、解锁或授予访问；日志遵循 build/logs。新增 install 的 `-source` 选择明确候选包，DEVELOPMENT 同步命令和边界。

回归先失败后通过：合成无窗口进程从私有 app 包运行，在删除旧包后保留既有 / 新连接身份。基线 `build/logs/test-20261116-202420-600420523.log` 重现相同 ErrIdentityUnavailable；修复后 `test-20261116-202517-805299318.log` 通过。最终 `go run ./tools check -pkg ./tools/... -race` 全绿：`build/logs/check-20261116-202908-race-2322959926.log`，包括 vendor 补丁、674源文件格式、前端依赖图、tools vet / race 测试；另覆盖同大小不同内容不复用、重复复用、链接拒绝、安装回滚和无残余备份。原 localipc 相同 PID 自 exec 拒绝与跨进程代码身份回归通过：`build/logs/test-20261116-202647-race-3019741578.log`。未更改 localipc 身份规则、IPC 授权或业务应用源码，不重复全仓应用测试。

第一次真实安装验收发现共享 build 目录的候选已被并行构建替换，三份程序的字节都变了（`build/logs/concurrent-candidate-change-20261116.json`），所以该次不能作为“相同版本身份保留”验收。操作前未再次对照候选，是本轮遗漏：`install-20261116-202646-2299727701.log` 安装成功后旧 PID73642 再次无法验证（失败 service 日志保留），未将这次记为通过。核对精确实例后 SIGTERM 该已失效后台，校验当前已安装新后台签名后启动 PID80782，真实 SDK 返回 locked / minor2；恢复记录 `build/logs/updated-service-recovery-20261116.json`，状态 `service-20261116-202839-2385915712.log`。没有终止或重启 GUI。

最终以 `install -source /Applications/SmallSSH.app` 明确固定为当前已核验的已安装内容，避免再次读取变化的共享构建包。真实安装 `build/logs/install-20261116-202936-4088862964.log` 通过签名检查；安装后同一后台 PID80782 动态 codesign 仍有效，SDK 状态仍是相同20:28:39.748的实例、locked / minor2，见 `service-20261116-202937-3247984743.log`。三份可执行文件前后 device / inode / SHA256 均相同，无新增备份，证据 `build/logs/current-identity-install-{before,after}-20261116.json`。后台重启后快速解锁内存记录为空，用户需重新打开 App 并输入一次主密码；没有读取 / 输入真实凭据来冒充用户解锁验收。结论限本机 macOS 的签名 / SDK握手及合成原生进程回归，不声称其它系统运行或无中断跨版本升级已验收。


## 2026-11-16 — Snell v6 单节点 / 数组 JSON 导入修复

用户提供的单节点对象、数组在结构上都有效。基线回归 `build/logs/test-20261116-205919-2600227064.log` 三种容器（含完整 outbounds）均被拒；根因是 restricted Config 没有 mode、Snell 白名单缺 mode，且 validate 只接受映射后的 v4。固定 vendor 本来已含 v6，无须升级依赖或改 vendor。新增 mode 字段，仅 Snell 可用；v6 保留 mode / reuse / version，mode 接受省略、default、unshaped、unsafe-raw，非法模式 / 版本及缺 PSK 分别报错。v5→v4 TCP兼容规则保留，mode 不用于 v4/v5；格式 / 协议错误分开反馈，均不回显凭据。该导入回归使用用户已脱敏的 xxxx / 全零示例地址，只验证结构，不把占位值当可连接的真实节点。

正式界面在导入框下列明单对象 / 数组 / outbounds，Snell 说明包含 v6；中英词条同步。新增UI回归覆盖两种容器解析、清空原输入、预览脱敏、保存到加密库、配置 / PSK重建及编辑不回填。JSON全局项仍不执行、未知出站字段仍拒绝，未引入DNS、监听、路由或代理控制面。

证据：

- parser 包完整测试 `build/logs/test-20261116-210157-2336306102.log`，含三种 JSON 形状、所有v6模式、重建、v5兼容、非法输入和秘密不回显；实际UI保存 / race `test-20261116-210221-race-984652578.log`。
- v6三种模式及 reuse=true 的握手上下文存活 / 关闭池回归 `test-20261116-210221-race-921101880.log`；v4/v5与v6的本机真实 SSH探测、登录、命令、SFTP、错误主机指纹拒绝 `test-20261116-210221-race-2441390709.log`。全部是合成loopback节点，无生产地址或凭据。
- 新后台通过工具编译 `compile-20261116-210309-492344326.log`；独立真实守护进程 / 多前端 / 七种线路（增加Snell v6）的 SSH/SFTP / race `test-20261116-210341-race-2134412407.log`，使用 build/compile 内程序与私有合成保险库。未把框架“能解析”当作真实握手通过。
- 全仓库门禁 `build/logs/check-20261116-210326-1890347646.log` 通过：vendor、674文件格式、前端依赖图、全部默认vet / 测试。未对支持v6作真实用户远端兼容性或其它OS实机承诺。
- 正式组件6张新建 / v6预览离屏图在 `build/snapshots/snell6-import-20261116/`，日志 `test-20261116-210341-2251009187.log`；已目视窄英文输入及深色Snell预览，未启动GUI。
- SmallPen 修订 `03a3d9022246f93b13e249c86e7d92e49c6119003ba447afae20725f667f37c3`，84页 / 17组件 / 66变体 / 145 Token。修改新建、重新导入、页面地图3，新增v6预览，pages.json同步三容器与版本 / 操作状态。初版输入说明增高后出现底部越界，输入态设计高度改560后新建 / 重新导入 / v6预览 / 地图3明暗渲染均0问题；仍有2条既有09工具 typography_overridden 警告及部分对比度跳过。结构 `snell6-design-check-20261116.json`；渲染 `snell6-design-{input,reimport,preview,map}-render-20261116.json`，PNG元数据 `snell6-design-preview-view-20261116.json`。已目视最终v6预览明亮稿，未另起预览服务。REQUIREMENTS / FEATURES / DESIGN同步。

macOS修复包编译及签名检查通过：`build/logs/snell6-app-build-20261116.log`、`snell6-generated-check-20261116.log`。固定候选包在 `build/snell6-review-agn7vh43/SmallSSH.app`，三份可执行文件SHA256与门禁 / 路径记录在 `build/logs/snell6-review-package-20261116.json`，避免共享构建目录变动。此时未替换正在运行的 App / 后台：只读状态确认后台已解锁（`service-20261116-210023-2361367327.log`），更新其程序会断开当前SSH会话，已把具体修复包准备完成后询问用户是否现在替换并重启，等待明确答复，不把默认选项当作批准。未新增备份，未改真实用户连接 / 凭据。


用户随后明确选择“现在替换并重启”。安装前再次核对固定候选包三份SHA256及深层严格签名，并通过既有SDK确认旧后台可验证、处于unlocked；精确核对UID / PID / 启动实例与动态签名后，以SIGTERM结束正式App PID82084和后台PID80782（授权包含断开SSH），未触碰独立开发后台。停止证据 `build/logs/snell6-restart-stopped-20261116.json`。安装从已批准的固定候选读取：`build/logs/install-20261116-211104-1157382788.log`，目标 `/Applications/SmallSSH.app`，不保留旧包备份。

已启动新后台PID46515，并按用户要求用 open -g -j 重启App（PID46523），请求后台隐藏启动，不以普通桌面窗口做验证。服务刚启动的首次探测遇到socket尚未创建，重试后完整进程 / TLS / SDK握手通过：`build/logs/service-20261116-211134-467922612.log`（locked、minor2、generation1）。两份实际运行进程动态codesign均有效；安装后的App / CLI / daemon SHA256与已批准候选一致，最终记录 `build/logs/snell6-review-package-20261116.json`。这次更新未读取或代填用户主密码；重新打开后需用户解锁再粘贴原JSON。候选包本机编译与安装已完成，不再处于等待批准状态。


## 2026-11-16 — 默认代理、本次代理与SSH跳板逻辑复核

本轮按用户“看看梳理一下”做只读实现复核与规格整理，没有修改应用源码或设计HTML，也未替换 / 重启App或后台。确认 `internal/desktop/connection_rail.go:connectionContextMenu` 当前没有单次选择线路入口；默认线路由 `connectionRoutePicker` 保存Network.RouteID，`vault.resolveRoute` 在后台解析并读取凭据；网络代理 / SSH中转与独立SSH隧道是不同机制。普通终端通过 `openShellInGroup` / ConnectWithOwner 按已保存服务器配置连接，当前不存在完整的临时会话线路上下文。

REQUIREMENTS新增“默认线路与本次线路”，DESIGN记录交互梳理状态；明确右键单次选择尚未实现，默认与临时共用线路库、临时不改默认、重连语义及工具跟随作为后续验收项。用词区分代理线路、SSH跳板机和SSH隧道，禁止先改配置再改回的临时实现。仅文档修改，不新增镜像测试或重复编译；以上结论来自当前本地源码，不声称临时代理功能已通过运行验收。


## 2026-11-16 — 默认线路 / 本次线路正式实现

完成服务器右键「本次使用线路连接」子菜单，可选本次直连、共享代理或 SSH 跳板线路；始终另开会话，不修改保存的默认线路。普通连接明确使用保存配置，不能误激活同服务器的临时线路标签。连接页主选择改名「默认线路」，保留旧连接专用代理 / 跳板配置；SSH跳板机与SSH隧道分别说明。标签及画布标题显示「本次：线路名」，失败卡片显示完整线路；窄分栏操作按钮换行，修复英文关闭按钮被裁切。

线路选择是可比较的不可变值，进入所有者请求与批准签名；普通请求和子授权拒绝覆盖。后台只覆盖私有Profile副本，保持凭据加密、目标与指纹校验；线路缺失、循环、不可达均失败，不回退直连。池按网络配置隔离，状态采样还按配置代次隔离。重连、派生终端（含容器/分栏）、文件窗格/编辑器/属性与变更、服务器/Docker操作保留源线路；文件缓存按线路隔离，传输任务及目录子任务持久保存线路引用，继续时不会继承另一个活动标签的选择。SSH跳板连接更新仍撤销临时路径的依赖授权。未新增依赖，vendor未变。

本机自动验收：

- 全量门禁通过：`build/logs/check-20261116-215001-3117929229.log`。包含vendor完整性、686个Go文件格式、桌面/CLI无SSH或代理内核依赖图、全量vet和测试。最后的失败卡片布局调整再通过桌面门禁：`build/logs/check-20261116-215152-3633037625.log`。
- 后台所有者签名、默认不变、池隔离、失效/缺失/循环不回退、采样线路与代次隔离：`build/logs/test-20261116-215002-race-3299730555.log`。
- Ghostty无窗口回归覆盖临时连接、新标签、原标签重连、普通连接恢复默认、画布派生、SFTP列表与跨标签编辑器读取：`build/logs/test-20261116-214907-race-4125585904.log`。该回归发现并修复了原标签重连遗漏线路的入口。
- 整个传输包race回归通过，新增持久线路、切换标签后上传与非所有者拒绝恢复测试：`build/logs/test-20261116-215002-race-451924309.log`。
- 使用下面冻结App内的真实独立守护进程，进程绑定管理会话通过本次直连、保存Snell v6及Hy2线路读取SFTP；七种既有代理SSH/SFTP、多前端共用和锁定仍通过：`build/logs/test-20261116-215239-race-1085390842.log`。
- 正式UI明/暗/窄窗英文离屏快照9张：`build/snapshots/session-route-20261116/`；最终日志 `build/logs/test-20261116-215055-3243584045.log`。已目视720宽英文长线路名失败卡片（按钮完整换行）、560宽英文默认线路表单。

SmallPen源稿及pages.json已同步。最终修订 `42955170f1635318c9e5145e073a34dff56d783759d449751c33ec271f930df9`，89页 / 17组件 / 66变体 / 145 Token；`build/logs/session-route-design-check-20261116.json` 结构0错误，保留两个既有09工具页typography_overridden警告。渲染证据 `session-route-design-{0..8}-20261116.json`（修订e3f5e437…，9页明暗组合）及 `session-route-final-design-{0..2}-20261116.json`（最终修订，菜单和新增直连/跳板页）合计11个唯一页面、22种页面/主题组合；全部完成无剩余，线路库页有2项原生窗口控制与侧栏的预期重叠info，部分对比度检查跳过。已目视本次线路菜单、默认线路表单、代理深色会话、直连明亮会话与跳板深色会话；spec及最终菜单view记录在 `session-route-menu-{spec,view}-20261116.json`。未另启服务或打开真实桌面窗口，不声称SmallPen App实机或像素一致。

正式包构建通过：`build/logs/session-route-app-final-build-20261116.log`；生成文件检查通过：`build/logs/session-route-generated-check-20261116.log`。已冻结到 `build/session-route-review-i647_itp/SmallSSH.app`（66,900,307字节，约63.8MiB），签名验证在 `build/logs/session-route-package-signature-20261116.log`，三个可执行文件SHA256在 `build/logs/session-route-review-package-20261116.json`。独立进程回归使用此冻结包。没有安装/替换用户当前App，没有重启用户后台、读取真实服务器凭据或连接真实代理；本轮运行验收仅代表本机macOS ARM64与合成回环服务，Windows/Linux及真实线路未运行验收。


## 2026-11-16 — VNC 直连、分框端口与通用按键

用户最终明确默认端口5900（覆盖前述口述5960），主机与端口分框。服务器右键VNC默认“直接连接VNC”，自动填当前服务器主机；可选择“通过SSH通道”。直连不需要SSH登录/授权或SSH代理，仍受App锁定/关闭生命周期管理；SSH方式继续使用所有者授权、保存线路和SSH指纹校验。修改VNC目标后重连保留，切换服务器取消旧建立请求并重新预填。IPv4、域名、IPv6及端口校验均覆盖。

VNC密码在服务协商要求时才填写，用户名/会话字段同样按需显示，无认证直接进入。密码不保存到Go、URL或配置，提交/取消清理输入。新增同窗“按键”面板，包含Esc、Tab、Win/Super、Ctrl Alt Del及Ctrl/Alt/Shift/Win组合选择，主键支持导航、字母、数字和F1–F12。调用固定noVNC sendKey接口，按下/抬起配对，修饰键逆序立即释放；只读禁用发送。物理键盘仍能用；本机拦截的组合键可从面板发，实际动作由远端决定。未新增依赖或修改固定noVNC/vendor源码。接口依据：[noVNC API](https://novnc.com/noVNC/docs/API.html#rfbsendkey) 与本仓库固定源码。

验证证据（macOS ARM64、本机生成的回环服务与私有无界面浏览器）：

- 全量门禁：`build/logs/check-20261116-222321-3355307755.log`，vendor完整性、686个Go文件格式、桌面/CLI无SSH和代理运行时依赖图、全量vet及测试通过。最后补充的连接方式英文翻译另通过 `build/logs/test-20261116-222529-1168392187.log`。
- VNC桌面UI/race：`build/logs/test-20261116-222106-race-2890435508.log`。直连使用无可用broker的client仍能收发真实TCP；SSH方式通过真实转发夹具；两种方式锁定都关闭WebSocket、目标TCP和查看器。默认地址、分框端口、IPv6、切换方式/服务器、取消及过期结果也通过。
- 真实headless Chrome + 固定noVNC/race：`build/logs/test-20261116-222200-race-4049831213.log`。覆盖RFB 3.8密码成功/失败、RFB 3.3无认证直连、raw framebuffer像素、输入清理、物理A键和面板Esc/Tab/Win/CtrlAltDel/CtrlShiftP/AltTab/F12的确切按下/抬起序列，只读不发送。RFB 3.3夹具不提供任何按键扩展，证明基础协议发送路径可用；不等于所有真实精简系统已验收。
- 原生连接Dialog明暗/560宽英文离屏截图：`build/logs/test-20261116-222418-1437540457.log`；与浏览器一起共10张保留在 `build/snapshots/vnc-flow-20261116/`，已目视英文连接窗及基础协议浏览器按键面板。
- SmallPen结构 `build/logs/vnc-flow-design-check-20261116.json`，最终修订 `2abc248a1177cc0663f402e278a0b93449599ae30b9828efe4eb624561d3557b`，91页/17组件/66变体/145 Token，0错误，两个既有字体覆盖警告。6页、明暗12组合：`vnc-flow-design-final-{0..2}-20261116.json` 是最终连接/SSH/按键页；`vnc-flow-design-render-{2,3,5}-20261116.json` 是未改范围的登录/查看器/页面地图7。最终0渲染问题，连接页各有1个对比度跳过；已目视连接明亮与按键深色稿。最初按键页溢出22项已修复，旧中间日志仅供定位，不作最终通过证据。未新开SmallPen服务或实际App窗口。

正式App构建 `build/logs/vnc-flow-app-build-20261116.log`，冻结审查包 `build/vnc-flow-review-mwtpdbcs/SmallSSH.app`（66,916,723字节，约63.8MiB），三个可执行文件SHA256见 `build/logs/vnc-flow-review-package-20261116.json`，完整签名通过 `build/logs/vnc-flow-package-signature-20261116.log`。生成文件检查见 `build/logs/vnc-flow-generated-check-20261116.log`。本轮未安装/替换用户App，未重启正式后台、使用个人浏览器资料或连接真实服务器。真实平台WebView、其它桌面系统及实际VNC服务仍需实机验收；服务器必须已有兼容VNC服务或控制台，此功能不自动安装远端桌面。


## 2026-11-16 — VNC 双向文本剪贴板

按用户“补”完成工具栏「剪贴板」面板：本机文字可手动粘贴或显式读取后发送到远端，远端复制内容经RFB接收后显式复制回本机。浏览器权限拒绝/不可用时回退手动粘贴、选中复制；不自动读写系统剪贴板。只传不含NUL的纯文本，每次按UTF-8最多64KiB。经典VNC支持Latin-1，未协商Unicode/Notify时拒绝中文等字符，避免noVNC回退成问号；Unicode扩展收发中文、表情与换行。能力判断隔离为固定noVNC1.7.0的只读兼容探测，未改上游/Vendor或新增依赖。

只读、断开、重连与关闭清空本窗缓存；清空/编辑/会话代次使异步读取失效，不能覆盖后输入的文字或进入另一个会话。面板收起保留当前会话文字；清空面板不清除系统或远端剪贴板。按键/剪贴板互斥展开，窄窗收发栏上下排列；没有新增Go业务RPC、URL内容、持久化或系统剪贴板后台同步。

验证证据：

- 完整门禁 `build/logs/check-20261116-230854-3215303003.log`：vendor完整性、687个Go文件格式、桌面/CLI运行时依赖守卫、全量vet及默认测试通过。
- 固定noVNC真实headless Chrome + RFB/race `build/logs/test-20261116-230946-race-2897488442.log`：同时重跑原密码认证/基础协议/按键回归；剪贴板覆盖真实经典CutText的Latin-1双向和压缩UTF-8扩展的中文/emoji/换行双向、权限拒绝的读写回退、远端/本机超限、旧协议中文拒绝、不自动发送读取内容、不自动读取/覆盖OS、编辑/只读/断线/重连后的迟到读取隔离。浏览器在载入页面前安装OS剪贴板替身，断言替身已安装；没有读取或写入用户实际系统剪贴板。协议交换与浏览器渲染是真实实现，只有OS边界替身。
- 8张快照在 `build/snapshots/vnc-clipboard-20261116/`，含4张剪贴板：1100明亮和640深色，两个协议；其余4张是原密码/基础协议查看器回归。已目视Unicode收发UI的明/暗稿，640宽无裁切，真实传入的远端文字仅作为textarea值展示。
- SmallPen源稿新增「09 工具 / VNC 剪贴板」，更新查看器、按键页、页面地图7及pages.json，共92页/17组件/66变体/146 Token。结构 `build/logs/vnc-clipboard-design-check-20261116.json` 最终修订 `97e71d0d95677650bea660bc0a58fbabf6c43a27a086b10beb037121d6da5b70`，0错误，两个既有字体覆盖警告。四页明暗8组合均0渲染问题/0跳过：剪贴板以 `vnc-clipboard-design-final-20261116.json` 最终修订为证，其余未改范围在 `vnc-clipboard-design-render-{1..3}-20261116.json`（修订06bb1c76…）。已目视最终剪贴板设计明暗稿。未新建SmallPen服务或打开前台窗口；不声称静态设计与系统WebView逐像素一致。

正式包构建 `build/logs/vnc-clipboard-app-build-20261116.log`；冻结到 `build/vnc-clipboard-review-_d9gy7bz/SmallSSH.app`（66,916,739字节，约63.8MiB），完整签名验证 `build/logs/vnc-clipboard-package-signature-20261116.log`；生成文件检查 `build/logs/vnc-clipboard-generated-check-20261116.log`。三个可执行文件及4份查看器源文件SHA256见 `build/logs/vnc-clipboard-review-package-20261116.json`，已确认完整源文件字节实际嵌在冻结App中，执行文件哈希未再变化。

本轮未安装或替换当前App、未重启正式后台、未读取真实剪贴板或连接真实服务器。验收仅为本机macOS ARM64、私有Chrome与合成服务；系统WebView及真实平台OS剪贴板交互仍需实机验收。文本双向功能不包括图片/文件，旧式VNC的Unicode限制会明确提示。


## 2026-11-16 — 安装双向剪贴板正式包并重启

用户明确要求“替换”，沿用不留备份和不抢前台的偏好。安装前再次验证冻结包 `build/vnc-clipboard-review-_d9gy7bz/SmallSSH.app` 的三个SHA256与完整签名；只停止已核对路径、UID、启动时刻与动态签名的正式App PID46523、后台PID46515，发送SIGTERM后确认退出。开发后台PID23417（11月14日启动）保持不动，未读取用户凭据。

以 `tools install -source` 发布该冻结包到 `/Applications/SmallSSH.app`，安装日志 `build/logs/install-20261116-233528-2427683504.log`；无保留备份或安装临时目录。随后 `open -g -j` 后台重启。正式App PID98716、后台PID98746 的动态签名有效，安装后三个二进制与审查包SHA256逐一相同：前后记录分别为 `build/logs/vnc-clipboard-install-before-20261116.json`、`build/logs/vnc-clipboard-install-after-20261116.json`；旧实例停止记录 `build/logs/vnc-clipboard-restart-stopped-20261116.json`。

首次只读探测早于套接字创建；稍后真实SDK进程身份/TLS握手与状态读取通过：`build/logs/service-20261116-233550-1680136382.log`，启动23:35:48、协议1.2、generation1、当时状态unlocked。没有绕过身份校验，没有重新编译或安装其它会话的可变build包。此节证明本机正式进程已使用新包并能通过SDK连接；剪贴板/远端协议的功能验收仍以上一节合成RFB与无界面浏览器证据为准，未读取实际系统剪贴板或替用户连接真实服务器。


## 2026-11-17 — Linux VNC 锁屏与 SSH 失败的只读诊断

用户确认远端为 Linux，VNC 已在 SmallSSH 内显示桌面，但 SSH 重试仍返回 TCP 层的 `no route to host`。正式 App 与后台的动态代码签名校验通过，只读 SDK 身份和解锁状态检查见 `build/logs/service-20261117-000406-1207831022.log`。两次系统 TCP 探测均成功，额外只读套接字读取到 OpenSSH 的服务器版本行；没有提交登录凭据或执行远程命令。诊断记录在 `build/logs/vnc-ssh-diagnosis-20261117.json`。

后台读取现有窗口错误，没有切换前台或发送输入。VNC 窗口的后台截图为空，用户确认实际能看到桌面，因此该截图不作为远端黑屏或查看器故障的证据。macOS nehelper 日志记录 SmallSSH 的本地网络权限已允许；LuLu 保存的后台规则为允许（按官方 v4.5.7 常量核实），查询范围内未发现目标的明确拦截记录；AVG 的近期相关日志亦未命中目标。这些读取不能证明每个网络过滤器对运行中后台的实际判定。

本轮仅将问题缩小到运行中 SSH 后台与系统探测之间的网络访问差异，尚未确认根因，也没有把 Linux 锁屏认定为 SSH 失败原因。未修改 App、Linux、代理、系统权限或防火墙规则，未重启或替换进程；重启后台可能中断当前 VNC 并要求重新解锁，需用户确认后再验证。本轮为本机诊断及文档记录，没有实现改动，未重跑构建或测试门禁。


## 2026-11-17 — 补充 x11vnc 锁屏复现前提

用户补充：必须先经 PVE 控制台解锁 Linux，SmallSSH 内的 VNC 才能显示桌面；远端服务确定为 x11vnc。因此上一轮的“VNC 当前可用”仅适用于经 PVE 解锁后的状态，不能作为锁屏问题已解决的证据。更新诊断记录 `build/logs/vnc-ssh-diagnosis-20261117.json`。

只读检查查看器与固定 noVNC 的初始全帧、后续增量/连续更新及按键路径，未发现项目代码主动按远端锁屏状态屏蔽画面或输入。核对上游 x11vnc 手册的 X display/授权、DPMS/FBPM 与 DAMAGE 选项；它们是待区分的排查方向，不是已确认根因。仍需远端桌面会话类型、显示器电源状态和实际服务配置；未改 x11vnc 参数、未禁用锁屏、未重启进程、未实现推测性修复。没有真实锁屏回归或新的构建/测试结论，SSH 故障仍独立保留。

用户随后提供的只读输出确认 XFCE + X11、DISPLAY `:0.0`、x11vnc 0.9.17，X 屏保 600 秒，DPMS 开启（Standby 600 秒、Off 900 秒），读取时 Monitor On；因此排除 Wayland，但不能以解锁后的 DPMS 状态证明黑屏根因。继续核对实际锁屏进程：light-locker 官方说明其会切换 VT 到 LightDM greeter，这只是待验证的候选机制。该补充仍未执行远端修改或完成锁屏实测。

用户确认进程包含 light-locker（PID1124）与 LightDM（PID749/882）。这支持 VT 切换的排查方向，但尚无锁屏时的显示会话或修复实测。拟替换组件前核对 xfce4-screensaver 4.18.4 的 `src/setuid.c:hack_uid`，该版本对 UID0 返回不可锁屏；终端 root 提示符不能证明桌面所有者，用户也尚未确定，所以继续只读查询 xfce4-session 的运行用户，未卸载、结束或替换现有锁屏程序。


## 2026-11-17 — 确认 root 桌面与锁屏替代方案的边界

用户提供进程所有者，确认 xfce4-session PID926 与 light-locker PID1124 均为 root。更新 `build/logs/vnc-ssh-diagnosis-20261117.json`。light-locker 的 VT 切换与已观察现象一致，但实际锁屏时的 X display/VT 状态尚无抓取，不能把支持机制的证据写成已验证修复。

因此不直接建议以 xfce4-screensaver 4.18.4 替换：该版本明确禁止为 UID0 锁屏。核对独立 i3lock 2.15 源码，其按 getpwuid(getuid()) 选择认证账号并调用 PAM，可作为当前 root X11 会话锁屏的候选；实际 Debian PAM 策略与 root 密码接受情况必须通过手动测试确认。建议先安装发行版包并在现有桌面试锁/解锁，原 light-locker 配置保留，PVE 的独立 root 终端作为测试恢复入口。自动锁屏接管需测试通过后再处理（例如 xss-lock），本轮没有修改、下载或执行远端程序，没有取消密码锁屏，没有 App 实现或构建测试结论，也未声称修复 SSH 的网络错误。


## 2026-11-17 — 用户确认 i3lock 手动解锁可用

用户反馈“确实好了”，记录为本机对应 Linux 虚拟机上的人工 i3lock 锁屏/SmallSSH 显示及解锁试验通过；这是用户实测反馈，不是无界面测试，也不能外推到自动空闲、休眠、XFCE 菜单或下次登录。证据与边界更新在 `build/logs/vnc-ssh-diagnosis-20261117.json`。

准备持久配置说明：通过用户 XDG 自启动启动 xss-lock 与 nofork i3lock，接管 XScreenSaver/logind 事件；将 xfce4-session 的 `/general/LockCommand` 设置为可自行 fork 的 i3lock 命令，避免同步调用等待整次解锁；使用同名 Hidden=true 用户项覆盖 light-locker 系统自启动。依据为 Debian xss-lock/i3lock 手册、XDG Autostart 规范及 libxfce4ui-4.20.0 的锁屏入口源码。先启动并检查新进程，再停现有 root light-locker；保留 X11/DPMS 现有计时，不重启桌面。说明交由用户在远端桌面终端执行，执行后的自动触发、下次登录及休眠验收尚未发生。没有修改 App 实现、没有停用密码验证，也没有 SSH 网络错误已修复的结论。


## 2026-11-17 — VNC 工作区标签、单框剪贴板弹出层与记住密码

按用户本轮要求完成四项改造。VNC 默认在主窗口工作区标签打开，可选独立窗口；每个查看器有独立身份、私有WebView与连接，标签切换、拖动、四向分栏、画布/标签聚合保留页面，关闭标签/窗口、锁定或退出回收。复用工作区工具所有权模型，VNC不显示工具停靠入口。原生连接Dialog异步恢复加密保存的VNC目标/端口/方式，切换服务器、取消、锁定和旧代次不能回填新Dialog。

剪贴板合并成一个只读远端文本镜像框：远端复制通知更新，粘贴一次完成本机读取与远端覆盖提交，复制把当前镜像写回本机；浏览器权限失败提供手动粘贴事件或选中文本复制。保留64KiB/NUL/经典Latin-1与Unicode协商限制；远端新值、只读、断线、重连使旧异步读取失效。按键与剪贴板均为互斥的绝对定位弹出层，点击外侧或Escape收起，不改变桌面内容高度。

登录可保存VNC用户名/密码。客户端只在RFB认证成功后发送私有同源POST，原生闭包固定服务器ID、VNC目标与直连/SSH方式，再通过一次性所有者证明写入加密保险库；普通调用方与公开摘要不可读取，载荷改动不能复用证明。保存与SSH资料编辑串行化，编辑SSH资料保留独立VNC信息；同目标自动填入，错误密码回到手动输入，原生Dialog支持清除保存信息。HTTP入口限制Host/Origin、JSON字段与大小，密码不进URL/静态HTML/公开摘要。未新增依赖、可信网页RPC或OS剪贴板后台同步。

验证证据：

- 完整门禁 `build/logs/check-20261117-020115-2114362358.log`：vendor完整性、692个Go文件格式、Desktop/CLI运行时依赖图、全量vet与测试通过。此前检查发现新增翻译缺失，已补中英文后重跑通过。
- Desktop专项race `build/logs/test-20261117-020346-race-80689795.log`：多VNC标签选择/移动/画布/独立关闭、保存目标恢复与跨端口不带密码、直连/SSH及取消/锁定/旧代次回归。嵌入页面使用headless WebView替身验证身份、布局与生命周期，不是系统WebView运行实测。
- Owner/Vault专项race `build/logs/test-20261117-020346-race-448294002.log`：保存/读取/清除、加密落盘与重开、公开摘要不带秘密、普通调用拒绝、一次性证明绑定密码、目标校验和SSH资料编辑保留。私有HTTP凭据端点race `build/logs/test-20261117-015501-race-3523242826.log` 验证Origin隔离、固定保存回调、no-store与关闭清理。
- 真实headless Chrome + 合成RFB/race `build/logs/test-20261117-015605-race-4174813424.log`：密码正确/错误、已保存密码自动认证及错误回退、成功后保存/失败不保存、基础RFB像素与组合键回归，单框剪贴板真实经典/压缩Unicode收发、复制/粘贴权限回退、超限、只读、断开/重连迟到读取隔离、弹出层不压缩画面与Escape关闭。浏览器OS剪贴板接口载入前已替换，未读取或写入用户实际剪贴板。
- 正式Dialog离线快照 `build/logs/test-20261117-020329-2003598057.log`，图片 `build/snapshots/vnc-dialog-workspace-20261117/`；浏览器10张图片 `build/snapshots/vnc-workspace-20261117/`。已目视中文Dialog、560宽英文Dialog，以及640深色单框弹出层。
- 五目标编译 `build/logs/compile-20261117-020346-2855005854.log`：darwin/arm64、darwin/amd64、linux/amd64、linux/arm64、windows/amd64。交叉编译不替代对应平台运行验收。
- SmallPen更新6个VNC页面与页面地图7，新增共享弹出宽度Token、单框文本高度同步140；同步pages.json、REQUIREMENTS U37、USAGE、SECURITY、设计入口与vncview说明。最终修订 `8bfee4bdbe24a76e7e5a983f98eabcc92c34bdca5de8af023014e68346872ae4`，92页/17组件/66变体/147 Token；结构 `build/logs/vnc-workspace-design-check-20261117.json` 0错误、2个既有字体警告。`vnc-workspace-design-{clipboard,keys,tab-final,login,connect,ssh}-20261117.json` 覆盖6页明暗12组合，0布局问题、0剩余；对比度跳过分别为工作区14、两个连接页各1，其余0。已目视工作区标签示意、最终剪贴板深色弹出层、登录深色稿；未另起SmallPen服务或打开前台窗口，在线App打开与原型点击链未作本轮实测。

macOS打包 `build/logs/vnc-workspace-app-build-20261117.log`，生成文件校验 `build/logs/vnc-workspace-generated-check-20261117.log`。候选包冻结为 `build/vnc-workspace-review-ljgwr6rq/SmallSSH.app`；三个可执行文件SHA256及4份查看器源文件哈希在 `build/logs/vnc-workspace-review-package-20261117.json`。已逐份确认完整HTML/JS/CSS字节实际嵌在冻结主程序中，严格签名核验 `build/logs/vnc-workspace-package-signature-20261117.log` 通过。

本轮尚未替换或重启用户当前App/后台，没有读取真实VNC密码、连接真实服务器或改动Linux锁屏配置。用户此前确认的i3lock手动恢复与本轮客户端UI改造分开记录；既有SSH网络故障没有在此声称已修复。实际macOS内嵌WebView、真实系统剪贴板、真实服务和其他平台运行仍需安装后验收。


## 2026-11-17 — VNC 双图标剪贴板、统一工具栏与原连接全屏

此轮替代前一节的打开位置选择、服务器副标题和单框剪贴板弹出层。连接直接进入工作区标签；去除查看器重复名称，工具栏统一线性图标、悬停说明与只读/适应窗口选中态。剪贴板仅粘贴到远端/复制远端剪贴板双图标，无文本框/面板，内部缓存跟随远端事件、操作短暂提示。按键保留小型弹出层，密码保存与自动填入不变。全屏保留同一原生WebView和RFB连接，不重建页面；关闭/锁定回收并退出。进入前主窗口已经全屏时保留原状态。

浏览器剪贴板不可用时通过私有同源有界POST调用原生文本剪贴板。与全屏端点一样，没有窗口/目标参数，不授予可信Go RPC来源；仅固定查看器的活跃未锁定运行可在UI线程执行。新增回归覆盖Origin、请求大小/字段/NUL校验、关闭取消以及排队后锁定、换代、关闭、取消时不执行原生操作。系统剪贴板测试边界使用合成回调与页面加载前的浏览器API替身，未读写用户真实剪贴板。

验证证据：

- 仓库门禁`build/logs/check-20261117-021717-867755258.log`：vendor完整性、695个Go文件格式、两前端依赖图、全库vet和默认测试全部通过。补充桌面VNC race（包含最新排队权限与全屏生命周期回归）`build/logs/test-20261117-021757-race-3601268991.log`；私有端点race`build/logs/test-20261117-021507-race-1690321186.log`。
- headless Chrome真实RFB认证、密码保存、组合键、双图标剪贴板/原生回退和只读/重连生命周期：`build/logs/test-20261117-021744-race-991766201.log`，8个合成场景全部通过。全屏网页按钮触发固定回调，连接计数不增加；原生全屏布局使用UI Tester和假WebView核对同一实例、退出与关闭回收，未调用系统全屏。
- 查看器明暗/窄窗图在`build/snapshots/vnc-icons-20261117/`；原生连接Dialog中文明暗、英文560px离线图在`build/snapshots/vnc-icons-dialog-20261117/`，日志`build/logs/test-20261117-021744-1810485763.log`。已目视1100px明亮及640px深色剪贴板工具栏、英文Dialog与代表性SmallPen图；没有使用真实服务器凭据。
- SmallPen最终修订`a360caef1f95d298f454a8d32c101e7b695e5ca4fc5160d9ae4e3ad5a67eda02`：93页/17组件/66变体/148 Token，0结构错误、2个既有字体覆盖警告。六页×明暗12组合：`build/logs/vnc-icons-design-{tab,fullscreen,clipboard,keys,connect,ssh}-{light,dark}-20261117.json`均0渲染问题。标签每主题14项、按键/两种连接每主题1项文字对比检查跳过，其余无跳过；各报告完整覆盖目标但不代表自动视觉验收。原型全屏/退出、按键overlay及反馈自动收起链接已保存，未在App执行导航；未另起服务、未前台打开SmallPen。
- 五目标编译`build/logs/compile-20261117-021837-2372756940.log`：darwin arm64/amd64、linux amd64/arm64、windows amd64全部通过，只证明编译，不代表其它系统运行验收。

macOS App打包`build/logs/vnc-icons-app-build-20261117.log`，生成文件校验`build/logs/vnc-icons-generated-check-20261117.log`。当前固定候选为`build/vnc-icons-review-v_pkihhr/SmallSSH.app`（59,757,891实际文件字节，不重复计符号链接），取代上一节候选。三个执行文件SHA256及4份完整查看器源码嵌入核对在`build/logs/vnc-icons-review-package-20261117.json`；完整签名通过`build/logs/vnc-icons-package-signature-20261117.log`。

本轮没有安装替换或重启用户App/后台，没有打开正常桌面窗口验证。原生WebView中的系统全屏、真实系统剪贴板以及真实VNC服务仍需实机验收；不把headless Chrome及UI Tester当作上述验收，也不声称修复了先前SSH的no route to host。密码存储与所有者IPC沿用前一节并通过本轮全库测试，未读取用户保险库或服务器密码。


## 2026-11-17 — 替换 VNC 图标工具栏版并后台重启

用户明确要求“替换我看看”，沿用不保留备份、不抢前台的偏好。固定来源为上一节已核验的`build/vnc-icons-review-v_pkihhr/SmallSSH.app`，安装前重新复核三份执行文件SHA256与深层严格签名。旧正式App当时已退出，后台PID98746处于locked（`build/logs/service-20261117-022128-2299028872.log`），没有可复用的所有者认证会话；核对真实路径、UID、启动时刻、映射文件与动态签名后发送SIGTERM，确认正常退出。开发后台PID23417保持原样。停止及前态证据：`build/logs/vnc-icons-install-before-20261117.json`、`build/logs/vnc-icons-restart-stopped-20261117.json`。

使用`tools install -source`替换`/Applications/SmallSSH.app`，日志`build/logs/install-20261117-022205-1462984715.log`。无保留旧包备份或安装暂存目录。安装后三个SHA256与固定候选逐份一致，完整包签名有效。新正式后台PID62677、App PID62679于02:22:22启动，两进程动态签名有效，记录`build/logs/vnc-icons-install-after-20261117.json`；后台启动日志`build/logs/vnc-icons-daemon-launch-20261117.log`。

App通过open -g -j并显式传入-background及正式数据目录启动，未切换前台。只读控制状态ready=true、visible=false、focused=false、activations=0、backgroundStart=true。首次服务/控制查询早于套接字创建；稍后重试成功，真实SDK身份/TLS握手和状态读取见`build/logs/service-20261117-022229-2088266756.log`（locked、协议1.2、generation1）。没有读取主密码或自动解锁。用户打开SmallSSH并解锁后可查看新版；本轮只证明实际安装/运行实例与候选一致、服务握手正常，不把此安装记录当作真实VNC/系统剪贴板功能验收。


## 2026-11-17 — VNC 地址端口同行、删除说明并更新当前 App

按SSH截图将VNC服务器地址与端口放在同一行，复用Form标签对齐、36px输入高度、8px行内间距与64px窄端口，预填地址、5900默认值和独立校验保留；错误文案同步“右侧”端口。随后按用户“没用的全删”删除连接Dialog中地址/端口、连接方式及密码保存的解释段落，并删除VNC登录、按键弹出层冗余说明，只保留必要字段、选项、操作及错误/身份核对提示。SmallPen四页、pages.json、REQUIREMENTS、USAGE与DESIGN同步。未改变协议、凭据存储或依赖。

最终门禁`build/logs/check-20261117-022723-2903189327.log`全部通过，包含vendor、695文件格式、两前端依赖图、vet与默认全库测试。最终六张原生Dialog离线快照在`build/snapshots/vnc-address-compact-20261117/`，中文明暗及英文560px覆盖直连/SSH，日志`build/logs/test-20261117-022723-3367236591.log`；已目视中文深色和英文SSH图，同排控件完整且说明已移除。

浏览器首次附加回归`build/logs/test-20261117-022723-race-3286547387.log`暴露两个测试时序问题：尚无document.body时读取dataset，以及断开事件后立即重连时旧合成桥仍占单查看器名额。仅修正测试等待条件（加载保护、等待旧桥清理），未修改生产连接逻辑。复跑`build/logs/test-20261117-022824-race-2884454430.log`：8个真实RFB合成场景全部通过，覆盖认证/保存密码、帧、组合键、只读和剪贴板生命周期；截图`build/snapshots/vnc-compact-20261117/`。系统剪贴板仍使用合成边界，无真实凭据或远端访问。

SmallPen最终修订`c5cbe1c0e633ed674c6b4d9d80657769a975fa9d6616990a2b9589bb85721b10`：93页/17组件/66变体/149 Token，0结构错误、两个既有字体覆盖警告。四页明暗8组合的`build/logs/vnc-compact-design-{direct,ssh,login,keys}-{light,dark}-20261117.json`均0渲染问题；直连/SSH/按键每主题1项文字对比跳过，登录无跳过。代表性连接、登录与按键PNG已目视。未打开前台窗口或另起SmallPen服务，未实际执行App内设计导航。

最终构建`build/logs/vnc-compact-app-build-20261117.log`、生成校验`build/logs/vnc-compact-generated-check-20261117.log`通过；冻结候选`build/vnc-compact-review-n7a9kimb/SmallSSH.app`，执行文件哈希及完整查看器HTML/JS/CSS嵌入核对见`build/logs/vnc-compact-review-package-20261117.json`，严格签名见`build/logs/vnc-compact-package-signature-20261117.log`。先前地址布局中间候选未安装，本轮实际发布此精简最终包。

沿用刚才用户要求替换查看的工作流，已核对旧App PID65916的路径/UID/启动时刻和动态签名后SIGTERM退出；后台/CLI字节完全相同，保持正式后台PID62677并通过安装任务硬链接保留后台inode，未重启后台或触碰开发后台。安装日志`build/logs/install-20261117-022944-3739142861.log`；前后证据`build/logs/vnc-compact-install-{before,after}-20261117.json`，无备份或安装暂存目录。安装后三个二进制SHA256与固定候选一致，App PID92108和后台动态签名有效。

App使用正式目录及-background、open -g -j于02:29:45启动；只读UI状态ready=true、visible=false、focused=false、activations=0，未抢前台。服务SDK身份/TLS状态核验通过`build/logs/service-20261117-022958-663503958.log`，旧后台启动时间02:22:23保持，当前locked、generation2。没有自动解锁；此记录证明已安装并运行最终包，不替代真实VNC/系统剪贴板/原生全屏验收。


## 2026-11-17 — VNC 剪贴板直接原生访问，去掉系统 Paste 确认

用户实际截图显示点击粘贴后出现WebView的Paste确认。排查本地实现仍优先调用navigator.clipboard.readText，浏览器确认之后才可能进入原生回退。本轮移除浏览器Clipboard API的全部读取/写入及回退分支，复制和粘贴均直接调用已有固定Origin、有界POST原生接口；保留明确点击才访问系统剪贴板、远端实时缓存、只读/生命周期、64KiB/NUL及Unicode限制。读取失败不再误报为要求浏览器权限，过大/无效文本明确提示。界面结构未改，SmallPen修订c5cbe1c0e633ed674c6b4d9d80657769a975fa9d6616990a2b9589bb85721b10保留；pages.json、DESIGN、USAGE、REQUIREMENTS、SECURITY与模块README同步行为契约。未新增依赖、可信Go RPC来源或自动系统剪贴板同步。

回归`build/logs/test-20261117-023414-race-3713824449.log`通过：三个真实RFB合成剪贴板场景，原生回调使用合成存储，同时将私有headless浏览器的navigator.clipboard设为任何访问即抛错。验证点击复制/粘贴、Unicode/旧式编码、超限/不可用、只读、断开重连与迟到HTTP响应；最终浏览器Clipboard API调用计数为0，未接触实际OS剪贴板。截图目录`build/snapshots/vnc-native-clipboard-20261117/`，仅行为变化不重复离线原生布局验证。

完整门禁`build/logs/check-20261117-023447-911948716.log`通过：vendor、695文件格式、两前端依赖图、vet及默认全库测试。macOS构建`build/logs/vnc-native-clipboard-app-build-20261117.log`、生成检查`build/logs/vnc-native-clipboard-generated-check-20261117.log`通过。冻结包`build/vnc-native-clipboard-review-nqjmd2z6/SmallSSH.app`，三执行文件及4份查看器源码嵌入证据`build/logs/vnc-native-clipboard-review-package-20261117.json`，严格签名`build/logs/vnc-native-clipboard-package-signature-20261117.log`。

沿用当前替换评审流程，核对App PID93735的UID、路径、启动时刻、动态签名后SIGTERM退出；后台/CLI字节不变，安装保留正式后台PID62677和inode，不重启后台、不动开发后台。安装`build/logs/install-20261117-023647-351539209.log`，前后证据`build/logs/vnc-native-clipboard-install-{before,after}-20261117.json`；无旧版备份或安装暂存目录，安装后三SHA256与候选一致。App PID6261后台启动，ready=true、visible=false、focused=false、activations=0，动态签名有效。SDK真实身份/TLS握手通过`build/logs/service-20261117-023716-2608759497.log`，状态locked，未自动解锁。

本轮未前台操作App、触发真实系统剪贴板读取或连接真实VNC；证明不再调用会触发Paste确认的浏览器API，并已将此代码装入当前运行包，实际macOS剪贴板与远端交互由用户继续验收。


## 2026-11-17 — 明确区分密钥使用授权与服务器操作授权

按用户“这两个授权做一下明显区分”落实术语、当前操作审批与独立签名授权设计。GLOSSARY新增密钥使用授权、服务器操作授权、保险库解锁定义；DELEGATION明确对象、字段、按钮、期限、撤销和权限不可兑换。Key-use许可不导出私钥、不等于SmallSSH内的服务器操作许可；标准外部SSH登录后的操作由远端账号权限决定，不能承诺受SDK逐操作检查。签名密钥与保险库解锁密钥分开，内置操作可按密钥策略签名，但不向外部客户端隐含授予密钥使用权限。REQUIREMENTS更新统一身份中心规划并保留默认按服务器身份独立生成/引用。

本轮已有运行能力仅为服务器操作授权。当前审批弹窗、授权中心待处理/活动/历史、窗口标题与系统验证理由新增明确盾牌及“服务器操作授权”；客户端会话显示“服务器操作会话授权”，决定为“允许操作 / 拒绝”，续期为“续期操作授权”。系统验证理由不使用自报申请方名称，包含权限类别与服务器，仍有120字符预算。未修改审批协议、票据、权限范围、所有者验证或后台策略；新密钥签名服务、标准Agent服务端及Secure Enclave SSH签名后端未实现，不增加伪签名入口。

更长动作文本暴露窄窗布局问题：允许/拒绝现作为不可拆分决定组整体换行且靠右；初版完整检查发现展开目录详情后配对码被挤出滚动区。最终将配对码固定在审批滚动内容之外，并强化原测试，要求读取/写入根目录及配对码高度至少11px且均在决定区上方。定向回归`build/logs/test-20261117-124722-3783680232.log`通过；最终离屏20个审批分支（含展开目录详情与380px英文）`build/logs/test-20261117-124757-2815711500.log`通过，图片`build/snapshots/authorization-types-20261117/`。已目视中文窄窗、展开目录权限和英文换行决定组，未启动正常窗口验证。

最终完整门禁`build/logs/check-20261117-124929-2551958417.log`全部通过：vendor完整性、695文件格式、两前端依赖图、全库vet与默认测试，包含强化后的目录/配对可见性和不自动批准回归。首次失败记录留在build/logs，未通过放宽断言掩盖剪裁问题。本轮验证仅代表本机macOS与合成UI/回环夹具，没有测试两类运行时互通，因为密钥使用权限族尚不存在。

SmallPen修订`167ffc61301efb4f9acc68931da318be2b563a3b33e0e2b457366e132003c541`：95页/17组件/66变体/151 Token，0结构错误、两个既有字体覆盖警告。现有三个中心页新增操作权限类别；新增`08 设置 / 服务器操作授权`为字段/动作对照稿（contract-preview），`10 待实现 / 密钥使用授权`为规划稿，分别显示盾牌/钥匙、服务器范围/密钥用途、允许操作/允许本次签名。页面地图7提供两类overlay入口并标明规划状态，pages.json同步入口、字段、实现状态和源码映射。

五页明暗10组合证据`build/logs/authorization-types-final-design-{server,key,pending,active,history}-{light,dark}-20261117.json`：两个独立对照页均0问题、无跳过；三个中心页每组合有1条已有info（原生窗口控制与侧栏背景叠加），各跳过7项文字对比检查。已目视两类对照与代表性中心稿，不宣称对照稿与完整生产审批分支逐像素相同。目录结构/链接已保存，未前台打开SmallPen、另启服务或实际执行App内设计导航。

macOS候选构建与生成文件校验通过`build/logs/authorization-types-app-build-20261117.log`、`build/logs/authorization-types-generated-check-20261117.log`。冻结候选`build/authorization-types-review-qu3d_b6m/SmallSSH.app`，三份执行文件SHA256见`build/logs/authorization-types-review-package-20261117.json`，严格签名`build/logs/authorization-types-package-signature-20261117.log`通过。此包仅含已实现的操作审批呈现，不含新签名服务。未安装/替换当前App、未重启正式/开发后台、未读取真实凭据或连接真实服务器。


## 2026-11-17 — 精简审批与英文窄窗，补齐 Touch ID 入口

按上一轮四张预览的反馈精简原生审批：删除应用连线、重复期限/撤销说明；保留申请方、权限类型、服务器与动作范围，长地址可换行。窗口默认内容高度520px改为400px，最小高度同步；卡片与期限控件连续排列，技术详情独立滚动，期限、配对码及决定区固定可见。闲置开关和132px时间选择同排，完整显示英文15 minutes；英文类型简为Server actions，验证方式使用Password/主密码…短按钮，决定组仍靠右。未改所有者验证、授权范围、协议或密码/系统验证的状态处理。

最终离屏20分支与相关交互回归通过`build/logs/test-20261117-130503-192728647.log`，图片`build/snapshots/authorization-compact-20261117/`；已目视380px英文、目录详情、中文明暗、密码及错误状态。英文预览新增文字/控件边界及验证按钮与决定按钮同排断言；目录根与配对码的高度/可见性断言保留。新增词条最初未满足中文原文往返规则，完整检查`build/logs/check-20261117-130252-2256465954.log`因此失败；改为原文一致的主密码…后，词条专项`build/logs/test-20261117-130503-1362244414.log`通过，未放宽验证。

SmallPen修订`b9567702098258cc022e66df3fc5916b6c3d30b995a5d6a83720aa2b8c4ecd60`，95页/17组件/66变体/152 Token，0结构错误、2个既有字体覆盖警告。待处理中心、操作授权对照页、规划中的密钥使用授权页新增同一指纹字形和40px验证入口，左侧验证、右侧决定；pages.json和DESIGN同步。这里的指纹指Touch ID按钮，没有额外添加SHA256字段。密钥页仍属规划，无签名后端、真实验证处理或可用Agent入口；主密码不能替代未来硬件密钥用户在场要求。

三页明暗6组合渲染/视图记录`build/logs/authorization-compact-{design,view}-{pending,server,key}-{light,dark}-20261117.json`，均已目视。独立对照稿均0渲染问题，每组合1项指纹字形对比检查跳过；待处理页每组合有1条已有info（窗口控制与侧栏背景叠加），8项文字对比检查跳过。最终changes检查0后续变动。未前台打开SmallPen或另起服务，未执行App内原型导航；CLI渲染不代表实际系统生物验证。

`go run ./tools build -check`通过，两个已提交生成文件仍为最新。本轮仅更新源码、设计和离屏预览，未重新打包或安装App，也未重启后台、读取真实凭据或发起远端连接；此前authorization-types候选不包含本轮精简，后续替换须重新打包。本机合成测试不替代各平台的原生认证与实际远端验收。

最终完整门禁`build/logs/check-20261117-130521-54098341.log`全部通过：vendor完整性、695文件格式、两前端依赖图、全库vet及默认测试，含中文词条往返与审批可见性/验证状态回归。最后一次源码变更后重新生成20张离屏图；本轮没有把中间失败检查当作通过。


## 2026-11-17 — 限时范围授权与风险升级需求梳理

记录用户减少授权疲劳的需求为REQUIREMENTS U38，DELEGATION新增“限时会话与风险分级”，明确范围内低风险放行、中风险仅确认当前动作、高风险直接拒绝，SSH密钥使用仍为独立登录签名授权。保留身份/范围/期限检查，未知命令不能因未命中护栏被当成低风险；普通外部SSH签名不代表后续命令受控。中风险审批与完整风险分类均标为尚未实现，执行入口仍待用户确认，没有改动策略或扩张现有运行权限。

本轮只读核对internal/approval/delegation.go、internal/client/delegation.go、internal/broker/control.go、internal/accesspolicy/{policy,command_guard}.go及cmd/ssh-client/session_exec.go：已有同进程活会话复用，单次新CLI请求另行批准；当前命令护栏为明确标注的尽力而为过滤器，不是完整风险判定器。未采集实际用户调用链，不将逐次启动CLI推断成已证实的根因。仅更新需求文档，未重复运行程序测试、修改SmallPen或安装App；上一轮代码门禁证据为build/logs/check-20261117-130521-54098341.log，不把它当作本轮风险功能验收。


## 2026-11-17 — SmallSSH CLI/SDK 限时受控会话与单次风险确认

用户确认执行入口为SmallSSH CLI/SDK，由后台逐项检查。新增`guarded`权限，连续exec和省略权限的JSON行入口默认使用，SDK通过现有RequestSession明确申请；read和显式full保留原语义。分类在内部operationrisk模块计算：结构化读取和固定受限查询低风险，写操作及未知/复合/脚本命令需要单次确认，已识别破坏性命令、关键路径变更和交互终端/隧道拒绝。原DestructiveCommand适配同一实现，未增加第三方依赖或改变vendor。

approval在单一锁内复核父授权并创建完整绑定的低风险或待确认子操作，审批证明绑定具体参数、路径、目标和父关系，期限不超过父会话。相同待确认操作原子去重，自报名称变化不绕过；重复申请返回ErrReviewPending，不共享票据或执行权。当前操作批准不扩大/续期父授权，拒绝当前操作保留其他有效范围；父授权撤销/自然到期/断线与原策略变更路径仍取消子操作。broker在签发与启动路径检查服务器策略，guarded即使遇到Unrestricted也不放行已识别高危或交互通道；审计保留requested/delegated、父关系、操作摘录与high_risk_operation拒绝。

CLI连续命令和JSON行入口已接Wait；拒绝当前操作可继续下一行，撤销/断线不回退新的一次性申请。SDK新增可识别的ErrOperationDenied（仍匹配原ErrDenied），普通Request返回pending时使用既有Wait/Start。App审批显示限时受控规则；子操作直接展示完整命令或路径、内容摘要及原因，只允许本次。没有更改SSH密钥签名授权、硬件用户在场策略或标准外部Agent服务，后者仍未实现。

针对性验证：approval状态机及并发去重race通过build/logs/test-20261117-152212-race-3151675344.log；加入结构化关键路径检查后的分类race通过build/logs/test-20261117-152641-race-2372551168.log；真实回环SSH/SDK与权限/审计race通过build/logs/test-20261117-152641-race-927940309.log；连续CLI及默认JSON行审批/拒绝/EOF/长度边界race通过build/logs/test-20261117-152641-race-2240601292.log。以上均使用合成凭据、本机临时服务和私有夹具目录，无真实远端访问。覆盖了Unrestricted不绕过guarded、未确认不得执行、变更参数/旧证明/旧票据不能复用、重复请求不弹多张、撤销和期限失效，以及拒绝当前操作后低风险可继续。

原生UI与27张离屏分支通过build/logs/test-20261117-152727-997093455.log，图片build/snapshots/guarded-sessions-20261117/，已目视受控会话、单次命令、写入摘要、英文窄窗及明暗主题；新增可见性断言核对完整命令/路径在决定区上方，未削弱既有根目录/配对码检查。SmallPen修订f7e7f8eb6882c7a2bbcd4a94765962841d1b4524c0d13bb1d5eb094aeb12ee25，97页/17组件/66变体/152 Token；新增两个字段对照稿和目录入口。两个新页明暗4组合0渲染问题，各1项指纹字形对比检查跳过，四图已目视；证据build/logs/guarded-{design,view}-{grant,review}-{light,dark}-20261117.json。保留两个既有字体覆盖警告，未打开前台窗口或另起SmallPen服务/验证App内原型导航。

GLOSSARY、REQUIREMENTS U38、DELEGATION、CLI/USAGE/README、SECURITY、ARCHITECTURE及设计映射已同步。已识别高危规则与静态路径检查是尽力而为，无法穷举脚本内部行为或解析所有远端别名；未知不会自动归为低风险，本模式不承诺服务器端沙箱。会话仍仅属于同进程活连接，不能逐条启动新CLI后继承旧授权。本轮未打包/替换App、重启正式或开发后台、读取真实凭据、触发真实Touch ID；各平台真实认证和远端环境仍待实机验收。


末次复核增加了独立闲置回归：未批准的中风险申请不能刷新父会话闲置期限，新的所有者批准才计入活动。回归先在build/logs/test-20261117-153014-234369692.log复现失败，随后调整创建/批准两个状态点；完整approval包race通过build/logs/test-20261117-153038-race-2442514695.log，覆盖闲置到期及有效所有者确认两种情形。未延长操作或父会话的绝对期限，也未通过放宽断言掩盖问题。此修正不改变UI/SmallPen；前述离屏图与渲染证据仍对应当前布局。

生成文件检查build/logs/guarded-generated-check-20261117.log通过，resources/THIRD_PARTY_NOTICES.txt与internal/systemauth/native-hashes.json均为最新。之前完整门禁build/logs/check-20261117-152750-3643724487.log通过，但因末次闲置约束有源码修改，另执行最终门禁，不将旧结果标为最终证据。

最终完整门禁`build/logs/check-20261117-153115-1866817084.log`全部通过：vendor完整性、701文件格式、两个前端依赖图、全库vet和默认测试，包含最后的闲置约束回归。最终SmallPen changes检查为0变动。以上完成代码/设计与本机合成自动验收，不代表已安装到用户App或完成真实硬件/远端验收。


## 2026-11-17 — 核对窗口承载、工作区标签与后台选项状态

按用户询问逐项只读核对workspace_menu/workspace_canvas/workspace_tabs、settings_window、editor_window、tool_window、vnc_workspace及presence/startup/backend生命周期：顶部Tab与标题栏不是互斥模式；标题栏承载工作区Tab，画布卡片为标题行。设置分类为独立设置窗，后台off/application只决定主窗口关闭退出或隐藏驻留，Dock/菜单栏只控制恢复入口。独立smallsshd进程已有，但工作台退出仍通过desktopSession.Close主动Lock，保留其他调用方/明确后台任务的完整后台产品模式未完成。

DESIGN新增“窗口承载与后台状态矩阵”，区分承载、排版、生命周期及开发隐藏启动，列出当前单独窗口关闭/锁定/驻留行为和未统一的独立窗跟随策略。GLOSSARY补充应用驻留/工作区标签/面板标题；pages.json六个已有设置分类的presentation由tabs-or-window纠正为settings-window，并明确运行与后台的状态/限制。未修改Go代码、SmallPen HTML、用户偏好或实际窗口行为，不把规划目标记为已交付。

headless定向回归build/logs/test-20261117-162333-3390372420.log通过，覆盖驻留恢复入口/退出保护、窗口承载和顶部Tab、画布及设置/工具窗口生命周期。仅文档与承载元数据变化，未重复完整程序测试或构建；上轮Go代码完整门禁仍为build/logs/check-20261117-153115-1866817084.log，不把它当作新窗口行为验收。

读取正式目录behavior.json与layout.json仅选择UI状态字段：application、Dock隐藏、菜单栏显示、canvas=false、工具区底部显示且选择命令；保存值不代表实时GUI状态。只读服务身份/TLS状态核验build/logs/service-20261117-162135-1272171456.log通过，后台locked、generation5、启动时刻2026-11-17 02:22:23。开发窗口status通道不存在，因此未声称已观察实际窗口可见性；未调用show或前台启动App、未解锁、未读取凭据、未重启后台。

## 2026-11-17 — 修复前台 Dock 入口与驻留窗口遗漏

用户澄清问题为打开窗口却没有Dock入口、VNC使用后可能在桌面遗留窗口。源码确认presence直接将ShowDockIcon保存偏好应用到前台激活策略，application关闭时只隐藏主窗口，各独立窗口的异步创建回调还会直接Show。新增presence_visibility统一前台Dock、相关窗口隐藏/恢复与异步创建门控：macOS窗口打开或最小化时保留Dock，偏好只控制隐藏驻留；驻留逐一隐藏当前可见的设置/文件/工具/审批/兼容VNC窗口，并在macOS调用App.Hide覆盖原生辅助窗口。恢复先显示Dock，再恢复主窗和仍归工作台所有的相关窗口，已关闭/替换/断开的窗口不复活。隐藏不取消VNC；兼容独立VNC窗添加主窗口Parent。默认VNC仍是工作区Tab，全屏复用主窗和原WebView，没有新增弹窗入口。

先以headless回归复现三个失败：前台Dock关闭、相关窗口遗漏、驻留后VNC迟到创建仍Show，证据build/logs/test-20261117-163940-1627906754.log。最初误将测试文件命名为presence_windows_test.go，导致macOS跳过，build/logs/test-20261117-163836-1539606406.log的“no tests to run”不计覆盖；改为presence_visibility_test.go后才取得上述真实失败。修复后的相关窗口/退出/迟到VNC竞争检测通过build/logs/test-20261117-164157-race-2735625600.log；补充off/application×Dock×菜单栏的组合、无恢复入口拒绝、关闭VNC不复活、未显示开发启动不弹窗后，race通过build/logs/test-20261117-164330-race-2182971467.log。所有夹具均为本机合成，不访问真实服务器。

原生设置改为“主窗口关闭行为”“驻留时显示Dock”，短文案说明打开/最小化保留Dock，独立服务驻留仍未接通；英文关闭选项缩为Keep running/Quit app。UI Tester及12张opt-in离屏图位于build/snapshots/presence-windows-20261117/，包含720px最小宽度的中文明暗和英文；新增三图与SmallPen明暗图已目视，Dock标签和关闭选项可见。原生图证据build/logs/test-20261117-164455-24333271.log，最终英文更新证据build/logs/test-20261117-164855-2238567900.log。SmallPen修订24e619a05e4c9e02e4420bbe2a8276f96baa390a49fb3bd5a1caba8553f31eee，97页/17组件/66变体/152Token；两主题结构0错误，保留传输页面两项既有字体覆盖警告；本页各有原生窗口按钮与导航背景相交的一项info，五项混合背景文字对比跳过。证据build/logs/presence-windows-{design,view}-{light,dark}-20261117.json。同步DESIGN规则、pages.json状态/动作/源码映射、REQUIREMENTS U05、USAGE及驻留术语。未另起SmallPen服务或打开前台页面。

最终完整门禁build/logs/check-20261117-164855-1432135557.log全部通过：vendor完整性、703文件格式、桌面/CLI依赖隔离、全库vet及默认测试。macOS arm64候选构建通过build/logs/presence-windows-app-build-20261117.log，生成文件检查通过build/logs/presence-windows-generated-check-20261117.log。冻结候选build/presence-windows-review-3q8m14af/SmallSSH.app，codesign深层严格验证通过build/logs/presence-windows-candidate-20261117.log；同目录candidate.json记录App/CLI/后台SHA256。候选未安装或启动，未停止现有后台、修改正式偏好或读取真实凭据。

本轮只读系统窗口/运行应用清单及NSRunningApplication查询均未找到正式SmallSSH GUI，未观察到用户报告的实际桌面残留窗口。上述证据证明代码状态、异步回调及本机离屏UI回归，不替代真实macOS Dock、WebKit辅助窗口和VNC全屏/驻留的系统窗口验收，也不代表Linux/Windows原生行为已验收。未调用dev show、前台启动App、全局按键或真实系统授权对话框。

## 2026-11-17 — VNC 右侧工具栏、自适应更多菜单与附加按键

按用户评审移除VNC查看器顶部横栏，使用右侧40px工具栏、32px图标目标和4px间距，桌面保留完整可用高度。常用按键/粘贴/复制/适应/全屏按优先级保留，高度不足将原DOM控件移入“⋯”并在恢复高度后移回，不复制回调/状态或重建RFB。只读、重连、断开和连接状态详情在菜单；状态圆点提供悬停信息，连接失败和密码保存结果另有短提示。更多菜单贴底部按钮，按键层贴顶部按钮，互斥且限高滚动；外侧点击和Escape关闭，按键入口收纳时Escape回到可见“⋯”。

参考noVNC v1.7.0官方vnc.html/app/ui.js附加按键交互（https://github.com/novnc/noVNC/blob/v1.7.0/vnc.html），以现有固定sendKey/sendCtrlAltDel API实现Ctrl/Alt/Shift/Win按住切换、Esc/Tab/CtrlAltDel直发和其余键折叠，删除复选框/下拉框/独立发送行。虚拟修饰键支持后续真实画面键盘输入，图标提示按住状态；取消按住、失焦、页面隐藏、断开/重连/销毁时逆序释放，切换只读前先释放。未修改上游noVNC或新增依赖/浏览器剪贴板权限，原生双向剪贴板、64KiB/Unicode协商及密码目标绑定保持。

headless Chrome及真实合成RFB验证使用tools入口、独立临时浏览器目录和假凭据，不触碰系统剪贴板。首轮race通过build/logs/test-20261117-173958-race-694779230.log；补充320×100全部控件收纳/按键入口与Escape恢复后通过build/logs/test-20261117-174358-race-2080806625.log。密码结果短提示更新通过build/logs/test-20261117-174600-race-2421245207.log。最终菜单底部锚点调整后，完整vncview包（含浏览器集成）的check/race通过build/logs/check-20261117-174708-race-879237110.log，亦复核703文件格式、vendor及前端依赖图。

新增断言覆盖40px右侧栏、无header、桌面高度为视口减8px；320×180下溢出菜单位于视口内；320×100下全部动作仍可经“⋯”访问；放大后原控件/适应状态与同一canvas保持；菜单与按键互斥、Escape焦点；Ctrl+Shift+P/Alt+Tab/F12/CtrlAltDel的真实RFB按下/抬起顺序、虚拟Ctrl配合物理A、只读前和失焦时释放。既有登录、错误密码、RFB3.3精简服务、原生剪贴板双向/编码/大小/迟到返回/只读与重连隔离、全屏固定回调测试仍通过，浏览器clipboard getter继续被毒化为一旦访问即失败。14张浏览器快照位于build/snapshots/vnc-right-toolbar-20261117/，已目视明暗按键层、底部更多菜单及紧凑收纳代表图。

SmallPen最终修订481660810445ae4f26294d3adc8a94fe59b8c5c89286939ff94472bd3616e27b，98页/17组件/66变体/155Token。查看器/全屏/按键明暗6组合在01794395edec1468aeb8706d8cf77054fcd8a4d8bbcd4bfcc101b5a55cb6b2ab渲染0问题，随后这三页和Token未变。菜单初稿空标签覆盖未隐藏组件默认文案，修正为隐藏标签并提高局部边框对比后，最终菜单及页面地图7明暗4组合0问题。结构0错误，保留传输页两项既有字体覆盖警告；查看器每主题15项、全屏与按键每主题1项混合背景文字对比跳过，菜单及地图无跳过。查看器四页明暗8图已目视。证据build/logs/vnc-right-toolbar-design-check-20261117.json及vnc-right-toolbar-{design,view}-{workspace,fullscreen,keys,more,map}-{light,dark}-20261117.json。末次changes为0，未另起SmallPen服务或打开前台App。DESIGN、pages.json、页面地图7、REQUIREMENTS U37、USAGE、SECURITY及模块README已同步。

全库check通过build/logs/check-20261117-174358-1365771208.log；加入密码结果提示后再次通过build/logs/check-20261117-174606-313411316.log。最终仅菜单锚点CSS及其浏览器断言的调整由上述174708定向check/race覆盖，未重复运行无关核心包。macOS arm64构建、生成文件检查分别通过build/logs/vnc-right-toolbar-app-build-20261117.log和vnc-right-toolbar-generated-check-20261117.log。冻结候选build/vnc-right-toolbar-review-bjk02qzb/SmallSSH.app，深层严格签名通过build/logs/vnc-right-toolbar-candidate-20261117.log；同目录candidate.json记录App/CLI/后台SHA256。候选未安装、未启动，也未停止正式/开发后台、修改正式偏好或读取真实远端凭据。自动证据只覆盖本机headless浏览器与合成服务，真实macOS/Linux/Windows WebView、系统窗口全屏及远端桌面快捷键动作仍需各环境实机验收。

## 2026-11-17 — VNC 更多按钮仅在真实溢出时出现

按用户纠正移除常驻“⋯”及固定收纳项。右侧空间足够时8个操作全部直显，含只读、重连和断开；只有完整工具栏放不下才预留更多按钮并移动溢出的原控件。放大后移回全部控件、关闭收纳菜单并隐藏“⋯”，HTML初始隐藏更多入口，避免加载闪现。未改变按键、剪贴板、认证、连接或后台行为。

针对性race通过build/logs/test-20261117-175935-race-3424112377.log；最终完整vncview包（含headless浏览器）check/race通过build/logs/check-20261117-180054-race-972084701.log，覆盖vendor、703文件格式、两前端依赖图、vet及包内全部测试。回归明确断言正常尺寸全部8项直显且无更多入口；320×180才有收纳；菜单打开时放大立即隐藏入口/菜单且保持适应状态和同一canvas；320×100下8项全部收纳仍可打开按键、Escape回到可见入口。原有RFB键序、只读松键、双向原生剪贴板、认证、重连隔离等回归保持通过。快照在build/snapshots/vnc-overflow-only-20261117/，已目视正常无“⋯”及紧凑收纳代表图。本轮只改查看器与布局回归，未重复上轮已通过的无关核心包全库测试（build/logs/check-20261117-174606-313411316.log）。

SmallPen修订d67849a06ca955fbce930dc7ca9851e68a2a40ad440f962bcdd5bd2bc9b37a05，99页/17组件/66变体/155Token；更新默认/全屏直显、更多菜单和页面地图，新增300px高的“VNC 工具栏收纳”，对应六项可见、重连/断开溢出。五页明暗10组合0渲染问题；工作区各14项、紧凑页各1项混合背景文字对比跳过，其他组合无跳过；保留两个既有传输页字体警告。证据build/logs/vnc-overflow-only-design-check-20261117.json及vnc-overflow-only-{design,view}-{workspace,fullscreen,compact,more,map}-{light,dark}-20261117.json。已目视正常明暗、全屏暗、收纳明暗及菜单亮；同步DESIGN、pages.json、REQUIREMENTS、USAGE及模块README。未新建SmallPen服务、前台打开工作区或声称静态原型已能响应真实窗口缩放。

macOS arm64构建及生成文件检查通过build/logs/vnc-overflow-only-app-build-20261117.log、vnc-overflow-only-generated-check-20261117.log。冻结候选build/vnc-overflow-only-review-p9l9875n/SmallSSH.app，深层严格签名通过build/logs/vnc-overflow-only-candidate-20261117.log，同目录candidate.json记录SHA256。未安装、启动或替换当前App，也未重启后台、读取真实凭据或系统剪贴板；本机headless/合成RFB结果不代替各平台WebView实机验收。

## 2026-11-17 — 安装 VNC 条件收纳候选并核对密钥功能边界

用户明确要求替换App。以冻结的build/vnc-overflow-only-review-p9l9875n/SmallSSH.app为唯一安装源，重新核对主程序/CLI/后台SHA256与深层严格签名。只读SDK确认正式旧后台locked（generation5），GUI未运行；核对PID62677的当前UID、启动时刻、精确路径、lsof实际txt映像及动态codesign后，向该已锁定进程发送SIGTERM，由其正常信号处理路径退出。没有可用的已解锁所有者控制会话，本次未调用OwnerSession.Stop，也未读取主密码/解锁以取得该会话；用户明确的替换授权用于本机正常停进程，未把普通IPC改成无需认证Stop或放宽身份校验。证据build/logs/vnc-overflow-install-before-20261117.json、vnc-overflow-service-stop-20261117.json及service-20261117-180933-3219524908.log。开发后台PID23417未停止。

安装任务通过build/logs/install-20261117-181016-2959345317.log，/Applications/SmallSSH.app三份程序SHA256与候选一致，无安装临时目录/旧版备份。启动新包内无界面smallsshd并明确使用正式数据目录；首次探测早于socket就绪，保留该次失败service日志，不把它作为成功。就绪后SDK进程身份/TLS/status握手通过build/logs/service-20261117-181034-3319306161.log：locked，generation1，启动2026-11-17 18:10:30.981852+08:00。新PID82715动态codesign有效；记录build/logs/vnc-overflow-installed-20261117.json。未启动/前置GUI，未访问系统剪贴板或真实服务器、未输入真实凭据；重启清除了后台快速解锁内存，下一次打开需重新解锁。候选的headless/设计及签名证据沿用上一节，不重新编译或重复无关测试。

同时仅通过源码与现有规格核对用户询问的本地SSH/密钥设计：已实现私钥文件选择/路径导入、格式与口令预检、公钥算法/指纹展示，私钥及口令随vault.Profile加密保存，Agent按公开指纹选择、PKCS#11签名。独立可复用密钥实体、本地密钥生成/公钥部署/独立验证切换向导、对外SSH Agent和Secure Enclave SSH签名后端仍未实现。确认的规划入口是服务器“安全与密钥”，每个服务器登录身份默认独立生成，先保留原登录渠道，部署仅发送公钥，新连接验证成功后才确认切换。未把保险库解锁用的Secure Enclave密钥或客户端配对密钥描述成SSH登录密钥，也未在本轮擅自生成/导入/部署真实密钥。
## 2026-11-17 — Secretive 式 SSH 密钥签名、通知与限时授权

按用户“先1:1一样，再优化”的确认，以Secretive `56c2f841ed6400b43b60c4c94c36e172e3f7407d`为授权行为基线。新增macOS Secure Enclave P-256 SSH密钥生成与公开清单、标准SSH Agent服务端、原生系统验证、并发集中审阅、签名成功后的系统通知，以及1/5分钟、1/24小时的按密钥内存许可；未增加程序/服务器绑定的自定义限时策略。设置新增SSH密钥分类，含复制公钥、明确删除确认、主动限时授权、结束许可、待处理入口和通知状态；连接表单显式引用SmallSSH密钥指纹。后台管理走既有owner proof及workspace审计，Agent只开放列公钥/签名。服务器操作授权及CLI/SDK风险检查保持独立。同步REQUIREMENTS、DELEGATION、SECURITY、ARCHITECTURE、USAGE、GLOSSARY和页面源码映射。

原生子进程由Go后台持有，启动前校验构建指纹，命令通过继承管道传递，逐项检查当前签名的同目录父后台。签名前后核对调用进程与返回签名。支持同一Agent连接的有界并发及FIFO响应；客户端断开、超时、锁定和退出取消等待并结束子进程/窗口，取消后的认证回调不得重新提交许可。原生清除与删钥也撤销已排队的限时认证。打包后台解锁时发布socket，原生进程首次使用才启动，启动后台不访问真实钥匙串或弹认证UI。

完整门禁通过`build/logs/check-20261117-185720-1104605602.log`，包含vendor、格式、前端依赖隔离、vet和全仓测试；过程中发现并补齐新中文/英文目录，修复了测试夹具误用带后台reader的agent.NewClient读取原始帧的问题，早先失败日志保留，不作为通过证据。最终签名协议race门禁`build/logs/check-20261117-190106-race-1237722066.log`覆盖合成P-256精确签名、未知密钥/不兼容flags/坏签名拒绝、禁止管理动作、帧上限、同连接并发、响应归属、断开和退出取消。broker合成控制器检查owner proof绑定精确期限及断连取消；SSH连接测试核对managed只用后台解析socket、无环境回退。最终页面失败/取消回归通过`build/logs/check-20261117-190909-212634340.log`；4张明暗720/1080离线快照与检查见`build/logs/check-20261117-190107-3243544227.log`、`build/snapshots/ssh-identities-final-20261117/`，已目视窄窗、深色与公钥指纹。工具入口调整检查通过`build/logs/check-20261117-190527-73854030.log`。

`tools identity-qa`编译/签名真实原生模块，另以IDENTITY_QA入口只执行请求集合、跨密钥隔离、一次消费、四档单调期限精确到期及清除的纯内存检查；证据`build/logs/identity-qa-20261117-190429-799598618.log`。该入口不实例化运行时、不访问钥匙串/硬件、不请求通知或显示系统认证。五个Go桌面目标编译通过`build/logs/compile-20261117-190138-650572877.log`。原生两种Mac架构的最终生成指纹检查通过`build/logs/identity-generated-check-20261117.log`。增加`tools build -generate-only`以便只刷新许可证/原生指纹，编译临时输出仍归build，不依赖外平台桌面运行库下载。

最终macOS arm64 App构建见`build/logs/identity-app-final-20261117.log`；候选`build/darwin-arm64/SmallSSH.app`，约64.4MB。深层严格签名通过`build/logs/identity-bundle-signature-20261117.log`；`build/logs/identity-artifact-20261117.json`记录主程序、CLI、后台、原生Agent的SHA256，并核对包内Agent与清单一致。原生Agent384368字节，SHA256 `7e895dd48aaf67a0054b9014614d2421913475dbe2847d09f39263c1d9abf71d`。Intel Mac完整打包因下载上游固定libghostty-vt动态库网络超时失败，见`build/logs/identity-build-final-20261117.log`；不能把Go交叉编译和Swift编译描述为Intel完整App打包通过。未安装/启动候选、前置窗口、重启现有后台、读取真实密钥/服务器凭据或剪贴板。

实际Touch ID/密码回退、真实Secure Enclave、系统通知授权与按钮、原生批量窗口Dock行为、钥匙串跨构建访问仍需人工验收，自动结果不替代实机确认。当前ad-hoc预览用登录钥匙串默认应用ACL保存安全隔区opaque表示；正式配置的DP Keychain、固定签名身份及两种存储间迁移尚未发布验收。不是Secretive全量复制：本轮不含ML-DSA/智能卡、独立登录启动、自动公钥部署、持久签名历史页；签名代理随已解锁后台运行。限时授权覆盖整把密钥，结束许可只阻止后续签名，不撤回已建立的SSH认证；外部SSH命令不经过操作风险检查。

SmallPen最终源与视觉验证详见DESIGN本轮记录；仅CLI及无窗口检查，没有新建SmallPen服务或打开工作区窗口。系统通知稿是行为示意，不代表macOS实际通知样式已验收。
## 2026-11-17 — 替换 Secretive 式密钥授权候选并重启正式后台

用户明确要求替换。安装前重新比对本轮候选四份可执行文件与`build/logs/identity-artifact-20261117.json`的SHA256，并验证完整包深层严格签名；记录`build/logs/identity-install-before-20261117.json`。SDK身份/TLS/status检查`build/logs/service-20261117-193101-245441346.log`确认正式旧后台PID82715为locked、generation1；主GUI未运行。当前没有可用的已解锁OwnerSession控制连接，未为了Stop读取主密码或解锁。按用户本机替换授权，在重新核对UID、启动时刻、精确映像路径与动态codesign后，对此已锁定进程发SIGTERM并观察正常退出；记录`build/logs/identity-service-stop-20261117.json`。没有修改管理协议、绕过owner proof或放宽实例校验，开发后台PID23417保持运行。

通过`go run ./tools install -source build/darwin-arm64/SmallSSH.app`安装到`/Applications/SmallSSH.app`，日志`build/logs/install-20261117-193154-837995358.log`；不保留旧版备份或安装临时目录。安装后主程序/CLI/后台/原生Agent的SHA256与候选一致，深层严格签名通过。只启动已安装包内无窗口smallsshd，明确使用正式数据目录；新PID82036动态签名有效。SDK验证通过`build/logs/service-20261117-193210-747466519.log`，状态locked、generation1，启动时间2026-11-17T19:32:10.41707+08:00。安装与进程记录在`build/logs/identity-installed-20261117.json`；后台输出在`build/logs/identity-installed-daemon-20261117.log`。未启动/前置GUI，未解锁、生成真实密钥、访问系统剪贴板或连接真实服务器。下次打开App需用户重新解锁；真实Touch ID、钥匙串和系统通知仍沿用上一节的待实机验收边界，不把安装检查当作这些交互已验证。

同时仅核对用户询问的Agent用途：Secretive的密钥保管/授权签名职责，以及OpenSSH/Git文档中的SSH认证、Git SSH传输认证、Commit/Tag的SSH格式签名、SSHSIG文件签名和Agent托管CA密钥签发SSH证书。Git认证与提交签名是独立配置；GitHub若同一公钥两用需分别登记authentication/signing。未修改个人Git配置、GitHub密钥或仓库历史，未声称已实测真实Git签名。来源：[Secretive FAQ](https://github.com/maxgoedjen/secretive/blob/main/FAQ.md)、[Git SSH签名配置](https://git-scm.com/docs/git-config#Documentation/git-config.txt-gpgformat)、[GitHub公钥用途](https://docs.github.com/en/authentication/connecting-to-github-with-ssh/adding-a-new-ssh-key-to-your-github-account)、[OpenSSH ssh-keygen](https://man.openbsd.org/ssh-keygen)。本轮无源码/UI改动，沿用候选构建和门禁证据。

## 2026-11-17 — 隧道角色图、密钥列表修复与逐页界面整理

SSH 隧道按实际 Forward 监听/拨号位置绘制三节点与 SSH/TCP 动态箭头，本地/远程切换当前设备高亮，回环目标归属明确；减少动态效果时静止。弹窗收高、监听范围保留短提示；移除重复服务器副标题与顶栏独立终端按钮。设置、线路、连接、文件、命令与任务页删除重复常驻说明，保留真实目标、风险、错误和保活启闭短句。按用户指定 ui-design-filipiuk-style 复核 103 张设计页，统一层级/焦点/状态色、设置对齐、列表与危险操作主次；线路/命令/属性弹窗长内容仍可滚动，底部动作固定。没有变更授权范围、网络协议或已有任务路径。

原生密钥列表错误根因为密码项查询使用 ReturnData + MatchLimitAll，系统返回 -50；改为先枚举公开属性，再按精确 account 单条读取，保持原有 opaque 记录格式和钥匙串模式，失败不伪装为空列表。独立系统 API QA 在随机服务名下用两个合成记录验证空/单条/多条/删除，禁用认证交互并清理记录；同时复现旧调用 -50。真实 native 模块编译/签名与授权状态 QA 证据：`build/logs/identity-qa-20261117-194604-2213094848.log`。没有读取真实密钥或触发 Touch ID/通知。查询契约见 [Apple: Searching for keychain items](https://developer.apple.com/documentation/security/searching-for-keychain-items)。

完整门禁通过 `build/logs/check-20261117-201556-1396485004.log`，包含 vendor 校验、格式、前端依赖隔离、vet 与全仓测试。最后删除连接分类重复文字后追加当前源码门禁 `build/logs/check-20261117-202117-539138286.log`，覆盖连接表单/退出草稿保护、线路、保活、后台设置及生产快照。测试曾发现分类 tooltip 会先截获 Escape，现使用辅助描述而不创建该 tooltip，原有未保存守卫断言保持；早先失败日志不作为通过证据。全仓初次设置居中断言已按本轮明确的左对齐方案更新，仍验证 720px 可读宽度、窄窗控件与滚动，未删除几何检查。

opt-in 全离线 UI 快照通过 `build/logs/test-20261117-201330-656059681.log`，454 张合成状态 PNG 在 `build/snapshots/filipiuk-final-20261117/`。直接命令夹具补上正式主题后刷新见 `test-20261117-201438-1165422759.log`；末次连接文案快照由上述补充门禁覆盖。生产样本检查了浅/深色、560/720/1080 宽、长输入、空态、加载、失败、命令参数滚动、属性窄窗以及当前/历史任务；目视主要页面族与受影响实尺寸，不把 454 张夹具当 454 个独立产品页面。新增标准库-only `tools ui-review` 将现有离线图汇成编号联系表与索引，不捕获或启动真实窗口、不引入依赖。

SmallPen 全量 103 页 + 70 组件变体 × 明暗共 346 目标完成，结构 0 错误/0 警告；16 条 info 为菜单及原生窗口控件的有意覆盖，966 条对比等自动检查跳过，未称 coverage.complete。完整报告 `build/logs/filipiuk-all-pages-final-full-20261117.json` 对应修订 `728c8959…`。最终仅目录一条说明（`54f52469…`）和两页分类冗余文字（最终 `570844b1a760f8d8773ef6252131fdedff0109f3a8e7de6461a0f68f3a11cde9`）追加明暗渲染 0 问题，见 `filipiuk-map-final-check`、`filipiuk-policy-final`、`filipiuk-policy-error-final`；策略页每主题四项对比跳过。206 张逐页浅/深色预览的页面/主题/修订与渲染哈希保存在 `build/logs/filipiuk-final-previews-20261117.json`；联系表仅为当轮 QA（`build/review/filipiuk-*`），没有另建日常静态导出图库。目录、元数据和设计说明均已同步。

macOS arm64 候选 App 构建在 `build/darwin-arm64/SmallSSH.app`，最终日志 `build/logs/filipiuk-app-final-build-20261117.log`，约 64.4MB；深层严格签名与四份程序 SHA256 见 `filipiuk-app-signature-20261117.log`、`filipiuk-app-artifact-20261117.json`。包内原生身份模块 SHA256 `abda398ff66a81300d97f8047ae1e10bd1a6386834e57ad689db0d72f211a883` 与生成清单一致；两种 Mac 架构原生生成文件检查通过 `build/logs/tunnel-copy-build-check-progress-20261117.log`。本轮未替换 `/Applications`、停止/重启用户后台、启动或前置 GUI、另起 SmallPen 服务或使用真实服务器凭据/剪贴板。真实 Secure Enclave、既有钥匙串条目的跨构建 ACL、系统认证/通知、在线工作区及原生窗口仍待人工验收；本地离线结果不扩展为其他系统或 Intel 完整 App 包已验收。

## 2026-11-17：连接拖拽、当前授权与统一审计

本轮按用户明确确认的「显示当前有效授权，支持调整和撤销」完成实现。分组整组拖动、组内排序、跨组换组通过原生typed drag/drop写回加密保险库；以完整列表快照校验并发更新，过滤/折叠不会遗漏成员，保存失败不先发布，回调不覆盖更新的列表快照。顺序采用持久化profile顺序，保留凭据、线路、备注、VNC配置和运行连接。MyGo从0.7.1升级到正式0.9.2，版本仍只在go.mod；审阅上游setter修复后保留本地两条密码getter防护，重新应用sing-vmess补丁并核对原有哈希。依赖流程通过`build/logs/deps-20261117-203252-303428560.log`；补丁依据及新哈希见patches/README.md与vendor-integrity.json。

当前授权分列服务器范围授权与SSH密钥签名租约，显示固定到期时间、最近使用、有效闲置规则；支持不因闲置撤销或1/5/15/30分钟、逐条立即撤销、关联审计。服务器规则变更绑定ID/Nonce与一次性所有者证明，不能延长总期限、重置最近使用或复活已失效授权；收紧规则可即时取消子任务，活动元数据不改变既有撤销证明。密钥租约由单调时钟控制、仅成功签名刷新使用时间，支持逐租约调整/撤销。真实owner RPC与UI菜单回归：`build/logs/test-20261117-211314-4287525945.log`；最后桌面回归：`build/logs/test-20261117-211949-2237517093.log`；最终完整门禁也包含这些测试。

统一审计保留授权生命周期、所有者直接操作、CLI/SDK执行、每次实际内置系统采样，以及密钥生成/删除、验证决定、限时租约和SSH签名结果。新记录保留进程实例、映像标识、可观察路径、可用的决定进程、目标/密钥指纹、权限范围、完整受支持命令与路径、结果/退出码及摘要；口令、私钥、挑战原文、签名和输出不落盘。原生事件先经可信管道写入，签名由Go校验并持久记录结果后才释放，审计失败拒绝签名。实际采样与公开同名命令仍是不同调用来源，缓存分发不伪装成远端执行；相关真实loopback回归通过`build/logs/test-20261117-211735-1467784354.log`。

归档改为编号保留，不再覆盖唯一旧文件；历史游标跨多次轮转继续。支持类型/应用/结果/时间/内容/关联ID筛选，统计覆盖匹配范围及应用，JSONL/CSV遍历全部匹配分页。JSONL保留原始JSON/prev，CSV处理公式前缀；写入失败不替换目标文件。JSONL首尾、CSV提交前校验完整链，分页不再重复全链扫描；统计缓存有界并随实际追加更新，返回值不共享可变切片；每页仅读取各文件稳定首行及所需记录。功能、帧预算、长期轮转、原文/hash、缓存更新、筛选、CSV与签名失败关闭回归通过`build/logs/test-20261117-212626-3672156915.log`，新增缓存回归由最终门禁覆盖。

最终必需门禁通过`build/logs/check-20261117-212841-185853800.log`：745个Go文件格式、vendor补丁/哈希、两个生产前端依赖隔离、vet及全仓测试。升级后的原生Tester会拒绝点击被遮挡/禁用控件，保存保护测试改用真实坐标指针事件；程序化创建8窗后的上限测试先排成可见网格。分组顺序断言按本轮持久顺序更新；编辑器按MyGo新版保留混合换行的实际保存字节计数。没有删除身份、保存、键盘、几何或撤销断言。

MyGo升级后的全离线页面回归通过`build/logs/test-20261117-212242-3222341681.log`，514张PNG在`build/snapshots/mygo-092-authorization-final-20261117/`，覆盖原页面族及新增授权中心。新增中心单独60张中英文/明暗/560、1080、1440宽的当前授权、审计、详情、空态、错误状态快照见`build/logs/test-20261117-211736-4205196669.log`、`build/snapshots/authorization-center-20261117/`。目视检查窄英文、明暗当前授权、宽审计和选中详情；修正英文导航截断及重色选中行。联系图位于`build/review/authorization-center-20261117/`。后续仅审计存储读取与完整字段边界修正，不改变这些GUI布局；全部夹具为合成数据，不创建真实桌面窗口、系统授权或剪贴板操作。

SmallPen最终修订`89f74e83962baa9eaa82650106d525665fdb7e9f96f9210efccde11afd97e3fa`：107页、18组件、70变体、159 Tokens。7个修改/新增页面与浮层各明暗，共14组原生渲染，报告`build/logs/authorization-design-final-render-20261117.json`，结构0错误0警告，无文字溢出或越界报告；8项信息为既有原生窗口控制叠在侧栏背景上的有意覆盖。透明/混合对比与视觉意图仍有跳过，未宣称完全自动验收。受影响源、pages.json、DESIGN及安全规格已同步。复查现有在线服务`127.0.0.1:58766`拒绝连接，证据`build/logs/authorization-smallpen-live-status-20261117.json`；没有另起服务、抢占窗口或声称完成在线点击验收。

原生模块/纯租约状态和禁用交互的随机钥匙串合成记录QA通过`build/logs/identity-qa-20261117-211535-2893111032.log`；覆盖单调时间、活动刷新、固定期限、闲置收紧/关闭、旧租约ID及到期后不得恢复。未读取真实密钥，也未执行真实Secure Enclave/Touch ID签名。生成文件检查`build/logs/authorization-generated-check-20261117.log`确认两份受提交产物最新。最终macOS arm64包`build/darwin-arm64/SmallSSH.app`约65.7MiB，构建`build/logs/authorization-final-app-20261117.log`，严格深层签名验证通过；体积和主程序SHA256记录在`build/logs/authorization-final-app-artifact-20261117.json`。

本轮未安装或启动候选App、停止用户SSH/VNC会话、重启现有后台或创建新的SmallPen实例。真实Secure Enclave验证、通知/指纹UI、旧钥匙串ACL、在线SmallPen和真实窗口仍需用户实机验收；离屏结果不代表其他桌面系统或Intel完整App已验收。审计是本机私有明文及链/保险库锚点校验，不是第三方签章；普通SSH Agent看不到登录后的命令；旧版本已覆盖/截短资料不可恢复。具体边界与归档异常处理见SECURITY本轮章节。

## 2026-11-17 · 授权审计空间重排与 SSH 离线保存

- 设计依据：用户指定的 ui-design-filipiuk-style。设置窗口内部的授权分类改为横向标签，移除重复侧栏；审计记录使用全部剩余宽度，搜索框伸展。记录区按实际布局宽度选择表格或紧凑摘要，至少780 DIP时详情靠右独立滚动，窄窗使用200 DIP底部详情；关闭详情恢复整宽记录。移除无用的选行说明，固定底部分页，修复窄窗详情挤出分页的问题。保留全部审计字段、申请方来源标识、筛选、统计、分页、关联记录与导出。
- 保存连接移除自动连接选项、状态与代码分支。新建、编辑、备注重试都只做本机校验和加密持久化；不解析目标 DNS，不读取远端身份，不联系目标或代理，不自动选择服务器或激活工具。存储允许尚无指纹的配置，但实际 SSH 拨号仍在任何外连之前拒绝未确认指纹。
- 首次显式连接才读取并确认主机指纹，探测和实际登录沿用保存 / 本次选择的线路，失败不回退。独立的所有者签名 HostTrustChange 精确绑定连接 ID、地址、端口、原指纹和新指纹，只更新指纹；拒绝过期确认及非规范 SHA256 指纹，落盘成功后才发布内存状态，不覆盖并发修改的凭据或线路。指纹变化的重新核对也改用该写入，并保留临时线路、终端选项和凭据。
- 回归覆盖：TCP / DNS canary 验证保存无外连、无解析；无效凭据及备注失败后的重试仍离线；首次探测取消不认证、不保存信任；默认线路故障不直连回退；临时线路用于首次与变更指纹核对；明确连接可建立并使用真实合成 SSH / Ghostty 终端；所有者证明绑定、篡改拒绝、原子写失败及过期确认均有测试。仅使用隔离本机合成服务和凭据。
- 完整门禁通过：`build/logs/check-20261117-220043-549488305.log`（vendor补丁、750个Go文件格式、两个前端依赖图、go vet、全部默认测试）。真实终端专项：`build/logs/test-20261117-215533-284865695.log`；临时线路重新核对与设置布局：`build/logs/test-20261117-220015-2102330349.log`。没有修改依赖或 vendor。
- 无窗口视觉核验：`build/snapshots/authorization-redesign-final-20261117/`共76张离线PNG，包含实际设置外壳的560/1080/1440宽度、中英文、明暗主题、有效授权、审计、详情、空态、错误及连接表单 / 指纹确认。初轮和最终快照分别见`build/logs/test-20261117-215809-2353612303.log`与`build/logs/test-20261117-215933-4106028083.log`。人工检查宽窗右侧详情、1080紧凑摘要及560英文底部分页，未启动桌面应用或打开真实服务器。
- SmallPen正式源及pages.json同步，修订`27a0e8413d12efaa2b6a0d4ede973703e63d7eeb79a421d0340d58169625c61e`，107页 / 18组件 / 159 Token，check为0错误、0警告。历史、活动、待处理、无有效授权和常规连接有明暗render记录，见`build/logs/smallpen-authorization-*-render-20261117.json`和`smallpen-offline-save-render-20261117.json`；历史PNG与spec已查看。历史 / 活动页的原生窗口按钮与导航背景重叠为预期的info，不是记录内容溢出；对比度检查仍有渲染器明确跳过的透明背景组合，不能当成全量视觉证明。已有58766服务未响应，未另启实例、未前置窗口；在线用户验收仍待进行。
- 本机候选App：`build/darwin-arm64/SmallSSH.app`，未替换或重启用户现有App / 后台。构建记录`build/logs/authorization-offline-save-release-build-20261117.log`，严格深度签名校验`build/logs/authorization-offline-save-app-signature-20261117.log`通过，生成文件检查`build/logs/authorization-offline-save-generated-check-20261117.log`通过。主程序SHA256为`6387e8a6f6d22c86a42124440371976efdb5058b37225da8d9ab2a9524724780`；完整产物元数据在`build/logs/authorization-offline-save-app-artifact-20261117.json`。本轮仅本机macOS arm64验证，未连接真实代理或外部SSH服务器。
- 一次包含受保护目录文字引用的shell文档替换被自动审批阻止，未执行；随后采用不引用该目录的文档追加说明完成USAGE、SECURITY、REQUIREMENTS及DESIGN同步，明确替代旧保存时探测 / 自动连接的描述，没有访问受保护内容。

## 2026-11-17 · 安装授权审计与离线保存修复包

用户明确要求替换。先核对候选包主程序SHA256与已验证记录一致，再退出正式App、后台及随后台退出的SSH密钥代理；开发后台未动。通过`go run ./tools install -source build/darwin-arm64/SmallSSH.app`替换`/Applications/SmallSSH.app`，安装后不保留旧包备份，不启动或前置窗口，不读取或修改用户连接凭据。

安装记录：`build/logs/install-20261117-221239-1634423062.log`。桌面、后台及CLI三份可执行文件逐一与候选包SHA256比较一致，安装目录深度严格签名校验通过；报告为`build/logs/authorization-offline-save-installed-20261117.json`。代码与前轮通过完整门禁的候选包相同，未重新编译或重复运行测试。用户需重新打开App并解锁，真实服务器与代理连接仍由用户主动发起。

## 2026-11-17 · 替换后左侧列表为空的现场排查

用户确认缺失的是左侧服务器 / 分组。正式App进程98392运行安装目录的新程序，直接启动的后台98489使用原正式数据目录；开发后台23417使用独立development目录，不与正式库混用。正式加密库22725字节，修改时间早于本次安装；正式后台身份核验通过且处于unlocked状态（`build/logs/service-20261117-221433-403706464.log`）。安装包内CLI只执行公开只读hosts查询，成功返回13条连接，未发起SSH / 代理连接；脱敏计数证据在`build/logs/postinstall-data-discovery-20261117.json`。

布局配置未隐藏侧栏，宽度约275 DIP；没有更改布局、加密库、凭据、分组或连接。后台能读到记录，但尚未确定左侧没有显示的具体原因。后台窗口工具返回window_not_found，45秒只读等待仍超时，未前置窗口或发送按键；已请用户将主窗口显示到当前桌面。当前结论仅证明正式库非空，不能据此声称界面问题已修复；本轮未改源码或重建安装包。

## 2026-11-17 · 主窗口可见后的左侧列表复核

用户将SmallSSH主窗口显示到当前桌面后，后台窗口检查成功读取进程98392、窗口7700（1280×900）。可访问性树包含13条服务器记录和4个分组，搜索框为空；ScreenCaptureKit截图的实际画面也显示全部13条记录，与前轮正式后台公开hosts查询计数一致。当前主窗口列表已显示，未复现用户此前报告的空白，不能据此认定触发原因已定位或代码已修复。

本轮仅观察现有窗口，没有点击连接、前置窗口、发送按键、变更配置或读写凭据，没有修改源码、重新导入数据、重建或替换App。没有把现场服务器信息写入QA夹具；检查截图为Orca临时只读捕获，不加入设计稿或仓库资产。


## 2026-11-17 · 终端一键选中继、首次指纹与运行代理快捷停止

- 按用户最新反馈，将日常入口改成服务器右键 → 终端连接，直接选择默认 / 直连、已连接的SSH中继、运行就绪的SOCKS5入口或已保存线路。移除本轮试做的用途向导；手填端口转发留在“高级 SSH 隧道…”。选择即打开新的目标终端，默认保存配置保持不变。
- 后台仅解析所有者批准且运行中的源操作；中继沿用源会话捕获的本次线路。运行入口及首次指纹探测的线路引用均进入签名证明。源失效、类型不匹配或外部调用者选择运行线路时拒绝，不回退直连。连接缓存按实际目标网络及本次线路引用分离，避免跨线路复用。
- 隧道和终端均在创建实际操作前完成逐跳身份确认，确认后继续原线路；取消 / Escape不保存信任或发送目标登录凭据。修复指纹层被隧道弹窗覆盖的问题。最初本机回归复现的是未保存指纹导致隧道拒绝，并未复现用户远端环境的原始EOF；修复后直接、跳板及本机SOCKS5的首次连接均可不经过直连初始化而完成实际传输。
- 顶栏右侧显示运行代理图标和数量，列出服务器 / 类型 / 实际监听地址，可立即停止单个或全部本地 / 远程 / SOCKS5转发服务。无就绪服务时隐藏；停止复用原取消上下文，关闭监听及接入连接。动态菜单使用操作引用作为稳定ID，避免服务启停后误选另一条。

验证证据：

- 全量门禁：`build/logs/check-20261117-230355-2847040349.log`，759个Go文件格式、vendor完整性、两前端依赖隔离、vet及全仓测试通过。Broker103.390s，Desktop52.106s。前一轮全量通过日志为 `check-20261117-230124-25752258.log`；最终菜单稳定ID改动后重新完成门禁。
- 后台竞态回归：`build/logs/test-20261117-225739-race-1126832886.log`。真实合成SSH和SOCKS5链路验证：保留源临时直连选择、目标先已有直连缓存仍需走所选中继、身份探测、源关闭后拒绝、错误类型拒绝、普通调用者拒绝及默认配置不变。
- UI竞态回归：`build/logs/test-20261117-230453-race-846920842.log`。原生菜单模型直接选择SSH / SOCKS5来源、目标首次指纹确认及登录、首次隧道真实字节传输、右上角停服务并检查监听关闭、取消 / Escape及全部停止通过；未打开OS窗口。
- 离线视觉：`build/logs/test-20261117-230124-1808026260.log`（32种隧道组合）及 `test-20261117-230220-3363383816.log`（8种运行图标组合），40张PNG在 `build/snapshots/tunnel-menu-final-20261117/`。中英文、明暗、窄宽布局均覆盖；已目视中文深色560顶栏、英文深色560远程转发、中文亮色1080上游代理。
- SmallPen：最终修订 `792cbef2f156fce1ca7127c8a187ee5a0e6e87b1e1e77090f717109721aa61f4`，111页、18组件、161Token；结构零错误 / 警告，9页×明暗共18组合渲染零问题。`build/logs/smallpen-live-routes-render-summary-20261117.json`记录完整覆盖及144项混合文字/背景对比检查跳过。已目视线路菜单、停止菜单和工作台图标，规格保留在 `smallpen-live-routes-spec-20261117.txt`；pages.json入口、状态、动作及源码映射同步，文件引用无缺失或重复。现有58766端口未监听，未另起SmallPen服务，未做在线工作区验收。
- macOS候选包：`build/darwin-arm64/SmallSSH.app`；编译日志 `build/logs/tunnel-menu-build-20261117.log`。生成文件检查见 `tunnel-menu-build-check-20261117.log`，深层严格签名通过见 `tunnel-menu-signature-20261117.log`；三个可执行文件SHA256保存于 `tunnel-menu-artifact-20261117.json`。

范围限制：使用隔离保险库与生成的loopback夹具，未读取或拨号真实服务器。本轮未替换或启动用户现有App，未前置窗口，未修改正式数据；离线及菜单模型测试不替代真实系统菜单、用户远端网络和跨平台运行验收。


## 2026-11-17 · 代理图标收紧与隧道用途直述

- 将右上角运行代理入口从52宽收至32×32，与邻近动作同宽同高；图形16→14、数量11粗体→9常规，保留数量、可访问名称、提示、菜单及单个 / 全部停止。原有UI测试增加与相邻按钮等宽的断言。
- 本地转发直接提示“本机访问远端的一个服务”；远程转发提示“服务器访问本机的一个服务”；SOCKS5提示“本机通过服务器访问不同目标”。中英文提示直接可见，补充说明保留悬停。未改拨号、监听、路由、信任和授权逻辑。
- SmallPen同步图标、字号、线宽、用途短句与pages.json。核验发现SVG声明尺寸变小但原始路径仍按20单位绘制，已将路径按14视口归一化；最终规格的图形约11.67×11.67、线宽1，父按钮32×32。使用指南补三种入口和固定 / 动态目标对照。

证据：

- 全量门禁通过：`build/logs/check-20261117-230910-3962889772.log`（vendor、759个Go文件格式、前端依赖隔离、vet及全仓测试）。
- 离线UI检查：`build/logs/test-20261117-230910-352619193.log`，40图在 `build/snapshots/tunnel-size-purpose-20261117/`；中英文、明暗、隧道560/1080与顶栏560/1440宽度。已目视560中文深色顶栏及英文深色SOCKS5用途，按钮不超出窗口。
- SmallPen修订 `85be348485c2588e0515445954e86aa5537e5cec52439f6c9a9eaf31f4100ac0`：111页、18组件、164Token；结构零错误 / 警告，6页×明暗12组合渲染零问题；144项混合文字/背景对比检查跳过。摘要 `build/logs/tunnel-size-purpose-smallpen-summary-20261117.json`，入口规格 `tunnel-size-purpose-proxy-spec-20261117.json`；已目视最终深色工作台图标，查看与测量未更改源修订。
- 更新候选包 `build/darwin-arm64/SmallSSH.app`，日志 `build/logs/tunnel-size-purpose-build-20261117.log`；`build -check`通过见 `tunnel-size-purpose-build-check-20261117.log`；深层严格签名通过见 `tunnel-size-purpose-signature-20261117.log`。

范围：本轮验证在本机headless环境完成，无真实凭据 / 服务器操作，未替换或启动当前App、未前置窗口。现有SmallPen58766未监听，未另起服务，在线工作区与跨平台实机未验收。


## 2026-11-17 · 隧道用途命名、端口归属与SOCKS5使用路径

- 澄清本机部署的网站映射到服务器端口属于远程转发；本地转发是在本机开入口去访问远端固定服务。类型入口与顶栏运行菜单显示“访问远端服务 / 让服务器访问本机服务 / SOCKS5代理”，图上保留技术名称与请求方向，字段显示具体入口及服务地址。
- 远程转发补本机开发网站的用途，明确目前仅服务器本机能使用入口；未改变回环监听限制，也未实现或声称公网发布。SOCKS5在字段下直接给出SmallSSH右键线路选择及其他软件配置方式；使用指南明确127.0.0.1、实际端口、无需代理账号密码，启动不会自动修改外部软件代理。
- 只调整显示和说明；网络、路由、指纹确认、权限及取消行为保持原有实现。更新已有类型切换、字段和停止菜单的UI契约测试，没有新增低影响样式测试。

证据：

- 全仓门禁通过：`build/logs/check-20261117-232911-775886913.log`，含vendor、759个Go文件格式、两前端依赖隔离、vet及全部测试。
- 40张离线图通过：`build/logs/test-20261117-232911-452033756.log`，产物 `build/snapshots/tunnel-intent-20261117/`；中英文 / 明暗 / 两档宽度。已目视560宽中文亮色远程转发和英文深色SOCKS5，长标签和使用说明可见。
- SmallPen修订 `3dd1d8ed24b5dbac92ce39e0f95dc94f22d616c273d66d85aa6bf0de48bc49b1`，111页、18组件、164Token；结构零错误 / 警告，5页×明暗10组合渲染零问题、24项混合文字/背景对比检查跳过。摘要 `build/logs/tunnel-intent-smallpen-summary-20261117.json`；已查看最终深色运行菜单，读取SOCKS5页面规格入口，pages.json的动作及边界同步。
- macOS候选包已重编译至 `build/darwin-arm64/SmallSSH.app`，日志 `build/logs/tunnel-intent-build-20261117.log`。生成文件检查通过见 `tunnel-intent-build-check-20261117.log`；深层严格签名通过见 `tunnel-intent-signature-20261117.log`。

边界：本机headless和合成夹具验证，不涉及真实凭据或远端操作；未替换、启动或前置用户当前App。现有SmallPen58766未监听，未另起服务或在线工作区验收；不代表跨平台实机和公网部署已验收。


## 2026-11-17 · 隧道无框路径图与高级收起

- 将本地 / 远程 / SOCKS5三种视图改成图标与箭头的无框路径，不再使用节点卡片；仅标记当前设备。默认图中选择中转服务器、填写入口和服务端口，普通两框、SOCKS5一框；不重复摆放服务器选择和完整地址表单。
- 高级选项默认收起，包含完整监听 / 目标地址、连接线路及已有上游代理。展开时图中显示只读地址，避免重复输入；折叠保留自定义IP、端口和线路，普通端口编辑只改端口，保留IPv6地址。已有自定义线路在折叠标题显示已配置。监听、拨号、信任及取消的后端规则保持原实现。
- 基于已有UI契约补折叠保留草稿、IPv6端口编辑和图中服务器菜单选择验证；覆盖首次隧道真实传输、首次身份确认及既有停止行为，使用隔离loopback夹具。
- SmallPen共享隧道节点改成无框、按内容收高结构，补三种高级展开页并连接对应基本页。默认与高级视图、输入数量、页面入口 / 状态 / 动作 / 源码映射同步；修正新服务SVG的固有尺寸，避免在设计渲染中裁切。

证据：

- 全量门禁通过：`build/logs/check-20261117-235421-2924892366.log`，vendor校验、759个Go文件格式、两前端依赖隔离、vet及全仓测试全部通过。前次 `check-20261117-234917-1753626367.log`因并行打包删除临时目录而在Go包扫描阶段失败，已改为串行打包后检查；`check-20261117-235107-2325967669.log`发现新节点的英文无障碍标签未入词表，补齐两语言后重新通过全仓门禁。
- 针对交互回归：`build/logs/test-20261117-234534-3896604875.log`。最后离线及交互回归：`test-20261117-234819-524433633.log`，包含三种默认、普通高级及上游代理高级视图，中英文 / 明暗 / 560与1080宽共40PNG，位于 `build/snapshots/tunnel-simple-20261117/`。已目视560中文本地 / 远程和英文深色上游代理，端口、节点与操作未遮挡。
- SmallPen最终修订 `783f6b755d368aa739ad81aa06cbe5a01ecd3b6f0e4841c2d41b2eca7868b86c`：114页、18组件、78变体、170Token；结构零错误 / 警告。7页×明暗14组合及节点12变体×明暗24组合，共38目标零渲染问题，138项对比检查跳过，见 `build/logs/tunnel-simple-smallpen-summary-20261117.json`。已目视最终亮色基本页、读取无框入口节点规格；规格保留在 `tunnel-simple-node-spec-20261117.txt`。pages.json无重复入口或缺失文件。
- 最终macOS候选包：`build/darwin-arm64/SmallSSH.app`，编译日志 `build/logs/tunnel-simple-build-20261117.log`；生成文件检查通过见 `tunnel-simple-build-check-20261117.log`，深层严格签名通过见 `tunnel-simple-signature-20261117.log`。三可执行文件SHA256保存于 `tunnel-simple-artifact-20261117.json`。

范围：当前App未替换或前置，未读取真实凭据、未拨号真实服务器。SmallPen58766未监听，未另起服务或打开在线工作区；CLI及离线UI不替代在线App、原生系统菜单、真实网络和跨平台实机验收。


## 2026-11-18 · 连续长箭头与斜线绘制修复

修复隧道图中箭头变成短横的问题：MyGo Painter.Line只绘制横 / 竖线，原实现用它画斜线导致箭头头部退化；现以StrokePath绘制完整矢量箭头，按三个图标的实际Bounds定位，连接相邻图标并对齐中心。移除固定40宽箭头栏、跳动小点与SSH / 转发小字，设置图静态表达访问方向。端口编辑、高级折叠及所有网络行为保持原逻辑。

- 全量门禁通过：`build/logs/check-20261118-000733-1830596204.log`，含vendor、759个Go文件格式、两前端依赖隔离、vet及全仓测试。
- 离线及交互回归通过：`build/logs/test-20261118-000427-542819010.log`。40图位于 `build/snapshots/tunnel-connectors-20261118/`，覆盖三种基本模式、普通高级、上游高级，中英文 / 明暗 / 560与1080宽。已目视560亮色本地与深色远程图，确认完整箭头、无短横假箭头且不遮挡标签；更新已有动画测试，确认选设置时图不会伪装实时流量。
- SmallPen修订 `dc97f33dd57d5ba1323485f655114b1dc992499575cf1af9c46f4075bfb22cc0`：114页、18组件、78变体、171Token；结构零错误 / 警告。7页×明暗14目标及组件12变体×明暗24目标全部完成。52条info均是箭头与线段有意拼接产生的possible_overlap，已核对；162项对比检查跳过，没有文字溢出或越界问题。摘要 `build/logs/tunnel-connectors-smallpen-summary-20261118.json`；已目视最终SmallPen亮色页并读取中转节点规格，记录在 `tunnel-connectors-node-spec-20261118.txt`。页面映射同步静态长箭头行为。
- 候选包 `build/darwin-arm64/SmallSSH.app` 已更新；编译日志 `build/logs/tunnel-connectors-build-20261118.log`，生成文件校验与深层严格签名分别见 `tunnel-connectors-build-check-20261118.log`、`tunnel-connectors-signature-20261118.log`。

边界：未替换或启动当前App、未前置窗口，未读真实凭据或访问真实服务器。SmallPen58766未监听，未另起服务或在线工作区；离线检查不替代原生系统菜单、真实网络或跨平台实机验收。


## 2026-11-18 · 隧道短标签与下方解释

将顶部类型和运行菜单收成“本地转发 / 远程转发 / SOCKS5”，用途统一显示在图下。远程说明明确本机服务映射到服务器端口及仅服务器本机可访问的范围。本地补一句用途，SOCKS5保留已有用法。稿子切换项按内容收宽，页面映射及使用指南同步；仅改文案和显示，网络规则与配置保留行为保持原实现。

- 本轮检查通过：`build/logs/check-20261118-001452-1573006341.log`。命令为 `go run ./tools check -run 'TestTunnel|TestAdvancedTunnel|TestFirstTunnel|TestLiveConnectionMenu|TestDesktopLiteralTranslationSourcesExist'`，全局vendor / 格式 / 前端依赖隔离 / vet通过，执行隧道、首次指纹、临时线路及词表相关测试。上一轮完整不筛选的门禁见 `check-20261118-000733-1830596204.log`。
- 离线UI检查：`build/logs/test-20261118-001424-1456631250.log`；40图在 `build/snapshots/tunnel-short-labels-20261118/`，覆盖中英文、明暗、基本及高级状态、两档宽度。已目视560宽中文远程图，短标签及图下说明完整显示，相关菜单 / 类型切换测试更新并通过。
- SmallPen最终修订 `46558c424800fc90339a2b13d721c22ae55f7e27571b7fec1df98d83f5391406`：114页、18组件、78变体、171Token，结构零错误 / 警告；8页×明暗16目标完成，28条既有箭头拼接info重叠及42项对比跳过，未见文案溢出或越界。详见 `build/logs/tunnel-short-labels-smallpen-summary-20261118.json`；已目视最终亮色远程稿。
- macOS候选包更新至 `build/darwin-arm64/SmallSSH.app`；日志 `build/logs/tunnel-short-labels-build-20261118.log`。生成文件检查、深层严格签名通过，分别见 `tunnel-short-labels-build-check-20261118.log` 和 `tunnel-short-labels-signature-20261118.log`。

边界：当前App未替换或启动，无前置窗口或真实服务器操作。SmallPen58766未监听，未新起服务或在线工作区；以上仅为本机离线与合成夹具验证。

## 2026-11-18 · 安装隧道交互与短标签修复包

用户明确要求替换App。核对既有候选包签名及三份程序哈希后，退出正式App，再替换至`/Applications/SmallSSH.app`。安装前后台身份握手通过且已锁定（`build/logs/service-20261118-003401-1632355762.log`），没有可用的所有者管理会话；本次通过系统退出事件结束App，并以后台程序显式处理的SIGTERM正常关闭服务，未调用需要所有者会话的Stop、未放宽IPC身份校验。退出结果见`build/logs/tunnel-short-labels-shutdown-20261118.json`；独立开发后台未停止。

安装命令为`go run ./tools install -source build/darwin-arm64/SmallSSH.app`，记录`build/logs/install-20261118-003423-1880552759.log`。安装后的主程序、后台与CLI逐一匹配候选包SHA256，深层严格签名验证通过；没有残留安装临时目录或旧包备份。以`open -g`后台启动正式App，后台沿用原正式数据目录；App PID75404、后台PID75559，身份握手成功且处于locked状态，见`build/logs/service-20261118-003455-1254570795.log`。只读窗口检查确认主窗口8759显示解锁保险库，未输入密码或发起连接。

正式`vault.json`、`layout.json`和`behavior.json`在停机后与重启后的SHA256一致，未迁移、重建或覆盖数据。完整产物与数据核对在`build/logs/tunnel-short-labels-installed-20261118.json`；启动记录在`tunnel-short-labels-restarted-20261118.json`。本轮没有源码变化，复用前轮已检查的候选包，没有重复编译或测试。验证仅限本机macOS arm64安装、身份握手及锁定页面；服务器列表需用户解锁后查看，真实SSH/VNC连接仍由用户主动发起。


## 2026-11-18 · RSSH 对照与隧道交互线稿（未改 App）

用户要求先解释路径、出线稿，再决定实现；本轮仅取证和生成聊天线稿，没有修改 App 源码或正式 SmallPen 包，没有构建、替换或启动正式 App。

- RSSH `main` 固定为 `2a7e4cbbe2ba67696dc3a9cce41d542b25ff0c4c`（提交时间2026-11-16）；只读下载源码保存在 `build/research/rssh-review-20261118/source/`，未安装或执行其代码。取证覆盖 ForwardEditor（配置绑定SSH profile、保存与启动分开）→ core/commands/forward（读取profile并解析bastion chain）→ ssh/forward（三种运行方向）→ ForwardPane（规则逐项启停、状态/流量）；MCP入口确认外部工具可选择已有终端并申请会话访问，不据此推断其完整授权/审计安全性。
- 动态发现文档来自 rssh-org/docs，当前基线 `ae1e9e3a45183efc1590d27042238a9ac4aef0e8`；文档说明保存context等来源、实时列出容器/Pod、不把结果保存为长期Profile。与本项目经SSH管理远端Docker属于不同工作流，可并存；本轮未迁移或新增发现功能。
- 本地状态对照使用 docs/FEATURES.md 和相关源码。特别核对 Windows agent 当前使用 OpenSSH named pipe，不能等同于已支持Pageant；SSH跳板能力不能等同于已实现OpenSSH配置导入；多标签/分栏不能等同于广播、命令分块或asciicast录制。
- 交互线稿：`build/review/ssh-tunnel-wireframe-20261118.html`。默认两端设备，SSH服务器默认当前选中且必选；额外跳板默认无，添加后才出现。目标可以是出口机器本身或出口侧能访问的其他设备；远程转发交换入口/出口，SOCKS5目标随程序请求变化。顶栏隧道入口始终保留，包括空状态。启动/停止仅模拟状态，不发起网络或修改真实配置。
- 证据：JS语法检查通过；隔离profile的headless Chrome完成 11 项交互断言，全部通过；800px视口无水平溢出，结果见 `build/logs/ssh-tunnel-wireframe-qa-20261118.json`，截图 `build/review/ssh-tunnel-wireframe-20261118.png` 已目视。没有前置窗口、发送全局按键或读取真实凭据。此次验证仅覆盖线稿，不代表App已实现这轮设计或RSSH已运行验收。

## 端口转发单条规则预览

按用户要求先渲染「访问远端服务」的默认设置一屏。SmallPen 独立草稿和亮色 PNG 位于 `build/review/forward-one-rule/`，沿用正式设计包的按钮、输入框和主题。结构及渲染检查记录在同目录 `check.json`，零报错、零报告布局问题，关闭图标的自动对比度检查因透明背景跳过。已目视核对成图。此次只交付静态预览，未修改正式设计页或 App。
