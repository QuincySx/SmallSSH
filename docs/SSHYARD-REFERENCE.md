---
repo: Licoy/sshyard
generated: 2026-11-06
type: implementation-study
source_ref: main
resolved_sha: 93c596b46efba3e3939f06fbff4c179e7241755b
accessed_at: 2026-11-06T10:14:44Z
scope: 来源功能、关键机制与边界；不含本地产品实现映射
---

# SSHYard 功能与机制参考

本轮研究对象是 [Licoy/sshyard][repo]，不是 FinalShell 或 FinalSpeed。本文供需求梳理与对齐使用，不代表已决定实现其中任何能力。

## 结论

- SSHYard 的公开功能主线是终端、文件、主机与凭据、AI 助手、外部 Agent 接入、备份恢复、网络连接和日常设置。README 的主要能力有对应用户入口；关键机制已按下文追到源码，平台兼容性与真实运行效果未做本机验证。[README][readme]、[主机菜单][sidebar]、[工具栏][rail]、[设置导航][settingsview]
- AI 助手绑定主机和面板；内置 Provider 与本地 ACP Agent 是两种后端。多个配置和多个面板可以同时活动，但这不等于同一轮任务自动调度多个 Agent。每个标签页最多一个 AI 面板。[助手生命周期][ai]、[ACP 生命周期][aiacp]、[面板数量约束][single]
- 内置/ACP 助手的 Ask、Review、Full，与开放 API/MCP/Skill 的授权边界不同。开放接口启用后，run_command 直接进入远端执行与审计，不经过助手的逐动作门禁。不能把助手权限模式的保证套用到开放接口。[助手门禁][gate]、[开放接口执行][mcpapp]
- 已核对的 SSH/SFTP 链路使用 TCP SSH、跳板、HTTP/SOCKS 代理和端口转发；没有证据支持将它列为真正的 UDP SSH/SFTP 加速方案。动态 SOCKS5 还明确只支持 CONNECT。[SSH 建连][dial]、[代理客户端][proxyclient]、[SOCKS5 服务端][proxyserver]、[转发][forward]、[SFTP][sftpsvc]

## 研究问题与证据口径

只回答三个问题：①当前功能分组与实际用户入口；②AI、开放接入、备份/导入/密钥的最小机制与边界；③网络能力是否包含真正 UDP 加速。

研究开始时解析 main 到上述 40 字符提交；以下决策性源码链接均固定到该 SHA。仓库目录树完整返回，未发生 tree 截断。取证采用公开 README、入口源码、核心实现和少量已有测试；未运行下载的代码、构建应用、启动桌面窗口、连接真实服务器或读取本机凭据。

- **README 宣称**：项目对外列出的能力；不足以证明内部实现和所有平台表现。
- **源码确认**：已经读到入口及相应关键实现；是静态确认，不表示本轮运行通过。
- **入口确认**：已经找到用户可达入口，但本轮没有核对其每个操作分支。
- **待确认**：需要运行、真实协议端点、外部 Agent 或特定导出样本才能建立的结论。

## 功能全景与实际入口

下表覆盖 README 当前八组主要宣称。证据列区分本轮确认的主体与仍停留在宣称层的细节，不能把整行理解为完整运行验收。[README][readme]

