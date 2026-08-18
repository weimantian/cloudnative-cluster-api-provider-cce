# 华为云 CCE 对齐问卷 — 官方文档检索确认结果

> 状态:**全部 14 项已完成官方来源核对**(4 个子代理 + 主代理一手抓取;来源:官方 API 文档 / 用户指南 / FAQ / 计费文档 / 官方 Go SDK)。
> 关联文档:[问卷](cce-verification-questionnaire.md)(问题原文)、[调研依据](research-sources.md)、[落地跟踪](poc-implementation-tracker.md)。
> 依据类型标记:**[官方文档]** / **[官方SDK]** / **[需实测或咨询]**(官方未公开或文档冲突)。
> 标注 **[需实测/咨询]** 的剩余项,是官方文档无法回答、必须对接真实华为云 CCE 才能关闭的。

## Q1 空集群(0 节点)创建 — [官方文档] 完全确认

- **官方原文(创建集群 API cce_02_0236)**: "该 API 用于创建一个**空集群**(即只有控制节点 Master,没有工作节点 Node)。请在调用本接口完成集群创建之后,通过创建节点添加节点。" → 空集群创建 100% 支持。
- 请求体仅 `kind/apiVersion/metadata/spec`,**无任何节点参数**;`hostNetwork`(VPC/子网)**必填** → 必须先有 VPC。
- **空集群照常计费**:计费项官方(price-cce cce_03_0006):"集群 | 集群管理费用。根据每个集群所在区域、控制节点数、集群规模(最大支持的节点数)的差异收取不同的费用"。
- flavor 定义最大可管理节点数(cce.s1.small=50 … cce.s2.xlarge=2000);超限报 400 `CCE.01400014`(节点数超出集群规模限制)。
- **phase 完整枚举**(SDK model_cluster_status.go):`Available/Unavailable/ScalingUp/ScalingDown/Creating/Deleting/Upgrading/Resizing/RollingBack/RollbackFailed/Hibernating/Hibernation/Awaking/Empty(废弃)/Error`;Unavailable "需手动删除",Error "可尝试手动删除"。
- **未确认(需实测)**:空集群最终 phase 是否即 Available;"Available 前调用节点池接口"的具体错误码;空集群是否占 1 个集群配额(配额数值见 Q7)。

## Q2 kubeconfig 证书获取与有效期 — [官方文档] 完全确认

- 有效期(官方 API 文档"获取集群证书" cce_02_0248 + SDK):**"取值范围 -1 或 [1,1827]…若填 -1 则为最大值 5 年"**;另有 `expire_at`:"证书到期时间须在当前时间后 15 分钟至 5 年之间";**duration 与 expire_at 至少指定一个,同时指定以 expire_at 为准**;文档无默认值。
- external/internal(官方 cce_02_0248 + SDK 响应模型):current-context **"若存在 publicIp 时为 external;若不存在 publicIp 为 internal"**;internal 时集群列表数量为 1(name=internalCluster,server 示例 `https://192.168.1.7:5443`);external 时扩展 cluster name=externalCluster 且 `insecure-skip-tls-verify=true`。
- **吊销(官方 cce_02_0249 + cce_10_0744)**:`clustercertrevoke`(userId/agencyId 二选一);"吊销后此证书申请人之前下载的证书和 kubectl 配置文件**无法再用于连接集群**…可重新下载";旧凭证"无法恢复"。前提:v1.19.16-r50 等及以上版本。
- 证书有效性(官方 cce_10_0107):"申请 kubeconfig 凭证后,即使该用户已被删除,其申请的 kubeconfig 凭证仍然有效。需要手动吊销" → 凭证默认持续有效至到期/被吊销。
- **注意**:控制面内部证书(默认 5 年,公告 cce_bulletin_0136)与 kubeconfig 客户端证书是**不同对象**。
- 权限:权限表"获取集群证书"= `cce:cluster:get`;API 接口约束另写 `cce:cluster:generateClientCredential` → **两处来源不同,需核对**。
- **未确认(需实测)**:duration/expire_at 都不传的行为;证书到期瞬间的失败表现;重新签发是否即时生效(如需立即失效应先 revoke)。

## Q3 节点池扩缩容语义 — [官方文档] 已确认(绝对值语义),仍建议实测定稿

