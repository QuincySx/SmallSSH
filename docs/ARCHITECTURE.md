# 架构

SmallSSH 由桌面、CLI 和本机连接后台组成。三个入口从同一个源码仓库构建。

```text
cmd/desktop ────┐
                 ├─ internal/client + 本机 IPC ── smallsshd ── SSH / SFTP / 隧道 ── 服务器
cmd/ssh-client ─┘                                      │
                                                       ├─ 保险库、授权与审计
                                                       └─ 本机 SSH Agent 与签名组件
```

## 模块职责

| 模块 | 职责 |
| --- | --- |
| `cmd/desktop`、`internal/desktop` | MyGo 原生窗口、工作区、终端与文件工具、设置和审批。界面布局与入口见 [桌面设计](design/DESIGN.md)。 |
| `cmd/ssh-client` | 人类 CLI 和受托调用。所有者验证与受托会话沿用各自的授权入口。 |
| `internal/client`、`internal/protocol` | SDK、进程和连接绑定、请求及响应类型。 |
| `cmd/smallsshd`、`internal/service` | 本机守护进程、后台身份、解锁交接、管理会话和锁定。 |
| `internal/broker` | 统一协调授权、目标、SSH 连接池、传输、服务采样和操作审计。 |
| `internal/approval`、`internal/accesspolicy`、`internal/operationrisk` | 会话与子操作、服务器策略、风险判定。 |
| `internal/vault`、`internal/auditlog` | 加密保存连接和策略，持久化本机审计及校验链。 |
| `internal/sshconn`、`internal/netroute`、`internal/serverops` | SSH/SFTP 执行、网络线路、结构化服务器操作。 |
| `internal/identityagent`、`native/macos` | 本机 SSH Agent 与 macOS 原生签名授权。 |
| `internal/transfer`、`internal/monitor` | 持久化传输任务、状态采样和有界数据模型。 |

构建门禁检查前端依赖图。生产桌面与 CLI 通过 SDK 调用同一个后台，SSH、SFTP、代理出站和端口转发编入 `smallsshd`。更多构建约束见 [开发与发布](DEVELOPMENT.md)。

## 请求、窗口与连接

1. 桌面在 UI 线程提交状态，由工作协程处理阻塞操作。异步结果按窗口、会话世代和请求身份核对，避免断开或重连后旧结果覆盖当前视图。
2. 保存连接只处理本机配置。显式连接按本次线路运行主机身份确认，再由后台完成认证和拨号。
3. 所有者管理能力来自已验证的本机控制连接。CLI 或 SDK 的受托操作走独立权限、挑战、批准与审计路径。签发和执行都复核目标及当前权限，详见 [安全与边界](SECURITY.md)。
4. 短操作在同一有效配置下复用已验证连接。交互式终端和长流使用各自的生命周期上下文。锁定、撤销和到期关闭对应访问，普通标签切换只改变展示。
5. 传输队列保存方向、路径、进度与目标身份。续传核对远端、数据前缀和内容指纹。目录下载通过父任务保留子文件清单和状态。

主窗口关闭是否驻留由「运行与后台」设置决定。独立守护进程的存在与可用的独立服务模式分开追踪，交付状态见 [需求](REQUIREMENTS.md#产品需求)。

## VNC 与 SSH 签名

VNC 默认使用工作区标签，WebView 中运行内置 noVNC。桌面运行固定目标的本机 WebSocket/TCP 适配，连接可直达 VNC 服务或使用经 `smallsshd` 授权的 SSH 回环隧道。关闭视图、取消连接和锁定都会清理它持有的通道。

macOS Secure Enclave 签名由包内原生辅助应用处理。签名调用方、密钥使用许可和服务器操作许可分别核验。SSH Agent 只返回签名，本地服务器操作审计由后台记录。

## 演进

当前交付本机模式。跨设备服务和远程网关需要独立主体、设备身份和授权入口。产品决定见 [本机核心与网关 ADR](adr/0001-local-core-and-gateway-evolution.md)。
