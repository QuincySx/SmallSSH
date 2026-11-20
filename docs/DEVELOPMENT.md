# 开发、构建与发布

本文是构建、测试、CI 与依赖升级的唯一权威说明。README 只保留用法；`AGENTS.md` 只保留工作流约束，两者都不重复这里的命令细节。

## 前置条件

- 使用 `go.mod` 的 `go` 指令指定的 Go 工具链或更新的兼容版本；CI 同样从该文件读取版本。
- macOS 上构建系统认证组件还需要 Swift 编译器（`/usr/bin/swiftc`）与 `codesign`。
- 依赖固定在 `vendor/`，普通构建自动使用；**不要用 `-mod=mod` 绕过**，那会丢掉 vendored 的 macOS 密码框修复。

## 任务入口

Go 没有 npm scripts，`go generate` 官方限定为代码生成，所以全部任务都是 `tools/` 下的 Go 命令，并收成一个前门：

```sh
go run ./tools                 # 列出全部任务
go run ./tools <任务> [参数]    # 与 go run ./tools/<任务> 等价
```

| 任务 | 用途 | 日志 |
|---|---|---|
| `archive` | 将指定平台已打包产物收成 `build/dist/*.tar.gz`，保留执行权限与符号链接；CI 与本机使用同一入口（`-platform`） | `build/logs/archive-<时间>.log` |
| `check` | vendor 补丁校验 + `gofmt -l` + 前端依赖图校验 + `go vet` + `go test`。**CI 跑的就是这条** | `build/logs/check-<时间>-<唯一后缀>.log` |
| `compile` | 只编译 Go / CLI，不打包或启动桌面（`-platform all`、`-pkg`）；产物 `build/compile/<goos>-<goarch>/` | `build/logs/compile-<时间>.log` |
| `proxy-probe` | 在 `build/proxy-probe/` 的独立模块编译选定代理出站，比较基线、无 Hy2、含 Hy2 的体积和首次/重复构建耗时（`-platform`）；不运行产物 | `build/logs/proxy-probe-<时间>.log` |
| `deps` | 显式依赖更新：`-update` 按 go.mod 重建 vendor、重新应用密码框与 Vision 补丁、复核原哈希并生成许可证 | `build/logs/deps-<时间>.log` |
| `security` | 固定 govulncheck 对桌面/守护进程/CLI 做已知漏洞可达性分析；`-snapshot` 下载官方数据库快照离线分析；`-binary` 检查构建产物 | `build/logs/security-<时间>.log`、同名 JSON 与数据库快照 |
| `test` | 只跑 `go vet` + `go test`（`-race`、`-short`、`-pkg`、`-run`、`-tags`、`-timeout`、`-serial`） | `build/logs/test-<时间>[-race]-<唯一后缀>.log` |
| `identity-qa` | 编译/签名 macOS SSH 密钥原生模块并运行纯内存授权 QA。`-keychain` 验证隔离的合成钥匙串记录，`-embedded` 测量隐藏的指纹控件，`-snapshots` 渲染合成密钥请求。所有路径均无系统认证弹窗 | `build/logs/identity-qa-<时间>-<唯一后缀>.log`、`build/snapshots/ssh-key-authorization/` |
| `ui-review` | 将 build/ 内离线 PNG 或 SmallPen view JSON 汇成编号联系表与 index.json；不捕获真实窗口、访问连接数据或打开 App | `build/review/`（`-output` 必须在 build/） |
| `build` | 编译原生组件与各平台 CLI，再调 MyGo 打包（`-platform`、`-check`）；`-generate-only`仅更新两份受提交生成文件，不打包或下载平台运行库 | 标准输出 |
| `dev` | 静默开发进程：`run` / `status` / `show` / `stop`（`-data-dir`） | — |
| `install` | 将已打包的本机 macOS 版本放入 `/Applications/SmallSSH.app`，校验签名、替换后删除旧版、不启动（可用 `-source` 选择固定候选包） | `build/logs/install-<时间>.log` |
| `service` | 只读核验指定后台的真实 SDK 身份握手与状态，不启动 / 解锁后台（`-executable`、`-data-dir`） | `build/logs/service-<时间>-<唯一后缀>.log` |

日志名称在时间后追加唯一后缀，避免并行任务互相覆盖；同一轮扫描的 JSON、数据库快照沿用对应日志的文件名前缀。

CI 必须调用同一批入口；需要新能力时新增一个 `tools/` 任务，而不是在工作流里另写一套命令。

