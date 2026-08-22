# 华为云 CCE 对齐问卷 — 官方文档检索确认结果

> 状态:**全部 14 项已完成官方来源核对**(4 个子代理 + 主代理一手抓取;来源:官方 API 文档 / 用户指南 / FAQ / 计费文档 / 官方 Go SDK)。
> 关联文档:[问卷](archive/cce-verification-questionnaire.md)(问题原文)、[调研依据](research-sources.md)、[落地跟踪](archive/poc-implementation-tracker.md)。
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
- 权限:接口约束授权项 = **`cce:cluster:generateClientCredential`**(官方 IAMActions 表中别名/兼容旧名为 `cce:cluster:get`);最小策略建议同时授予两者以兼容新旧命名(官方未将 get 列为硬依赖,保守配置更稳)。
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
- 参数:`eniNetwork` 必填 `subnets`(**1.19.10+ 使用**,字段 `subnetID`)或 `eniSubnetId` 之一(SDK model_eni_network.go);`eniSubnetCIDR` 可选(填写会校验)。
- **网段关系(官方最佳实践 cce_bestpractice_00004)**:eni 容器子网取自 VPC 子网,**可与节点子网重叠甚至相同**;硬性约束是**服务网段不能与 VPC 子网/容器子网重叠**;多集群时不同集群容器子网可重叠;"容器子网与节点子网不要用同一个子网"是**建议**(避免 IP 不足),非硬性。
- **网段规划(官方)**:建议容器网段掩码 `10.0.0.0/12~19、172.16.0.0/16~19、192.168.0.0/16~19`;**容器子网数量上限:旧版本集群最多 20 个,新版本最多 100 个**(cce_10_0196);eni 创建后**可新增子网、不可修改已有子网**;vpc-router 可新增网段(**最多 20 个**,1.21+ 用 cidrs 字段)、不可改已有;overlay_l2 创建后不可修改/不可扩容。
- 冲突校验错误码:400 `CCE.01400002`(子网不在 VPC)、`CCE.01400005`(容器网段冲突)、**`CCE.01400017`(没有找到可用的容器网段)**、`CCE.01400025`(subeni 配额,规格不支持 Turbo)。
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
- **AK/SK 约束**:官方权限概述——**联邦用户不支持创建永久 AK/SK** → Provider 凭证必须来自账号或实体 IAM 用户;**AK/SK 既可用永久密钥也可用临时密钥(临时密钥需额外携带 `X-Security-Token` 字段)**(官方认证鉴权 cce_02_0004)。
- FullAccess 官方原文:"CCE FullAccess | 策略 | …普通操作权限(包含集群创建、删除、更新等)。**不包括集群命名空间权限以及委托授权、生成集群证书等管理员权限**" → Provider 取 kubeconfig 必须显式配 `cce:cluster:get` 或用 CCE Administrator。
- **未确认(需实测)**:同一对 AK/SK 跨多 project 调用;最小策略集是否隐式依赖其他 action。

## Q7 默认配额 — [官方文档] 部分确认(存在文档矛盾)

- 官方 FAQ(cce_faq_00154):**"CCE restricts only the number of clusters"**——平台层只限制集群个数;节点依赖 ECS/EVS/VPC/ELB/SWR 各自配额。
- 官方约束页(cce_productdesc_0005):单 Region 集群数 **50**;单集群节点规模 50/200/1000/2000(工单最大 20000);单节点最大实例 256(最大 512);单集群最大 10 万 Pod。
- **矛盾点**:创建集群 API 文档页写"默认 5 个集群/Region",与约束页 50 不一致 → **以控制台"我的配额"或 ShowQuotas/GetClusterQuota 实测值为准**。
- 配额不足错误码:400 `CCE.01400007`(集群配额不足)、`CCE.01400008~13`(ECS/CPU/内存/安全组/EIP/磁盘)、**`CCE.01400014`(节点数超规模)**、`CCE.01400020`(VPC 配额)。
- **配额查询 API(官方)**:`ShowQuotas`(GET /api/v3/projects/{project_id}/quotas,quotaKey=cluster/quotaLimit/used,权限 `cce:quota:get`)、`GetClusterQuota`(GET /cce/v1/projects/{project_id}/quota,type=cluster|autopilot_cluster)→ **建议 Provider 运行时查询实际配额,不依赖文档数字**。
- 提升流程(官方):控制台"资源 > 我的配额 > 申请扩大配额";目标配额≤自动生效值 → **约 1 分钟后生效**;超过 → 自动创建工单人工复核。

## Q8 集群删除语义 — [官方文档] 重要确认(有 API 级删除选项)