| 分组 | README 宣称的范围 | 实际用户入口与本轮证据 |
|---|---|---|
| 终端 | 标签、分屏、终端搜索；真彩/宽字符/输入法/鼠标，面向 tmux/vim/htop；同主机面板共享连接、自动重连 | 主机菜单的连接/向右分屏，工作区标签与面板工具条/查找；共享连接有 Pool。标签、分屏、查找入口与池已确认；终端交互兼容性和自动重连效果本轮保留 README 层。[工作区][workspace]、[Pool][pool] |
| 文件 | 终端旁 SFTP、目录跟随、内置编辑器；拖放、目录传输；队列暂停/继续/重试 | 主机菜单“文件”、右侧文件按钮、工作区文件面板；上传/下载进入规划和队列，队列有部分文件续传；编辑器有打开/保存实现。拖放与目录传输有对应文件操作/规划入口，未逐分支验收。[文件面板][files]、[目录跟随][follow]、[编辑器][editor]、[传输队列][queue] |
| AI 助手 | 每主机执行命令/编辑文件；多个 Provider 或本机 ACP Agent（Claude Code/Codex/Grok/Gemini CLI 等）同时使用；各自模型/思考级别；Ask/Review/Full 与可配置 AI 审查 | 主机菜单、右侧 AI 按钮、终端右键“右侧/下方打开”；设置 AI 页配置 Provider/ACP/默认权限。后端、主机上下文、权限门禁均确认；并行含义见下一节。[助手][ai]、[AI 设置][aisettings]、[ACP 设置][acpsettings] |
| 外部 Agent 接入 | 开放 API、MCP、Skill；列主机/连接/执行命令，保存凭据不直接交给 Agent；Claude Code/Codex 一键安装 Skill；默认关闭 | 设置 Agents 页启用、复制端点/配置/Token、安装 Skill、管理 Agent 连接；三种入口共用工具集合，启用条件及独立执行边界均确认。[Agents 设置][mcpview]、[应用工具][mcpapp]、[协议处理][mcpcore]、[Skill][skill] |
| 主机与凭据 | SSH config 导入、user@host:port 快连；密码/OpenSSH/PEM/PuTTY/ssh-agent；本地加密、生成密钥、安装公钥 | 侧栏新增/快连/导入、主机编辑、设置凭据、主机菜单“安装公钥”；解析、导入、认证、密钥生成与安装后验证均确认。[侧栏][sidebar]、[快连][quick]、[导入][importer]、[密钥][keys]、[认证][dial]、[安装][installkey] |
| 备份恢复 | 密码加密主机/凭据/片段/设置、跨设备恢复；本地/WebDAV/S3 兼容/Gist；手动/计划与保留份数 | 设置 Backup 页本地导出/恢复和目标配置；打包→密码封装→上传、定时检查、保留清理、合并恢复均有实现。跨设备与各远端服务兼容性未运行；Gist 的“private”措辞需要校正，见下文。[备份][backupapp]、[远端与计划][cloud] |
| 网络 | 多跳跳板、HTTP/SOCKS5 代理；本地/远端/动态转发 | 主机连接配置、设置代理页、工具栏/主机菜单新增转发；源码确认其 TCP SSH/隧道机制，不能称为 UDP 加速。[SSH 建连][dial]、[代理][proxyclient]、[转发 UI][forwardapp]、[转发实现][forward] |
| 日常使用 | 服务器指标；明暗主题；英/简中/繁中/韩/日五语言 | 右侧监控按钮、设置 Monitoring/Appearance；指标面板与语言列表/设置入口确认，采集精度与所有平台呈现未验证。[工具栏与监控][rail]、[设置][settingsview]、[语言与默认值][settings] |

### README 未强调但源码已有入口的补充项

这些是额外发现，避免需求清单只照抄 README；本轮以入口确认或窄机制确认结束，不扩张为整仓功能审计。

| 补充范围 | 已确认入口/范围 | 边界 |
|---|---|---|
| 多客户端迁移与文本主机列表 | 设置 Migration 页，列出 Xshell、MobaXterm、PuTTY、WindTerm、Electerm、Tabby、Netcatty、Remmina、mRemoteNG、WinSCP；另有 typed/pasted/file 的 key=value 文本入口。[迁移 UI][migrationview]、[来源枚举][migrationmodel] | 确认支持格式入口，不等于解密各软件的所有历史凭据仓库；格式文档明确诊断设备加密字段/不支持的封装及选项。[格式边界][migrationformats] |
| 本地终端、Telnet、Serial | 侧栏新增菜单含本地终端；协议选项含 SSH/Telnet/Serial；非 SSH 主机只提供终端，文件/AI 等 SSH 入口禁用。[侧栏][sidebar]、[协议选项][streamhost] | 未验证设备、串口参数、Telnet 服务端兼容性。 |
| 进程、Docker、tmux、命令片段 | 右侧工具栏有 ProcessUsage、DockerManager、TmuxManager、Snippets；源码分别有进程列表、容器/镜像及日志/终端、tmux 会话操作状态。[工具栏][rail]、[进程][usage]、[Docker][docker]、[tmux][tmux] | 已确认入口和主体状态，不推断完整容器平台或通用服务器管理套件。 |
| 分组/收藏、已知主机、快捷键 | 主机菜单有收藏/移动分组/忘记主机密钥；设置含 Known Hosts、Shortcuts。[侧栏][sidebar]、[设置导航][settingsview] | 只确认这些入口，不替代主机密钥策略或键位行为的完整审查。 |