`proxy-probe` 是显式运行的依赖评估，不属于 `check` 或发布构建。它复制 `tools/proxy-probe/fixture/` 的独立 `go.mod` 到 `build/`，在那里解析依赖并用纯 Go、裁掉符号的参数编译，不改应用的 `go.mod`、`go.sum` 或 `vendor/`。报告中的独立可执行文件体积包含 Go runtime 和公共依赖，不能直接当成桌面应用包的增量；本任务也不验证协议握手、连接成功或运行时内存。

正式构建、编译、测试与开发入口统一带 `with_utls,with_quic,with_grpc` 及上游需要的 `-checklinkname=0`。MyGo 通过 GOFLAGS 的标签和 MYGO_LDFLAGS 接收同一配置；不要用裸 `go build` 代替任务入口，以免遗漏协议。SSH、SFTP、隧道、代理与会话运行时只编入 `smallsshd`；Desktop 与 CLI 通过已验证的本机 IPC 调用。代理模块在守护进程内按 SSH 连接创建，返回的 socket 关闭时释放会话池，不另起代理内核进程。`check` 检查两个生产前端的 go list 依赖图，禁止重新引入 broker/service/sshconn/netroute 及 sing-box 运行时。共享消息和证明规则放在 protocol，公开 SSH 数据在 sshdata，本机密钥预检在 sshkey。

依赖版本先改 `go.mod`，然后在明确更新依赖的任务中执行 `go run -mod=mod ./tools deps -update`。该例外仅用于更新，不用于日常编译。密码框补丁应用后必须与原哈希一致；上游变化时任务会失败，不能自动刷新哈希绕过审核。随后运行 `go run ./tools check`、`go run ./tools security -snapshot` 和相应平台的 `compile` / `build -check`。govulncheck 的 JSON 模式不会仅因输出 finding 自动失败，security 任务会解析结果：源码模式对可达漏洞失败，二进制模式对受影响符号告警失败。二进制模式没有调用图，存在符号不代表存在可利用路径。数据库、扫描或结果解析失败同样失败；0 只表示所用数据库内未检出对应告警，不覆盖未知漏洞、未正确归属的 fork 或全部动态调用。

发布用 CLI 与守护进程统一去除调试符号。macOS 本机 ad-hoc 打包后，将资源目录旧 CLI 入口改为指向 `cli/smallssh` 的相对符号链接，避免同一可执行文件重复占用体积；随后只重新封装外层应用签名并执行深层严格校验，保留原生辅助组件的签名与哈希。指定发行签名身份时跳过此压缩步骤；其它目标仍保留兼容副本。

macOS 本机安装使用 `go run ./tools install`。先按本机架构打包；并行构建时可用 `-source /固定候选包/SmallSSH.app` 明确选择已核验的包，避免读入被其它构建替换的产物。安装任务核对已有应用的 bundle ID，在同一目录暂存完整新包并校验签名，再替换。不保留旧版备份或失败的候选副本。替换期间仅临时保留原包用于失败还原，成功后立即删除；若替换和还原都失败，则报告原包的临时位置以便恢复。它不请求管理员权限、不启动或退出应用；没有目录写入权限时直接报错。 主程序、CLI 和后台若与当前版本的权限和 SHA256 完全一致，会通过硬链接保留原可执行文件 inode，再检查完整候选包签名，避免替换同一程序时令仍在运行的 macOS 进程失去身份。后台代码发生变化时仍须按下节先经已验证管理会话 Stop 后更新，不能把身份校验放宽成只认路径。

### 开发时的静默重启

```sh
go run ./tools dev run       # 监听修改并自动重编译，每次重启均隐藏
go run ./tools dev status    # 只读取就绪、可见、焦点和激活次数
go run ./tools dev show      # 只有明确要求查看时才执行，显示已有开发窗口
go run ./tools dev stop      # 正常退出开发进程和监听器
```

`dev run` 先构建并暂存本机 `smallsshd`，由 MyGo 将其放入开发包。修改后端代码前，先通过已验证管理会话的 Stop 停止旧服务，再停止并重新运行开发任务以重建守护进程。服务身份与新产物不同时会拒绝接入，不强制抢占，也不回退 GUI 内嵌后端；跨版本服务更替的用户流程仍属 U04 后续工作。

开发进程默认使用正式数据目录下独立的 `development/`，不会自动解锁或读取正式凭据；`-data-dir /完整/测试目录 run` 可指定，后续 `status / show / stop` 用同一个目录。后台模式在启动原生事件循环前使用 accessory 策略，主窗口以 Hidden 创建，不先显示再隐藏、不恢复上次可见状态。显示窗口不会解锁或批准 SSH 权限。

### 本机后台只读诊断

```sh
go run ./tools service -executable /Applications/SmallSSH.app/Contents/Resources/smallsshd
```