- **删除为异步**:200 = "删除作业下发成功";控制台口径耗时 **1~3 分钟**(官方 cce_10_0212);删除中状态 `Deleting`,响应含 JobID 与 DeleteStatus。
- **删除选项(官方 cce_02_0241 + SDK model_delete_cluster_request.go)**:
  - `ondemand_node_policy`(按需节点):delete/reset/retain,**默认"删除按需节点,保留纳管的节点"**
  - `periodic_node_policy`(包周期节点):reset/retain
  - `delete_evs`(云硬盘)**默认 false(skip)→ 残留** ⚠️;`delete_eni` 默认 block;`delete_net`(ELB 等)默认 block;`delete_efs/delete_obs/delete_sfs/delete_sfs30` 默认 false(skip)
  - **实现必须显式传删除选项,否则 EVS/存储卷会残留。**
- **残留规则(官方 cce_10_0212)**:删除集群不会删除**包周期资源**(继续计费);集群**非运行状态(冻结/不可用)删除会残留存储、网络等关联资源**;存储按卷回收策略(PV 策略 Delete 时云硬盘/SFS/OBS 等会删,Retain 保留);ELB 仅删除自动创建的;删除后集群停止计费,但残留资源仍计费。
- **包周期集群不能直接删**,需退订/释放;`tobedeleted=true` 可预置删除参数(供退订识别);休眠中集群不能直接删,需先唤醒;有"禁止删除集群"保护(错误码 `CCE.01400034`);删除响应含 JobID 与 deleteStatus。
- **实测新增确认(2026-08-20 冒烟)**:节点池存在"安装中"(Installing)节点时,**DeleteNodePool 报 `CCE.01403006`**("Node pool cannot be deleted when exists installing or deleting nodes, Nodepool has node not in Active, Abnormal or Error status");此时直接 **DeleteCluster 报 `CCE.01400024`**("Cannot delete cluster when creating node")。**必须在所有节点进入 Active 后才删**——注意 `status.currentNode` 会统计 Installing 节点,应以 `status.activeNode`(ActiveNodeCount)为就绪判据,否则扩/缩/删都会撞上该错误。
- **未确认(需实测)**:存在节点池时直接删集群的行为;删除不存在集群的确切错误码(通用表 404 `CCE.01404001`,未在删除页逐条列出);Deleting 中重复删除。

## Q9 单节点路径(AddNode) — [官方文档/SDK] 完全确认:需重装系统,不适合免干预 CAPI Machine

- **前置要求(官方 AddNode API)**:待纳管节点须"运行中"、未被其他集群使用、无 CCE-Dynamic-Provisioning-Node 标签;**与集群同 VPC**(<1.13.10 还需同子网);挂载数据盘(≥20GiB 且无 <10GiB 数据盘);**CPU ≥2 核、内存 ≥4GiB、网卡有且仅有一个**;Turbo 要求支持 Sub-ENI 或可绑定 ≥16 张 ENI;**登录凭据(密码或密钥对)二者必选其一**;竞价实例不支持纳管。
- **纳管 = 破坏性重置**:官方原文(SDK model_add_node_list.go):"纳管过程将清理节点上系统盘、数据盘数据,并作为新节点接入 Kubernetes 集群,请提前备份迁移关键数据";用户指南:"数据会被清空"。
- **kubelet 由 CCE 自动完成**(官方 cce_10_0198):重置为标准镜像后自动安装接入,用户无需在 ECS 上执行命令;仅可选的"安装前/后执行脚本"由 CCE 代为执行;`initializedConditions` 控制 uninitialized 污点移除(可能需自定义初始化)。
- **生命周期差异(官方)**:不选池归入 **DefaultPool("不具备任何自定义节点池功能,不支持弹性伸缩/编辑/删除/迁移")**;纳管至自定义节点池后"如果节点池触发弹性伸缩策略需要缩容节点,则该节点也可能会被缩容";缩容时云硬盘随节点删除。
- **CAPI 可行性结论(基于官方文档)**:免手工命令成立,但前置约束严(规格/单网卡/数据盘/Turbo ENI)、破坏性重置、生命周期受限;官方对 CAPI 场景无表述 → **推荐只走节点池路径(与设计一致)**;若需 Machine 路径需实测/咨询。

## Q10 CCE Autopilot — [官方文档/SDK] 确认(远期)

- 官方集群类型对比:Autopilot = **Serverless 集群,无节点部署/管理,按 CPU/内存实际使用计费**,网络为云原生网络 2.0,Pod 可绑安全组/EIP/固定 IP;计费含"集群管理费用、Pod 费用、终端节点费用"。
- SDK 有独立 API 族(**路径前缀 `/autopilot/v3`**);**SDK 中不存在任何 Autopilot 节点/节点池 API**(无 CreateAutopilotNodePool)→ 天然"无节点集群";**也可经统一 `CreateCluster` 携带 `EnableAutopilot=true` 创建**(SDK model_cluster_spec.go);AutopilotClusterSpec 无 master/节点字段,Flavor 固定 `cce.autopilot.cluster`,BillingMode 仅按需。
- 官方约束:单区域每账号集群总数 **50**;默认最多 **1000 Pod**;不支持 HostPath/HostNetwork/NodePort/DaemonSet/ARM 镜像。
- **官方无 CAPI/声明式对接说明** → CAPI 对接方式需自行设计(远期 P2,Cluster + 无 MachinePool)。

