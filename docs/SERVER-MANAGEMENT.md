# 服务器状态与操作

入口仍在终端下方的「服务器状态」，跟随当前终端标签。面板可以放大、收起或移到右侧。概览、CPU / 网络 / 磁盘详情和 Docker 页面使用同一组资源卡片、指标、分段表和环形表组件。

点击资源卡片只在原面板内切换详情，不改变终端和面板的分栏比例。详情顶部固定显示“返回概览”，滚动时保持可见；返回后保留概览原来的滚动位置。底部详情使用紧凑标题和随面板高度调整的趋势图。需要更多空间时可主动从面板菜单“放大工具面板”；放大后顶部直接提供“恢复分栏”按钮。

## 展示的信息

| 区域 | 信息 |
| --- | --- |
| CPU | 总使用率与历史、系统 / 用户 / Guest / I/O 等待 / Steal，各逻辑核心的分段负载；最多展示 256 个核心；负载、运行时间 |
| 内存 | 已用、总量、可用、空闲、页缓存、缓冲区、Swap 和历史趋势 |
| 网络 | 主网卡及默认路由、IPv4 / IPv6 地址、每秒接收 / 发送、累计流量；虚拟网卡默认折叠，展开后使用虚拟列表；TCP 重传比例、累计主动 / 被动建连和当前已建立连接 |
| 文件系统 | 挂载路径、设备、类型、已用 / 总量、可用空间与只读 / 读写模式；概览保留根 overlay、`/overlay`、`/rom`；详情可看所有挂载点。不同挂载点可能共享底层存储，不累计相加。只读系统分区接近满容量时使用中性色 |
| Docker | 未安装 / 无法连接 / 正常可用；容器、镜像及 Compose 项目卡片。默认显示资源卡片，可从显示菜单切换列表；名称 / CPU / 内存排序，列表还可按各列排序 |
| 容器资源 | CPU（单核口径，可超过 100%）、内存和上限、累计磁盘读 / 写、累计网络收 / 发；缺失、超时、过期、不同 Docker 实例的数值不显示为零 |

同一保存连接、SSH 用户、实际连接地址和已核对主机指纹共用一个采集器，主机 `/proc` 数据约每 2 秒采样；系统清单、网卡地址和 Docker 清单约每 60 秒采样。容器资源使用独立 SSH channel，查询完成后间隔 10 秒再次读取；单次 4 秒超时，不阻塞主机计数。没有安装 Docker 时降低到每 60 秒检查。多终端共用当前快照与趋势，新订阅先接收缓存；采集连接退出后由仍获授权的连接接替，最后一个订阅离开时停止。不同保存连接条目和账号不共享结果。容器资源每次最多 128 个运行中容器；清单最多 1024 项。状态采集不读取完整容器环境变量；Compose 清单提取项目、服务、工作目录及配置文件来源标签，按需执行下面的更新流程。

