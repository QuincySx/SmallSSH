# 桌面组件

共享原生组件在 `internal/desktop/components/`，页面在 `internal/desktop/` 组合。视觉约定与页面入口见 [桌面设计](design/DESIGN.md)。

| 组件 | 用途 | 源码 |
| --- | --- | --- |
| Button、SVGButton、ActionGroup | 主次与危险动作、禁用和忙碌状态、图标按钮、动作对齐 | `button.go`、`icons.go` |
| Dialog、DialogSidebar、WindowFrame | 模态结构、分类导航、独立窗口 | `dialog.go`、`window.go` |
| HeaderBar、HeaderLabel、PanelBar | 窗口和工具标题、标签和操作槽 | `panel.go` |
| PanelLayoutMenu、PanelLayoutMenuAt | 工具面板停靠、独立、放大及收起 | `dock.go` |
| NavigationSidebar、列表与表格 | 连接与分组、暗亮选中、焦点、虚拟列表和详情 | `navigation_rail.go`、`list_item.go` |
| SplitPercent、SplitVerticalPercent | 分栏拖动、比例约束与缩放 | `split.go` |
| 权限面板与输入控件 | 读写执行勾选、权限值、敏感输入 | `permissions.go`、`secret_input.go` |
| 资源卡片与趋势图 | 主机和容器资源呈现 | `metrics.go` |

## 复用规则

- 基础密度、颜色、留白和字号集中在共享主题。先改同类组件，再核对受影响页面。控制状态应能通过颜色、文字或形状辨识，保留键盘焦点和可访问名称。
- 新增图标动作使用共享 SVG 入口，许可证在 [Lucide 许可](../internal/desktop/components/lucide/LICENSE)。内容等待、失败、重试和不可用状态使用明确控件。
- 窗口级入口与局部操作分开。顶部工作区标签管理活动视图，卡片标题管理当前内容，文件和状态操作留在相应工具区。设置及文本编辑使用独立窗口。
- Dialog 共用标题、正文和操作区。分类作为整体面板，保存 / 取消与附属动作成组靠右。长表单内滚动，主要操作保持可达。
- 列表、网格和标签使用稳定业务 ID 及明确的空态、选择和键盘行为。把当前目标、校验和数据读写留在业务层。拖拽在提交前重新确认当前分组、目标或文件状态。
- 状态工具可以在概览和详情间切换，详情维持原面板承载并保留返回动作。按卡片实际可用宽度排版，数值异常及缺采样显示可辨认的状态。

## 验证

按 [开发说明](DEVELOPMENT.md#需要额外依赖的测试) 运行 headless UI Tester 和离屏快照。覆盖明暗、窄宽、键盘、选中态和错误态，使用合成连接资料。