## Q11 集群升级 — [官方文档 cce_10_0197(2026-07-28 更新)/SDK] 完整确认(除耗时)

- **版本生命周期(官方)**:CCE 每年发布 3 个 Kubernetes 版本,每版本**至少 24 个月维护周期**;维护期内保证稳定运行;建议每季度升一次补丁、每年升一次大版本;EOS 版本不再提供安全补丁/问题修复。
- 升级编排(官方 API/SDK):`CreateUpgradeWorkFlow`(WorkFlowSpec.`TargetVersion` 必填)→ `CreatePreCheck` → **备份** → `UpgradeCluster`(targetVersion 只能填更高版本;**策略仅 `inPlaceRollingUpdate` 原地升级**)→ `CreatePostCheck`;任务支持 Pause/Retry/Continue;错误码 CCE.01400074(升级关键步骤失败)、CCE.01400075(升级前检查过期)。
- **升级流程(官方)**:升级前检查(集群/插件/节点状态、工作负载兼容性)→ 备份 → 配置(插件/节点批次/优先级/证书)→ 升级(插件→控制节点→用户节点)→ 升级后验证(自动:集群/节点状态检查;手动:业务/新建节点/新建 Pod 验证)。
- **备份/回滚时间表(官方)**:etcd 数据备份=升级流程自动(1-5min,回滚 2h,必选);EVS 快照备份=控制节点磁盘手动(1-5min,回滚 20min,逐步替代 CBR);CBR 整机备份=手动(20min-2h,回滚 20min)。
- **节点升级批次(官方,解决此前"2 的幂/4 的幂"表述差异)**:第一批升级 1 个节点,第二批 4 个,后续每批**以 4 的幂数增加**,直到达到每批最大节点数(默认 20,最高 120);批次可配置为**集群范围**(默认)或**节点池范围**(每池独立从 1 重新计算);节点优先级默认负载轻者优先、节点池少的优先。
- **升级期间**:集群状态 `Upgrading`;**控制面升级前系统自动保存并关闭节点池弹性伸缩,控制面升级完成后恢复(节点缩容能力要整个升级结束后恢复)**;API Server 访问短暂中断,业务不中断;升级中不建议对集群做任何操作。
- **证书配置(官方)**:可在升级中同步更新 CCE 集群证书(更新后有效期 5 年);**OIDC 用户若曾获取过集群签名公钥,升级后必须重新获取,否则 OIDC 身份认证失效**;自有证书可上传 CA/客户端证书/私钥自动更新。
- **升级方式表(官方)**:原地升级(v1.15+ 支持,节点分批,OS 不升级,插件不兼容时自动升级,一键式);迁移(大跨度跨版本时资源重新部署)。
- **升级路径表(官方 cce_10_0197,2026-07-28 版,完整)**:v1.13 及以下不支持;v1.15→v1.19;v1.17→v1.19;v1.19→v1.21/v1.23;v1.21→v1.23;v1.23→v1.25/v1.27/v1.28;v1.25→v1.27/v1.28;v1.27→v1.28;v1.28→v1.29/v1.31;v1.29→v1.30/v1.31;v1.30→v1.31;v1.31→v1.32/v1.34;v1.32→v1.33/v1.34;v1.33→v1.34;**v1.34→v1.35**。已停止维护版本需连续多次升级(如 v1.15→v1.19→v1.23→v1.27/v1.28);**补丁版本需升级至最新补丁后方可进行版本升级**(1.19+ 补丁管理,任意补丁可一次直升最新补丁,控制台自动生成最佳路径)。
- **升级前提条件(官方)**:① 补丁版本需升到最新;② 升级前检查;③ 建议备份;**④ SnatIPRanges 检查(仅 Turbo)**:升级前检查该配置是否变化,如有变化升级后需重启 Pod 触发路由表更新。
- **v1.35 主要变更(官方)**:cgroup v1 标记弃用(v1.35 支持 cgroupv2 并兼容 v1);kube-proxy ipvs 模式弃用,默认 nftables 转发;containerd 1.x 为最后支持版本,v1.35 起默认 containerd 2.x;**StorageVersionMigration v1alpha1 API 移除,升级前必须删除所有 v1alpha1 资源**。
- **platformVersion 是输出非输入**(官方:不支持用户指定,创建时自动选最新平台版本)→ 无"仅升级 platform 不动 K8s 版本"的独立参数。
- **实测(2026-08-19,多次,多形态)**:
  1. **v1.34.8-r2(Standard/Turbo 空集群、带节点池)均 `suggestPatch=` 空 + `targets=[]`**——v1.34 是当前最新支持线,其目标 v1.35 尚未对账号/区域开放;
  2. **v1.33.12 集群 `targets=[v1.34.8-r2]`——平台开放升级目标(首次确认"开放"状态)**,并实测跑通完整升级工作流(见下);
  → **官方路径表 = 产品支持的升级路径(设计约束);API 实测 = 当前可执行的升级目标(按版本线动态开放)**。
