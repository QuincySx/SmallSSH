# Development workflow

Rules for anyone (human or agent) changing this repository. Commands live in [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md); this file only states the constraints.

- The user works on this Mac concurrently. Development must never bring a window to the foreground automatically. Do not raise windows, launch a regular desktop build for verification, use global key events, or open system dialogs just to inspect the app. Use `go run ./tools dev show` only after an explicit human request.
- Verify with the headless UI Tester and the opt-in offline snapshots. Do not enter real server credentials into generated QA fixtures.
- Every task is a Go command under `tools/`: `go run ./tools` lists them. Go has no npm scripts, so do not add shell scripts, a Makefile, or ad-hoc `go test` invocations beside that list. CI calls the same entry points; a new need becomes a new task.
- `go run ./tools check` (vendor-patch check + `gofmt -l` + `go vet` + `go test`) is the gate; its log under `build/logs/` is the evidence to quote. Do not scatter logs in `/tmp`.
- Build output belongs to `build/` and the ignored `resources/<goos>-<goarch>/` staging directories. Do not commit either, do not hand-edit them, and do not introduce a third output location. The two committed generated files are listed in [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md); `go run ./tools build -check` verifies them.
- Preserve the pinned vendor tree and the macOS password accessibility patch in `patches/`. Do not regenerate vendor or bypass it with `-mod=mod` except for an explicit dependency upgrade, which must re-apply the patch, update `patches/vendor-integrity.json` and re-check the native password field.
- Versions live in `go.mod` (MyGo, Go, any `tool` directive), never in scripts or workflows.
- Document what changed in `docs/VERIFICATION.md` as one appended section per round: what was done, where the evidence is, and which claims only hold locally.

- UI 页面与窗口的设计入口是 [docs/design/DESIGN.md](docs/design/DESIGN.md)，可编辑源是 `docs/design/smallssh.smallpen/`。先用 `smallpen help skills --json` 找到并读取当前安装版的 `smallpen-ui-design/SKILL.md`，按 CLI 帮助维护 HTML 和 Token。每轮写入后执行 `smallpen check`，用 `check --render` 和 `view` 核验。修改页面结构时同步 `docs/design/pages.json` 的入口、状态、操作及源码映射。
- 按稿实现时，记录作为验收基准的 SmallPen 修订，读取实际控件的 Spec 和 Token，并用同尺寸、主题和状态的 App 离屏截图复核。发现 App 的视觉缺陷时修 App，用户要求调整设计时再更新评审基准。
- 页面评审复用用户已运行的 SmallPen 服务，先确认现有实例，再打开正式包并保留其他已打开项目。除非用户明确要求新实例，不另起 Web / Background 服务；日常评审使用在线工作区。