- **`desiredNodeCount` = 绝对值("节点池期望节点数")**,已确认(官方 API 文档 ScaleNodePool.html + SDK 注释一致):
  - 官方原文:"节点池的预期总数量。执行扩容操作时,**需将当前节点数与扩容数量相加**;执行缩容操作时,需从当前节点数中减去缩容数量。约束限制:必填参数,如果省略则默认值为 0,会导致删除节点池伸缩组下的所有节点。取值范围:0 或正整数"。
  - **解读**:"相加/相减"是教你**计算要传入的目标总数**(目标=当前数±本次伸缩数),不是 API 内部按增量处理;否则"取值范围 0 或正整数"(缩容需要负数)与"省略默认 0 会删除所有节点"(增量 0 应为无操作)都无法自洽。
  - 用户指南"本次节点数与已有节点数相加"是控制台 UX(控制台替你算目标值)。
  - **实现**:`ScaleNodePool(desiredNodeCount = 期望总数)`;仍建议实测关闭歧义(建 2 节点池传 desiredNodeCount=2 → 绝对:仍为 2;增量:变 4)。
- `scaleGroups`(官方):**必填**,默认伸缩组名 `"default"`;扩容可选多个伸缩组,缩容仅能指定单个。
- `options`:`scalableChecking`(instant/async,默认 instant)、`scalePolicy`(AZBalance/Random,默认 Random)。
- **UpdateNodePool 陷阱(官方 cce_02_0356)**:更新时 `initialNodeCount` 为**必填且默认 0**——不填时期望数默认变 0,节点数>0 时**会导致缩容**;不想动节点数必须显式设 `ignoreInitialNodeCount: true`。可更新字段:metadata.name、nodeTemplate(os/runtime/taints/k8sTags/userTags 等,**flavor/az 不允许改**)、initialNodeCount、ignoreInitialNodeCount、autoscaling、customSecurityGroups 等。
- 自动扩缩容(官方 cce_10_0209):autoscaling 在 [minNodeCount, maxNodeCount] 内触发;**手动伸缩不受该范围影响**(仍可手动扩容到 max 以上)→ 手动(CAPI)与自动可并存,无冲突报错机制记载。
- 伸缩中状态(官方 cce_02_0355):旧 phase 已废弃,建议用 `currentNode/creatingNode/deletingNode` + conditions(`Scalable`=False 时"不会再次触发节点池扩容行为")感知;**伸缩中再调 ScaleNodePool 的错误码官方未记载,需实测**。
- 补充:默认节点池不支持扩缩容(官方用户指南);通过 ECS 控制台删节点,CCE 10 分钟后自动补足"节点池期望节点个数"(官方用户指南)。

## Q4 Turbo(eni)网络模型的 VPC/子网要求 — [官方文档] 大部分确认

- eni(云原生网络 2.0):Pod 直接绑定 VPC 弹性网卡(ENI)/辅助弹性网卡(Sub-ENI),**Pod IP 来自 VPC 子网**(官方 cce_10_0284);`containerNetwork.mode=eni` 时 category 强制 Turbo。
- 参数:`eniNetwork` 必填 `subnets`(1.19.10+)或 `eniSubnetId` 之一(SDK model_eni_network.go)。
- **网段关系(官方最佳实践 cce_bestpractice_00004)**:eni 容器子网取自 VPC 子网,**可与节点子网重叠甚至相同**;硬性约束是**服务网段不能与 VPC 子网/容器子网重叠**(cce_10_0284);"容器子网与节点子网不要用同一个子网"是**建议**(避免 IP 不足),非硬性。
- eni 创建后可**新增子网、不可修改已有子网**;vpc-router 可新增网段(**最多 20 个**)、不可改已有;overlay_l2 创建后不可修改/不可扩容。
- 错误码 `CCE.01400025`(400):subeni 配额不足,虚机规格不支持 Turbo → Turbo 节点规格需 sub-ENI 配额。
- **未确认(需实测)**:官方文档**找不到**"ENI 子网需覆盖 2 个可用区"或数量下限的说法;仅见每容器网络配置最多 20/100 个子网(cce_10_0196)。

## Q5 安全组 — [官方文档] 完全确认