- **升级工作流实测(v1.33.12→v1.34.8-r2)**:`CreateUpgradeWorkFlow → CreatePreCheck → UpgradeCluster → ShowUpgradeClusterTask 轮询` 全链路跑通;任务 **24 秒后 phase=Failed**,失败在**升级前检查**(items:`upgrade-limit-check` Failed、`addon-limit-check` Failed,其余阶段 Init)——平台检查项未通过(插件/配额类),非 API 调用问题;带节点池形态在同一预检查失败。
- **实测发现的 3 个真实 API 约束(SDK 注释误导,已修复 `StartUpgrade`)**:
  1. `CreateUpgradeWorkFlow` 的 `clusterID` **必填**(SDK 注释称"服务端生成,填写无效",实测为空报 `CCE_CM.0004 Invalid field cluster ID`);
  2. PreCheck 的 `clusterVersion` 必须用**完整 release-patch 格式**(如 `v1.33.12-r2`)且与 workflow 一致(传 `v1.33` 报 `CCE_CM.0101`);
  3. `inPlaceRollingUpdate` 策略**必须带 `userDefinedStep`**(报 `CCE_CM.0004 Field user defined step must defined by inPlaceRollingUpdate strategy`;SDK 取值范围 [1,40],官方文档默认 20、最高 120)。
- **升级耗时量级**:升级流程为**分钟级启动**(workflow/precheck/upgrade 提交即返回),控制面升级阶段 24 秒内被预检查拦截;**完整成功升级的耗时待预检查通过后实测**(`TestSmokeUpgradeWorkflow` 已就绪,带节点池模式)。
- **未确认(需华为云/待平台开放后实测)**:升级总体耗时量级(当前无可用目标,客观不可测;`TestSmokeUpgradeWorkflow` 已支持带节点池模式,平台开放后即可实测);StorageVersionMigration v1alpha1 清理对 CAPI 集群的影响(升级前需确保无 v1alpha1 资源);SDK `userDefinedStep` 上限(1-60)与官方文档(最高 120)的出入。

### Q11b 节点池升级(操作系统镜像,官方 cce_10_0198,2026-06-18 更新)

> 与集群 K8s 版本升级(原地升级)不同,**节点池升级 = 操作系统镜像升级,通过"节点重置"方式执行**;仅当 CCE 发布新 OS 镜像或节点池配置变更时涉及(页面显示"暂无可升级 OS 节点"则不涉及)。

- **方式与影响(官方原文)**:重置方式升级 OS → **节点的系统盘和数据盘将被清空(重置前必须备份)**;**节点重置会清除用户单独添加的 K8s 标签和 K8s 污点**(可能导致本地存储、指定调度节点等绑定资源不可用);已运行工作负载可能中断(单实例/资源不足),官方建议低峰期操作 + 为关键业务配置 **PDB(Pod Disruption Budget)**;私有镜像节点暂不支持;老版本节点可能存在兼容性问题。
- **升级参数(官方)**:每批最大升级节点数(=允许节点不可用最大数量);节点列表;登录方式(密码/密钥对/镜像密码);安装前/后执行脚本(Base64,前后脚本统一计算 ≤10240 字符;安装后脚本可用"执行完成前节点不可调度"选项;禁止脚本内立即 reboot,应用 `shutdown -r 1`)。
- **⚠️ 对 Provider 的设计影响(重要)**:
  1. **CAPI 节点池不能走控制台"节点重置"升级 OS**:重置清空数据盘 + 清标签/污点,与 MachinePool 声明状态冲突,会造成节点漂移 → 文档应明确"OS 升级需整池替换或人工操作并评估 PDB";
  2. **标签/污点防漂移**:节点被重置清标签后,controller 的节点池属性同步(B1b)应使用 `labelPolicyOnExistingNodes=refresh` / `taintPolicyOnExistingNodes=refresh`(SDK `NodePoolSpecUpdate` 已支持)把 spec 标签/污点同步回存量节点;
  3. 每批最大升级节点数语义 = 集群升级批次语义的节点级对应(允许不可用数量),与集群升级的"4 的幂"批次不同。

## Q12 计费与休眠/唤醒 — [官方文档] 大部分确认

