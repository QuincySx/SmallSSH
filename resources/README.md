`go run ./tools/build` 将各平台的 CLI 构建到相应子目录，MyGo 随桌面程序打包并在 macOS 中签名。生成的二进制文件不进入源码版本管理。

`icon.png` 为应用图标源，保留透明圆角。`mygo.json` 将其交给打包器生成 macOS ICNS、Windows 可执行文件资源和 Linux 桌面图标；不需要单独维护各平台尺寸。生成提示和来源记录在 `design/app-icon-prompt.txt`。产品尚未定名，图标没有嵌入文字。
