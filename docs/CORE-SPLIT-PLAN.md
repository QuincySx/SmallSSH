# SmallSSH 0.1：B 实施计划与 D 演进约束

状态：实施中。用户选择 B（本机独立后台），明确要求保留当前 App 的所有功能，并为 D（远程访问网关 / 跳板机）保留演进路径。D 的远程入口不在本轮启用。本文由主代理维护；用户选定的可用 delegate worker 按单项任务实现，主代理逐项审查和验收。

完整需求及当前待确认范围见 [REQUIREMENTS.md](REQUIREMENTS.md)。本文是实施计划，不把写入计划的功能记为已完成；具体编码使用用户选定的可用 delegate 节点，不固定为某个模型。

## 验收目标

- GUI 的窗口 / 进程与安全核心的生命周期分离；关闭工作台不停止其他仍存活调用方的有效授权。
- 冷启动、后台重启默认锁定；锁定与停止后台是明确不同操作。锁屏 / 休眠默认撤销权限并停止相关访问。
- 调用方退出、断线、进程实例变化后原授权不可继承。后台存活不等于授权永续。
- GUI 保留终端、文件及递归下载、代理 / 中转 / 路径测量、隧道、硬件密钥、服务器及 Docker / Compose 管理、命令参数与独立历史、备注、权限审批、审计和布局 / 语言设置。
- 凭据、策略、受控执行及审计由安全核心负责；客户端只取得必要的公开数据和限定操作能力。用户录入的秘密短暂处理后清除，不从后台回读 SSH 私钥或密码。
- 待审批请求有明确通知、期限与上限；无审批入口时不自动批准。拒绝、撤销、重启和未知任务结果不自动重放写操作。
- 所有改动通过统一 tools 入口验证；最终重新打包、校验并更新 `/Applications/SmallSSH.app`，不自动启动、前置或退出当前应用。

## 模块职责

| 模块 | 职责 | 禁止依赖 / 行为 |
|---|---|---|
| protocol | 现有通讯契约、结构化请求 / 响应及兼容规则 | 不依赖 broker、GUI、具体 IPC；不授予权限 |
| 身份与认证适配 | 把可验证的本机进程或将来的远程主体转换为可信身份；处理明确的所有者验证 | 不以自报标签、PID 数字、路径或“我是 GUI”放行 |
| broker / 安全核心 | 保险库、权限决策、逐次验证、SSH / SFTP / 隧道执行、审计 | 不依赖窗口、菜单栏和 UI 生命周期；不导出登录秘密 |
| service / 后台宿主 | 单实例、锁定 / 解锁 / 停止、客户端会话与后台任务、健康和版本协商 | 不用磁盘票据恢复客户端授权；不开放网络监听 |
| client SDK | 已认证的管理会话、请求 / 响应和流、取消及版本兼容 | 不读取核心私有存储；不自行执行 SSH 绕过核心 |
| desktop / 审批入口 | 工作台渲染、用户输入、明确审批、菜单栏状态与恢复窗口 | 不长期持有 Vault.Session，不拥有核心退出权的隐式生命周期 |

现有 `broker.Message` 先以类型别名兼容，避免一次迁移全部调用者。协议抽取不是新增 RPC，也不代表旧本机认证方式可以直接用于远程。

## 顺序任务与检查点

每项输入包含文件所有权、前置结果、不可破坏约束和验收条件；worker 完成后停止，等待主代理检查再下发下一项。禁止擅自扩展任务、改依赖、提交、删除旧功能或启动用户应用。