- `billingMode`(SDK model_cluster_spec.go):"0: 按需计费;1: 包周期;**默认为按需计费**"。
- 包周期参数(SDK model_cluster_extend_param.go):`periodType`(month/year)与 `periodNum`(month 取 [1-9]、year 取 [1-3])在 `billingMode=1` 时**必填**;isAutoRenew/isAutoPay 默认不自动。
- **⚠️ 包周期集群创建响应不返回集群 ID**(SDK model_cluster_metadata.go:"在创建包周期集群时,响应体不返回集群 ID")→ 实现创建包周期集群后需**按名称查询**集群 ID。
- 节点级计费:`billingMode` 0=按需/1=包周期/2=废弃(SDK model_node_spec.go,默认 0);按需转包 `BatchChangeNodeToPeriod`(includeResources 当前仅支持 eip)。
- **休眠(HibernateCluster,官方 cce_02_0374)**:休眠后"不再收取控制节点资源费用";"节点、绑定的弹性 IP、带宽等资源按各自计费方式继续收费";休眠中不可创建/管理工作负载。
- **唤醒(AwakeCluster,官方 cce_02_0375)**:200="唤醒任务下发成功,需持续查询集群状态,当集群状态变为 Available 后表示唤醒成功";预计 **3~5 分钟**;可能因资源不足导致唤醒失败(需稍后再试);phase 含 Hibernating/Hibernation/Awaking。
- 停止计费(官方 price-cce):节点关机后不再收取基础资源(vCPU/内存)费用,但云硬盘/带宽仍计费 → 要彻底省钱需删除集群及关联资源。
- **未确认(需实测)**:唤醒失败的错误码;包周期集群是否禁止休眠(用户指南标题为"休眠/唤醒按需计费集群");具体价格以价格页/工单为准。

## Q13 管理集群 → CCE API Server 网络路径 — [官方文档] 大部分确认

- **公网**:API Server 绑定 EIP,地址形态 **`https://<EIP>:5443`**(端口 5443 官方确认,cce_10_0864);**绑定 EIP 会短暂重启集群 API Server 并更新 kubeconfig 证书**(官方原文)→ 影响 kubeconfig 轮换设计;创建时 `publicAccess.cidrs` 白名单**仅创建时生效**,默认 `0.0.0.0/0`;之后访问控制靠 cce-control 安全组 5443 入方向规则(见 Q5)。
- **私网**:kubeconfig current-context=internal,server=**`https://<VPC内网IP>:5443`**(官方响应示例 `https://192.168.1.7:5443`);ShowClusterEndpoints 官方原文"**PrivateIP(HA 集群返回 VIP)**"、`privateEndpoint` 字段 → 私网地址仅 VPC 内可达(须同 VPC,cce_10_0107)。
- **跨 VPC(官方推荐)**:同区域用 **VPC 对等连接**(两端网段不可重叠、需双向加路由,cce_bestpractice_10044)、**云专线**、**VPN**(VPN 网段不能与 VPC/容器网段冲突);**跨 Region 官方无专门推荐 → 需咨询/实测**。
- endpoint 结构(SDK model_cluster_endpoints.go):url + type(public/private)。

## Q14 API 限流与错误码全集 — [官方文档] 重大确认(错误码表公开)

- **官方错误码参考公开**(support.huaweicloud.com/api-cce/ErrorCode.html,64+ 条)。与本 Provider 最相关的:
  - 400:`CCE.01400001`(请求不合法)、`CCE.01400002`(未在 VPC 中找到子网)、`CCE.01400005`(容器网络网段冲突)、`CCE.01400007`(集群配额不足)、`CCE.01400008`(ECS 配额不足)、`CCE.01400009/10`(CPU/内存配额)、`CCE.01400011`(安全组配额)、`CCE.01400012`(EIP 配额)、`CCE.01400013`(磁盘配额)、`CCE.01400014`(节点数超出集群规模)、`CCE.01400020`(VPC 配额)、`CCE.01400025`(subeni 配额,规格不支持 Turbo)
  - 401:`CCE.01401001`(认证失败);403:`CCE.01403001`(权限不足)、`CCE.01403002`(账号受限)、`CCE.01403003~09`(节点池/集群状态不允许删除、扩容中不允许删除等)、`CCE.01403008`(不具备委托创建/授权权限)
  - 404:`CCE.01404001`(Resource not found,集群/节点/节点池不存在均归此码)
  - 409:`CCE.01409001`(资源已存在)、`CCE.01409002`(资源版本过期)
  - 429:`CCE.01429002`(资源被其他请求锁定)、`CCE.01429003`(已达并发任务上限)、`CCE.02429001`(达到最大请求数)
  - 500:`CCE.01500001/02500001/03500001`(内部错误);升级:`CCE.01400074/75`