- **自动创建**(官方 FAQ cce_faq_00265):node 安全组 `{集群名}-cce-node-{随机ID}`、master 安全组 `{集群名}-cce-control-{随机ID}`;Turbo 额外创建 `{集群名}-cce-eni-{随机ID}`(默认给容器绑定)。
- **自定义**(官方 cce_10_0784/cce_10_0426):创建集群时可指定**自定义节点安全组**(作为默认节点安全组);**不支持指定 master(控制节点)安全组**;已创建集群可修改默认节点安全组。
- `customSecurityGroups`(节点级,官方 cce_02_0354):**只对节点池新扩容节点生效**;"建议不超过 5 个";未指定则用 node 默认安全组。**Standard 是否支持官方未标注,需实测**。
- `podSecurityGroups`(Pod 级,官方):**仅 Turbo(≥1.21)支持**,每池最多 5 个;更新后只对新 Pod 生效(需驱逐重建旧 Pod)。
- **生效范围**(官方 cce_10_0426/cce_10_1079):修改集群节点默认安全组**只对新创建/新纳管节点生效,存量节点需手动修改(重置也不换组)**;Pod 级只对新 Pod 生效。
- **API Server 公网访问控制**(官方 cce_10_0864):公网访问 = API Server 绑定 EIP;白名单 = 修改 `cce-control` 安全组 **5443 入方向源地址**(默认 0.0.0.0/0,官方"建议修改");无独立"白名单 API"参数;`publicAccess.cidrs` 仅创建时生效,默认 0.0.0.0/0。
- **未确认(需实测)**:Standard 对 `customSecurityGroups` 的支持;控制台"关联安全组创建后不可修改"与 UpdateNodePool 可更新 customSecurityGroups 的文档差异。

## Q6 IAM 权限与凭证约束 — [官方文档] 大部分确认

- 完整 action 表(官方"CCE 权限概述" cce_10_0187):集群 `cce:cluster:list/get/create/update/delete/upgrade/start/stop/resize`;节点 `cce:node:list/get/create/update/delete/remove/migrate`;节点池 `cce:nodepool:list/get/create/update/delete/scale`;证书 `cce:cluster:get`(clustercert);Job `cce:job:*`。
- **关键(子代理确认)**:`CCE FullAccess` 是系统策略但**明确不含"生成集群证书"能力**——取 kubeconfig 需 `cce:cluster:get`(或 CCE Administrator);ECS/VPC/EVS 的资源操作由系统委托(`cce_admin_trust`/`cce_cluster_agency`)代行,**AK/SK 无需配 ECS/EVS action**;企业项目授权下创建节点需额外全局权限 `evs:quotas:get`、`evs:types:get`(官方原文)。
- 官方未要求主账号:官方原文"账号具备所有 API 的调用权限,如果使用账号下的 IAM 用户调用当前 API,该 IAM 用户需具备调用 API 所需的权限" → **IAM 用户配足细粒度策略即可**。
- 委托(官方 cce_02_0236 + cce_10_0556):创建集群**必须已有系统委托**,`agencyName` 不传时自动使用 `cce_admin_trust` 或 `cce_cluster_agency`(1.21+;后者仅含 CCE 组件依赖权限)。
- **AK/SK 约束**:官方权限概述——**联邦用户不支持创建永久 AK/SK** → Provider 凭证必须来自账号或实体 IAM 用户。
- **未确认(需实测)**:同一对 AK/SK 跨多 project 调用;最小策略集是否隐式依赖其他 action。

## Q7 默认配额 — [官方文档] 部分确认(存在文档矛盾)

- 官方 FAQ(cce_faq_00154):**"CCE restricts only the number of clusters"**——平台层只限制集群个数;节点依赖 ECS/EVS/VPC/ELB/SWR 各自配额。
- 官方约束页(cce_productdesc_0005):单 Region 集群数 **50**;单集群节点规模 50/200/1000/2000(工单最大 20000);单节点最大实例 256(最大 512);单集群最大 10 万 Pod。
- **矛盾点**:创建集群 API 文档页写"默认 5 个集群/Region",与约束页 50 不一致 → **以控制台"我的配额"或 ShowQuotas/GetClusterQuota 实测值为准**。
- 配额不足错误码:400 `CCE.01400007`(集群配额不足)、`CCE.01400008~13`(ECS/CPU/内存/安全组/EIP/磁盘)、**`CCE.01400014`(节点数超规模)**、`CCE.01400020`(VPC 配额)。
- 提升流程(官方):控制台"资源 > 我的配额 > 申请扩大配额";不超自动生效值约 1 分钟后生效,否则人工复核。

## Q8 集群删除语义 — [官方文档] 重要确认(有 API 级删除选项)