1. **B01 通讯契约抽取**：只移动 Message 及必要契约到 protocol，保留 broker.Message 别名；不改变 JSON、操作、认证和生命周期。验证契约往返及现有编译。
2. **B02 核心访问接口**：集中当前 GUI 读取 / 写入保险库的功能；建立覆盖公开配置、备注、命令及参数历史的受验证管理接口，不提供通用导出或签名预言机。
3. **B03 后台生命周期与可信启动**：独立宿主、启动 / 状态 / 锁定 / 解锁 / 停止、单实例、协议协商；锁定状态下也必须先认证服务端身份，不能把主密码送给未知端点。
4. **B04 所有者管理会话**：短期、进程实例与实际活连接绑定，认证证据覆盖一次性挑战和当前请求；后台自行验证，不信任 GUI 返回的布尔结果。
5. **B05 App 接入**：GUI 改用 SDK、移除生产路径中的核心实例与 Vault.Session 持有；保留所有表单、凭据保留语义、策略确认和新旧请求代次检查。
6. **B06 任务与断线语义**：明确客户端所有任务与经过批准的后台任务；迁移文件传输等可后台化任务。终端连接不得因 GUI 重启隐式恢复；远端结果不明时重新查询。
7. **B07 常驻入口与系统集成**：菜单栏状态 / 待审批 / 打开工作台 / 锁定 / 停止后台；macOS 用户级后台注册及禁用检测。注册不自动解锁，不使用 root 或新增 TCP 入口。
8. **B08 全量验收与 App 交付**：断线撤权、并发审批、锁屏 / 休眠、后台崩溃重启、GUI 重连、任务恢复、旧功能回归、离线截图、五目标编译、打包和本机安装。

任务可按实际依赖进一步拆小，但不得跳过检查点。不能把只有协议抽取或只有托盘隐藏称为 B 已完成。

## D 的演进路线

- 保持协议、授权决策、受控执行、审计独立于本机传输与窗口。B 的本机 peer 验证属于适配，不是远程身份方案。
- D 再增加远程传输、用户 / 设备认证、主体与资源归属、多用户隔离、证书 / 服务身份、速率与网络攻击防护、远程审批可用性。
- 服务端身份、协议协商、操作目标快照、取消、任务状态和审计关联在 B 就确定；不提前部署 HTTP/SSH 入站或默认监听公网。
- 原有进程 / 活连接约束在本机继续有效；远程会话需要独立设计等效的设备、主体、连接与期限约束，不能声称仅改地址就完成迁移。
- 后台任务身份与交互调用方身份分开，迁往 D 后也不能把断开的客户端授权保存在任务文件里继续复用。
- 当前同 OS 用户任意调试 / 注入仍是既有威胁模型限制，拆进程本身不宣称解决此问题。

## 证据

每项 worker 输入与过滤后的结果保留在 `build/worker-tasks/`、`build/logs/`；逐项结果和最终验收统一追加到本轮 `docs/VERIFICATION.md` 记录中。

### B01 验收

- 已由外部 DeepSeek 完成协议结构移动，保留 `broker.Message` 别名。原始源码与新结构逐字比较一致，记录 `build/logs/b01-contract-review.json`。
- 协议竞态门禁通过：`build/logs/check-20261105-183139-race.log`。初次统一编译发现遗留 monitor import，退回 B01 修正后五目标编译通过：`build/logs/compile-20261105-183459.log`。
- B02 分成 B02a 只读工作区接口、B02b1 存储审计锚点修复、B02b2 写入接口，先保持 GUI 原行为，等新接口验收后再迁移 GUI。

### B03 实施前的信任约束

