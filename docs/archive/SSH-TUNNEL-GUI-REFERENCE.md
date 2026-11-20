# SSH 转发 GUI 对照

2026-11-18 核对。用于讨论 SmallSSH 的「中转与服务」入口、单条转发和运行列表。

| 参考 | 现有交互 | SmallSSH 可吸收的做法 |
| --- | --- | --- |
| Termius | 转发规则集中管理，支持按步骤配置或跳到完整表单。向导依次区分 SSH 主机、监听入口和服务目标，规则可逐条启停。 | 简易流程一次只问当前用途必须填的字段。完整配置仍使用同一规则。 [官方向导与界面截图](https://docs.termius.com/organize-and-connect-to-hosts/port-forwarding-and-tunneling) |
| VS Code Remote SSH | 在已连接的主机里，Ports 面板只要求输入远端端口，随后显示实际分配的本机访问地址。需要固定本机端口时，可以之后修改。 | 从服务器右键或当前终端进入时预选服务器。「打开服务器网页」默认只填一个端口，成功后在原位置显示地址和复制、打开动作。 [官方 Ports 界面和步骤](https://code.visualstudio.com/docs/remote/ssh#_forwarding-a-port-creating-ssh-tunnel) |
| RSSH | 一个有名字的转发配置选择一个 SSH 连接配置，包含多条各自指定类型和端点的规则。运行视图按规则显示实际入口、启停、错误和流量。依据固定版本前端源码。 | 同一 SSH 连接下的多个端口可以并列管理。每行保留路径、状态和当下可用动作，详情再展开。 [规则编辑](https://github.com/rssh-org/rssh/blob/806a455ad2d871d9564a6fa72ab00d0efbe4d4af/frontend/lib/components/ForwardEditor.svelte#L100-L133) · [运行视图](https://github.com/rssh-org/rssh/blob/806a455ad2d871d9564a6fa72ab00d0efbe4d4af/frontend/lib/components/ForwardPane.svelte#L200-L275) |
| SSHDeck | Windows 桌面把连接资料和端口转发放在各自页面。转发页按主机展开，新建规则及执行连接都在该主机下。 | 在规则上方显示清楚的服务器上下文。入口只负责查看运行状态和发起新建。 [项目自带转发界面截图](https://github.com/Trilives/sshdeck#port-forwarding) |

## 跳板与规则

跳板机描述 **如何连接 SSH 服务器**。Termius 在目标主机的高级设置中选择一台或多台跳板机，以后连这个主机时复用线路。SmallSSH 可以在简易页标示使用当前会话或服务器默认线路，在完整配置里提供本次覆盖。 [Termius 跳板机设置与桌面截图](https://docs.termius.com/organize-and-connect-to-hosts/connecting-to-a-server#connect-through-jump-hosts)

转发规则描述 **入口通去哪个服务**。同一服务器的网页与数据库可以各占一行，两个规则并列运行。跳板机串在 SSH 线路中。对本地转发来说，服务地址由 SSH 服务器访问。对于动态 SOCKS5 转发，目标由实际使用代理的应用决定。 [Termius 端点说明](https://docs.termius.com/organize-and-connect-to-hosts/port-forwarding-and-tunneling#local-port-forwarding) · [RSSH 逐条字段](https://github.com/rssh-org/rssh/blob/806a455ad2d871d9564a6fa72ab00d0efbe4d4af/frontend/lib/components/ForwardEditor.svelte#L112-L128)

## 评审范围

建议先审「打开服务器网页」的单端口界面，以及启动后包含入口、状态、操作的一行结果。简易用途决定主要按钮，服务器沿用来源上下文，线路和监听端口按需展开。「共享本机服务」成功后标明访问地址所在的服务器，并按 SmallSSH 的访问范围标示回环入口。保存多规则配置涉及额外的持久化与启停语义，需单独排定。SmallSSH 当前状态见 [简易配置](../../internal/desktop/tunnel_setup.go) 和 [服务视图](../../internal/desktop/tunnels.go)。