可用 `-data-dir` 指向其它明确选择的私有目录。该任务只通过现有 SDK 完成进程身份与 TLS 校验并读取状态，不读取保险库内容、不解锁、不授予连接权限，也不自动重启身份不符的后台。更新后报“无法验证调用进程实例”时，先用 macOS `codesign --verify --verbose=4 PID` 对照固定后台路径检查进程；不要关闭身份校验来恢复连接。

### 独立守护进程回归

```sh
go run ./tools compile -platform darwin/arm64
SSH_CLIENT_TEST_DAEMON="$PWD/build/compile/darwin-arm64/smallsshd" go run ./tools test -pkg ./internal/client -tags service_integration -race -run TestDaemonProcessSharesOneCoreAcrossFrontendsAndProxyProtocols
```

该测试只启动真实无界面守护进程、合成 loopback 服务器和临时私有目录，覆盖六种代理 SSH/SFTP、多前端共用、单个管理会话关闭及明确锁定。可将 daemon 路径换为打包产物；macOS 上再设置 SSH_CLIENT_TEST_CLI 指向包内 CLI，并将 run 选择器加上 TestPackagedCLIAgentUsesDaemonAndLocksOnInterrupt，可用私有伪终端验证 agent 解锁/中断锁定，不打开桌面或读取真实凭据。

### 需要额外依赖的测试

终端集成测试默认不参与 `check`：

```sh
go run ./tools test -race -tags terminal_integration -run TestGhostty -pkg ./internal/desktop
```

该测试需要 Ghostty 库，MyGo 构建会从其官方发布下载并核对固定 SHA256。

离线界面快照须显式启用，输出仍放在 `build/`，不会打开系统窗口：

```sh
SSH_CLIENT_PREVIEW_QA=1 SSH_CLIENT_UI_SNAPSHOT_DIR="$PWD/build/snapshots/connection-capabilities" \
  go run ./tools test -pkg ./internal/desktop -tags preview_qa -run TestConnectionCapabilitiesSnapshotsWithoutWindows
```

`-tags` 同时传给 vet 和测试；`check` 也接受这些测试参数。PKCS#11 的 macOS / Linux 原生 ABI 与卡住签名回归需要本机 C 编译器，只编译仓库内合成令牌夹具，产物位于 `build/test-fixtures/`。它们不枚举或访问真实硬件。Windows / Linux 的交叉编译不替代各系统的运行验收。


可用 Linux Docker / Compose 测试主机验证更新保留数据和未选中的服务。管理页面快照使用合成数据，无需真实服务器。

```sh
SSH_CLIENT_DOCKER_QA=1 go run ./tools test -pkg ./internal/broker -run TestLiveDockerComposeUpdatePreservesDataAndUnselectedService -timeout 300s
SSH_CLIENT_PREVIEW_QA=1 SSH_CLIENT_UI_SNAPSHOT_DIR="$PWD/build/snapshots/management" \
  go run ./tools test -tags preview_qa,terminal_integration -pkg ./internal/desktop -run TestManagementResourceSnapshotsWithoutWindows
```

### noVNC 客户端与无窗口浏览器回归

noVNC 源码随 `internal/vncview/web/novnc/` 保存，版本、官方 commit、归档哈希及逐文件哈希在 `ORIGIN.json`；上游源码保持未修改，测试复核哈希，构建生成许可文本。无需 npm 安装、打包或运行时 CDN。Go 复用已固定的独立 WebSocket 编解码库；前端仍不得引入 SSH 或代理内核。

```sh
SMALLSSH_BROWSER_TEST=1 go run ./tools test -pkg ./internal/vncview -tags browser_integration -run TestNoVNCBrowser -race
```

默认查找本机 Chrome，也可用 `SMALLSSH_BROWSER_PATH` 指定 Chromium。测试使用 build/test-fixtures 下的临时独立配置、headless 模式、合成 RFB 服务器及凭据，覆盖密码成功 / 失败、原始像素、组合键、只读和断开，不使用个人浏览器配置或系统窗口。默认输出截图在 build/snapshots/novnc，可用 SSH_CLIENT_UI_SNAPSHOT_DIR 指定 build 内的绝对路径；这不替代各平台 WebView 的实机验收。无浏览器时不能把跳过当作通过。

## 产物与提交边界

构建会写到的路径只有下表这些：