- **删除为异步**:200 = "删除作业下发成功";控制台口径耗时 **1~3 分钟**(官方 cce_10_0212);删除中状态 `Deleting`,响应含 JobID 与 DeleteStatus。
- **删除选项(官方 cce_02_0241 + SDK model_delete_cluster_request.go)**:
  - `ondemand_node_policy`(按需节点):delete/reset/retain,**默认"删除按需节点,保留纳管的节点"**
  - `periodic_node_policy`(包周期节点):reset/retain
  - `delete_evs`(云硬盘)**默认 false(skip)→ 残留** ⚠️;`delete_eni` 默认 block;`delete_net`(ELB 等)默认 block;`delete_efs/delete_obs/delete_sfs/delete_sfs30` 默认 false(skip)
  - **实现必须显式传删除选项,否则 EVS/存储卷会残留。**
- **残留规则(官方 cce_10_0212)**:删除集群不会删除**包周期资源**(继续计费);集群**非运行状态(冻结/不可用)删除会残留存储、网络等关联资源**;存储按卷回收策略(Retain 则保留);ELB 仅删除自动创建的;删除后集群停止计费,但残留资源仍计费。
- **休眠中集群不能直接删**,需先唤醒;有"禁止删除集群"保护(错误码 `CCE.01400034`)。
- 包周期集群不能直接删,需退订/释放;`tobedeleted=true` 可预置删除参数。
- **未确认(需实测)**:存在节点池时直接删集群的行为;删除不存在集群的确切错误码(通用表 404 `CCE.01404001`,未在删除页逐条列出);Deleting 中重复删除。

## Q9 单节点路径(AddNode) — [官方文档/SDK] 完全确认:需重装系统,不适合免干预 CAPI Machine

- **前置要求(官方 AddNode API)**:待纳管节点须"运行中"、未被其他集群使用、无 CCE-Dynamic-Provisioning-Node 标签;**与集群同 VPC**(<1.13.10 还需同子网);挂载数据盘(≥20GiB 且无 <10GiB 数据盘);**CPU ≥2 核、内存 ≥4GiB、网卡有且仅有一个**;Turbo 要求支持 Sub-ENI 或可绑定 ≥16 张 ENI;**登录凭据(密码或密钥对)二者必选其一**;竞价实例不支持纳管。
- **纳管 = 破坏性重置**:官方原文(SDK model_add_node_list.go):"纳管过程将清理节点上系统盘、数据盘数据,并作为新节点接入 Kubernetes 集群,请提前备份迁移关键数据";用户指南:"数据会被清空"。
- **kubelet 由 CCE 自动完成**(官方 cce_10_0198):重置为标准镜像后自动安装接入,用户无需在 ECS 上执行命令;仅可选的"安装前/后执行脚本"由 CCE 代为执行;`initializedConditions` 控制 uninitialized 污点移除(可能需自定义初始化)。
- **生命周期差异(官方)**:不选池归入 **DefaultPool("不具备任何自定义节点池功能,不支持弹性伸缩/编辑/删除/迁移")**;纳管至自定义节点池后"如果节点池触发弹性伸缩策略需要缩容节点,则该节点也可能会被缩容";缩容时云硬盘随节点删除。
- **CAPI 可行性结论(基于官方文档)**:免手工命令成立,但前置约束严(规格/单网卡/数据盘/Turbo ENI)、破坏性重置、生命周期受限;官方对 CAPI 场景无表述 → **推荐只走节点池路径(与设计一致)**;若需 Machine 路径需实测/咨询。

## Q10 CCE Autopilot — [官方文档/SDK] 确认(远期)

- 官方集群类型对比:Autopilot = **Serverless 集群,无节点部署/管理,按 CPU/内存实际使用计费**,网络为云原生网络 2.0,Pod 可绑安全组/EIP/固定 IP。
- SDK 有独立 API 族(**路径前缀 `/autopilot/v3`**);**SDK 中不存在任何 Autopilot 节点/节点池 API**(无 CreateAutopilotNodePool)→ 天然"无节点集群",与 Fargate 类似。
- 官方约束(子代理抓取 Autopilot 约束页):每区域每账号集群总数 50、默认最多 1000 Pod、不支持 HostPath/HostNetwork/NodePort/DaemonSet/ARM 镜像等。
- **官方无 CAPI/声明式对接说明** → CAPI 对接方式需自行设计(远期 P2,Cluster + 无 MachinePool)。

## Q11 集群升级 — [官方文档/SDK] 大部分确认

