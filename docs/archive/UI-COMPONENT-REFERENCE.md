---
generated: 2026-11-06
accessed_at: 2026-11-06T12:38:00Z
type: comparison
scope: 官方来源 Quick；按钮规范与小批 SVG 复用；未安装依赖或运行来源代码
---

# 按钮与图标组件参考

建议采用 **Radix Themes 按钮视觉规范 + Lucide 原始 SVG + 现有 MyGo 按钮交互与无障碍**。保留当前紫灰配色、已确认圆角/8px 间距及透明度设置；这是原生按钮的样式适配，不能宣称已把 React 库直接接入或替换 renderer。

本项目条件按本轮任务提供：Go 1.27.3、MyGo v0.7.1，ui.Icon 支持 SVG/currentColor，现有 components.Button 已有 primary/secondary/quiet/destructive。**SVG 资产可复用；React 按钮不能直接 import 到 Go。** Radix 实际依赖 React/React DOM、Radix runtime 与 CSS；shadcn 固定源码也使用 React、Slot、CVA 与 Tailwind 类。[Radix 包声明][radix-package]、[shadcn 按钮源码][shadcn-source]

## 候选比较

“成熟”在这里指官方分发/文档、明确许可证及可复查状态实现，不按图标数量或 Stars 判断。日期是 2026-11-06 查询的默认分支最近提交时间，不代表维护承诺。[提交基线见下表](#固定来源与许可)

| 候选 | 官方能力与适配判断 | 最近提交 / 许可 |
|---|---|---|
| **Lucide** | 官方 Static 分发独立 SVG，不需要 JS 框架；已检查选定文件为 24×24、currentColor 描边。适合直接替换手写图标几何，统一一个线性图标家族。它不是按钮库，不提供按钮 focus/disabled/tooltip。[官方 Static][lucide-docs]、[SVG 示例][lucide-svg] | 2026-11-06；ISC，部分 Feather 派生图标另有 MIT 归属。[完整许可][lucide-license] |
| Tabler Icons | 官方提供独立 SVG/静态资产与框架包，24×24 网格、一致描边，适合作为另一整套图标家族；同样不替按钮承载交互。[官方文档][tabler-docs]、[仓库][tabler-repo] | 2026-10-27；MIT。[许可][tabler-license] |
| Phosphor Core | 官方 Core 仓库提供按 weight 分组的原始 SVG，明确用于自定义实现/移植。可选，但需先固定单一 weight，避免将填充/双色和当前描边体系混搭；本轮不选它。[官方 Core/资产规范][phosphor-repo] | 2026-01-06；MIT。[许可][phosphor-license] |
| **Radix Themes Button / IconButton** | 真正的预设样式按钮库：尺寸、solid/soft/surface/outline/ghost 等变体、radius、disabled、loading；有独立 IconButton/Tooltip。loading 保留尺寸并默认禁用，源码保留隐藏文本的无障碍名称。作为本轮按钮规范来源最明确。[Button][radix-button]、[IconButton][radix-icon]、[状态实现][radix-base] | 2026-04-11；MIT。[仓库][radix-repo]、[许可][radix-license] |
| shadcn/ui Button | 真正按钮组件源码：default/outline/secondary/ghost/destructive/link、尺寸与图标尺寸、hover/focus-visible/disabled/invalid；Tooltip 另组装。可参考，但不是 Go 控件；loading 也不是该固定 Button 源码的独立 prop。[官方 Radix 版文档][shadcn-docs]、[固定源码][shadcn-source] | 2026-11-06；MIT。[仓库][shadcn-repo]、[许可][shadcn-license] |

## Radix → 当前原生原型的最小适配表

下表是**本项目建议值**，不宣称 32px/18px 是所有 Radix 用法的强制标准。只在已授权的双 tag 离线原型验证；正式业务回调、窗口生命周期和无障碍继续由现有 MyGo 组件负责。

| 项目 | 原型适配 |
|---|---|
| 顶栏/左右工具图标按钮 | 32×32 逻辑像素点击区、18×18 图标；保留布局中的 8px 间距，图标大小与点击区分开。完整保留当前圆角和紫灰色 token。 |
| primary / solid | 弹窗唯一主要动作；现有强调色实底，hover/pressed 色阶明确，避免每个顶栏动作都变成主按钮。[Radix 变体][radix-button] |
| secondary / soft | 取消、普通次级动作；轻底与正文色，按钮轮廓、内边距和文字基线统一。 |
| quiet / ghost | 顶栏、侧栏工具；默认透明，hover/pressed 出现轻底；开关工具的持续选中态单独表达。不要移植 Radix CSS 的负 margin 布局技巧到原生布局。[源码状态][radix-css] |
| destructive | 明确删除等破坏动作，沿用现有 destructive 语义，使用红色 solid/soft 的同一组状态，不改变业务确认流程。 |
| disabled | 降低前景/背景对比、阻止鼠标和键盘触发，AX 暴露 disabled；不只改透明度。源码有独立 disabled 色阶。[源码][radix-css] |
| loading | 保留按钮宽高/标签名称，显示 spinner/busy 并防止重复提交；MyGo 侧保留可访问名称及任务状态，不逐行照搬 DOM 的隐藏节点。[官方行为][radix-button]、[实现][radix-base] |
| focus / tooltip / AX | Tab 可聚焦，Enter/Space 仍按现有按钮语义触发，清晰 focus ring；纯图标按钮有动作名称，tooltip 在 hover 和键盘 focus 出现、Escape 关闭。tooltip 只补充说明，AX label 独立保留。[官方 Tooltip][radix-tooltip]、[键盘交互][radix-tooltip-primitive] |

图标库不会自动带来这些按钮行为；Radix 的 Web 无障碍实现也不会随 SVG 移植。worker 应在现有按钮入口保留 focus、AX、tooltip 和业务回调，验证浅色/深色、普通/hover/pressed/selected/disabled/loading 及键盘状态；渲染兼容性以本项目 headless 快照为准。

## 固定来源与许可

| 来源 | 固定默认分支提交 |
|---|---|
| Lucide | [1a49fd88d85202bad69f39462241b309a484bba0](https://github.com/lucide-icons/lucide/commit/1a49fd88d85202bad69f39462241b309a484bba0) |
| Radix Themes | [1faff10ac26ae17f09944d418c6949b93fc6b566](https://github.com/radix-ui/themes/commit/1faff10ac26ae17f09944d418c6949b93fc6b566)，该提交包声明 3.3.0 |
| shadcn/ui | [d549251bab2e57dbf72f7c909d6458c02c43f529](https://github.com/shadcn-ui/ui/commit/d549251bab2e57dbf72f7c909d6458c02c43f529) |
| Tabler Icons | [ed177a085b652fe04ec4f37ced867c94621e7840](https://github.com/tabler/tabler-icons/commit/ed177a085b652fe04ec4f37ced867c94621e7840) |
| Phosphor Core | [2b75f3ad12b420c9504ef05df8d2564a28f8500e](https://github.com/phosphor-icons/core/commit/2b75f3ad12b420c9504ef05df8d2564a28f8500e) |

Lucide 小批候选清单（文件存在、XML、viewBox/currentColor 已只读检查；本轮未写入 assets）：[固定 icons 目录][lucide-icons] 下的 settings.svg、panel-left.svg、panel-right.svg、panel-bottom.svg、plus.svg、search.svg、x.svg、refresh-cw.svg、ellipsis.svg、folder.svg、file-text.svg、chevron-right.svg、circle-check.svg、circle.svg。settings 是齿轮；不要用 settings-2 滑杆替代用户要求的齿轮。

来源文件 URL 模板为 https://raw.githubusercontent.com/lucide-icons/lucide/1a49fd88d85202bad69f39462241b309a484bba0/icons/文件名.svg。实际只取得原型用到的小集合，保留原 SVG 几何、名称与 SHA 清单，由 MyGo 提供状态颜色。

取得/分发 Lucide 时保留**完整上游 LICENSE**：其中包含 Lucide 的 ISC，以及列出的 Feather 派生图标（包括 plus、search、x 等）的 MIT 归属，不能只附一行“ISC”。若改编/复制 Radix 或 shadcn 源码，也保留相应 MIT 版权和许可文本；仅借用视觉规则不应宣传为官方 Go 移植或已安装这些 React 包。[Lucide 许可][lucide-license]、[Radix 许可][radix-license]、[shadcn 许可][shadcn-license]

[lucide-docs]: https://lucide.dev/guide/static
[lucide-svg]: https://github.com/lucide-icons/lucide/blob/1a49fd88d85202bad69f39462241b309a484bba0/icons/plus.svg
[lucide-icons]: https://github.com/lucide-icons/lucide/tree/1a49fd88d85202bad69f39462241b309a484bba0/icons
[lucide-license]: https://github.com/lucide-icons/lucide/blob/1a49fd88d85202bad69f39462241b309a484bba0/LICENSE
[tabler-docs]: https://docs.tabler.io/icons
[tabler-repo]: https://github.com/tabler/tabler-icons/tree/ed177a085b652fe04ec4f37ced867c94621e7840
[tabler-license]: https://github.com/tabler/tabler-icons/blob/ed177a085b652fe04ec4f37ced867c94621e7840/LICENSE
[phosphor-repo]: https://github.com/phosphor-icons/core/blob/2b75f3ad12b420c9504ef05df8d2564a28f8500e/README.md
[phosphor-license]: https://github.com/phosphor-icons/core/blob/2b75f3ad12b420c9504ef05df8d2564a28f8500e/LICENSE
[radix-button]: https://www.radix-ui.com/themes/docs/components/button
[radix-icon]: https://www.radix-ui.com/themes/docs/components/icon-button
[radix-tooltip]: https://www.radix-ui.com/themes/docs/components/tooltip
[radix-tooltip-primitive]: https://www.radix-ui.com/primitives/docs/components/tooltip
[radix-repo]: https://github.com/radix-ui/themes/tree/1faff10ac26ae17f09944d418c6949b93fc6b566
[radix-license]: https://github.com/radix-ui/themes/blob/1faff10ac26ae17f09944d418c6949b93fc6b566/LICENSE
[radix-package]: https://github.com/radix-ui/themes/blob/1faff10ac26ae17f09944d418c6949b93fc6b566/packages/radix-ui-themes/package.json
[radix-base]: https://github.com/radix-ui/themes/blob/1faff10ac26ae17f09944d418c6949b93fc6b566/packages/radix-ui-themes/src/components/_internal/base-button.tsx
[radix-css]: https://github.com/radix-ui/themes/blob/1faff10ac26ae17f09944d418c6949b93fc6b566/packages/radix-ui-themes/src/components/_internal/base-button.css
[shadcn-docs]: https://ui.shadcn.com/docs/components/radix/button
[shadcn-repo]: https://github.com/shadcn-ui/ui/tree/d549251bab2e57dbf72f7c909d6458c02c43f529
[shadcn-source]: https://github.com/shadcn-ui/ui/blob/d549251bab2e57dbf72f7c909d6458c02c43f529/apps/v4/registry/new-york-v4/ui/button.tsx
[shadcn-license]: https://github.com/shadcn-ui/ui/blob/d549251bab2e57dbf72f7c909d6458c02c43f529/LICENSE.md
