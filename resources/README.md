手写资源只有两项：`icon.png`（应用图标源，保留透明圆角，由 `mygo.json` 交给打包器生成各平台尺寸，来源记录在 `design/app-icon-prompt.txt`）与 `README.md`。

`<goos>-<goarch>/` 是构建暂存目录：`go run ./tools build` 把原生组件与各平台 CLI 写进去，MyGo 随后把它当作应用资源目录打包。它们不含源码、被 git 忽略、每次构建先整目录删除，路径与提交边界见 [docs/DEVELOPMENT.md](../docs/DEVELOPMENT.md)。

`THIRD_PARTY_NOTICES.txt` 也是生成的（`vendor/` 许可证汇总），但提交进仓库并随包分发，`go run ./tools build -check` 可确认它是最新的。