- 升级编排(官方 API/SDK):`CreateUpgradeWorkFlow`(WorkFlowSpec.`TargetVersion` 必填)→ `CreatePreCheck` → `UpgradeCluster`(targetVersion 只能填更高版本;**策略仅 `inPlaceRollingUpdate` 原地升级**)→ `CreatePostCheck`;任务支持 Pause/Retry/Continue。
- 升级期间:集群状态 `Upgrading`;**控制面升级期间暂停节点池弹性伸缩**,API Server 访问短暂中断;节点由 CCE **分批原地升级**(默认每批 20、最高 120,升级时节点不可调度)。
- 失败处理:可重试;可按备份回滚(升级成功后做过其他操作则无法回滚)。
- 升级路径:官方有明确路径表(如 v1.23→v1.25/v1.27/v1.28;v1.13 及以下不支持;停止维护版本需连续多次升级);**补丁版本可一次直升最新**。
- **未确认(需实测)**:升级总体耗时量级;仅升 platformVersion 不升 K8s 版本的 API 行为。

## Q12 计费与休眠/唤醒 — [官方文档] 大部分确认

- `billingMode`(SDK model_cluster_spec.go):"0: 按需计费;1: 包周期;**默认为按需计费**"。
- 包周期参数(SDK model_cluster_extend_param.go):`periodType`(month/year)与 `periodNum`(month 取 [1-9]、year 取 [1-3])在 `billingMode=1` 时**必填**;isAutoRenew/isAutoPay 默认不自动。
- **休眠(HibernateCluster,官方 cce_02_0374)**:休眠后"不再收取控制节点资源费用";"节点、绑定的弹性 IP、带宽等资源按各自计费方式继续收费";休眠中不可创建/管理工作负载。
- **唤醒(AwakeCluster,官方 cce_02_0375)**:200="唤醒任务下发成功,需持续查询集群状态,当集群状态变为 Available 后表示唤醒成功";预计 **3~5 分钟**;可能因资源不足导致唤醒失败(需稍后再试);phase 含 Hibernating/Hibernation/Awaking。
- 停止计费(官方 price-cce):节点关机后不再收取基础资源(vCPU/内存)费用,但云硬盘/带宽仍计费 → 要彻底省钱需删除集群及关联资源。
- **未确认(需实测)**:唤醒失败的错误码;包周期集群是否禁止休眠(用户指南标题为"休眠/唤醒按需计费集群");具体价格以价格页/工单为准。

## Q13 管理集群 → CCE API Server 网络路径 — [官方文档] 大部分确认

- **公网**:API Server 绑定 EIP,地址形态 **`https://<EIP>:5443`**(端口 5443 官方确认,cce_10_0864);创建时 `publicAccess.cidrs` 白名单**仅创建时生效**,默认 `0.0.0.0/0`;之后访问控制靠 cce-control 安全组 5443 入方向规则(见 Q5)。
- **私网**:kubeconfig current-context=internal,server=**`https://<VPC内网IP/VIP>:5443`**(官方响应示例 `https://192.168.1.7:5443`);ShowClusterEndpoints 官方原文"PrivateIP(HA 集群返回 VIP)"→ 私网地址仅 VPC 内可达(须同 VPC,cce_10_0107)。
- **跨 VPC(官方推荐)**:同区域用 **VPC 对等连接**(两端网段不可重叠、需双向加路由,cce_bestpractice_10044)、**云专线**、**VPN**(VPN 网段不能与 VPC/容器网段冲突);**跨 Region 官方无专门推荐 → 需咨询/实测**。
- endpoint 结构(SDK model_cluster_endpoints.go):url + type(public/private)。

## Q14 API 限流与错误码全集 — [官方文档] 重大确认(错误码表公开)

- **官方错误码参考公开**(support.huaweicloud.com/api-cce/ErrorCode.html,64+ 条)。与本 Provider 最相关的:
  - 400:`CCE.01400001`(请求不合法)、`CCE.01400002`(未在 VPC 中找到子网)、`CCE.01400005`(容器网络网段冲突)、`CCE.01400007`(集群配额不足)、`CCE.01400008`(ECS 配额不足)、`CCE.01400009/10`(CPU/内存配额)、`CCE.01400011`(安全组配额)、`CCE.01400012`(EIP 配额)、`CCE.01400013`(磁盘配额)、`CCE.01400020`(VPC 配额)、`CCE.01400025`(subeni 配额,规格不支持 Turbo)
  - 403:`CCE.01403001`(权限不足)、`CCE.01403003~09`(节点池/集群状态不允许删除、扩容中不允许删除等)
  - 404:`CCE.01404001`(Resource not found,通用)
  - 409:`CCE.01409001`(冲突)
  - 429:`CCE.01429002`(资源被其他请求锁定)、`CCE.01429003`(已达并发任务上限)、`CCE.02429001`(达到最大请求数)
