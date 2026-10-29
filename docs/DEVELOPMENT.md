# 开发、构建与发布

本文是构建、测试、CI 与依赖升级的唯一权威说明。README 只保留用法；`AGENTS.md` 只保留工作流约束，两者都不重复这里的命令细节。

## 前置条件

- Go 1.27.3 或更新的兼容版本（版本号只写在 `go.mod`，CI 从那里读）。
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
| `check` | `patches/vendor-integrity.json` 校验 + `gofmt -l` + `go vet` + `go test`。**CI 跑的就是这条** | `build/logs/check-<时间>.log` |
| `test` | 只跑 `go vet` + `go test`（`-race`、`-short`、`-pkg`、`-timeout`、`-serial`） | `build/logs/test-<时间>[-race].log` |
| `build` | 编译原生组件与各平台 CLI，再调 MyGo 打包（`-platform`、`-check`） | 标准输出 |
| `dev` | 静默开发进程：`run` / `status` / `show` / `stop`（`-data-dir`） | — |

CI 必须调用同一批入口；需要新能力时新增一个 `tools/` 任务，而不是在工作流里另写一套命令。

### 开发时的静默重启

```sh
go run ./tools dev run       # 监听修改并自动重编译，每次重启均隐藏
go run ./tools dev status    # 只读取就绪、可见、焦点和激活次数
go run ./tools dev show      # 只有明确要求查看时才执行，显示已有开发窗口
go run ./tools dev stop      # 正常退出开发进程和监听器
```

开发进程默认使用正式数据目录下独立的 `development/`，不会自动解锁或读取正式凭据；`-data-dir /完整/测试目录 run` 可指定，后续 `status / show / stop` 用同一个目录。后台模式在启动原生事件循环前使用 accessory 策略，主窗口以 Hidden 创建，不先显示再隐藏、不恢复上次可见状态。显示窗口不会解锁或批准 SSH 权限。

### 需要额外依赖的测试

终端集成测试默认不参与 `check`：

```sh
go test -race -tags terminal_integration -run TestGhostty ./internal/desktop
```

该测试需要 Ghostty 库，MyGo 构建会从其官方发布下载并核对固定 SHA256。

## 产物与提交边界

构建会写到的路径只有下表这些：

| 路径 | 谁写 | 是否提交 |
|---|---|---|
| `build/<platform>/`（`SmallSSH.app`、exe、deb、tar.gz） | `tools build`、MyGo | 否（`/build/`） |
| `build/logs/`、`build/snapshots/` | `check` / `test` / 无窗口快照测试 | 否（同上） |
| `resources/<goos>-<goarch>/`（`biometric-helper`、`libsmallssh-auth.dylib`、`cli/smallssh`、`ssh-client`） | `tools build` 暂存 | 否（`/resources/*-*/`） |
| `internal/systemauth/native-hashes.json` | `tools build` | **是**，app 会拒绝与它不匹配的原生组件 |
| `resources/THIRD_PARTY_NOTICES.txt` | `tools build` | **是**，随包分发许可证 |
| `.mygo/` | `tools dev` | 否 |
| 保险库 / 审计 / 传输 / 套接字 | 运行时 | 否，默认位于仓库外的数据目录 |

`resources/<goos>-<goarch>/` 之所以必须在源码目录里，是 MyGo 把这个目录当作应用资源目录；它不含源码，每次构建先整目录删除再生成，所以旧文件不会混进包。源码资源（`icon.png`、`README.md`、`THIRD_PARTY_NOTICES.txt`）留在 `resources/` 顶层。

两个提交进仓库的生成文件只在内容变化时改写，用 `go run ./tools build -check` 确认它们是最新的（只校验、不写入）。

## 依赖与原生补丁升级

MyGo 版本只在 `go.mod` 声明，`tools build` 与 `tools dev` 都从那里读取。升级 MyGo 时必须重新核对 vendored 的 macOS 密码框补丁，不能直接更新哈希跳过：

```sh
go get github.com/egoist/mygo@<版本>
go mod tidy && go mod vendor
git apply -p1 patches/mygo-macos-password-accessibility.patch
shasum -a 256 vendor/github.com/egoist/mygo/internal/darwin/access.go   # 写入 patches/vendor-integrity.json
go run ./tools check                                                  # 含补丁校验
```

补丁覆盖面、新增 AX 路径的核对方法与尚未实机验证的部分见 `patches/README.md`。第三方开发工具同样用 `go.mod` 的 `tool` 指令钉版本，不在脚本里写版本号。

## CI

三个独立工作流，各自一个职责，不要把事情塞进同一个名字下面：

| 工作流 | 触发 | 作用 | 是否阻断 PR |
|---|---|---|---|
| `checks.yml` → `Checks` | push main、PR、手动 | `go run ./tools check`（补丁校验 + gofmt + vet + 全量测试） | **是**，分支保护里设为 required |
| `cross-build.yml` → `Cross build` | push main、PR、手动 | 五个目标（darwin/arm64、darwin/amd64、linux/amd64、linux/arm64、windows/amd64）各 `go build -mod=vendor ./...`，只编译 | **是**，另一个平台编译坏了也不该合并 |
| `publisher.yml` → `Publisher` | **仅手动** `workflow_dispatch` | 一次打出全部五个平台的发布物（`-platform all`），按平台上传 tar 包作为 artifact | 否，不参与合并 |

建议的分支保护设置：`main` 要求 `checks` 与全部五个 `build <goos>/<goarch>` 通过。

CI 只做检查，从不发版：没有 tag 或 release 触发；`Publisher` 也只产出 artifact（保留 7 天），不建 Release、不推任何平台，真正的发布仍由人看过产物后手动完成。发版仍由所有者在自己的机器上手动完成。runner 与 action 版本写死（`macos-26`、`ubuntu-24.04`、`actions/*@v7`，均为 Node 24 运行时），避免 `-latest` 迁移或运行时弃用悄悄改变结果。

打包不进 `Checks` 的理由：`tools build` 需要 macOS 的 swiftc / codesign，且不同 Xcode 版本产出的签名二进制哈希可能不同，`build -check` 放进自动流程会变成假失败。

## 发布前检查（手动，不由 CI 触发）

1. `go run ./tools check` 全绿，记录日志路径。
2. `go run ./tools build -check` 确认两个生成文件已提交。
3. `go run ./tools build -platform <目标>` 打包，确认包内原生组件哈希与清单一致、签名有效。
4. 在 `docs/VERIFICATION.md` 追加本轮记录：做了什么、证据在哪、哪些断言只是本地成立。
