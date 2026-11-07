# 客户端会话与子授权

会话只属于当前进程实例持有的活连接。进程退出、连接断开、锁定或代理重启后，原会话不能恢复；新客户端生成新 Ed25519 key，重新等待所有者批准。PID、路径、应用 ID、自报名称、同一可执行文件、复制的 ticket 或签名均不能继承授权。

## 先验证本次代理

先由人类在可信桌面或自行启动的 `smallssh agent` 中解锁保险库：首次可信解锁将稳定的代理公钥配对到当前用户的私有配置。外部客户端默认自动校验此配对，无需每次复制指纹；锁定 / 重启保留同一保险库的公开身份。未知 / 不同身份拒绝连接，不能从待验证服务端接受新公钥。更换保险库后由人类在「设置 → 安全与解锁 → 配对 / 修复本机 CLI」明确更新信任；高级场景仍可用 `--broker-key` / `SSH_CLIENT_BROKER_KEY` 固定完整 `sha256:` 指纹。配对仅认证服务端，不授予客户端访问权限，进程重启仍需新的会话和批准。

IPC 使用 TLS 1.3。唯一不需要显式 pin 的情况是内核确认服务端就在客户端当前进程内，即桌面内置 broker；同用户、同程序的另一个进程不享受此例外。

## 常驻 CLI

```sh
# 指纹必须完整复制自可信所有者界面，不能使用省略号。
export SSH_CLIENT_BROKER_KEY='sha256:完整的64位十六进制公钥指纹'
smallssh --client '我的 AI 工具' session serve server-id read 2h 15m

# 选项放在连接 ID 前面；限定目录会话不开放任意命令。
smallssh session serve --read-root /srv/project server-id read 30m 10m
```

所有者核对配对码、服务器、当前进程和权限后批准。`serve` 在批准后持续读取 stdin 的 JSON 行，并在 stdout 返回对应结果；说明和续期通知写 stderr。它不发布可供其他进程领取权限的本地服务。

```json
{"id":"list-1","operation":{"kind":"list","path":"/srv/project"}}
{"id":"info-1","operation":{"kind":"file-info","path":"/srv/project/readme.txt"}}
{"id":"state","action":"status"}
{"id":"end","action":"close"}
```

每行一个请求，按顺序处理；省略 `host` 时使用会话服务器。支持 `exec`、`list`、`get`、`file-info`，实际可用范围由已批准权限与服务器策略共同决定。任意命令需要 `full`，限定目录会话拒绝命令。JSON 结果包含 `id`、`exitCode` 和可用的 `files`、`fileInfo`；`stdout/stderr` 是 base64 编码的字节，失败带 `error`。每行最多 64 KiB，每路输出最多 8 MiB；流式写入、终端和大型下载使用同进程 Go SDK。

纯命令也可用 `smallssh exec 连接ID` 或 `smallssh session exec 连接ID [2h] [15m]`，申请一次完整会话后每行执行一条命令，原样返回输出，`:quit` 退出。AI 保持同一子进程输入输出，不逐条重启 CLI；每行独立执行，不持久化远端工作目录或环境变量。它与 JSON 行入口共用批准、范围校验及续期实现。

stdin EOF、`close` 或进程退出会关闭控制连接，撤销该会话及子授权。正常的一次性 `exec/ls/get/put` 等命令仍可逐次申请批准，每次从申请到执行保持自己的连接。旧 `session request/status/revoke 文件`、`--session`、`SSH_CLIENT_SESSION` 已停用；指定旧文件的操作明确失败，不静默改成新的请求。

## SDK 与操作绑定

`Client{Dir: ..., BrokerKey: ...}.RequestSession/RequestScopedSession` 返回只驻留当前进程内存的 `Delegation`。客户端负责 `defer d.Close()`，其创建 context 也控制会话生命周期。`Save`、`ReadDelegation`、JSON 导入和导出全部拒绝，不读取或创建会话文件。公开元数据和 ticket 不等于授权，不能用于重连。程序内的多个 goroutine 可以共享同一个 `Delegation`。

每次操作先打开独立、经过代理认证的操作连接。TLS exporter 确定该连接的标识；客户端在原控制连接上请求挑战，签名覆盖操作、目标、主机指纹、期限、nonce 和这个连接标识。broker 同时核验控制连接、操作连接、两者的 OS 进程实例和可执行文件身份。子授权只能在实际绑定的操作连接上消费一次，不能把 ticket 交给另一个进程使用。连接标识只是定位符，不能代替连接本身。

`read` 允许受控读取，不含任意命令或 Shell；`full` 仍受服务器 AI 策略、保护路径和操作规则约束。可设置 `readRoot/writeRoot`；只要设置其一，另一空项即表示不开放对应文件能力。限定目录会话不包含命令、终端、采样或容器日志。每次签发和启动都重新检查当前服务器、范围、期限与撤销状态；远端路径仍做规范化和符号链接检查。

父授权最长 24 小时，子授权最多 5 分钟。原控制连接持续存活时，自然过期可以申请原范围的新一代所有者批准；并发调用共用一次待批准续期。只重试尚未开始的授权步骤，已经启动的命令和文件写入不会自动重放。拒绝、撤销、断线或身份无法核验时拒绝续期，不能从磁盘 key 恢复权限。

## OS 身份与信任根

- macOS：从 socket 的内核 audit token 取得进程世代，用 Security.framework 校验动态代码与 code-directory hash；每个输入帧和签发 / 执行前重新核验。
- Linux：要求内核支持 `SO_PEERPIDFD`（Linux 6.5+），使用 socket 的实际 peer pidfd 防止数字 PID 回收竞态，核对 `/proc` 启动标识和可执行文件身份，记录 SHA256。不可核验时拒绝受托授权，不降级到 PID / 名称匹配。
- Windows：从命名管道取得客户端 PID，持有进程句柄、创建时间和 image 摘要并核对存活与 image 身份。客户端核对的是管道的**服务端** PID，不能把自己的 PID 当作 broker 身份。

可执行文件摘要标识映像，不代表可信发行者、脚本内容、扩展插件或安全行为。路径和应用 ID 只辅助人类核对；不会授予长期权限。broker 每秒检查存活身份；控制响应在释放全局授权锁后发送，每帧写入最长等待 10 秒，停止读取的客户端不能占住授权锁。控制连接结束会清除可续期状态并取消子任务。SDK 的私有连接不跨 exec 继承，不提供进程重启后的恢复机制。

此模型依赖 OS 进程隔离、可信 broker 和所有者入口。它阻止凭据文件 / PID / 名称复用，不能阻止 root、调试器、注入、内存读取或获准进程主动转发其输入输出、密钥与连接状态。完整抵抗同账户任意代码需要部署层的系统隔离与 broker 完整性保护，不能仅凭这些应用层检查宣称已经具备。Go 密钥切片会主动清零，但不承诺全内存擦除。当前 Windows / Linux 身份适配只有交叉编译证据，运行验证以本轮 macOS 记录为准。

审计记录授权与父子关系、可信进程实例、程序摘要、服务器、操作摘录和结果，不记录 key、ticket 或输出。摘录可能含命令中的秘密，详见 [安全与边界](SECURITY.md)。