## AI 助手：最小闭合机制

### 主机、面板与后端

用户从主机菜单或终端打开 AI → OpenAI(hostID, …, link) 创建/复用绑定主机的面板 → SendAI 检查首次主机同意、秘密存储解锁及该面板是否正在运行 → 选择内置后端或 ACP → 把主机信息、工作目录、关联终端上下文送入本轮任务。仅打开面板不会立即打开 SSH 连接；实际工具需要时才取得连接。[入口与发送][ai]

Provider 列表保存名称、BaseURL、模型与可选参数，API Key 单独进入 keychain；内置协议有 Chat Completions、Responses、Anthropic，另有仅用于审查的 TypeSafe 格式。面板可固定 Provider/模型；ACP 配置则保存可执行文件、直接参数、Agent ID 与该 Agent 自己的选项，模型/思考选项从 ACP 会话返回。[Provider 配置][settings]、[Provider 选择][aiview]、[ACP 设置][acpsettings]、[ACP 会话][acpsession]

**并行的准确含义**：配置层允许多个 Provider 和多个 ACP；运行状态、取消函数、会话及 goroutine 属于各面板。单面板同一时间只接收一个运行中的 turn，每个标签页最多一个 AI 面板，其他标签页可以有独立助手。因此可以确认多配置、多面板并行使用；未发现必须解释为“一个问题同时自动分派给所有模型并汇总”的依据。[运行状态][ai]、[ACP turn][aiacp]、[单面板约束][single]

### 远端工具与 ACP 边界

内置 Agent 的工具在 ensureAI 中绑定当前主机；ACP 则先通过 Pool 取得该主机的 SSH 连接，再提供远端文件服务和命令 Runner。ACP 的客户端 terminal 回调，以及专用 MCP bridge 的 remote_run/read_file/write_file，均把操作交给该面板的 Gate；命令执行前 Authorize，结束后 Done 记审计，超时/取消会传递到执行生命周期。[内置构造][ai]、[ACP SSH 绑定][aiacp]、[远端 bridge][acpremote]、[ACP terminal][acpterminal]

ACP Agent 本身是**在本机启动的子进程**，工作目录是新建空临时目录。Claude/Codex 有专门适配：限制本机工具、指定远端 bridge、提供远端工作指令；其他 Agent 主要依赖 ACP 客户端能力、提供的 bridge 和指令。源码中的 sandbox 命名不能单独证明操作系统级隔离，也不能保证任意自定义 Agent 都遵守同等本机工具限制。[启动进程][acpsession]、[按 Agent 的配置][acpagents]

### Ask / Review / Full 的真实规则

默认是 Ask；解析顺序包含全局、主机覆盖和面板覆盖，切换模式或主机范围会刷新现有会话门禁。进入 Full 有专门确认 UI。[默认值][settings]、[模式刷新与 Full 入口][ai]

| 模式 | 源码行为 |
|---|---|
| Ask | 规则判定的 ReadOnly 自动运行；其他风险询问用户。文件写入展示 diff，Ask 下不提供写入的“允许类似”。 |
| Review，配置 reviewer | ReadOnly 自动运行；其余风险交 reviewer，但 Critical 始终问用户。审查拒绝、错误或超时退回人工确认。 |
| Review，未配置 reviewer | ReadOnly 和 Low 自动运行；其余询问用户。 |
| Full | 除 Unknown 仍需红色确认外，其余风险自动运行，**包含 High 和 Critical**。 |

规则来自 [决策表实现][engine] 和 [Gate 执行/回退][gate]；已有 [Review 测试][reviewtest] 覆盖只读免模型、失败退回询问和 Critical 不交审查模型放行。本轮只阅读测试，没有执行。这里的风险分类是项目自己的规则结果，不能据此推导普遍的安全保证。

