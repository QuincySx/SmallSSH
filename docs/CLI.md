# CLI 与 AI 使用

从源码通过统一任务入口编译（不启动桌面）：

```sh
go run ./tools compile -pkg ./cmd/ssh-client
```

产物在 `build/compile/<goos>-<goarch>/ssh-client`（Windows 带 `.exe`）。以下以 `smallssh` 代指安装后的 CLI。先在可信桌面解锁保险库，或在自己启动的 `smallssh agent` 中解锁：首次解锁会将稳定的代理公钥写入当前用户私有数据目录的 `broker-trust.json`。之后的外部命令自动校验这条配对，不需要复制指纹，锁定 / 重启后仍可使用。

配对只验证代理身份，不授予服务器访问权限。客户端进程或连接重启后仍须生成新会话密钥并重新批准。不存在配对或公钥不同则拒绝连接，不会从未知端点自动接受新身份。更换 / 重建保险库后，在「设置 → 安全与解锁 → 配对 / 修复本机 CLI」明确更新信任。高级用法可用 `--broker-key sha256:...` 或 `SSH_CLIENT_BROKER_KEY` 覆盖配对；旧环境变量仍优先，应在不再需要显式固定时取消设置。

连续命令优先使用 `smallssh exec 连接ID`，批准一次后逐行输入命令。下列带完整命令的形式是单次操作，每次启动均单独申请：

```sh
smallssh hosts
smallssh --client '我的 AI 助手' exec server-id 'uname -a'
smallssh --client '我的 AI 助手' ls server-id /srv
smallssh get server-id /srv/config.txt ./config.txt
smallssh put server-id ./new.txt /srv/new.txt
smallssh shell server-id
smallssh monitor server-id
smallssh transfer list
smallssh transfer resume TRANSFER_ID
smallssh host group server-id '生产环境'
smallssh biometric status
smallssh biometric enable
smallssh --biometric approve REQUEST_ID
```

## 连接选项、硬件枚举与隧道

`host options` 需要所有者验证，从 stdin 接受一个不超过 64 KiB 的 JSON 对象，仅更新提供的选项组；该组内未提供字段使用零值。例子只使用占位配置：

```sh
smallssh host options server-id <<'JSON'
{"terminal":{"encoding":"gb18030","backspace":"bs"},"network":{"proxy":{"mode":"socks5","address":"127.0.0.1:1080"},"keepaliveSeconds":30,"keepaliveFailures":3}}
JSON
```

编码值为 `auto` / `utf-8` / `gb18030` / `gbk`，退格为 `auto` / `del` / `bs`；空值表示默认。代理 mode 为 `none` / `http` / `https` / `socks5`。中转使用 `network.relay` 填已保存连接 ID，同一节点不能同时选择代理与中转；省略 `network` 保留旧网络组。同一代理未提供密码时保留已保存密码，显式 `"clearProxyPassword":true` 清空；换地址 / 用户不会沿用旧代理密码。保活间隔为 `0`（关闭）或 5–3600 秒，阈值 1–10，零值使用 3 次。

PKCS#11 公开枚举同样需要所有者验证；省略序列号先列令牌，给定序列号后按设备需要从本机 TTY 读取 PIN，PIN 不通过命令参数输入：

```sh
smallssh host hardware /完整路径/可信的PKCS11模块
smallssh host hardware /完整路径/可信的PKCS11模块 TOKEN_SERIAL
```

输出包含模块摘要及令牌 / 公钥元数据。`host options` 的 `hardware` 组使用 `module`、`moduleSHA256`、`tokenSerial`、`keyId`、`fingerprint`、`pin`；提供该组会切换到 PKCS#11 登录，默认保留原登录凭据的行为不适用于换硬件。只在可信本机 stdin 输入秘密，模块须与当前系统 / 架构匹配。驱动会执行原生代码，设备兼容性边界见 [使用说明](USAGE.md#pkcs11-硬件密钥)。

隧道命令申请一次性操作，等待所有者批准后运行，实际监听地址写到 stderr；前台进程需保持运行，Ctrl+C 关闭监听与现有连接：

```sh
smallssh tunnel local server-id 127.0.0.1:0 127.0.0.1:5432
smallssh tunnel remote server-id 127.0.0.1:0 127.0.0.1:8080
smallssh tunnel socks5 server-id 127.0.0.1:1080
```

仅支持回环 TCP；SOCKS5 不接受固定目标。其他本地进程可使用这个端口，动态隧道可访问任意 TCP 目标。默认保护模式、只读及文件根目录范围不允许隧道，审批不会绕过服务器策略。远程实际绑定范围须核对 SSH 服务端 `GatewayPorts` 配置。