| 路径 | 谁写 | 是否提交 |
|---|---|---|
| `build/<platform>/`（`SmallSSH.app`、exe、deb、tar.gz） | `tools build`、MyGo | 否（`/build/`） |
| `build/dist/`（按平台的 tar.gz） | `tools archive` | 否（`/build/`） |
| `build/logs/`、`build/snapshots/` | `check` / `test` / 无窗口快照测试 | 否（同上） |
| `build/compile/`、`build/test-fixtures/` | 交叉编译 / 原生测试夹具 | 否（同上） |
| `resources/<goos>-<goarch>/`（`biometric-helper`、`libsmallssh-auth.dylib`、`cli/smallssh`、`ssh-client`） | `tools build` 暂存 | 否（`/resources/*-*/`） |
| `internal/systemauth/native-hashes.json` | `tools build` | **是**，app 会拒绝与它不匹配的原生组件 |
| `resources/THIRD_PARTY_NOTICES.txt` | `tools build` | **是**，随包分发许可证 |
| `.mygo/` | `tools dev` | 否 |
| 保险库 / 审计 / 传输 / 套接字 | 运行时 | 否，默认位于仓库外的数据目录 |

`resources/<goos>-<goarch>/` 之所以必须在源码目录里，是 MyGo 把这个目录当作应用资源目录；它不含源码，每次构建先整目录删除再生成，所以旧文件不会混进包。源码资源（`icon.png`、`README.md`、`THIRD_PARTY_NOTICES.txt`）留在 `resources/` 顶层。

两个提交进仓库的生成文件只在内容变化时改写，用 `go run ./tools build -check` 确认它们是最新的（只校验、不写入）。

## 依赖与原生补丁升级

MyGo 版本只在 `go.mod` 声明，`tools build` 与 `tools dev` 都从那里读取。升级 MyGo 时必须重新核对 vendored 的 macOS 密码框补丁，不能直接更新哈希跳过：

先编辑 `go.mod` 中的依赖版本，再使用同一更新任务重建 vendor、重应用补丁并核对原哈希：

```sh
go run -mod=mod ./tools deps -update
go run ./tools check
```

补丁覆盖面、新增 AX 路径的核对方法与尚未实机验证的部分见 `patches/README.md`。第三方开发工具同样用 `go.mod` 的 `tool` 指令钉版本，不在脚本里写版本号。

## CI

三个独立工作流，各自一个职责，不要把事情塞进同一个名字下面：

| 工作流 | 触发 | 作用 | 是否阻断 PR |
|---|---|---|---|
| `checks.yml` → `Checks` | push main、PR、手动 | `go run ./tools check`（补丁校验 + gofmt + vet + 全量测试）及 `go run ./tools security -snapshot` | **是**，分支保护里设为 required |
| `cross-build.yml` → `Cross build` | push main、PR、手动 | 五个目标（darwin/arm64、darwin/amd64、linux/amd64、linux/arm64、windows/amd64）各 `go run ./tools compile -platform <目标>`，只编译 | **是**，另一个平台编译坏了也不该合并 |
| `publisher.yml` → `Publisher` | **仅手动** `workflow_dispatch` | 打出指定平台的发布物（默认 `-platform all`），通过 `tools archive` 收成 tar 包并上传 artifact | 否，不参与合并 |

建议的分支保护设置：`main` 要求 `checks` 与全部五个 `build <goos>/<goarch>` 通过。

CI 只做检查，从不发版：没有 tag 或 release 触发；`Publisher` 也只产出 artifact（保留 7 天），不建 Release、不推任何平台，真正的发布仍由人看过产物后手动完成。发版仍由所有者在自己的机器上手动完成。runner 与 action 版本写死（`macos-26`、`ubuntu-24.04`、`actions/*@v7`，均为 Node 24 运行时），避免 `-latest` 迁移或运行时弃用悄悄改变结果。

打包不进 `Checks` 的理由：`tools build` 需要 macOS 的 swiftc / codesign，且不同 Xcode 版本产出的签名二进制哈希可能不同，`build -check` 放进自动流程会变成假失败。

## 本地化

- 代码里的默认界面文案使用简体中文。界面词条由 `internal/localize/zh-Hans.json` 和 `internal/localize/en.json` 管理。使用 `tr(c, source)` 从当前界面语言取词。添加文案时同时更新两份目录，保持 ICU 与 Go 格式化参数一致。
- 连接名称、命令与输出、路径和服务返回的数据保留原值。语言切换需覆盖同一个窗口打开时的刷新和重启后的设置恢复。
- 用 `go run ./tools test -pkg ./internal/localize` 验证词条。离屏语言快照使用 `TestInternationalizationSnapshotsWithoutWindows`，启用 `preview_qa`。MyGo 版本由 `go.mod` 固定。

## 发布前检查（手动，不由 CI 触发）

1. `go run ./tools check` 全绿，记录日志路径。
2. `go run ./tools build -check` 确认两个生成文件已提交。
3. `go run ./tools build -platform <目标>` 打包，确认包内原生组件哈希与清单一致、签名有效。
4. 在 `docs/VERIFICATION.md` 追加本轮记录：做了什么、证据在哪、哪些断言只是本地成立。