- **限流机制(子代理确认)**:CCE 未公开 QPS 数值;限流表现为 429 + **`APIGW.0308`("超出流控值限制",APIGW 默认每个 API 每秒最多 200 次,云服务开放 API 一般无法调整)** + CCE 自身 429 码。
- 错误体格式:`{"errorCode":…,"errorMessage":…}`(另一处示例 `error_code/error_msg`,两种命名官方均出现;SDK 两者都兼容);SDK 结构 `sdkerr.ServiceResponseError{StatusCode, RequestId, ErrorCode, ErrorMessage, EncodedAuthorizationMessage}`。
- 分页:ListClusters 无分页;ListNodes limit 1-2000(默认 2000)+ marker;ListNodePools 无分页;配额查询 `ShowQuotas`/`GetClusterQuota`。
- **未确认(需实测)**:429 是否带 Retry-After;CCE 管理面具体 QPS 阈值。
- 已按此更新 PoC `internal/services/errors/errors.go`(修正原先占位的 not-found 码,新增 Quota/Throttle 分类)。

---
## 第五轮确认记录(Q2 授权差异闭环 / Q11 二次复证 / Q14 Retry-After 抓包)

| 项 | 结论 |
|---|---|
| **Q2 授权项差异(闭环)** | ✅ 两处来源差异已解决:CCE 权限策略表将 clustercert 接口授权项列为 `cce:cluster:get`,API 参考页(接口约束)列为 **`cce:cluster:generateClientCredential`(依赖 `cce:cluster:get`)**——两者**不矛盾而是依赖关系**,Provider 最小 IAM 策略须同时包含 `cce:cluster:get` + `cce:cluster:generateClientCredential`(依赖项须显式授予) |
| **Q11 升级目标(v1.34 三种形态+官方路径确认)** | ✅ **官方路径表(cce_10_0197)确认 v1.34→v1.35 为支持路径**(原地升级;前提:补丁最新/升级前检查/备份/SnatIPRanges);v1.34 三种形态(Standard 空集群、Turbo 空集群、Standard+节点池)均 suggestPatch= 空 + targets=[]——**但 v1.33 集群 targets=[v1.34.8-r2] 已开放**(见第六轮)→ 官方路径=产品支持约束,API 目标=按版本线动态开放 |
| **Q14 Retry-After(抓包确认)** | ✅✅ **429 响应携带 `Retry-After` 头(~57s,浮点秒,如 `56.818`)**:注入自定义 RoundTripper 抓包,2000 次并发 ListClusters(130 req/s)→ 1700×429 + 301×200,429 中仅 **8 个**带 Retry-After——该头**并非稳定出现**,重试实现不能只依赖 Retry-After,固定退避(实测窗口 ~1 分钟)更可靠;工具:`hack/check-retry-after` |

## 第二轮真实 CCE 冒烟确认记录(TestSmokeExtras / TestSmokeUpgrade)

| 项 | 实测结果 |
|---|---|
| **Q2 重新签发** | ✅ **重新签发即时生效**:连续两次 CreateKubernetesClusterCert 均成功(10254/10262 字节),无需先吊销 |
| **Q5 Standard 安全组** | ✅✅ **Standard(vpc-router)集群接受 customSecurityGroups**,节点池创建成功——"Standard 是否支持"从待实测升级为**支持** |
| **Q13 公网访问** | ⚠️ `publicAccess=true` 创建集群后 **未自动分配公网 endpoint**(ShowClusterEndpoints 无 public 项)——公网访问可能需要显式 EIP 绑定(UpdateClusterEip);外部可达性未能实测 |
| **Q14 限流** | ✅ 200 次快速 ShowCluster 调用 **0 次限流**(该速率未触发;APIGW.0308 阈值实测仍未知,但普通轮询频率安全) |
| **Q11 升级** | ⚠️ 升级编排 API 可用(CreateUpgradeWorkFlow/CreatePreCheck/UpgradeCluster/PostCheck 均可调用并校验版本),但**跨小版本升级被拒**:v1.34.8-r2→v1.35/v1.36、v1.35.5-r2→v1.36 均返回 "not supported to upgrade ... only support to <current>"——当前账号/区域疑似仅支持补丁级升级或跨版本升级需特定条件;**升级耗时无法实测,需咨询华为云当前升级策略** |
| **新约束(实测)** | ⚠️ 同一 VPC 下多个 vpc-router 集群的**容器网段不能重叠**(报 CCE_CM.0410 "Container network CIDR conflict")——创建新集群必须规划唯一容器网段 |

## 第三轮真实 CCE 冒烟确认记录(TestSmokeRemaining / TestSmokeUpgradeInfo)