- **限流机制(子代理确认)**:CCE 未公开 QPS 数值;限流表现为 429 + **`APIGW.0308`("超出流控值限制",APIGW 默认每个 API 每秒最多 200 次,云服务开放 API 一般无法调整)** + CCE 自身 429 码。
- 错误体格式:`{"errorCode":…,"errorMessage":…}`;SDK 结构 `sdkerr.ServiceResponseError{StatusCode, RequestId, ErrorCode, ErrorMessage, EncodedAuthorizationMessage}`。
- 分页:ListClusters 无分页;ListNodes limit 1-2000(默认 2000)+ marker;ListNodePools 无分页;配额查询 `ShowQuotas`/`GetClusterQuota`。
- **未确认(需实测)**:429 是否带 Retry-After;CCE 管理面具体 QPS 阈值。
- 已按此更新 PoC `internal/services/errors/errors.go`(修正原先占位的 not-found 码,新增 Quota/Throttle 分类)。

---
## 汇总:14 项确认状态

| # | 主题 | 官方结论 | 剩余需实测/咨询 |
|---|---|---|---|
| Q1 | 空集群创建 | ✅ 官方原文"创建空集群(只有 Master 无 Node)";空集群照常计费;flavor 上限 CCE.01400014 | 空集群最终 phase;Available 前调节点池的错误码 |
| Q2 | kubeconfig | ✅ 有效期 -1/[1,1827];external/internal 按 publicIp;吊销立即失效 | 不传 duration 的行为;重新签发是否即时生效;证书授权项两处来源不一致 |
| Q3 | 扩缩容 | ✅ **desiredNodeCount=绝对值(期望总数)**;scaleGroups 必填 default;UpdateNodePool 不填 initialNodeCount 会缩到 0 | 绝对值/增量实测关闭;伸缩中再伸缩的错误码 |
| Q4 | eni 网络 | ✅ eni 容器子网可取 VPC 子网(可与节点子网重叠);硬约束=服务网段不重叠;eni 可增不可改 | "ENI 子网覆盖 2 AZ"类说法无官方依据 |
| Q5 | 安全组 | ✅ 自动建 node/control/eni SG;podSG 仅 Turbo 每池≤5;改 SG 只对新建节点生效;5443 白名单=改 control SG | Standard 对 customSecurityGroups 支持 |
| Q6 | IAM 权限 | ✅ 细粒度 action 表;FullAccess 不含生成证书;委托代行 ECS/VPC;联邦用户无永久 AK/SK | 跨 project;最小策略隐式依赖 |
| Q7 | 配额 | ✅ CCE 只限集群数;约束页 50/Region(API 页写 5,矛盾);错误码 CCE.01400007 等 | 以控制台实测值为准 |
| Q8 | 删除 | ✅ 异步 1~3 分钟;delete_evs 默认残留、delete_eni/net 默认删;ondemand_node_policy 默认删按需节点保留纳管节点;休眠中不可删 | 存在节点池时直接删集群;删除不存在集群的错误码 |
| Q9 | 单节点 | ✅ AddNode=重装 ECS(清数据)+严格前置(≥2C4G/单网卡/数据盘);CCE 自动安装;DefaultPool 无弹性 | CAPI Machine 路径取舍 |
| Q10 | Autopilot | ✅ Serverless 无节点 API;50 集群/区域;按 CPU/内存计费 | CAPI 对接方式(远期) |
| Q11 | 升级 | ✅ 仅原地升级(inPlaceRollingUpdate);TargetVersion 必填;升级中暂停节点池伸缩;补丁可直升 | 耗时量级;仅升 platformVersion 行为 |
| Q12 | 计费/休眠 | ✅ billingMode 默认按需;休眠停控制节点费用、节点/EIP 照常;唤醒 3~5 分钟 | 包周期是否禁休眠;唤醒失败错误码 |
| Q13 | 网络路径 | ✅ 公网 https://EIP:5443;私网 VPC 内 IP/VIP;跨 VPC=对等/专线/VPN | 跨 Region 方案 |
| Q14 | 限流/错误码 | ✅ 错误码表公开(404=01404001、429=01429002/003、配额 01400007 系);限流 APIGW.0308 每 API 每秒 200 次 | QPS 具体值;Retry-After |

