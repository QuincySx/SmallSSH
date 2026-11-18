# SmallSSH 桌面设计

可编辑设计源位于 [smallssh.smallpen](smallssh.smallpen/)。[页面目录](pages.json) 记录各页面的入口、承载方式、状态、操作和 Go 源码位置。产品行为见 [使用说明](../USAGE.md)，需求见 [REQUIREMENTS](../REQUIREMENTS.md)。

## 当前交互

- **工作台**　顶部标签切换终端、VNC 和已打开的功能。画布模式支持拖动和四向分栏，卡片只显示当前视图的标题和操作。标签模式聚合显示当前视图。文件、命令和服务器状态可停靠，也可打开为标签或独立工具窗。
- **服务器侧栏**　新建连接位于左上角。连接可以在组内排序或拖到其他组。保存仅写入配置，双击连接时才拨号。首次连接及主机指纹变化时，沿用本次线路核对服务器身份。
- **线路与转发**　设置中的「加速与代理」集中管理可复用的线路。连接引用线路 ID，编辑后下次连接生效。SSH 隧道从服务器操作菜单创建，运行中的转发由全局入口管理。
- **文件与任务**　文件管理同时显示目录树、列表和选择状态，可跟随终端或选择来源。编辑单个文本文件使用独立原生窗口。任务入口位于主窗顶栏，完成记录可单条移除或清空。
- **服务器状态**　资源概览、进程、服务、系统信息、端口和 Docker 可在状态面板切换。时间范围放在状态栏右侧更多菜单。
- **VNC**　远程桌面显示在工作区标签，使用同一连接进入全屏。常用操作用图标，在显示空间不足时收进更多菜单。
- **授权中心**　待确认、当前授权和审计记录在设置内切换。服务器操作授权与 SSH 密钥使用授权分别管理。当前受控会话可实时调整中风险文件确认，审计详情保留当时的勾选和切换记录。较宽窗口在右侧展示详情，窄窗口展示在列表下方。

## 窗口和后台

| 内容 | 承载方式 |
| --- | --- |
| SSH 终端、VNC、打开为标签的工具 | 主窗口顶部工作区标签 |
| 文件、命令、服务器状态 | 工具区、画布卡片、工作区标签或独立工具窗 |
| 设置、文本文件编辑 | 独立原生窗口 |
| 新建连接、主机身份确认 | 当前宿主内弹窗 |
| 授权确认 | 独立审批窗口，或设置中的授权中心 |

| 动作 | 行为 |
| --- | --- |
| 关闭窗口时退出 | 关闭工作台并锁定连接服务 |
| 关闭窗口后驻留 | 隐藏工作台，已有会话仍受原授权期限和闲置设置约束 |
| 退出应用、主动锁定、系统锁屏或休眠 | 锁定连接服务并撤销会话 |
| 最小化 | 保留窗口、会话和原授权状态 |

驻留期间可通过启用的 Dock 或菜单栏入口恢复窗口，至少保留一个入口。工作区呈现方式在「设置 → 工作区」调整，关闭行为在「设置 → 运行与后台」调整。

## 可编辑设计

- `index.html` 保存设计包入口，`tokens.css` 保存主题、字体和尺寸。共享组件在 `components/`，页面在 `pages/<模块>/`。
- 修改页面时同步 [页面目录](pages.json)，并优先复用现有组件和 Token。
- SmallPen 渲染使用 [assets/fonts](smallssh.smallpen/assets/fonts/) 中的 WOFF。[font-notices](font-notices/) 保存字体来源、许可证和图标字形映射。
- 日常查看复用用户已运行的 SmallPen 工作区。设计使用合成内容，应用截图用 [离屏测试](../DEVELOPMENT.md#需要额外依赖的测试) 生成。

当前 CLI 的帮助和内置 Skill 给出维护方式。

```sh
smallpen help skills --json
smallpen check docs/design/smallssh.smallpen --json
smallpen check docs/design/smallssh.smallpen --render --all-themes --json
smallpen view docs/design/smallssh.smallpen --as text --json
smallpen view docs/design/smallssh.smallpen --page '08 设置 / 授权中心 · 历史' --as png --theme appearance/light --json
smallpen changes docs/design/smallssh.smallpen --since REVISION --json
```