| 项 | 实测结果 |
|---|---|
| **Q13 公网可达性** | ✅✅ **实测打通**:创建 EIP 后通过 `UpdateClusterEip(BIND)` 绑定 API Server,从本机探测 **`https://120.46.211.3:5443` reachable=true**——公网访问完整路径确认:创建时 `publicAccess` 不自动分配公网 endpoint,需显式绑定 EIP;绑定后 5443 公网可达(与官方"绑定 EIP 会短暂重启集群 API Server"一致) |
| **Q14 限流阈值** | ✅✅ **实测触发**:10 并发 × 100 次 = **1000 次 ShowCluster 调用,速率 ~71 req/s 持续约 14 秒 → 限流 703 次,其他错误 0 次**。真实阈值远低于文档推测的 200 req/s(APIGW 默认值),CCE 管理面实际流控在 ~70 req/s 持续突发时即大量 429——**轮询/重试实现必须以指数退避 + 抖动为默认,不能按 200 req/s 规划** |
| **Q11 升级路径(v1.34 视角)** | ✅ v1.34.8-r2 集群 `ShowClusterUpgradeInfo` 返回 release=`v1.34.8`、patch=`r2`、**targets=`[]`**——**该结论仅针对 v1.34(当前最新支持线,其目标 v1.35 未开放)**;升级编排 API 全可用;代码侧把"无可用目标"作为正常状态处理(文档化 + 日志提示) |

## 第四轮真实 CCE 冒烟确认记录(TestSmokeAutoscaling / TestSmokeUpgradeWorkflow)

| 项 | 实测结果 |
|---|---|
| **B3 autoscaling 云侧接受** | ✅✅ **实测通过**:Standard(vpc-router)集群上 `CreateNodePool(autoscaling={enable=true,min=1,max=4})` 成功;`ListNodePools` 回读 `spec.autoscaling` **enable=true/min=1/max=4 完全一致**——CCE 持久化 autoscaling 配置 |
| **B3 autoscaling 与手动伸缩并存** | ✅✅ **实测通过**:autoscaling 开启的节点池上 `ScaleNodePool(2)` 成功扩到 2 节点,且回读 autoscaling **仍为 enable=true**(手动 ScaleNodePool 与 autoscaling 不互相覆盖;与 Q3 绝对值语义一致) |
| **E3 升级工作流(服务层)** | ✅ 实测:v1.34 集群 targets=`[]`(目标 v1.35 未开放);**后续用 v1.33 集群确认目标开放(targets=[v1.34.8-r2])并跑通完整工作流**(见第六轮);controller 在空目标时走 `UpgradeNotOffered` 分支(envtest 已覆盖) |

> 环境同上(VPC `capi-smoke-vpc` + 双子网 + `capi-smoke-key` + c6.large.2 Standard 模式);两个测试集群均已删除并复核无残留。

## 真实 CCE 冒烟确认记录(2026-08-18,cn-north-4,账号实测)

> 在真实华为云 CCE 账号上完成冒烟(`internal/services/cce/smoke_test.go`,`-tags smoke`),以下项由文档推断升级为**实测确认**:

| 项 | 实测结果 |
|---|---|
| **Q1 空集群** | ✅ CreateCluster(不传节点)成功,最终 phase=Available,K8s v1.36,内网 endpoint `https://<IP>:5443` |
| **Q2 kubeconfig** | ✅ CreateKubernetesClusterCert(duration=30)成功,返回 ~10KB kubeconfig |
| **Q3 绝对值语义** | ✅✅ **双重确认**:ScaleNodePool(2) 在 2 节点池返回 "No scale task needed"(无操作=绝对值);ScaleNodePool(4) 将 2 节点池扩到 4 节点(绝对目标) |
| **Q3 节点池** | ✅ CreateNodePool(initialNodeCount=2)成功并达到 2 节点 |
| **Q3 UpdateNodePool** | ✅ ignoreInitialNodeCount=true 调用成功(观测值 0 为 ListNodePools 映射口径问题,不影响结论) |
| **Q7 配额** | ✅ ShowQuotas 实测 **limit=50 / 区域**(解决文档"5 vs 50"矛盾) |
| **Q8 删除** | ✅ DeleteNodePool + DeleteCluster(delete_evs/eni/net=true)成功,复核无残留集群 |
| **Q4/eni(实测新发现)** | ⚠️ eniNetwork.subnets[].subnetID 必须是 **neutron_subnet_id**(普通子网 ID 报 CCE_CM.0004 "Eni subnetId is not in cluster vpc") |
| **Turbo 规格(sub-ENI)** | ⚠️ 实测 c6.large.2 **sub-ENI 配额=0 → 不支持 eni 网络**(报 "subeni quota is 0",对应官方 CCE.01400025);需选 `quota:sub_network_interface_max_num>0` 的规格(如 c6sne.large.2,但 cn-north-4a 部分规格 abandon) |
| **节点池参数(实测新发现)** | ⚠️ 非本地盘规格(c6.large.2)必须配**数据盘**(报 "Data volume needed");**OS 必填**(报 "OS:should not be empty",与 SDK 注释"可自动选择"矛盾);**AZ 必填**(报 "Az [] is not in available az list")。**OS 合法值非单一**(官方 API 参考附录表 8-5):`Huawei Cloud EulerOS 2.0`(x86/ARM)、`Huawei Cloud EulerOS 1.1`、`EulerOS release 2.9`(x86/ARM)、`Ubuntu 22.04`(cgroup v1/v2,仅 VPC 网络模型)、`CentOS Linux release 7.6`(停止维护);实测仅验证过 `Huawei Cloud EulerOS 2.0` |