## 开放 API / MCP / Skill：共享工具，独立授权边界

设置 Agents 页显式启用 → 确认风险、解锁 → 生成/读取独立 Bearer Token → 监听 127.0.0.1 的 HTTP 服务。未启用或未解锁时不监听；默认设置没有打开 MCPEnabled。协议处理还有 Host/Origin 检查和恒定时间 Token 比较。[设置与确认][mcpview]、[服务生命周期][mcpapp]、[监听与认证][mcpcore]、[默认值][settings]

三种接入是同一个能力表的不同包装：HTTP API 位于 /api/v1，MCP 暴露工具调用，Skill 告知外部 Agent 如何调用 HTTP API。当前工具为 list_groups、list_hosts、connect、run_command、list_sessions、disconnect；这套开放工具表没有直接提供任意文件读写工具。[路由][mcpcore]、[工具表][mcpapp]、[生成 Skill][skill]

connect 根据保存主机 ID 建立会话，凭据用于 SSH 握手而不作为响应字段返回；需要临时输入、未满足保存认证条件或未信任主机密钥的连接会失败。命令结果仍是远端输出，不能把“不返回保存凭据字段”扩大成“所有输出都不含秘密”。[连接与凭据路径][mcpapp]

**关键差异**：开放 run_command 先准备审计并记录 started，然后直接 remote.Run，执行后 Classify 并记录结果；它没有调用 aiGate/Authorize，也不读取助手 Ask/Review/Full 来审批本条命令。它有输出大小、超时、取消和审计失败边界；这些不等于助手的逐命令授权。界面启用确认也专门列出免逐条询问的风险。[执行路径][mcpapp]、[启用确认][mcpview]

Skill 可安装到 Claude Code、Codex 和 .agents 的技能目录。内容含端点访问 Token，按当前用户可读方式写入；安装/更新检查 SSHYard 自有标记与符号链接冲突，Token 轮换会更新其拥有的 Skill。它是接入说明和授权材料，不是另一套独立执行引擎。[Skill 生命周期][skill]

## 备份恢复、导入与 SSH 密钥

### 备份恢复

设置 Backup → 收集资产快照、设置、known_hosts 和可选秘密 → 压缩归档 → 密码封装 → 本地文件或远端目标；恢复反向解封并合并到当前设备。秘密包括主机/凭据密钥与密码、代理/转发密码、片段正文、Provider Key；不导出本机 master key 或开放接口 Token。导出 UI 默认包含秘密但可选择省略。[备份载荷、收集与导出][backupapp]

加密最小机制是 Argon2id 从密码派生包裹密钥，随机数据密钥用 XChaCha20-Poly1305 加密归档；认证覆盖明文头。自动备份保留派生密钥于本地加密存储，省去每次重新输入密码；不能把它描述成每次运行都只持有密码、完全不保留解密能力。[vault 实现][vault]

远端目标抽象只有 Put/List/Get/Delete 等备份对象操作，接入 WebDAV、S3 兼容桶和 Gist。每次新建带时间/设备标识的文件，成功上传后只清理本设备超出 Keep 的旧备份；Keep=0 不清理，清理失败不令已上传备份失败。计划由应用存活期间的 timer 驱动，锁定时等待、无变化时跳过；不是确认存在的系统级后台备份服务。[目标与调度][cloud]、[对象与保留规则][backupcore]

README 把 Gist 写作 private，但适配器明确是 **secret/unlisted gist**：持链接者可读到加密文件，内容保密依赖上传前的加密。此处按源码采用准确措辞。[Gist 适配器][gist]

恢复采用秘密批量写入、资产合并和补偿回滚；源码明确不是跨文件崩溃原子事务，known_hosts/偏好后续失败也不会回滚前面。恢复保留本机的 MCP 开关/端口/本机密钥授权与备份目标，因此备份不会替另一设备自动开启 Agent 接入或选择云目标。[恢复与设置合并][backupapp]

### 导入

SSH config：入口先预览、标记冲突与选择行，再保存认证/主机并建立跳板链。解析保留 HostName、User、Port、IdentityFile、IdentitiesOnly、ProxyJump、Include 与匹配默认值；通配 Host 作为默认值/模式处理而非批量生成主机。不支持的 ProxyCommand、LocalForward 等会记录诊断，不能声称完整等价执行 OpenSSH 配置。[SSH config 解析][sshconfig]、[预览与保存][importer]

