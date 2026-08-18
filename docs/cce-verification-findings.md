# 华为云 CCE 对齐问卷 — 官方文档检索确认结果

> 状态:**检索中**(4 个子代理并行核对 Q1–Q14,以下为已确认部分,子代理结果将合并入对应小节)。
> 关联文档:[问卷](cce-verification-questionnaire.md)(问题原文)、[调研依据](research-sources.md)。
> 依据类型标记:**[官方文档]** / **[官方SDK]** / **[需实测或咨询]**(官方未公开或文档冲突)。

## Q1 空集群(0 节点)创建 — [官方文档] 基本确认 + 2 项需实测

- **CreateCluster API 请求体不含节点参数**(官方 API 文档 cce_02_0236 请求参数表无 masterNode/节点参数;节点通过 `CreateNodePool`/`CreateNode`/`AddNode` 独立管理)。→ 空集群可创建(API 层面)。
- 官方用户指南"扩缩容节点池"(usermanual-cce cce_10_0658)原文:**"节点池创建完成后,节点数量为 0"** → 节点池也可为空。
- 创建集群前置条件(官方 cce_02_0236):**必须先有 VPC**;提前规划容器/服务网段(隧道模式创建后不可改,vpc-router/eni 可增不可改);**必须已创建委托,委托校验失败将导致集群创建失败**。
- **需实测**:空集群是否计费、计费口径;空集群是否受 flavor 节点上限约束;空集群 phase 是否即 Available(官方 phase 枚举:Available/Unavailable/ScalingUp/ScalingDown/Creating 等,SDK model_cluster_status.go)。

## Q2 kubeconfig 证书获取与有效期 — [官方文档] 完全确认

- 有效期(官方 API 文档"获取集群证书" cce_02_0248 + SDK `model_cert_duration.go`):**"集群证书有效时间,最小值为 1 天,最大值为 5 年,取值范围为 1-1827(以天为单位,实际上限取决于 5 年内闰年数量,例如 5 年内存在一个闰年则上限为 1826 天);若填 -1 则为最大值 5 年"**。
- external/internal(官方 cce_02_0248):**"若存在 publicIp(虚拟机弹性 IP)时为 external;若不存在 publicIp 为 internal"**;internal 时集群列表数量为 1(name=internalCluster);external 时扩展 cluster 的 name 为 externalCluster 且 `insecure-skip-tls-verify=true`。
- 吊销:官方权限表有 `clustercertrevoke`(cce:cluster:revokeClientCredential);行为未在检索到的文档中详述。
- 示例:官方文档示例"申请 30 天有效的集群访问证书"(duration: 30)。

## Q3 节点池扩缩容语义 — ⚠️ 官方文档存在歧义,必须实测

- **SDK 注释**(`model_scale_node_pool_spec.go`):"节点池的预期总数量。**执行扩容操作时,需将当前节点数与扩容数量相加**;执行缩容操作时,需从当前节点数中减去缩容数量。**必填参数,如果省略则默认值为 0,会导致删除节点池伸缩组下的所有节点**" → 增量语义。
- **官方用户指南**(usermanual-cce cce_10_0658):"**扩容时,本次需要扩容的节点数与已有节点数相加**不可超过当前集群管理规模;缩容时,本次需要缩容节点数不可超过已有节点数" → 增量语义(与 SDK 一致)。
- **API 文档**(ScaleNodePool,ae-ad-1-api-cce/ScaleNodePool.html):`desiredNodeCount` 描述为"**节点池期望节点数**" → 疑似绝对值,与 SDK/用户指南矛盾。
- `scaleGroups`(官方 API 文档):**必填**,扩缩容时"只能填一个伸缩组,如果要伸缩默认伸缩组填 `default`"。
- **结论:增量语义概率更高(SDK + 用户指南一致),但 API 文档表述冲突 → 必须用真实集群实测**(建 2 节点池 → desiredNodeCount=2 → 观察是 4 还是 2)。
- 补充:默认节点池不支持扩缩容(官方用户指南);通过 ECS 控制台删节点,CCE 10 分钟后自动补足"节点池期望节点个数"(官方用户指南)。

## Q4 Turbo(eni)网络模型的 VPC/子网要求 — [官方文档] 部分确认

- eni(云原生网络 2.0):Pod 直接绑定 VPC 弹性网卡(ENI)/辅助弹性网卡(Sub-ENI),**Pod IP 来自 VPC 子网**(官方"云原生网络 2.0 模型说明" usermanual-cce cce_10_0284)。
- 官方"网段规划建议"(cce_10_0284):集群所在 VPC 下所有子网**不能和服务网段冲突**;每个网段要有足够 IP;eni 模式下**"建议容器子网与节点子网不要使用同一个子网"**(避免 IP 不足);容器网段创建后可增加子网扩展。
- 官方错误码 `CCE.01400025`(400):"subeni 配额不足,虚机规格不支持 Turbo 集群" → **Turbo 节点规格需 sub-ENI 配额**。
- **需实测/咨询**:ENI 子网数量下限(是否有"覆盖 2 个可用区"硬性要求);VPC 网段与 Pod 网段是否必须不重叠(eni 模式下 Pod 用 VPC 地址,与 overlay 模式的独立容器网段不同)。