## 连续客户端会话

连续命令可以直接使用：

```sh
smallssh --client '我的 AI 助手' exec server-id
# 同一进程中逐行输入 uname -a、df -h 等命令；:quit 或 EOF 退出
smallssh --client '我的 AI 助手' session exec server-id 2h 15m
```

这两个入口申请同一类 `full` 会话，等待所有者核对服务器、进程、权限、有效期与闲置设置后批准；随后每条命令逐次校验、签发与审计，不再逐条等待人工批准。输出原样写入 stdout，远端 stderr、授权提示和每条退出码写入 stderr。非零退出码不会自动重跑命令；EOF / `:quit` 返回最后一条命令的退出码并撤销会话。策略拒绝会报告后继续等待下一行；撤销、断线等会话错误直接退出，不回退到新的一次性授权。每行上限32KiB，各行不共享工作目录或环境变量，需要连贯操作时写在同一行。请求中的2h / 15m只是建议，审批界面由所有者决定最终期限。

AI 调用方应保持这个子进程及其 stdin / stdout 活着，后续命令写到同一输入，不能每条命令都重新启动 `smallssh exec server-id '命令'`。后者保留一次性语义，不会凭程序名或 PID 继承之前授权。

需要结构化结果、文件操作时，继续使用 JSON 行客户端，在同一个进程内保持会话：

```sh
smallssh --client '我的 AI 助手' session serve server-id read 2h 15m
smallssh session serve --read-root /srv/project server-id read 30m 10m
```

所有者核对配对码、服务器、权限、期限和当前进程后批准。随后在 stdin 发送 JSON 行，例如 `{"id":"one","operation":{"kind":"list","path":"/srv"}}`；stdout 返回 JSON 结果，字节输出使用 base64。支持 `exec/list/get/file-info`，`status/close` 作为控制动作。写入、终端和大型传输使用同进程 SDK 或逐次批准的一次性 CLI。详细协议见 [客户端会话与子授权](DELEGATION.md)。

会话仅属于当前进程实例及活连接。退出、断线或重启后生成新 key 并重新批准，不能用同 PID、程序名称或 ticket 恢复。旧 `--session` / `SSH_CLIENT_SESSION` 和文件式会话命令明确拒绝，不再创建或读取能力文件。仅自然过期且控制连接持续存活时，允许申请原范围续期并等待所有者批准。

默认只读；`full` 仍受每台服务器的 AI 访问、保护路径和操作规则限制。命令护栏可被引号、变量、编码和解释器绕过，不能当作安全边界；受保护模式不开放交互终端。父授权最长 24 小时，子操作最多 5 分钟，每个操作仍重新核验当前范围和策略。申请方标签是自报信息，不能代替内核取得的进程实例。

无桌面时，由用户在本地终端启动代理，在另一终端处理授权：

```sh
smallssh init
smallssh host add
smallssh agent
# 首次可信解锁建立本机配对；其他终端自动验证同一保险库的代理身份。
```

```sh
smallssh pending
smallssh approve REQUEST_ID
smallssh deny REQUEST_ID
smallssh revoke REQUEST_ID
```

`hosts` 不需要主密码：没有所有者证明时只返回连接 ID、分组与名称，地址、账号、登录方式、指纹与自动化策略一律不返回，因此自动化拿得到"要为哪台机器申请访问"，拿不到"怎么连上它"。`pending` 仍需所有者验证，普通调用方只能在原活连接上配合自己的票据查询对应请求。批准、拒绝和撤销先解锁并展示具体请求，再从控制终端输入 `yes` 确认。

受保护目录内的文件读取会直接拒绝。新上传使用排他创建；真正续传（写入已存在的远端分片）在保护模式中禁用，因为 SFTP 无法证明那个 inode 不是受保护目录里的硬链接。非所有者的一次续传上传若被策略拒绝，会自动退回"整文件重传进独占创建的新分片"，而不是永久失败。

主密码只从人类使用的控制终端读取，不接受密码参数、环境变量或管道输入。`host add` 的离线编辑需要先退出代理；桌面保存连接通过代理完成并撤销旧授权。

覆盖保存需要提供读取时的原文件 SHA256：

```sh
smallssh replace server-id ./edited.txt /srv/config.txt ORIGINAL_SHA256
```

复制旧文件、检查冲突后，程序使用服务器的原子重命名扩展提交。成功时输出旧文件副本路径。SFTP 不具备原子的内容比较交换，外部程序仍可能与最后一次检查竞争；文件属主、ACL 等扩展元数据也不会完整复制。详见 [安全与边界](SECURITY.md)。
