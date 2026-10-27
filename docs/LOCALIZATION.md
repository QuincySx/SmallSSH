# 界面语言

当前支持简体中文、English 和跟随系统。入口为「设置 → 工作区 → 界面语言」。选择模式保存在 layout.json 的 localeChoice；MyGo 的 App.SetLocale 同时管理平台语言偏好。仅真实用户更改选项时调用它，保存失败会恢复原模式。

## 接入点

- internal/localize 使用 MyGo 官方 i18n.NewCatalog / Register / Catalog.Format，中文为 source。en.json 和 zh-Hans.json 各含 1109 条应用及 MyGo 控件文案。
- 视图与组件使用 tr(c, source)，按 c.Locale() 每帧取词。没有 UI Context 的提示使用 tr(nil, source)，由官方 i18n.Locale() 读取当前语言。
- 原生菜单保留稳定 group.id，在呈现时翻译 label。App.OnLocaleChanged 只更新标题、菜单与失效帧，不显示窗口或改变终端焦点。
- 语言与区域格式是不同设置；切换界面语言不覆盖系统的日期、数字区域偏好。
- 当前没有启用 mygo.json 的 i18n 自动生成。固定 v0.5.5 的生成器会引用当前 vendor 未包含的中文 locale 包；本项目直接使用其公开 Catalog API，保持 vendor 和安全补丁不变。

## 新增文案

目录 key 使用原始中文文案；两种语言必须同时新增。value 是 ICU 消息，字面量单引号与花括号需要按 ICU 转义。现有 Go fmt 参数在翻译后格式化，占位符类型和顺序必须一致；完整句子优于碎片拼接。

用户服务器名、文件名、路径、命令、容器名、服务名和远端输出是数据，不能作翻译源。即便用户给连接取名“设置”，也应原样显示。Key、协议值、排序字段、状态判断和选择值保持稳定；固定选项通过显示映射回原始值。

未知源立即原样返回，不进入 MyGo 文案缓存。MyGo 缓存已格式化的无参数消息；语言切换不新增服务器轮询。已有提示、历史与远端输出保持产生时语言，不追溯改写数据。

## 验证

go test ./internal/localize 会逐条严格解析中英文、比较目录 key、ICU 参数与 Go fmt 参数，检查中文 round trip 和代码中固定 tr 调用的目录覆盖。NewCatalog 本身会忽略坏消息，因此不能代替这些检查。

桌面语言测试使用 ui.Tester.SetLocale 和 MYGO_LOCALE 的进程级测试覆盖，不调用持久化 App.SetLocale。语言设置测试注入回调，核对选择映射、保存失败、跟随系统模式及无变化时无写入。

preview_qa 的 TestInternationalizationSnapshotsWithoutWindows 生成英文主页面、中文窄栏及伪语言设置截图，仅使用隔离演示数据。语言变化不能更改连接、容器目标、侧栏状态或输入内容。

## 来源

MyGo v0.7.1，revision 76dd068b72cc15526cda19db84d70a63c7a8843a。

- 官方语言文档：https://mygo.egoist.dev/docs/i18n
- 固定版本：https://github.com/egoist/mygo/releases/tag/v0.7.1