## Q5 安全组 — [官方文档] 部分确认(待子代理补充)

- 节点池绑安全组:**Turbo ≥1.21 支持,每池最多 5 个**(官方 cce_02_0354)。
- SDK `node_pool_spec.go`:`customSecurityGroups`(节点池级)、`podSecurityGroups`(Pod 级)字段存在。
- 待补充:集群 master/node 安全组的自动创建与自定义;修改生效方式。

## Q6 IAM 权限与凭证约束 — [官方文档] 大部分确认

- 完整 action 表(官方"CCE 权限概述" cce_10_0187):集群 `cce:cluster:list/get/create/update/delete/upgrade/start/stop/resize`;节点 `cce:node:list/get/create/update/delete/remove/migrate`;节点池 `cce:nodepool:list/get/create/update/delete/scale`;证书 `cce:cluster:get`(clustercert);Job `cce:job:*`。
- **关键(子代理确认)**:`CCE FullAccess` 是系统策略但**明确不含"生成集群证书"能力**——取 kubeconfig 需 `cce:cluster:get`(或 CCE Administrator);ECS/VPC/EVS 的资源操作由系统委托(`cce_admin_trust`/`cce_cluster_agency`)代行,**AK/SK 无需配 ECS/EVS action**;企业项目授权下创建节点需额外全局权限 `evs:quotas:get`、`evs:types:get`(官方原文)。
- 官方未要求主账号:官方原文"账号具备所有 API 的调用权限,如果使用账号下的 IAM 用户调用当前 API,该 IAM 用户需具备调用 API 所需的权限" → **IAM 用户配足细粒度策略即可**。
- 委托(官方 cce_02_0236 + cce_10_0556):创建集群**必须已有系统委托**,`agencyName` 不传时自动使用 `cce_admin_trust` 或 `cce_cluster_agency`(1.21+;后者仅含 CCE 组件依赖权限)。
- **AK/SK 约束**:官方权限概述——**联邦用户不支持创建永久 AK/SK** → Provider 凭证必须来自账号或实体 IAM 用户。
- **未确认(需实测)**:同一对 AK/SK 跨多 project 调用;最小策略集是否隐式依赖其他 action。

## Q7 默认配额 — [官方文档] 部分确认

- 官方 FAQ(Which Resource Quotas Should I Pay Attention To When Using CCE?):**"CCE restricts only the number of clusters"**——CCE 平台层只限制集群数量;节点依赖 ECS/EVS/VPC/ELB/SWR 各自配额。
- **具体数值官方未公开**,需控制台"资源 > 我的配额"(My Quotas)查看,提升走 Increase Quota(工单)。
- 配额不足错误码已确认(见 Q14)。

## Q8 集群删除语义 — [官方文档] 重要确认(有 API 级删除选项)

- **DeleteCluster API 带删除选项 Query 参数**(官方 cce_02_0241,取值 `true/block`=执行失败阻塞、`try`=执行失败忽略、`false/skip`=跳过):
  - `delete_efs`(SFS Turbo)**默认 false(skip)→ 残留**
  - `delete_eni`(eni ports)**默认 block(删)**
  - `delete_evs`(云硬盘)**默认 false(skip)→ 残留** ⚠️
  - `delete_net`(ELB 等 Service/Ingress 资源)**默认 block(删)**
  - `delete_obs`/`delete_sfs`/`delete_sfs30` 默认 false(skip)
  - **实现必须显式传删除选项,否则 EVS/SFS 会残留。**
- 删除为异步:200 表示"删除作业下发成功"。
- 需实测:删除时长、Unavailable 集群可删性、节点池是否需先行删除。

## Q9 单节点路径(AddNode) — [官方SDK] 关键确认:需重装系统,不适合免干预 CAPI Machine

- `AddNode`(纳管已有 ECS,SDK `model_add_node.go`):body = `{serverID(已有ECS), spec: ReinstallNodeSpec}` —— **纳管 = 重装该 ECS 的系统(ReinstallNodeSpec 含必填 Os)**。
- `ReinstallNodeSpec` 的 `initializedConditions`(SDK 注释):CCE 节点初始化完成前打 `node.cloudprovider.kubernetes.io/uninitialized` 污点;**纳管/重置时可通过 initializedConditions 控制污点移除(可能需用户自定义初始化脚本)**。
- **结论:AddNode 路径涉及重装 ECS + 可能的自定义初始化,不适合 CAPI Machine 的免 SSH 自动引导;CCE 场景应只走节点池路径(与设计一致)。**

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

## Q12 计费与休眠/唤醒 — [官方SDK] 部分确认

- `ClusterSpec.BillingMode`(SDK):"0: 按需计费;1: 包周期;**默认为按需计费**"。
- 官方文档导航确认存在"集群休眠 - HibernateCluster"与唤醒(AwakeCluster)API。
- 待补充:休眠计费变化、唤醒条件(子代理)。

## Q13 管理集群 → CCE API Server 网络路径 — 待子代理补充(已确认 endpoint 结构 `model_cluster_endpoints.go`:url + type public/private;kubeconfig current-context 按有无公网 IP 切换,见 Q2)

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
*(待子代理返回后合并:Q5 细节、Q7 补充、Q10、Q13,及 Q1/Q8/Q11/Q12 的补充项)*