累计网络 / I/O 是内核或 Docker 容器生命周期内的计数，不等于套餐计费流量。TCP 重传比例使用本次间隔的重传包数 / 发出包数；没有有效增量时显示「—」。内存按 [Docker CLI 的口径](https://docs.docker.com/reference/cli/docker/container/stats/)扣除可回收缓存，CLI 格式化后的字节值有舍入。

服务器无需安装额外守护进程。当前主机状态针对 Linux；服务控制针对 systemd。OpenRC / SysV 保留只读清单。精简路由系统若缺少兼容的 `ps`，会明确说明进程清单不可用；磁盘探测在不支持 `df -T` 时回退并通过 `/proc/mounts` 补类型和模式。

## 操作方式

选中一行后使用「操作」，或直接右键。容器卡片也有「⋯」菜单。所有变更先显示目标和影响，再确认：

- 进程：正常结束（TERM）、强制结束（KILL）。不自动由 TERM 升级为 KILL。PID 0 / 1、负 PID 和缺少身份的数据不可操作；执行前检查 boot ID、`/proc/PID/stat` 的 starttime 和 comm。检查与发送信号之间仍有极短的系统调用竞态；未宣称使用 pidfd 原子保证。
- systemd 服务：启动、停止、重启、开启自启、关闭自启。停止与关闭自启独立；关闭自启后保留服务，便于再次开启。使用 [systemctl 的非交互方式](https://www.freedesktop.org/software/systemd/man/256/systemctl.html)，当前 SSH 账户没有权限时显示失败，不自动提权或请求额外密码。
- Docker 容器：启动、停止、重启。停止 / 重启先正常停止，等待 10 秒后 Docker 可强制结束，按 [Docker stop 的语义](https://docs.docker.com/reference/cli/docker/container/stop/)。本轮未加入容器删除、镜像删除、Docker 安装或清理。

变更使用 `internal/serverops` 构造白名单动作，拒绝有歧义的服务名和短容器 ID。命令通过现有共享 broker、所有者证明、单次授权及审计执行；没有额外的 SSH 连接后门。确认固定 session 和具体命令，表格刷新 / 排序按稳定身份选择，切换标签不转移目标。发起操作时同时约束已连接的 endpoint 和主机指纹，连接配置变化会拒绝。

Docker 的读取和动作清除 `DOCKER_HOST` / `DOCKER_CONTEXT` / TLS 环境覆盖，并显式使用目标机器上的本地 Unix socket；支持系统 socket 和当前用户的常规 rootless socket。写操作还固定 Docker 实例 ID 与完整 64 位容器 ID。自定义 socket 尚无设置入口。

执行后更新该采集源的共享状态，同源的其他标签同步可见；过期清单不能覆盖更新结果。重复提交、锁定、连接关闭、超时和失败路径均有限制，失败不会显示为成功，也不会替其他调用方批准待处理请求。

## Docker 日志、容器终端与 Compose 更新

容器右键或卡片「⋯」为同一入口：日志与详情、打开容器终端、启动 / 停止 / 重启。卡片名称及列表双击打开日志。日志详情留在当前 Docker 工具区，返回保留工作区大小，不自动放大或覆盖终端；工具面板仍可由用户放到底部 / 右侧或独立弹出。

日志使用 [Docker logs](https://docs.docker.com/reference/cli/docker/container/logs/) 的时间戳、200 / 1000 / 2000 行尾部及持续跟随。搜索只筛选已接收缓存，可复制筛选结果；暂停接收关闭通道，继续时重新读取所选尾部范围。跟随最新只控制滚动。缓存按最多 512 KiB / 4000 行限制，单行约 8 KiB，超限保留最近部分；渲染使用虚拟行，过滤转义控制序列及 OSC 指令，不执行日志，也不自动操作剪贴板。UI 更新合并至最多约 10 次 / 秒，未变化时不重建筛选行。

详情页「终端 ▾」可选 sh / bash，右键提供默认 sh。通过共享 broker 为固定容器建立 SSH PTY，再运行 [Docker exec](https://docs.docker.com/reference/cli/docker/container/exec/)；沿用容器配置的用户，无 privileged 或宿主机 Shell 回退。标签支持现有 Ghostty 输入、缩放、关闭及重连。重连继续固定原 Docker 实例和完整容器 ID；容器已被重建则拒绝旧目标，需要从清单选择新容器。文件区仍通过 SSH 访问宿主机，跟随容器标签时明确显示「跟随宿主机」。

Compose 首页一张卡代表一个项目，显示运行比例、服务数、总 CPU / 内存及最多三个容器子状态，异常项优先。按 Docker 的 `com.docker.compose.project` / `service` 标签识别，不按容器名称猜测；普通容器仍在“容器”页。统计只覆盖 Docker 当前保留的容器，无法推断配置里从未创建或已删除的服务。没有健康检查只显示“运行中”；健康检查失败、重启循环和非零退出会显示需关注。CPU 按单核口径相加，可超过 100%；内存沿用 Docker 口径，包含暂停容器的占用。缺少、过期或其他 Docker 实例的资源样本不当作零；不完整的合计注明“部分”。

点击卡片在同一工具区查看该项目的容器，容器日志返回项目、项目返回卡片列表，保留终端大小及项目列表位置。搜索支持项目、服务、容器、镜像和状态，详情内只筛选显示，不改变项目更新范围。项目详情右上角「⋯ → 更新项目…」或项目卡片右键「更新项目…」打开服务选择与计划确认框，默认选择运行中的服务，已停止的服务需主动勾选。现阶段更新已有的 Compose 项目，不创建新项目或修改版本配置；按服务器现有 image 标签 / 摘要拉取，检查计划不查询版本列表或提前下载。目标绑定项目、工作目录、按原顺序合并的配置文件、所选服务及已解析模型的摘要；来源标签不完整 / 不一致、Docker 实例更换或容器清单不完整时禁用入口并说明原因；更新前继续核对原容器与配置。纯本地 build、没有 image 的服务提示先发布镜像。

项目详情「⋯」及卡片右键共用项目菜单，提供更新、启动、停止、重启。后三项先确认服务器、项目、全部已发现服务及配置位置，再通过现有 owner / exec 审批通道运行固定命令；搜索只改变显示，不改变项目范围。启动使用 `compose start`，只启动已有容器；停止保留容器和数据卷；重启包含当前已停止的服务，并使用 `--no-deps` 限定到已确认的服务。不会创建容器、拉取镜像或应用配置变化。已暂停容器不会由“启动”自动解除暂停。停止和重启每个容器等待 10 秒退出，本地最长等待 6 分钟，结果显示在原服务器的操作反馈中并刷新清单。

执行前固定本地 Docker socket、核对实例 ID，并比对项目完整容器 ID 集合及每个容器的服务 / 项目 / 工作目录 / 配置文件标签；成员或来源发生变化即拒绝，需刷新后重新确认。操作最多支持 64 个已有容器、32 个服务，过大的确认请求会拒绝；配置中未创建或已删除的服务不在操作范围。校验与命令之间仍可能发生外部变更，不提供 Docker 层事务；连接中断或等待超时应核对远端状态，不宣称回滚。

确认后依次核对模型、拉取镜像、再次核对、重建、等待及读取实际状态。重建显式使用 `--no-deps --no-build --pull never --wait --wait-timeout 120 --remove-orphans=false`；不删除卷、旧镜像或其他服务，不重新生成匿名卷。清除环境中的项目 / 文件 / profile 覆盖，固定不清理 orphan 容器，避免 `COMPOSE_REMOVE_ORPHANS` 等继承设置扩大操作范围。[Compose config](https://docs.docker.com/reference/cli/docker/compose/config/) 的解析模型只在代理内用于摘要与提取镜像名称，正文和环境值不返回前端、不进入任务输出、不保存到本地文件。模型摘要包括大整数原值，避免浮点舍入造成不同配置获得相同摘要。

长任务在右上任务中心的「服务器操作」查看，输出只存当前进程内的有界缓存；不会自行打开面板或切换终端。任务显示下载、重建 / 等待、核对结果与错误；失败前未开始重建时明确说明，重建后就绪失败不会报告成功。无健康检查的容器仅说明运行状态，不称为健康。停止等待、断线或超时不能证明远端动作回滚，需核对实际状态；镜像回退不等于数据库数据回滚。保留最多 20 项内存任务，锁定 / 退出取消本地等待并释放显示数据。

模型复核用于发现预览后及下载期间的配置变化，不是服务端文件事务；复核和执行之间仍存在外部修改窗口。外部 `--env-file`、非标准 socket、含逗号而无法从标签拆分的配置文件名没有桌面设置入口；需先在服务器核对 / 整理来源。目标 Compose 需支持 `--wait-timeout`，过旧版本在变更前拒绝执行。更新耗时上限约 12 分钟，单次就绪等待 120 秒。服务器不新增常驻程序。

共享 CLI 新增类型化命令，标志放在位置参数之前；socket 默认 `/var/run/docker.sock`，实例 ID 和完整容器 ID 来自状态清单：

```sh
smallssh docker logs --engine ENGINE_ID --tail 200 HOST_ID FULL_CONTAINER_ID
smallssh docker terminal --engine ENGINE_ID --shell sh HOST_ID FULL_CONTAINER_ID
smallssh docker plan --engine ENGINE_ID --container FULL_CONTAINER_ID \
  --directory /srv/app --files /srv/app/compose.yaml --services web HOST_ID PROJECT
smallssh docker update --engine ENGINE_ID --container FULL_CONTAINER_ID \
  --directory /srv/app --files /srv/app/compose.yaml --services web \
  --hash HASH_FROM_PLAN HOST_ID PROJECT
```

CLI / AI 同样经过所有者审批或已批准会话。只读子授权可读取日志，拒绝容器终端及 Compose 检查 / 更新；完整范围才可请求这些操作。审批证明绑定 socket、实例、完整容器 ID、Shell / 日志参数，或 Compose 配置来源、服务及模型摘要。桌面使用现有解锁会话，不替其他调用方批准请求。

## 验证与预览

测试使用隔离保险库、回环 SSH / SFTP 协议、临时 systemctl / Docker 命令夹具，以及测试自己创建的子进程。覆盖确认 / 取消、切换目标、排序刷新、权限失败、重复提交、daemon 变化、数值与单位、只读挂载点、Docker 慢查询不阻塞主机、128 个容器采样上限。未在生产机器上测试停止服务、结束进程或容器启停，也未把命令夹具验收描述成真实 Docker daemon / 路由器硬件验收。

Docker 工作流另有显式启用的真实本机验收：创建唯一命名、无发布端口 / 无宿主机挂载的测试项目，web 从 Alpine 3.21 更新为 3.22；验证原数据卷内容、未选容器及 orphan 容器 ID 保持，在 `COMPOSE_REMOVE_ORPHANS=1` 的干扰下仍不扩大清理范围。日志、真实容器 sh 交互输入经回环 SSH 和共享 broker 完成，PTY 请求 / 缩放另有协议测试；测试后清理专用项目及卷。没有操作生产服务器或已有项目。

```sh
SSH_CLIENT_DOCKER_QA=1 go test ./internal/broker \
  -run TestLiveDockerComposeUpdatePreservesDataAndUnselectedService -count=1 -timeout 300s
```

离屏工作流：[日志区](docker-work-logs.png)、[暗色](docker-work-logs-dark.png)、[小工具窗](docker-work-logs-small.png)、[窄右侧](docker-work-logs-right.png)、[Compose 计划](docker-work-compose.png)、[任务中心](docker-work-task-center.png)、[失败结果](docker-work-task-error.png)、[容器终端](docker-work-terminal.png)。

离线预览来自实际原生组件，不创建窗口、不连接生产服务器：

```sh
SSH_CLIENT_PREVIEW_QA=1 SSH_CLIENT_UI_SNAPSHOT_DIR=/tmp/ssh-management \
  go test -tags preview_qa,terminal_integration ./internal/desktop \
  -run TestManagementResourceSnapshotsWithoutWindows
```

[CPU](management-cpu.png) · [网络](management-network.png) · [挂载点](management-mounts.png) · [Docker 卡片](management-docker.png) · [Docker 暗色](management-docker-dark.png) · [右侧面板](management-right.png)