- 冷启动锁定时核心尚无保险库身份，不能借此关闭服务端验证，或从待验证端点学习信任后立即发送解锁材料。
- 后台控制通道使用内存中的临时 TLS 身份。客户端应通过操作系统取得此连接实际服务端的进程实例与程序身份，与可信安装位置的后台可执行文件核对；握手前后验证，后续请求也继续验证。不把后台私钥明文持久化给同用户其他进程读取。
- 程序身份只确认对端运行预期代码，不授予管理权限。所有者仍须明确验证；管理会话另外绑定真实活连接、进程实例、期限与后台代次。重启后台或重启 GUI 均不能继承旧会话。
- macOS 的静态代码身份核对依据 [Apple Code Signing Services](https://developer.apple.com/documentation/security/code-signing-services)，Windows 的服务端管道身份需使用 [GetNamedPipeServerProcessId](https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-getnamedpipeserverprocessid)，不能误用仅适用于客户端的 PID 查询。
- 这些是本机传输的信任条件；D 另行提供远程服务身份和用户 / 设备认证，不复用本机进程 ID。

### B02 验收

- 工作区读取与写入只提供命令、服务器备注及参数上下文历史的明确操作，证明覆盖全部载荷。新可选字段省略时保留旧签名字节；命令列表分页限制字节、数量与游标，列表不含参数历史。
- 普通保险库写入已修复保留审计锚点，工作区写入记录开始与成功 / 失败，只含固定元数据，不记录正文、备注或输入值。
- 统一格式 / vet / 定向竞态门禁：`build/logs/check-20261105-191156-race.log`；保险库全套竞态：`build/logs/check-20261105-185434-race.log`。
- 本阶段没有迁移 GUI，也没有独立运行核心。下一项 B03a 是服务端程序身份认证传输，B03b 才增加冷启动锁定的独立宿主与版本协商。

### B04 管理会话的权限边界

- GUI 解锁时只短暂处理所有者认证材料。后台验证后，绑定当前控制连接、真实进程实例、随机代次与限期的内存签名公钥；GUI 保留短期签名器，不长期持有保险库解密会话。
- 普通工作区读取 / 写入和明确的 GUI 自有操作使用管理会话证明，后台仍验证完整请求、一次性挑战与当前进程。控制断开 / 到期 / 锁定后撤销该 GUI 的操作，其他独立调用方不因 GUI 关闭而获得或失去权限。
- 管理会话不能批准其他进程的 AI 请求、授予自动化会话或生成根证明。外部请求仍需现有的逐次明确审批与新鲜所有者认证。GUI 自有操作必须由核心已记录的 OwnerIssued 请求关联，不能信任传来的布尔值。
- 经过明确批准的后台任务另外绑定后台任务身份与固定范围；GUI 重连只查询状态，不恢复旧 GUI 权限，不自动重放可能已完成的写操作。
- B04 再分成内存会话签名 / 验证、核心验证接入、锁定宿主的认证交接三项，分别验证；不把三项合成一个没有检查点的大任务。
- 核心接入再细分 SDK 的 OwnerProof 接口准备和 broker 的能力验证接入；前一项仅消除具体解密存储类型耦合，不提前接受新的证明类型。

### B03a 验收

- 本机控制传输已增加可信可执行文件身份核对、连接实际服务端身份提取及 TLS 握手前后 / 后续收发校验。没有写服务私钥或从未知端点学习信任，程序身份不授予所有者权限。
- 本机完整 IPC 格式 / vet / 竞态门禁通过：`build/logs/check-20261105-193806-race.log`；五目标生产代码编译通过：`build/logs/compile-20261105-193305.log`。其他操作系统仍需各自运行验收。
- 修正原生指针转换及对照夹具连接存活顺序后才验收；文件所有权记录：`build/logs/b03a-files-review.json`。下一项 B03b 只增加锁定宿主，不启用尚未验证的解锁或管理接口。

### App 接入时的回归清单

- 新后台入口在常规参数解析前保留 hardware.MaybeWorker 的 PKCS#11 隔离子进程分支，不能只迁移普通 SSH。
- 打包时将后台可执行文件放入现有平台资源目录，并核对其安装身份；开发路径 / 测试替身不能成为生产环境的任意后台路径覆盖。
- 解锁认证与 UI 接受结果的交接继续可取消，未被 UI 接受的旧结果不得发布后台权限；重连丢弃旧代次回调。
- GUI 自有终端 / 监控 / 隧道取消只影响其会话，不能清空其他调用方仍然有效的授权。后台文件任务需要另行明确的范围和生命周期，不能保留已退出 GUI 的身份继续执行。

### B03b 验收

- smallsshd 已可独立以锁定状态运行，提供有限 hello / status、版本协商、随机启动代次、单实例与完整退出，入口保留 PKCS#11 worker；未开放解锁或管理权限。
- 初轮协议 / SDK / 宿主定向门禁 `build/logs/check-20261105-195249-race.log` 通过后，主代理另要求意外 listener 关闭的故障收敛回归；修正门禁 `build/logs/check-20261105-195749-race.log`、五目标编译 `build/logs/compile-20261105-195749.log` 均通过。当前生产 App 尚未迁移到此宿主。

### B04a 验收

- 内存会话签名 / 真实进程实例 / 活控制ctx / 代次 / 绝对和闲置期限已由 ownercap 模块集中处理。私钥不落盘，公开会话信息不授予权限；核心仍需逐次消费自己的请求挑战。
- 格式 / vet / 全套竞态门禁 `build/logs/check-20261105-201717-race.log` 与五目标编译 `build/logs/compile-20261105-202017.log` 通过。它尚未接入 broker，后续先把 SDK 改为 OwnerProof，再接入核心验证和可信宿主认证交接。

### B04b1 验收

- SDK / transfer 已使用只要求Proof的接口；保存连接的旧凭据合并保留在核心，SDK验证公开目标与明确的新私钥。保留旧Session调用、pin来源、typed-nil公开路径及所有者不自动回退语义。
- 客户端全套门禁 `build/logs/check-20261105-203523-race.log`、传输全套门禁 `build/logs/check-20261105-204025-race.log`、五目标编译 `build/logs/compile-20261105-203453.log` 通过。修改范围记录在 `build/logs/b04b1-files-review.json`，尚未改变后端接受的证明类型。

### B04c / B05 后续交接约束

- 后台认证先prepare，再由GUI接受本次有效结果后commit；取消、旧代次、过期的prepare不能发布解锁权限。成功交出的控制会话与短认证ctx分开，仍由实际控制连接和核心OwnerLease决定存活。
- App迁移按数据存取、认证材料、后端会话、启动定位与生命周期分项进行；生产GUI不长期持有Vault.Session或核心实例。测试用真实合成认证材料和可信适配，不引入“认证成功”布尔假接口。
- 区分清理工作台与全局锁定：关闭GUI只取消其自有操作；明确锁定/停止及可信OS锁屏/休眠事件才全局收敛。会话丢失的UI回调不能再次触发全局锁定而误伤其他调用方。
- 继续保留窗口先出现、再异步读取/连接的行为，以及现有回调代次、草稿保护、参数上下文历史和错误导航。
- 数据迁移先覆盖commands / commands_history / server_notes及连接编辑、文件面板中的对应存取，全部使用已验收工作区RPC；RPC等待不阻塞主线程，原缓存与回调代次检查继续保留。
- 认证适配应尽量只短暂持有签名/解锁材料，避免为了计算证明而在GUI长期加载整个凭据目录；完整Session只属于核心和合成测试夹具。
- App后端接口须能由真实service会话与测试专用可信适配实现；生产路径不得保留启动内嵌broker的自动降级。旧测试直接依赖a.server的地方迁到测试夹具，不能删旧测试来绕过迁移。
- 可信后台定位/启动单独验收，固定包内程序路径和身份；缺失、版本不兼容或身份不符时保持锁定并报错，不向未知进程发送认证材料，也不自动前置窗口。

### B04c 的逐项实施顺序

- B04c1：仅共享认证交接数据与确定性签名字节；不开放宿主权限。
- B04c2：宿主prepare与一次性挑战；正确认证只产生有期限的pending，状态仍锁定，断开清除。
- B04c3：同一活连接commit、OwnerLease和显式lock/stop；核心由宿主持有，关闭一个管理会话不能全局关核心。
- B04c4：SDK的PreparedOwner / OwnerSession与取消交接；成功交出的长期会话不再依赖短认证ctx。

每项先审查和门禁，再下发下一项。原`build/worker-tasks/b04c-authenticated-handoff.md`保留为总体约束，不能作为一次性大编码任务直接下发。后续App接入、后台任务与发行仍分别验收。

### B04b2 验收

- 核心接受进程/活控制ctx/代次/期限绑定的管理签名，统一复用原完整载荷证明；根HMAC字节保持兼容。管理会话只能审批自己已记录的OwnerIssued非grant操作，不能审批外部AI或其他管理会话。
- 四组集成测试包括真实OS身份、篡改/重放拒绝、精确自有操作、隔离撤销、独立请求存活、跨会话和实际子进程复制证明拒绝。完整broker竞态门禁`build/logs/check-20261105-214136-race.log`通过；随后收紧ownerRequest撤销提交边界，定向竞态`build/logs/check-20261105-214618-race.log`、五目标编译`build/logs/compile-20261105-214630.log`通过。最终全仓门禁尚待整合后执行。
- 新OwnerLease只含公开信息与Done信号，由可信宿主注册，没有wire注册或根签名导出接口。关闭一个管理会话仅取消自己的通道，不全局Lock或清空其他授权。

### 管理控制操作的时效约束

- 控制连接的心跳只证明连接仍可达，不刷新管理权限的闲置或绝对期限。
- 显式lock/stop必须再次验证该OwnerLease的管理签名及实际控制身份，载荷绑定操作名、宿主代次、状态generation、实际TLS binding和管理会话ID；不能仅检查Done尚未被定时清理器关闭。
- 宿主只能通过可信Go接口请求核心验证自己的OwnerLease，公开ID或客户端角色字段不能选择lease。证明不能转译成返回客户端的根HMAC。
- 全局lock需使所有pending认证立即失效并清除临时解密Session；不能只关已commit的core，而把prepared解密材料留到30秒超时。
- lock/stop请求不自动重试；响应丢失表示结果未知。锁定时保留宿主监听，停止在确认已接受后收敛进程，不能在handler内等待包含自身的WG。

### commit 的保险库新鲜度与 CLI 兼容

- prepare只持临时认证Session，不占用原broker目录独占锁；期间已有CLI离线编辑可能合法更新vault。commit不能直接把30秒前解密的旧Session作为核心存储，否则后续审计封存可能覆盖中间的新配置。
- 锁定到解锁的commit须由核心先取得原目录独占锁，再用短期key重新读取最新vault，核对prepare时已经认证的稳定broker公钥pin，验证成功后才开放监听。prepare临时Session随后关闭、key副本清除。这样同时拒绝保险库被更换/改密后的陈旧认证，不用把目录锁占在等待UI接受的阶段。
- 此新入口由broker封装锁、重新读取、pin核对与完整失败清理；Host不在锁外读取后再调用旧入口，也不重试未知结果。原OpenSession接口保留给兼容/合成测试，生产Host只用严格入口。
- CLI的agent与离线addHost路径也要纳入B的交接规划：管理读写使用已验证宿主和短期认证材料，不能因GUI迁移就遗漏CLI。已有CLI接口的生命周期语义需明确更新提示并提供显式lock/stop；不再让生产CLI随意绕过独立宿主另开broker。

### 加速传输的增量约束（用户补充FinalSpeed）

- 用户指出FinalSpeed方向。本轮已取得[原上游历史源码](https://github.com/d1sm/finalspeed/tree/b819a35a48e6569e224198a9162ae3bb2fa8174a)，确认数据序号、确认、顺序交付、重传与双端配置；当前master只剩工程元文件，不能当作完整实现。作者后续README明确原版没有自己的传输加密，所读字节路径未证实压缩。[FinalShell官方教程](https://www.hostbuf.com/t/1076.html)说明本地端口映射接入其加速服务，不能据此认定其内部协议必为FinalSpeed。完整来源与限制见[参考研究](FINALSHELL-REFERENCE.md)，本轮不是协议完整性或性能审计。
- 现有代理/SSH中转/路由测量与未来双端可靠UDP传输分开表述，不宣称已实现FinalSpeed或自动优化公网路由。传输效率需经丢包/延迟/实际吞吐对照验证，不能保证总会更快。
- 用户已明确要求双端 UDP 加速；当前仍未实现，必须作为独立交付项推进，而非可选的未来设想。实现作为核心网络传输能力单独实施与验收，提供有序可靠字节流供SSH使用；继续端到端校验SSH主机指纹与目标范围，不把加速中转身份当成目标授权。需要配套服务端，不能仅靠本机GUI开关宣称生效。B的权限/审计边界保留，D网关演进另行设计，不默认开放UDP/TCP公网监听。

### UDP 加速的独立交付清单（尚未实施）

此清单记录用户已经提出但尚未交付的范围，不表示已有实现或性能结论。B 是本机独立安全核心，D 是将来的远程访问网关，UDP 加速另有客户端与网络对端，三者分别验收。

1. **协议与实现选型**：核对 FinalSpeed 上游 / 可维护实现的协议、许可、认证和跨平台条件；明确是兼容现有 FinalSpeed，还是独立的可靠 UDP 传输，禁止混称。只读研究和方案不能记为编码完成。
2. **加速客户端与配套服务端**：实现可取消、有序可靠的传输及明确认证 / 对端固定；先只在隔离回环夹具运行。部署到用户服务器、开放公网监听或改防火墙须另有明确目标与授权，不在研究阶段自动执行。
3. **核心 SSH / SFTP 接入**：在现有 `sshconn.dial` 的底层连接创建处接入，继续由 SSH 做目标主机指纹验证和原授权校验，终端 / 文件传输共用能力；保留直连、HTTP / SOCKS5 与 SSH 中转路径，不绕过 broker。
4. **连接配置与运行反馈**：提供真实的直连 / UDP 加速选择、对端配置、连接中 / 失败 / 取消状态与可解释回退策略。回退须明确配置，不能在失败时悄悄更换路径或重放操作；没有配套服务端时明确未配置。
5. **性能及安全验收**：对照直连与加速，在固定延迟、抖动、不同丢包率和限速下测试终端交互、SFTP 吞吐、重传开销、CPU / 内存，以及取消 / 锁定 / 到期 / 撤权。UDP 不通、不更快或更慢时如实报告，不能把一次握手耗时当吞吐提升。

上述五项目前均未验收；产品中现有“加速与代理”标题不代表它们已经存在。

### App/宿主整合时必须区分的生命周期

- 工作台断开与核心锁定分开显示和实现：管理会话到期或GUI退出只撤销自己的权限，不能让UI把仍在服务其他调用方的核心描述成已经全局锁定。显式lock的确认须等核心关闭和权限收敛后返回。
- 迁移现有闲置锁定策略，不能因权限过期导致UI只断开、核心却绕过原有全局锁定策略；普通授权活动的idle与用户闲置策略需分别明确，心跳不能延长授权。OS锁屏/休眠策略最终由无GUI宿主接管。
- 整合code review要检查数据监听/核心意外停止的收敛与宿主状态，避免留下已停止Core却显示unlocked的指针。重新认证前须等待原核心目录锁释放，不自动降级到内嵌broker。
- GUI现有传输任务的暂停/续传行为必须保留；只有另行明确接受为后台作业并由核心持有固定范围的任务，才允许GUI退出后继续。不能把GUI签名器写入任务文件充当后台身份。

### B05 源码盘点后的交付组

按现有调用点迁移，每组由外部worker完成后停止，主代理验收再继续。先迁移数据，再切换认证与生命周期，避免一次重写终端、文件和Docker功能。

1. `filebrowser.go` / `commands.go`：命令列表、保存及重读改用已有工作区RPC，等待不阻塞UI，保留generation检查。
2. `commands_history.go`及运行参数触发点：参数历史读取、保存、清空改RPC，保留FIFO写入顺序及runState/target/system/generation和用户输入保护。
3. `server_notes.go` / `layout.go` / `connections.go`：备注全部改RPC；编辑连接的异步备注不得覆盖已修改草稿，连接保存成功但备注失败仍按现有分步语义报告。
4. vault / systemauth：增加只含`Proof/UnlockKey/Close`的短期Root认证材料；完整Session继续属于核心和合成夹具。首次创建集中到持锁、禁止覆盖的初始化入口。
5. `approval_auth.go` / `approval_center.go` / `app.decide` / SDK离线owner助手：外部approve、deny、revoke均使用一次fresh Root证明并立即释放。普通OwnerSession不能悄悄扩大到外部审批权限；现有deny/revoke复用常驻Root，必须显式迁移。
6. tools打包、固定资源定位与启动：暂存`smallsshd`，核对固定程序身份；启动不依赖短认证ctx，不自动前置窗口，不提供生产任意路径覆盖或内嵌broker回退。
7. App会话切换：`app.go` / `unlock_auth.go` / `connections.go` / `workspace.go`及生命周期调用处，使用prepare→UI接受→commit→OwnerSession；移除生产`app.server`和`atomic.Pointer[vault.Session]`。测试adapter仅存在测试文件内，使用真实合成认证，不删除旧测试。
8. CLI单独迁移：`agent`接后台；`host add`改SaveConnection；`host options`用公开列表构造变更并保留凭据保留语义；短管理命令用fresh Root来源。普通Request/Wait/Start/session仍保持外部申请方语义，增加明确lock/stop并更新退出提示。
9. 本机设备认证设置与初始化专项验收：Enroll/Disable使用轻量材料，不塞入普通工作区RPC，不读回登录秘密。

退出、会话到期及旧回调只detach本GUI；显式立即锁定和已有OS/idle策略才发全局Lock。发Lock之前不能先关闭自己的控制会话。GUI退出后的锁屏/休眠与闲置覆盖仍由B07无GUI宿主接管，不把B05的运行中覆盖说成后台已完整实现。

### B07 已确认的原生接入缺口

现有`desktop/app.go`在MyGo.WhenReady内注册Power.OnLockScreen/OnSuspend；`desktop/idle.go`由GUI首帧启动15分钟阈值/10秒采样。`lockWhenIdle`的双条件策略可抽出，但MyGo.Power监听和IdleTime都依赖App主线程。`cmd/smallsshd`目前只有信号与service.Run，不具有这些保护。

后台需拥有独立OS事件适配器和idle采样，可信事件调用Host.Lock，注销/取消由宿主生命周期管理。解锁时间由当前核心产生，心跳和GUI是否在线不能重置用户idle。Darwin原生通知与事件闲置API可参考固定vendor实现，但不能导入MyGo internal包或为接事件启动完整工作台；其他系统运行保证须分别验收。

当前尚无生产NewTray调用，也无LaunchAgent/SMAppService注册。常驻状态栏入口可独立于普通窗口；关闭入口不能停止安全策略。注册仅用户级、可禁用、默认锁定，开发验证不弹系统设置、不前置窗口。完整后台安全策略与可见入口均待实施，不能把隐藏GUI说成独立宿主完成。

### B06 源码盘点与最小真实后台任务

现有transfer.Manager在调用方进程内执行，Job/manifest/进度可落盘，但没有票据、证明或恢复授权；active/live仅在内存。GUI RunWithOwner使用operationContext，退出还会PauseAll。即使改用Background context，OwnerSession.Close仍撤销相应传输channel，所以不能仅搬goroutine或保留GUI signer。transfersDetached只是面板窗口位置。

现有单文件大小/前缀与完整hash、0600 partial、Sync、无覆盖hard-link提交、断点恢复应原样复用。目录还有10000文件/2048目录/4MiB manifest上限、路径/大小写/深度/特殊文件校验、任务标记及os.Root保护；manifest不是内容快照或授权来源。顶层单文件目前主要用pathname检查，后台落地前要明确本地目标锚点。目录上传原本不支持，不因拆分伪造支持。

依赖目前为transfer→client→broker→vault，另transfer只用vault.ShortID。因此service可导入transfer，broker不可直接导入现有Manager；最小中性执行seam应不依赖client/broker/OwnerProof，由客户端和核心各自提供适配。

1. 先抽出单文件下载执行接口，保留现有前台API、检查点、路径/hash/partial及无覆盖提交规则，前台目录下载算法继续工作。
2. 首个真实后台能力为“创建时明确交给Host的单文件下载”：冻结服务器身份、远程路径、本地目标、大小/范围及期限；Host持有独立受限任务身份与ctx。GUI只查状态/暂停，退出不撤销；全局Lock/Stop、到期或核心退出必须停止。重启只恢复展示/续传材料，不恢复权限或自动运行。
3. UI复用Job/进度/错误/暂停展示，明确GUI自有和后台任务，退出只暂停自身任务。后台上传、后台目录下载、运行中途转交另设检查点，不把仍随GUI暂停的任务标作已后台化。现有前台上传、目录下载与续传均保留。

后台身份必须由核心实际记录并验证固定范围，不能仅以Job.Background字段授予权限，也不能在任务存储中保留GUI signer、OwnerSession或Root证明。D演进继续以任务主体/资源范围为边界，不复用远程PID或客户端自报标签。

### B05 轻量Root材料的源码设计核对

在GCM认证成功、payload对象化之前分流。完整Session继续解析profiles/snippets/notes；新增RootMaterial只持mutex、自有key及已认证Header/ID，提供Proof/UnlockKey/Close与所需公开pin，不提供Session访问器、BrokerIdentity或凭据目录操作。当前格式仍需短暂产生整块GCM明文；本项避免目录对象化和长期持有，不能宣称明文从未进入内存。

建议分成共享私有helper、材料实现、设备认证接入三个外部任务：vault.go集中受限文件读取、envelope校验/GCM认证；root_material.go实现短期材料；broker_identity.go共享派生；systemauth/auth.go新增OpenRoot/OpenRootEmbedded并保留原Session入口。先用旧测试和golden证明原Session/证明/pin字节不变，再测试新材料和设备流程。不改平台native backend或SDK/GUI同一批文件。

必须保留的现有契约：

- Read/ReadKey对同一open句柄Stat，普通文件、32MiB上限、Unix无group/other权限、max+1读取限制；两入口原权限错误映射不同，不静默合并。
- Argon2id固定参数与Version/16字节Salt和ID/12字节Nonce/32字节key校验，GCM AAD仍为json.Marshal(Header)，认证成功后才信任Header。
- Root证明两级HMAC的第一层域为`ssh-client/owner-approval/v1`加Header.ID，没有尾随NUL；中间key清理。broker身份域为`smallssh/broker-identity/ed25519/v1\x00`加ID；公开pin为Ed25519公钥PKIX/SPKI DER的SHA256，不能改为原始公钥hash。
- Root分支必须用json.Valid拒绝坏JSON，但不等同完整payload类型校验；不能先OpenKey再丢目录冒充轻量分支。只wipe本模块拥有的key/派生缓冲，不改调用方输入，UnlockKey返回调用方负责wipe的副本，Close只擦自身key，Close后秘密操作失败。
- systemauth共享私有key-consumer流程必须完整覆盖readRecord→两分钟ctx→OS Unwrap→ctx检查→VaultHash匹配→Vault认证→再次ctx检查→返回。临时key由helper清理，最后取消时关闭刚构造结果；不能unwrap后提前cancel而丢掉后半段覆盖。
- embedded保留fallback、取消和readiness错误类别；Enroll保留Wrap后取消清理新条目、先替换record再清旧条目；Disable认证后立即关闭材料，再按原15秒期限清记录/native条目。新材料不缓存，不引入Authenticated bool。

### B05 固定daemon资源与启动源码核对

现有compile默认`go build ./...`已产出smallsshd，打包stageCLI尚未暂存它。新增stageDaemon沿用vendor/trimpath/目标平台与CGO=0参数，放资源根的smallsshd[.exe]，packageApp前完成；不加入native-hashes。install整包复制并验签，替换前额外检查固定daemon文件存在且可执行。

MyGo资源布局与入口对应唯一固定相对路径：Mac主程序MacOS→../Resources/smallsshd；包内Resources/cli/smallssh→../smallsshd；legacy Resources/ssh-client→同目录；Linux/Windows主程序或legacy→同目录，cli目录入口→上一级。compile/<target>/ssh-client同目录daemon。生产构造器从真实os.Executable推导一个绝对路径，用同一路径启动和ServiceClient.Executable验证；不搜PATH/cwd、不接受生产环境任意覆盖、不降级内嵌broker。

EnsureRunning先可信Status，仅确实无端点/连接拒绝才启动；身份/协议/权限/stopping错误直接报告。并发启动由Host单实例锁收敛，readiness仍通过ConnectExecutable与hello/status验证。exec.Command不绑定短认证ctx，Unix独立session/进程组，Windows独立进程组并隐藏console；不连接GUI标准流管道，安排Wait回收。认证取消/GUI退出只取消有界就绪等待，不杀已启动Host。只传data-dir，cold start仍locked，不传key/pass/proof。实际独立进程行为要用合成目录和工具编译的smallsshd做headless验收。

### 2026-11-06：用户闲置策略与应用驻留已交付，独立核心仍待接入

设置新增独立的保险库闲置锁定、新CLI会话默认闲置断开和“运行与后台”分类，开关与时长保存于非秘密behavior偏好。默认保持15分钟；用户可关闭。新grant由用户默认/显式选择决定最终签名terms，续期从核心保存的上次已批准scope初始化，同ID+Nonce公开选择跨视图保持，旧认证与其他nonce不继承；关闭提前idle期限使用IdleSeconds==LifetimeSeconds，绝对期限与断线/锁定撤权仍有效。上述用户闲置开关不等于取消ownercap管理连接心跳失联保护。

当前真实可选后台方式为off与application（GUI进程驻留），配有Dock/菜单栏入口。隐藏时请求静默留队列，用户恢复后才提示；Cmd-Q真实退出。它不代表B独立后台完成，设置明确标注独立安全后台尚未接入。独立宿主仍需后续认证/GUI/CLI迁移、独立OS保护与任务生命周期验收，不能保存GUI signer替代后台任务身份，D远程入口继续不开放。

本轮完整证据与安装/截图路径见docs/VERIFICATION.md的“闲置策略由用户控制、后台驻留与图标设置”段。全仓门禁check-20261106-143501-race.log通过，修复版已静默安装到Applications；未替用户重启当前App或改真实偏好。
