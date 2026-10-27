# CLI 与 AI 使用

桌面端解锁后，CLI 直接连接同一个本地代理并使用相同数据目录。从源码构建：

```sh
go build -o build/bin/smallssh ./cmd/ssh-client
```

从源代码构建：

```sh
go build -o build/bin/smallssh ./cmd/ssh-client
```

桌面端解锁后，CLI 直接连接同一个本地代理，使用相同数据目录。

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

可一次批准指定客户端的有期限会话，再由代理自动签发受限的一次性子授权：

```sh
smallssh --client '我的 AI 助手' session request server-id my-ai.json read 2h 15m
smallssh --session my-ai.json ls server-id /srv
smallssh session status my-ai.json
smallssh session revoke my-ai.json
```

首次批准需要核对申请端和所有者界面的配对码、服务器、权限与期限。默认只读；`full` 提供读写能力，仍受每台服务器的 AI 访问和关键路径策略限制。默认保护模式不开放任意命令或终端；所有者明确关闭保护并确认风险后才开放该 SSH 账号完整能力。会话文件只存于代理私有目录；它代表该客户端的委托权限，不含 SSH 凭据。父授权最长 8 小时，子操作最多 5 分钟，锁定 / 退出 / 撤销后失效。详情见 [客户端会话与子授权](DELEGATION.md)。

默认关键路径保护会直接拒绝任意命令和终端；关闭保护后的普通命令会申请授权并等待，所有者批准后才连接服务器。使用 `--session` 时由代理核对既有范围并签发短期票据。申请方标签是调用方自报信息，**不代表经过验证的进程身份**。

无桌面时，由用户在本地终端启动代理，在另一终端处理授权：

```sh
smallssh init
smallssh host add
smallssh agent
```

```sh
smallssh pending
smallssh approve REQUEST_ID
smallssh deny REQUEST_ID
smallssh revoke REQUEST_ID
```

主密码只从人类使用的控制终端读取，不接受密码参数、环境变量或管道输入。`host add` 的离线编辑需要先退出代理；桌面保存连接通过代理完成并撤销旧授权。

覆盖保存需要提供读取时的原文件 SHA256：

```sh
smallssh replace server-id ./edited.txt /srv/config.txt ORIGINAL_SHA256
```

复制旧文件、检查冲突后，程序使用服务器的原子重命名扩展提交。成功时输出旧文件副本路径。SFTP 不具备原子的内容比较交换，外部程序仍可能与最后一次检查竞争；文件属主、ACL 等扩展元数据也不会完整复制。详见 [安全与边界](SECURITY.md)。