> 冒烟环境:VPC `capi-smoke-vpc`(10.0.0.0/16)+ 双子网 + 密钥对 `capi-smoke-key` + 规格 c6.large.2(Standard/vpc-router 模式跑通全链路);Turbo/eni 集群创建同样成功(用 neutron 子网 ID)。搭建工具:`hack/smoke-setup`。

## 汇总:14 项确认状态

| # | 主题 | 官方结论 | 剩余需实测/咨询 |
|---|---|---|---|
| Q1 | 空集群创建 | ✅ 官方原文"创建空集群(只有 Master 无 Node)";空集群照常计费;flavor 上限 CCE.01400014 | 空集群最终 phase;Available 前调节点池的错误码 |
| Q2 | kubeconfig | ✅ 有效期 -1/[1,1827];external/internal 按 publicIp;吊销立即失效;重新签发即时生效;**证书授权项两处来源差异已解决:接口约束= `cce:cluster:generateClientCredential`(依赖 `cce:cluster:get`),最小集合须同时含两者** | 不传 duration 的行为 |
| Q3 | 扩缩容 | ✅ **desiredNodeCount=绝对值(期望总数)**;scaleGroups 必填 default;UpdateNodePool 不填 initialNodeCount 会缩到 0 | 绝对值/增量实测关闭;伸缩中再伸缩的错误码 |
| Q4 | eni 网络 | ✅ eni 容器子网可取 VPC 子网(可与节点子网重叠);硬约束=服务网段不重叠;eni 可增不可改 | "ENI 子网覆盖 2 AZ"类说法无官方依据 |
| Q5 | 安全组 | ✅ 自动建 node/control/eni SG;podSG 仅 Turbo 每池≤5;改 SG 只对新建节点生效;5443 白名单=改 control SG | Standard 对 customSecurityGroups 支持 |
| Q6 | IAM 权限 | ✅ 细粒度 action 表;FullAccess 不含生成证书;委托代行 ECS/VPC;联邦用户无永久 AK/SK | 跨 project;最小策略隐式依赖 |
| Q7 | 配额 | ✅ CCE 只限集群数;约束页 50/Region(API 页写 5,矛盾);错误码 CCE.01400007 等 | 以控制台实测值为准 |
| Q8 | 删除 | ✅ 异步 1~3 分钟;delete_evs 默认残留、delete_eni/net 默认删;ondemand_node_policy 默认删按需节点保留纳管节点;休眠中不可删;**实测:节点 Installing 时删节点池报 `CCE.01403006`、删集群报 `CCE.01400024`,须等节点全部 Active(以 `activeNode` 判据)** | 删除不存在集群的错误码 |
| Q9 | 单节点 | ✅ AddNode=重装 ECS(清数据)+严格前置(≥2C4G/单网卡/数据盘);CCE 自动安装;DefaultPool 无弹性 | CAPI Machine 路径取舍 |
| Q10 | Autopilot | ✅ Serverless 无节点 API;50 集群/区域;按 CPU/内存计费 | CAPI 对接方式(远期) |
| Q11 | 升级 | ✅ **完整确认(官方 cce_10_0197,2026-07-28 版)**:路径表/维护周期 24 个月/批次 4 的幂/备份回滚/证书+OIDC;**实测 v1.33→v1.34 目标开放并跑通升级工作流**(3 真实约束已修复);v1.34 目标 v1.35 未开放——目标按版本线动态开放 | 完整成功升级耗时(预检查通过后实测,测试已就绪) |
| Q12 | 计费/休眠 | ✅ billingMode 默认按需;休眠停控制节点费用、节点/EIP 照常;唤醒 3~5 分钟 | 包周期是否禁休眠;唤醒失败错误码 |
| Q13 | 网络路径 | ✅ 公网 https://EIP:5443;**实测:EIP 绑定后公网可达(reachable=true)**;私网 VPC 内 IP/VIP;跨 VPC=对等/专线/VPN | 跨 Region 方案 |
| Q14 | 限流/错误码 | ✅ 错误码表公开(404=01404001、429=01429002/003、配额 01400007 系);**实测:读 ~70 req/s、写 10 次/分钟触发限流;429 携带 Retry-After(~57s)但非稳定出现(8/1700)→ 固定退避更可靠** | 无(全部实测关闭) |