多客户端/Text 迁移使用单独入口和来源格式枚举；能读的元数据与可用秘密进入预览，设备加密仓库/版本封装/不支持选项保留明确诊断。各客户端所有版本的导入成功率、密码恢复率不在本轮结论内。[迁移 UI][migrationview]、[来源模型][migrationmodel]、[格式范围][migrationformats]

### 密钥与本地凭据

凭据编辑可导入 OpenSSH/PEM/PuTTY PPK v2/v3，PPK 转为 OpenSSH；可生成 Ed25519 密钥，并选择私钥口令及是否每次输入。元数据保存类型/指纹等，私钥、口令、密码分别写入 keychain；SSH 认证路径支持保存密码、私钥和 ssh-agent。[密钥格式][keys]、[凭据 UI/保存][credentials]、[SSH 认证][dial]

本地秘密存储用设备 master key 和 XChaCha20-Poly1305 加密，master key 经系统 Backend 保存/读取；这与可跨设备的“密码加密备份”是两层不同机制。本文未逐平台审计系统钥匙串实现或恢复策略。[本地加密存储][encrypted]

安装公钥：主机菜单选择已存密钥 → 用当前认证取得该主机连接 → 通过 SFTP 安装公钥 → 新建仅使用该密钥的连接验证 → 用户可切换主机认证；验证失败和安装失败区分呈现。切换还检查实际登录用户一致，不能把“公钥写入成功”当成“新登录方式已验证”。[安装与验证][installkey]

## 网络链路与 UDP 判断

已确认的闭合链路是：主机网络配置 → session.Dial → 第一跳直接 TCP 或 HTTP CONNECT/SOCKS5 → 后续跳板通过前一 SSH 客户端的 direct-tcpip 到下一跳 → 每跳独立校验主机密钥并认证 → 最终 SSH 客户端承载终端/SFTP。关闭/丢失下层连接会影响依赖它的跳链。[建连与释放][dial]、[代理握手][proxyclient]、[SFTP channel][sftpsvc]

转发分别是 Local(-L)、Remote(-R)、Dynamic(-D)：本机或服务端 TCP Listener 接收连接，再经 SSH/目标 TCP 建连转发。动态代理可提供 HTTP 与 SOCKS4/4a/5；SOCKS5 服务端明确只接受命令码 1（CONNECT），其他命令码拒绝，因此不提供 UDP ASSOCIATE。[转发][forward]、[动态代理][proxyserver]

队列并发、部分文件续传、自动重试有实现：目标旁写 .sshyard-part，暂停保留，恢复按偏移 Seek，完成再重命名。这属于传输管理与可靠性；它不会把底层 TCP SSH 改成 UDP，也不证明弱网吞吐加速。[队列][queue]

本轮没有测量网络性能，也没有对所有依赖做协议审计。足够支持的产品结论是：**可参考跳板/代理/转发与传输队列机制；真正 UDP SSH/SFTP 加速不是本轮确认的 SSHYard 功能。** FinalShell/FinalSpeed 的加速、套餐或服务端机制不作为 SSHYard 证据。

## 对需求对齐可携带的最小机制与保留项

以下只提取来源机制，不判断本地 SmallSSH 已实现哪些、应该放在哪层或本轮开发优先级。

| 最小机制 | 需要独立对齐的边界 |
|---|---|
| 绑定当前主机/终端的助手上下文，Provider/ACP 可替换 | 是否需要多面板并行；不默认增加同轮多 Agent 编排。 |
| 规则分类→授权→执行→审计，命令与文件 diff 共用动作边界 | Ask/Review/Full 的实际语义；Full 会放行 Critical，不自动成为我们的默认。 |
| 本机开放端点共用工具表、凭据留在连接层 | 是否开放、哪些主机/工具可用、是否逐命令审批；不默认照搬外部接口免审执行。 |
| 备份载荷与密码封装分层、本机状态与可迁移状态区分 | 秘密是否包含、密码/派生密钥保管、恢复冲突/失败；云目标和计划单独选，不作为默认外传。 |
| 导入预览/诊断/冲突处理；安装公钥后独立验证 | 支持哪些来源/版本、无法解密如何提示；不把文件识别当作完整迁移。 |
| TCP 跳链/代理/转发与可靠传输队列 | 与“真正 UDP 加速”分别列需求和验收指标。 |

