---
repo: d1sm/finalspeed
generated: 2026-11-06
type: comparison
source_ref: b819a35
resolved_sha: b819a35a48e6569e224198a9162ae3bb2fa8174a
accessed_at: 2026-11-06
---

# FinalShell 功能基线与 FinalSpeed 加速资料

本轮主要竞品是用户指定的 [SSHyard](https://github.com/Licoy/sshyard)，由另一份清单核查。**本文只补充用户此前给出的 FinalShell / FinalSpeed 参考，不将它们替代为本轮 SSHyard 需求来源。**

本文只核查公开的一手资料，未安装、运行 FinalShell / FinalSpeed，未测试其服务，也未读取用户配置。下文“已确认”表示官方文档或所注明版本的源码有直接证据，不表示已做本机实测。本文不代替本项目的实现验收。

## 本轮结论

- FinalShell 官方确实列出 SSH / 远程桌面加速；更新日志还记载 2017-02-22 的“服务器双边加速”。因此，竞品对照必须单列双边加速，不能只列代理、跳板或建连耗时比较。[功能页][F1]、[更新日志][F2]
- **FinalShell 的加速是否就是 FinalSpeed：未证实。** 本轮打开的 FinalShell 官方页面没有说明实现所用协议、源码或 FinalSpeed 的名称。官方自定义加速教程证明的是本地端口映射到指定远端服务的使用方式，不能据此认定底层实现。[SSH 教程][F3]、[远程桌面教程][F4]
- **FinalSpeed 的 UDP 可靠传输有历史上游源码证据。** 数据序号、确认、顺序交付、重发队列与速率控制均有代码；它需要对应服务端，不是给普通 SSH 客户端增加一次代理设置就自动获得的能力。[Receiver][U4]、[ResendManage][U5]、[服务端入口][U7]
- 原版 FinalSpeed 的历史上游 README 明确区分其与 TCPSpeed，并说明前者没有传输加密；已读数据路径也没有压缩步骤。**传输加速、SSH 加密、SSH 协议压缩、文件打包压缩是四项不同能力。** 本轮没有性能实测，不沿用带宽利用率的营销数字作为承诺。[历史 README][U3]、[Pipe][U8]、[DataMessage][U9]

## 研究问题与资料基线

本轮回答三个问题：官方能证明哪些产品功能和收费边界；能否建立 FinalShell 与 FinalSpeed 的实现关系；FinalSpeed 哪些传输机制有直接证据。协议完整性、现代系统兼容性和性能评测留待后续专项研究。

| 资料 | 页面 / revision 日期 | 本轮用途与限制 |
|---|---|---|
| [FinalShell 功能页][F1] | 初发 2017-01-17；标题标注 4.6.3、2025-05-21 | 产品声明基线；标题日期不代表每条特性的更新时间 |
| [更新日志][F2] | 初发 2017-01-16；最新列项 2025-05-21 / 4.6 | 提供历史功能日期；与功能页的 4.6.3 标记不同，保留原始差异 |
| [SSH 自定义加速][F3] / [远程桌面自定义加速][F4] | 均为 2017-09-12 | 描述使用入口，没有披露传输协议 |
| [FAQ][F5] | 初发 2017-11-02；未见逐项更新日期 | 提供前提与限制，不能将旧说明自动视为当前所有平台行为 |
| [高级版 / 专业版 / 同步说明][F6] | 初发 2017-09-24；未见统一更新日期 | 页面当前可见的版本归属；未进行购买或结算验证 |
| [d1sm/finalspeed 当前 master][U1] | `f59274d607f33a581b19466bbb5b7c1eb3031458`，2017-11-30 | 仓库可访问，当前树仅剩 `.classpath`、`.gitignore`、`.project`；不能把当前分支当作完整源码 |
| [d1sm/finalspeed 历史源码][U2] | `b819a35a48e6569e224198a9162ae3bb2fa8174a`，2016-05-05，提交说明 1.2 | 本文上游机制结论固定于此版本；不是当前维护版本声明 |
| [d1sm/finalspeed 历史 README][U3] | `26b4fe14df4a67ccd95ee8a3b7b00059e8a2285e`，2017-11-19 | 源码删除后的作者说明；与 2016 源码基线分开使用 |
| [edwinyzh/finalspeed-2][B1] | master `071eaa83e39270aeec09ad6c5045eacfd756d99a`，2017-05-05 | 独立分支的说明；GitHub 标示 fork 自 `aweyonhub/finalspeed`，不冒充原上游 |

以上资料均于 **2026-11-06** 访问。GitHub revision 日期经公开 API 的 commit 元数据核对，源码链接固定到 SHA。

## FinalShell 功能分组清单

“未知”表示所查官方资料没有明确划分免费 / 收费，并不表示免费。高级版项目由专业版包含；这里记录页面归属，未独立核验客户端授权。

| 功能组 | 官方已明确的范围 | 版本 / 证据边界 |
|---|---|---|
| 平台 | Windows、macOS、Linux；下载列出 x64、部分 ARM64 和龙芯版本 | 各平台能力未保证一致。[功能页][F1] |
| 连接与管理 | SSH、Windows 远程桌面、多标签、批量服务器管理 | 免费 / 收费未知；“批量管理”未明确等于批量执行或广播。[功能页][F1] |
| 终端与文件联动 | 终端 / SFTP 同屏，目录联动 | 免费 / 收费未知；FAQ 提示联动依赖 Exec Channel，浏览依赖服务器 SFTP。[功能页][F1]、[FAQ][F5] |
| SFTP 与文件操作 | 浏览目录；历史更新列出排序、权限、上传、新建及外部拖拽上传 | 免费 / 收费未知；不能从这些条目推定所有递归、续传、跨服务器传输能力。[更新日志][F2] |
| 文本编辑 | 内置编辑器、着色、折叠、查找与替换 | 免费 / 收费未知。[功能页][F1] |
| 命令入口 | 本地输入框、补全、匹配、命令历史、快捷命令面板 | 基础项归属未知；无限命令 / 路径历史列为高级版。[功能页][F1]、[版本说明][F6] |
| 命令参数 | 根据输入参数生成命令 | 基础项归属未知；参数历史属于同步内容。[功能页][F1]、[版本说明][F6] |
| 增强输入 | 主机目录 / 文件选择、历史字段选择、命令名称快速输入 | 专业版说明列出；不等于终端内的远端 Shell 历史。[版本说明][F6] |
| 性能与磁盘 | CPU、内存、网络和磁盘监控 | 功能页提供总览；2017 日志把硬盘监控记作专业版历史新增项，当前完整归属未证实。[功能页][F1]、[更新日志][F2] |
| 网络诊断 | Ping 延迟 / 丢包、Trace、多地点 Ping | 免费 / 收费未知。[功能页][F1] |
| 高级网络与进程 | 多网卡速率、进程端口 / 连接状态、进程详细信息 | 高级版；基础进程管理器归属未知。[版本说明][F6]、[功能页][F1] |
| 系统资料 | 查看服务器系统信息 | 专业版。[版本说明][F6] |
| 监控部署 | 官方称无需服务器插件，主要调用现有系统命令 | FAQ 提示权限、系统兼容性、命令缺失和 Exec Channel 均会影响结果。[功能页][F1]、[版本说明][F6]、[FAQ][F5] |
| 网络代理 | SSH 与远程桌面可使用代理 | 具体代理协议、认证范围未在所查页面明确；FAQ 提到跳板机场景。[功能页][F1]、[FAQ][F5] |
| SSH 隧道 | 本轮官方资料未明确列出本地 / 远程 / 动态三类 SSH 转发契约 | 待核对；不能以“支持代理”或自定义加速映射代替证明。[功能页][F1]、[SSH 教程][F3] |
| 打包传输 | 文件 / 文件夹打包，自动压缩与解压 | 高级版；这是文件传输功能，没有证明 SSH 协议压缩或 UDP 链路压缩。[版本说明][F6] |
| ZMODEM | rz / sz | 免费 / 收费未知。[功能页][F1] |
| 外观 | 平滑字体、多套配色；图片背景、模糊与文字阴影 | 本机图片背景列为专业版；其他项完整归属未知。[功能页][F1]、[版本说明][F6] |
| 快捷键 | 2024-08-13 日志说明命令输入框以单击 Ctrl 切换 | 没有据此确认全面自定义快捷键设置页。[更新日志][F2] |
| 配置与恢复 | 配置备份、连接删除恢复及删除信息同步 | 日志载明备份、删除后 30 天恢复；当前免费 / 收费划分未知。[更新日志][F2] |
| 云端同步 | 连接、代理、密钥及命令 / 路径 / 参数历史、快捷命令 | 独立付费服务，需先具备高级 / 专业版；页面描述本机加密后上传。[版本说明][F6] |
| 内置与自定义加速 | SSH / 远程桌面加速；外部客户端可接本地映射端口 | 功能页宣传免费海外远程桌面加速；SSH、节点 / 配额及双边部署权益未明确。[功能页][F1]、[SSH 教程][F3]、[远程桌面教程][F4] |
| 双边加速与节点切换 | 2017 日志新增服务器双边加速；2022 日志新增自动切换加速节点 | 历史存在性已证实；当前部署步骤、协议和授权归属待核对。[更新日志][F2] |

官方版本页当前列出高级版 35 元、专业版 128 元，均称永久；一年同步 49 元，另有组合价。这是页面所示报价，未核验结算价格或地区规则，且不能推出未列功能的免费 / 收费归属。[版本说明][F6]

平台限制要单独核对：FAQ 仍写 macOS 暂不支持远程桌面，而功能总页笼统列出跨平台和远程桌面；本轮不能把两段说明合成为“macOS 当前已支持 RDP”。[FAQ][F5]、[功能页][F1]

## 加速链路：已确认到什么程度

### FinalShell 官方资料能证明的入口

SSH 教程让用户在 FinalShell 中登记本地监听端口和目标 SSH 地址 / 端口，再让 XShell 等外部客户端连接回环地址上的该端口。远程桌面教程采用同样方式映射 RDP 端口。因此可以确认“第三方客户端 → 本地映射入口 → 加速服务 → 目标服务”这一使用模型；教程没有交代中间传输协议、双端握手、服务端安装或源代码。[SSH 教程][F3]、[远程桌面教程][F4]

这与更新日志中的双边加速条目共同证明竞品有该功能名称及历史入口，但**不足以证明它采用 UDP、采用某个 RUDP 算法，或内置原版 FinalSpeed**。这些关系列为未证实，不用名称相似、作者背景或界面相似补齐证据。[更新日志][F2]

### FinalSpeed 历史上游的最小证据链

| 项目 | 本轮直接证据 | 可以下的结论 / 不能扩张的结论 |
|---|---|---|
| 两端与配置 | 历史 README 区分客户端 / 服务端，客户端配置包含协议、服务地址 / 端口、上下行速率及本地端口映射；`FSServer` 创建服务端 Route | 需要对应两端及配置，协议选项包含 UDP / TCP；不能直接说所有模式只用 UDP。[README][U2R]、[FSServer][U7] |
| TCP 服务的出口 | `MapTunnelProcessor` 读取目的端口后连接服务端的 `127.0.0.1`，双向桥接数据流 | 所查原版出口是服务端本机 TCP 端口，可承载 SSH 等 TCP 服务；这不是 SSH 协议实现，也没有证明任意第三方目标地址转发。[MapTunnelProcessor][U6] |
| 可靠与有序传输 | `Receiver` 保存按序号收到的数据，确认数据包，按下一序号交付，并处理对端确认 | 有自定义可靠、有序数据流机制；“UDP 本身可靠”是错误表述。[Receiver][U4] |
| 重传 | `ResendManage` 按延迟与最小重发时间排队，检查仍待确认的数据并调用重发，受次数配置限制 | 存在独立重发调度；未做极端丢包、重排或公平性验证。[ResendManage][U5] |
| 速率与拥塞 | `ClientControl` 使用配置上下行速率、对端 Ping 速率信息和发送等待；`Receiver` 有发送窗口检查 | 可以确认速率节制与窗口控制。维护分支 README 将其描述为自有拥塞算法；本轮没有验证完整算法性质，不能宣称普遍优于 TCP。[ClientControl][U10]、[Receiver][U4]、[分支 README][B1] |
| 加密 | 上游 2017 README 对比 TCPSpeed 时明确说明 FinalSpeed 没有加密；2016 `sendPacket` 中的加密注释后直接转交发送 | 所查原版没有自己的传输加密；注释不是实现。此结论不评价内层 SSH 的加密，也不推定 FinalShell 商业实现。[历史 README][U3]、[ClientControl][U10] |
| 压缩 | `Pipe` 直接转交字节，`DataMessage` 复制负载并添加传输头 | 本轮已读路径没有压缩步骤，未证实 FinalSpeed 提供链路压缩；不得把加密、序列化或 UDP 封装叫作压缩。[Pipe][U8]、[DataMessage][U9] |

维护分支 `edwinyzh/finalspeed-2` 自述改为控制台、支持多实例及指定配置文件；示例可映射多个本地端口。它的 README 是该分支的一手设计说明，不能用来证明 FinalShell 的实现或原上游所有版本行为。[分支 README][B1]

## 未证实、未取得与后续核对

**未证实：** FinalShell = FinalSpeed；FinalShell 当前双边加速使用 UDP；完整 SSH 隧道类型；批量命令广播；SSH 协议压缩；可自定义所有快捷键；未列出的免费 / 付费边界。所查页面没有足够证据，不能写成支持或不支持。

**未取得：** FinalShell 当前商业客户端的实现源码、双边服务端安装说明与协议文档、逐平台功能矩阵和加速服务 SLA。本轮没有安装软件、登录账号或进入支付流程。原上游仓库并非无法访问；区别在于当前分支缺完整代码，但历史源码可取得。web 工具直接打开 GitHub API 曾失败，随后通过公开 API 成功核对 revision，未将访问失败当成无结果。

**待后续研究：** 如果选择实现或兼容 FinalSpeed，再单独核查许可证、协议兼容性、生命周期、安全前提和受控高延迟 / 丢包实测。本轮只建立需求与证据边界，不给性能或安全审计结论。

## 本项目对照待核对清单

本地现状由 [FEATURES.md](FEATURES.md) 维护，实现和演进约束由 [CORE-SPLIT-PLAN.md](CORE-SPLIT-PLAN.md) 维护。主代理应结合代码和统一验证入口填写映射，不依据本研究推断实现状态：

- [ ] 将竞品“已明确功能”逐组映射到已实现、部分实现、未实现或不引入；每项保留具体差异。
- [ ] 双边 UDP 加速单列：本机端、远端端、可靠传输、配置 / 状态、停用 / 退出 / 断线清理及测试证据。
- [ ] 直连 / 跳板路径握手比较与真正的有损链路加速分别验收，避免用前者代替后者。
- [ ] SSH 协议压缩、打包压缩传输、rz / sz 各自列项；不与 UDP 加速合并。
- [ ] RDP、批量执行、完整主题 / 快捷键、同步与付费策略保留独立决策；官方未证实项不升级为竞品事实。

[F1]: https://www.hostbuf.com/t/988.html
[F2]: https://www.hostbuf.com/t/989.html
[F3]: https://www.hostbuf.com/t/1076.html
[F4]: https://www.hostbuf.com/t/1077.html
[F5]: https://www.hostbuf.com/t/1088.html
[F6]: https://www.hostbuf.com/t/1081.html
[U1]: https://github.com/d1sm/finalspeed/tree/f59274d607f33a581b19466bbb5b7c1eb3031458
[U2]: https://github.com/d1sm/finalspeed/tree/b819a35a48e6569e224198a9162ae3bb2fa8174a
[U2R]: https://github.com/d1sm/finalspeed/blob/b819a35a48e6569e224198a9162ae3bb2fa8174a/README.md
[U3]: https://github.com/d1sm/finalspeed/blob/26b4fe14df4a67ccd95ee8a3b7b00059e8a2285e/README.md
[U4]: https://github.com/d1sm/finalspeed/blob/b819a35a48e6569e224198a9162ae3bb2fa8174a/src/net/fs/rudp/Receiver.java#L59-L169
[U5]: https://github.com/d1sm/finalspeed/blob/b819a35a48e6569e224198a9162ae3bb2fa8174a/src/net/fs/rudp/ResendManage.java#L22-L66
[U6]: https://github.com/d1sm/finalspeed/blob/b819a35a48e6569e224198a9162ae3bb2fa8174a/src/net/fs/server/MapTunnelProcessor.java#L49-L105
[U7]: https://github.com/d1sm/finalspeed/blob/b819a35a48e6569e224198a9162ae3bb2fa8174a/src/net/fs/server/FSServer.java#L48-L87
[U8]: https://github.com/d1sm/finalspeed/blob/b819a35a48e6569e224198a9162ae3bb2fa8174a/src/net/fs/client/Pipe.java#L36-L91
[U9]: https://github.com/d1sm/finalspeed/blob/b819a35a48e6569e224198a9162ae3bb2fa8174a/src/net/fs/rudp/message/DataMessage.java#L28-L74
[U10]: https://github.com/d1sm/finalspeed/blob/b819a35a48e6569e224198a9162ae3bb2fa8174a/src/net/fs/rudp/ClientControl.java#L90-L275
[B1]: https://github.com/edwinyzh/finalspeed-2/blob/071eaa83e39270aeec09ad6c5045eacfd756d99a/README.md
