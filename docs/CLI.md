# CLI 与 AI 使用

从源码通过统一任务入口编译（不启动桌面）：

```sh
go run ./tools compile
```

CLI 产物在 `build/compile/<goos>-<goarch>/ssh-client`（Windows 带 `.exe`），同目录包含必需的 `smallsshd`；分发时保留两者，或使用应用包内完整资源布局。以下以 `smallssh` 代指安装后的 CLI。先在可信桌面解锁保险库，或在自己启动的 `smallssh agent` 中解锁：首次解锁会将稳定的代理公钥写入当前用户私有数据目录的 `broker-trust.json`。之后的外部命令自动校验这条配对，不需要复制指纹，锁定 / 重启后仍可使用。

`smallssh agent` 启动/连接同目录固定的无界面 `smallsshd`，完成本机验证后只持有当前进程绑定的管理能力，根解锁材料随即关闭。多个前端复用同一服务；管理会话最长8小时，到期需重新解锁。agent 的 Ctrl+C 明确请求全局锁定；单独关闭管理控制连接只撤销自己的管理会话。

配对只验证代理身份，不授予服务器访问权限。客户端进程或连接重启后仍须生成新会话密钥并重新批准。不存在配对或公钥不同则拒绝连接，不会从未知端点自动接受新身份。更换 / 重建保险库后，在「设置 → 安全与解锁 → 配对 / 修复本机 CLI」明确更新信任。高级用法可用 `--broker-key sha256:...` 或 `SSH_CLIENT_BROKER_KEY` 覆盖配对；旧环境变量仍优先，应在不再需要显式固定时取消设置。

CLI 只用主密码验证所有者（从本机终端读取）。系统验证（Touch ID 等）只在桌面端使用：CLI 可被其他进程放进它控制的终端运行，弹出的系统框无法说明真正授权的内容，所以 `--biometric` 与 `smallssh biometric …` 已停用并会直接报错。

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

这两个入口默认申请`guarded`限时受控会话，等待所有者核对服务器、进程、权限、有效期与闲置设置后批准；随后已识别低风险查询直接执行，需要核对的命令单独确认，已识别高风险直接拒绝。每条操作仍校验、签发与审计；单次确认不扩大或续期父会话。输出原样写入 stdout，远端 stderr、授权提示和每条退出码写入 stderr。非零退出码不会自动重跑命令；EOF / `:quit` 返回最后一条命令的退出码并撤销会话。策略拒绝或拒绝当前操作会报告后继续等待下一行；撤销、断线等会话错误直接退出，不回退到新的一次性授权。每行上限32KiB，各行不共享工作目录或环境变量，需要连贯操作时写在同一行。请求中的2h / 15m只是建议，审批界面由所有者决定最终期限。

`session serve`省略权限时也默认`guarded`，也可显式选择`read`、`maintenance`或`full`。固定自动查询包含uname、id、df、free、uptime、ps等受限参数；管道、重定向、脚本及未知命令需要核对，不因没命中高危护栏而自动通过。

AI 调用方应保持这个子进程及其 stdin / stdout 活着，后续命令写到同一输入，不能每条命令都重新启动 `smallssh exec server-id '命令'`。后者保留一次性语义，不会凭程序名或 PID 继承之前授权。

需要结构化结果、文件操作时，继续使用 JSON 行客户端，在同一个进程内保持会话：

```sh
smallssh --client '我的 AI 助手' session serve server-id read 2h 15m
smallssh session serve --read-root /srv/project server-id read 30m 10m
```

所有者核对配对码、服务器、权限、期限和当前进程后批准。随后在 stdin 发送 JSON 行，例如 `{"id":"one","operation":{"kind":"list","path":"/srv"}}`；stdout 返回 JSON 结果，字节输出使用 base64。支持 `exec/list/get/file-info`，`status/close` 作为控制动作。写入、终端和大型传输使用同进程 SDK 或逐次批准的一次性 CLI。权限边界见 [安全与边界](SECURITY.md#受托会话)。

会话绑定当前进程实例、程序摘要和活连接。macOS 从系统校验过的签名读取申请端的应用 ID。退出、断线或重启后重新批准，旧 `--session`、`SSH_CLIENT_SESSION` 和文件式会话入口均拒绝。受控等会话自然过期后可申请原范围续期，维护准备会话到期即结束。

默认权限为 `guarded`，只读会话须显式选 `read`。后台按每台服务器的策略与本次会话范围复核操作。JSON 行每行最多 64 KiB，每路输出最多 8 MiB。

### 一次授权完成维护准备

```sh
smallssh session maintenance --read-root /srv/backups/server.enc server-id
```

默认请求一小时，最终期限和闲置设置由所有者确认。通过同一个进程的 stdin 逐行发送请求，stdout 逐行返回结果。

```json
{"id":"inspect","action":"run","operation":{"kind":"exec","command":"df -h"}}
{"id":"preview","action":"preview-upgrade"}
{"id":"download","action":"backup","remote":"/srv/backups/server.enc","local":"./server.enc"}
{"id":"resume","action":"resume-backup","transferId":"下载结果中的 transferId"}
```

`--read-root` 可以指定一个已有文件，或一个目录。省略该参数时仅开放系统检查和升级预览。备份使用 SFTP 复制现有文件，目录可通过 `"directory":true` 递归下载。下载核对内容，不覆盖本地文件。原文件已加密时保留原密文。续传填入下载结果中的任务 ID。预览面向 Debian 和 Ubuntu，使用远端现有软件包索引模拟升级。服务器的路径保护继续生效。任意脚本、安装更新、重启和远端写入会在维护会话内被拒绝。

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

## SDK 会话

`Client.RequestSession` 或 `RequestScopedSession` 返回待审批请求和绑定当前进程的 `Delegation`。等待父请求获批后，把该会话交给 SDK 客户端。工作完成时调用 `Close`。子操作返回 `pending` 时先 `Wait` 再 `Start`。相同待确认操作返回 `approval.ErrReviewPending`，应等待原申请。单次拒绝返回 `client.ErrOperationDenied`，原受控会话仍可执行获准范围内的操作。

维护准备会话使用 `RequestScopedSession`，权限设为 `maintenance`，备份来源设为 `readRoot`，`writeRoot` 留空。没有备份任务时可留空两者。会话证明及过期规则见 [安全与边界](SECURITY.md#受托会话)。

## Docker 与 Compose

```sh
smallssh docker logs --engine ENGINE_ID --tail 200 HOST_ID FULL_CONTAINER_ID
smallssh docker terminal --engine ENGINE_ID --shell sh HOST_ID FULL_CONTAINER_ID
smallssh docker plan --engine ENGINE_ID --container FULL_CONTAINER_ID \
  --directory /srv/app --files /srv/app/compose.yaml --services web HOST_ID PROJECT
smallssh docker update --engine ENGINE_ID --container FULL_CONTAINER_ID \
  --directory /srv/app --files /srv/app/compose.yaml --services web \
  --hash HASH_FROM_PLAN HOST_ID PROJECT
```

把预览返回的模型摘要用于更新。后续步骤复核引擎、服务器、配置和选定服务，并按权限要求等待审批。