待确认项集中为：三平台运行表现；外部 ACP Agent/适配器版本兼容性和自定义 Agent 的本机工具约束；各备份服务与真实跨设备恢复；各迁移版本/加密导出的端到端接受度；终端交互兼容性、断线重连与性能数据。它们不影响本文已确认的入口和关键源码边界，也未被标为运行验证通过。

[repo]: https://github.com/Licoy/sshyard
[readme]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/README.md#features
[sidebar]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/app/sidebar.go#L280-L334
[rail]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/app/rail.go#L106-L154
[settingsview]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/app/settingsview.go#L57-L78
[workspace]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/app/workspace.go#L125-L161
[pool]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/session/pool.go#L43-L107
[files]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/app/files.go#L586-L737
[follow]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/app/follow.go#L50-L126
[editor]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/app/editor.go#L370-L433
[queue]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/sftpsvc/queue.go#L120-L144
[ai]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/app/ai.go#L153-L555
[single]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/app/single.go#L11-L19
[aisettings]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/app/aisettings.go#L96-L226
[aiview]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/app/aiview.go#L205-L410
[acpsettings]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/app/aiacpsettings.go#L22-L130
[aiacp]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/app/aiacp.go#L115-L296
[acpsession]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/ai/acp/session.go#L100-L188
[acpagents]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/ai/acp/agents.go#L13-L139
[acpremote]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/ai/acp/remote.go#L18-L190
[acpterminal]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/ai/acp/terminal.go#L50-L144
[engine]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/ai/policy/engine.go#L70-L125
[gate]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/ai/policy/gate.go#L130-L214
[reviewtest]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/ai/policy/mode_review_test.go#L10-L85
[settings]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/settings/settings.go#L134-L583
[mcpview]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/app/mcpview.go#L19-L298
[mcpapp]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/app/mcp.go#L96-L647
[mcpcore]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/mcp/mcp.go#L88-L210
[skill]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/app/skill.go#L24-L285
[quick]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/app/quick.go#L94-L148
[sshconfig]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/sshconfig/sshconfig.go#L1-L85
[importer]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/app/importer.go#L107-L310
[migrationview]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/app/migration_view.go#L14-L54
[migrationmodel]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/migration/model.go#L8-L53
[migrationformats]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/migration/FORMATS.md
[keys]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/keys/keys.go#L86-L177
[credentials]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/app/credentials.go#L193-L323
[encrypted]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/keychain/encrypted.go#L96-L157
[installkey]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/app/installkey.go#L76-L199
[backupapp]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/app/backup.go#L25-L331
[backupcore]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/backup/backup.go#L67-L171
[vault]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/vault/vault.go#L100-L159
[cloud]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/app/cloud.go#L145-L358
[gist]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/backup/gist/gist.go#L1-L5
[dial]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/session/dial.go#L113-L339
[proxyclient]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/netproxy/client.go#L61-L106
[proxyserver]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/netproxy/server.go#L237-L334
[forward]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/forward/forward.go#L147-L364
[forwardapp]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/app/forwards.go#L72-L110
[sftpsvc]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/sftpsvc/sftpsvc.go#L59-L95
[streamhost]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/app/streamhost.go#L112-L124
[usage]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/app/usage.go#L20-L82
[docker]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/app/docker.go#L17-L57
[tmux]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/app/tmux.go#L19-L63

## 增量核对：文件设置、tmux/串口、认证与剪贴板（2026-11-06）

复用同一 SHA，仅补查用户新增关注的设置和动作；以下均为源码静态确认，未启动窗口、读本机私密数据或连接真实设备。来源的共享凭据实现仅用于说明现状，不据此替本地产品选择密钥管理方式。

| 关注项 | 实际配置 → 执行入口与最小边界 |
|---|---|
| 文件并行传输数 | 设置 Files 页的 TransferConcurrency 为 **1–10，默认 3**。创建文件面板时设置该面板的 transfers 队列；ApplySettings 也更新已有文件面板。它是**每文件面板队列**的并行数；降低值时已运行任务继续，空出名额后再启动后续任务。[设置 UI][inc-filesprefs]、[创建与更新][inc-appfiles]、[队列限额][inc-queuelimit] |
| 超过多大询问 | OpenConfirmMB 为 **1–1024，默认 10**。editFile 在文件大小严格大于值左移 20 位时询问，即实际按 MiB 计算；本地与远端进入此编辑器路径都适用。确认后仍受 **256 MiB** 编辑器硬读取上限约束；这是打开编辑文件的确认，不是大文件上传/下载的通用确认。[设置 UI][inc-filesprefs]、[读取与确认][inc-fileopen]、[默认值][settings] |
| 终端下方文件 / 独立窗口 | FilesEdge 只有 **right / below**，OpenFilesBeside 把它转换为右侧/下方分屏，终端菜单同时提供另一个方位。独立窗口来自另一条文件编辑器路径：openFileEditor → editorFor → OpenWindow，同主机的文件可复用该编辑器窗口的标签。**独立窗口不是 FilesEdge 的第三个选项**。[文件分屏][inc-workspacemenus]、[编辑器窗口][inc-editorwindow] |
| tmux 会话发现与快速附着 | 右侧 tmux 按钮 → tmuxPanel → readTmux → SSH 上执行 TmuxList，列会话、窗口及附着客户端，显示期间约 5 秒刷新，缺少 tmux 有单独提示。会话/窗口按钮可附着当前终端或开新标签；当前已在 tmux 时切换 client，shell 确认空闲时输入 attach 命令，终端忙或无法确定空闲时转开新标签。空闲识别依赖远端 /proc，不能承诺所有系统都能原地附着。[列表命令][inc-tmuxlist]、[附着决策与按钮][inc-tmuxactions] |
| Serial 连接 | 主机编辑协议选择 Serial → 发现/刷新或手填设备，配置波特率、5–8 数据位、校验、1/1.5/2 停止位，以及换行/本地回显 → streamSession → session.Serial → serial.Open → 本地串口字节流。默认归一化为 **115200、8N1**；同一设备再次打开会复用现有终端，非 SSH 只允许终端能力。[串口表单][inc-serialform]、[配置与复用][inc-streamhost]、[串口打开][inc-openserial]、[参数默认值][inc-serialconfig] |
| 终端/标签菜单动作 | termEntries 明确提供复制、粘贴、查找、清屏（发送 Ctrl-L）、左右/上下分屏、两种文件方位、两种 AI 方位、选中文本问 AI、关闭面板；选中态/SSH 能力决定可用性。Telnet/Serial 额外提供 Send Break，实际经 Breaker 发送约 250ms Break。标签右键另有重命名、关闭当前/其他/左侧/右侧/同主机/其他主机/全部。这里确认动作入口，不把截图外观当作行为证据。[终端菜单][inc-workspacemenus]、[标签菜单][inc-tabmenus]、[Break 实现][inc-streambreak] |
| 自动 / 密码 / 密钥 / SSH agent | 当前主机 UI 的实际选择是：自动（禁用本机密钥尝试时显示“每次询问”）、SSH agent、各已保存凭据（标注密码/密钥类型）。自动对应 AuthAsk：已有本次输入密码就用密码，否则在 LocalKeys 开启时尝试本机 keys/agent，再进入需要输入的流程；默认 LocalKeys=true。已存密码/密钥经 CredentialID 解析，另外保留直接主机密码/密钥的兼容分支。因此 UI/数据模型不能简单视为四个平级硬编码枚举。[认证选项][inc-authui]、[凭据解析与自动尝试][inc-authrun]、[SSH 认证][dial] |
| 安装公钥向导 | 主机菜单打开向导，选择已有密钥凭据，必要时补当前登录密码/私钥口令 → 用当前认证连接并安装公钥 → 新建仅用目标密钥的连接验证 → 结果区区分已添加/已存在/验证失败，验证与用户匹配后才提供切换认证。这个向导本身不是“为每台服务器自动生成一把新密钥”的流程；生成入口在凭据表单。[向导与结果][inc-installwizard]、[安装/验证/切换][installkey]、[生成入口][credentials] |
| OSC52 | 设置 Terminal 的 Clipboard 分组提供允许/询问/拒绝及 **64–8192 KiB** 限额，默认询问、1 MiB；主机可覆盖全局。终端解析回调经 deliverOSC52 → clipGate → 本机写剪贴板；询问可允许一次或当前面板本次会话。远端读取剪贴板的 query 被拒绝；超量丢弃，10 秒第 11 次写入触发暂停，并有短时撤销机制。它是远端程序写本机剪贴板的控制。[设置 UI][inc-clipprefs]、[主机覆盖/默认值][settings]、[OSC52 门禁与交互][inc-osc52] |
| 选中即复制 / 右键行为 | CopyOnSelect 是独立开关（默认 false）：本机终端选择完成的 PointerUp 时，把非空选区写剪贴板。右键另可选择菜单、粘贴、选中时复制否则粘贴；终端程序已启用鼠标报告时先走程序鼠标协议。不能把选中即复制与 OSC52 远端写入授权合并成一个开关。[设置 UI][inc-clipprefs]、[选择完成与鼠标路径][inc-termcopy] |

[inc-filesprefs]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/app/settingsview.go#L609-L633
[inc-appfiles]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/app/app.go#L707-L976
[inc-queuelimit]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/sftpsvc/queue.go#L174-L188
[inc-fileopen]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/app/fileopen.go#L74-L169
[inc-workspacemenus]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/app/workspace.go#L586-L625
[inc-editorwindow]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/app/editor.go#L143-L321
[inc-tmuxlist]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/session/tmux.go#L18-L66
[inc-tmuxactions]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/app/tmux.go#L103-L504
[inc-serialform]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/app/streameditor.go#L71-L197
[inc-streamhost]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/app/streamhost.go#L43-L72
[inc-openserial]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/session/open.go#L37-L55
[inc-serialconfig]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/serial/serial.go#L39-L106
[inc-tabmenus]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/app/tabs.go#L436-L465
[inc-streambreak]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/session/stream.go#L27-L154
[inc-authui]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/app/hosteditor.go#L84-L104
[inc-authrun]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/app/app.go#L1193-L1302
[inc-installwizard]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/app/installkey.go#L248-L427
[inc-clipprefs]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/app/settingsview.go#L537-L573
[inc-osc52]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/app/osc52.go#L74-L410
[inc-termcopy]: https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/app/termview.go#L774-L991


## 增量核对：画布卡片与功能开关（2026-11-07）

沿用已解析基线 `93c596b46efba3e3939f06fbff4c179e7241755b`，本次补查三个问题：主画布怎样承载不同内容、右侧开关怎样管理功能、如何保留我们更紧凑的标签工具区。读取了固定 revision 的 workspace.go、rail.go、single.go、panels_test.go，并检查 README 的终端 / 状态截图；未构建或执行来源代码。取证底稿在 `build/reference/sshyard-canvas/`，Go 源码以 `.go.txt` 保存，避免 Go 将它们作为本项目包扫描。

[workspace.go](https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/app/workspace.go#L319) 的画布由分栏树构成，卡片各有标题、拖动和关闭入口；文件可在终端右侧或下方打开。它不是任意 x/y 浮窗的无限画布。[rail.go](https://github.com/Licoy/sshyard/blob/93c596b46efba3e3939f06fbff4c179e7241755b/internal/app/rail.go#L104) 区分主画布内文件 / AI 与旁边可同时打开的多个 inspector；开关亮灭反映对应功能，不用一个工具标签替换全部功能。

本地采用相同的卡片标题栏和独立功能开关思路，复用已有递归分栏与目标绑定：文件、命令、状态和实际任务队列能分别显示，卡片可拖到一起聚合成 tab，也能再次拆开。次要内容的标签工具区只在底部 / 右侧；主画布仍可四向拆分。主区支持“平铺为画布 / 聚合为标签”，既有源会话、浏览器和任务队列不随呈现切换重建。不移植其 AI、开放接口或凭据模型；该部分不属于本轮请求。
