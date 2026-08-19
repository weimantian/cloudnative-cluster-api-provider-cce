# 华为云 CCE 对齐问卷(供 Provider 实现前确认)

> 英文版 / English: [cce-verification-questionnaire.en.md](cce-verification-questionnaire.en.md)
>
> 背景文档:[调研依据与事实清单](research-sources.md) §4(14 项待验证清单)、[架构设计文档](architecture-design.md)、[需求设计文档](requirements-design.md)

## 一、背景与目的

我们正在开发 `cloudnative-cluster-api-provider-cce` —— 一个管理华为云 **CCE 托管集群** 的 Cluster API 基础设施 Provider(对标 CAPI + AWS EKS 托管模式)。实现前需要就以下 **14 项** CCE API 行为与约束与华为云确认,避免按文档假设开发后返工。**在全部问题得到确认/实测之前,我们不进入 PoC 编码。**

## 二、填写说明

- 每项请给出:**结论**(支持/不支持/需注意的约束)+ **依据**(官方文档链接 / 实测结果 / 工单结论)+ **确认人 + 日期**。
- 建议验证方式已给出(控制台步骤或 API 调用,API 均可用华为云 Go SDK `huaweicloud-sdk-go-v3/services/cce/v3` 或 KooCLI 实测)。
- 如某项需要由我方实测,请提供测试账号/子项目环境配合即可,也可直接给出文档依据。

## 三、问题清单

### Q1 空集群(0 节点)创建

- **背景/设计影响**:我们的主路径是"先建 CCE 集群(控制面)→ 再建节点池"。官方文档显示 `CreateCluster` 请求体不含节点参数,但需确认空集群行为。
- **具体问题**:
  1. 通过 `POST /api/v3/projects/{project_id}/clusters` 创建**不带任何节点**的集群(Standard 与 Turbo)是否支持?
  2. 空集群是否计费?计费口径(集群本身 vs 节点)?
  3. 空集群是否占用集群配额?是否受"集群规格 flavor 对应最大节点数"限制?
  4. 空集群状态是否为 `Available`?在 `Available` 前调用节点池接口会返回什么错误码?
- **建议验证**:控制台创建"空集群"或 SDK `CreateCluster`(不传节点)→ 观察状态与费用。
- **回答**:结论 ___ / 依据 ___ / 确认人 ___ / 日期 ___

### Q2 kubeconfig 证书获取与有效期

- **背景/设计影响**:Provider 用 `CreateKubernetesClusterCert` 获取 kubeconfig 并写入 Secret;需确定刷新策略。
- **具体问题**:
  1. `CertDuration.duration` 取值范围官方注释为 **-1 或 [1,1827]**(最大 5 年),请确认:填 `-1` 是否即 5 年?默认值是多少?是否有更短的上限(如工单/合规限制)?
  2. 证书过期后,`clusterctl get kubeconfig` 拿到的 kubeconfig 是否失效?重新调用 `CreateKubernetesClusterCert` 是否即时生效(是否需吊销旧证书)?
  3. 响应 `current-context` 为 `external`(公网)与 `internal`(私网)的切换逻辑:无公网 IP 的集群是否必然返回 `internal`?`internal` 的 server 地址是什么形式(VIP/域名)?
  4. 吊销接口 `clustercertrevoke` 的行为(是否影响已下发的 kubeconfig)?
- **建议验证**:SDK `CreateKubernetesClusterCert`(duration=1 与 duration=-1)各建一次,检查返回 kubeconfig 与证书有效期。
- **回答**:结论 ___ / 依据 ___ / 确认人 ___ / 日期 ___

### Q3 节点池扩缩容语义(`ScaleNodePool` / `UpdateNodePool`)

- **背景/设计影响**:CAPI 的 `MachinePool.spec.replicas` 变更 → Provider 调用节点池伸缩。SDK 注释显示 **`ScaleNodePoolSpec.desiredNodeCount` 是"增量"语义**(扩容=当前数+增量;缩容=当前数-增量),且**省略时默认 0 会删除伸缩组下所有节点**——这是必须确认的高风险点。
- **具体问题**:
  1. 请确认 `ScaleNodePool` 的 `desiredNodeCount` 到底是**绝对值**还是**增量值**?(SDK 注释"需将当前节点数与扩容数量相加"暗示增量,但字段名是 desired 易误用)
  2. `scaleGroups` 参数:默认伸缩组名是否为 `"default"`?扩缩容是否必须传该字段?缩容是否只能指定单个伸缩组?
  3. `UpdateNodePool`(修改 `initialNodeCount`)与 `ScaleNodePool` 的关系:两者都能改节点数吗?推荐哪种用于"对齐期望数量"?
  4. 节点池配置了 `autoscaling`(自动扩缩容)时,外部调用 `ScaleNodePool` 是否冲突?谁优先?
  5. 扩缩容期间集群/节点池状态(ScalingUp/ScalingDown)下,再发起伸缩是否报错?错误码?
- **建议验证**:建 2 节点池 → 分别用 `ScaleNodePool` 传 desiredNodeCount=2 与 3,观察节点数变化(是变成 4/5 还是 2/3)。
- **回答**:结论 ___ / 依据 ___ / 确认人 ___ / 日期 ___

### Q4 Turbo(eni)网络模型的 VPC/子网硬性要求

- **背景/设计影响**:Turbo 集群 `containerNetwork.mode=eni` 对网络有强约束,配置错误会反复创建失败。Provider 需在校验层拦截。
- **具体问题**:
  1. eni 模式下,VPC/子网有哪些硬性要求?(子网是否必须支持 ENI、子网数量与可用区要求、VPC 网段与 Pod 网段(eniNetwork)是否必须不重叠?)
  2. `eniNetwork.subnets`(ENI 子网)与节点子网的关系:是否必须显式指定?数量下限(官方文档曾见"eni 子网需覆盖 2 个可用区"等说法,请确认)?
  3. Turbo 集群创建时若 VPC/子网不满足要求,返回什么错误码/错误信息?
  4. Standard 的 `vpc-router` 模式"创建后可新增网段、不可修改已有网段"的具体边界:新增子网/网段是否有数量上限?
- **建议验证**:用不满足条件的子网创建 Turbo 集群(eni),记录错误;对照控制台创建向导的校验规则。
- **回答**:结论 ___ / 依据 ___ / 确认人 ___ / 日期 ___

### Q5 安全组(master / node / eni)

- **背景/设计影响**:Provider 计划支持节点池绑定安全组(Turbo ≥1.21,每池 ≤5 个)与集群级安全组策略。
- **具体问题**:
  1. CCE 集群创建时是否自动创建 master/node 安全组?是否可指定自定义安全组?`customSecurityGroups`(节点池)与 `podSecurityGroups`(Pod 级)的区别与约束?
  2. 节点池绑定安全组上限确认:每池最多 5 个?Standard 集群是否不支持绑定?
  3. 已创建的集群/节点池,能否修改安全组?修改的生效方式(存量节点 vs 新增节点)?
  4. API Server 访问控制:是否通过安全组/网络 ACL 控制公网 5443 端口来源?`publicAccess` 的白名单机制?
- **建议验证**:控制台创建集群/节点池,查看自动生成的安全组及其规则;SDK 传 `customSecurityGroups` 实测。
- **回答**:结论 ___ / 依据 ___ / 确认人 ___ / 日期 ___

### Q6 IAM 权限与凭证约束

- **背景/设计影响**:Provider 使用 AK/SK 调用 CCE API;需确定最小权限集合与账号约束。
- **具体问题**:
  1. 管理 CCE 集群/节点池/节点所需的最小 IAM 权限(官方"权限说明"表:集群 cce:cluster:list/get/create/update/delete/upgrade/start/stop/resize;节点池 cce:nodepool:*;节点 cce:node:*;证书 cce:cluster:get)——**使用这些 action 的**自定义策略是否足够?是否还需要 VPC/ECS/EVS 相关权限(创建节点依赖 ECS 配额,企业项目授权下需 evs:quotas:get、evs:types:get)?
  2. AK/SK 所属账号是否必须是项目(Project)主账号?子账号/委托(agency)是否可用?`agencyName`(cce_cluster_agency)的作用与版本要求(1.27+)?
  3. `CCE FullAccess` 与上述细粒度 action 的关系(FullAccess 是否必要)?
  4. 同一对 AK/SK 能否管理多项目(多 project)资源?跨项目调用是否需要额外配置?
- **建议验证**:用最小自定义策略账号实测创建/删除集群与节点池;对照官方"CCE权限概述"文档。
- **回答**:结论 ___ / 依据 ___ / 确认人 ___ / 日期 ___

### Q7 默认配额与超限错误

- **背景/设计影响**:大规模管理场景需预知配额;超限错误码决定 Provider 的错误分类与提示。
- **具体问题**:
  1. 每个项目默认配额:CCE 集群数、节点池数、节点数、VPC/子网/ENI 数量上限分别是多少?(官方"约束与限制"页面未公开具体数字,请提供或指引)
  2. 超过配额时返回的错误码与错误信息格式?(如 CCE.xxxx / VPC.xxxx / Ecs.xxxx)
  3. 配额是否可申请提升?流程与周期?
- **建议验证**:控制台"资源配额"页;SDK 触发超配实测错误码。
- **回答**:结论 ___ / 依据 ___ / 确认人 ___ / 日期 ___

### Q8 集群删除语义与残留

- **背景/设计影响**:删除路径是 Provider 最容易出事故的地方(finalizer 卡死、云资源残留)。
- **具体问题**:
  1. `DeleteCluster` 的删除流程:是否自动级联删除节点池/节点?删除时长量级(分钟)?删除中集群状态?
  2. 删除集群时,集群创建的 EIP/EVS/ELB 等附属资源是否自动释放?哪些会残留(需人工清理)?
  3. 删除前是否必须先把节点池/节点删完?若直接删集群而存在节点池,行为如何(拒绝/级联/遗留)?
  4. `Unavailable`(异常)状态的集群能否删除?删除卡住的兜底手段(强制删除)?
  5. 删除接口的幂等性:重复删除/删除不存在的集群返回什么?
- **建议验证**:真实删除一次集群,记录时长、残留资源、各阶段状态。
- **回答**:结论 ___ / 依据 ___ / 确认人 ___ / 日期 ___

### Q9 单节点路径(`AddNode` / `AddNodesToNodePool`)可行性

- **背景/设计影响**:我们二期考虑 CAPI `Machine`(非 MachinePool)路径,把已有 ECS 纳入 CCE。其引导语义决定可行性。
- **具体问题**:
  1. `AddNode` / `AddNodesToNodePool`(把已有 ECS 加入集群/节点池)对 ECS 有什么前置要求?(是否要求 ECS 预装 node agent/脚本?是否支持"免 SSH、自动引导"?)
  2. 节点加入后如何完成 kubelet 初始化与注册?(CCE 侧自动完成,还是需要用户在 ECS 上执行脚本?)
  3. 这种"纳管 ECS"与"节点池自动创建 ECS"在生命周期管理上的差异(更新/替换/删除语义)?
  4. 若该路径引导依赖手工步骤,是否建议 CAPI 场景**只支持节点池路径**?
- **建议验证**:控制台"添加节点/纳管节点"向导;SDK `AddNodesToNodePool` 实测。
- **回答**:结论 ___ / 依据 ___ / 确认人 ___ / 日期 ___

### Q10 CCE Autopilot(Serverless)与 CAPI 的对接(远期)

- **背景/设计影响**:Autopilot 无节点概念(对标 AWS Fargate),CAPI 模型(Cluster + MachinePool)如何表达待定。
- **具体问题**:
  1. Autopilot 集群的 API 与 Standard/Turbo 的差异(独立 API 族 `CreateAutopilotCluster` 等)?是否支持无节点集群 + 仅工作负载调度?
  2. 若远期支持,华为云是否有推荐的 CAPI 对接方式(如 Autopilot 集群 + 无 MachinePool)?
  3. Autopilot 的配额、计费与可用区域约束?
- **建议验证**:文档确认;此问题不阻塞首版,可给出初步结论即可。
- **回答**:结论 ___ / 依据 ___ / 确认人 ___ / 日期 ___

### Q11 集群升级(`CreateUpgradeWorkFlow`)

- **背景/设计影响**:P1 支持改 `version` 触发升级。需确认编排参数与状态。
- **具体问题**:
  1. `CreateUpgradeWorkFlow` / `CreatePreCheck` / `CreatePostCheck` 的调用顺序与必填参数(目标版本、是否支持跨大版本、升级类型)?
  2. 升级期间集群状态(`Upgrading`?)与 API 可用性(升级中能否继续建节点池/扩缩容)?
  3. 升级失败的处理与回滚手段?升级耗时量级?
  4. 是否支持只升级平台版本(platformVersion)不升级 Kubernetes 版本?
  5. 升级后节点池/节点是否需要滚动(CCE 自动处理还是需要重建节点)?
- **建议验证**:文档确认 + 测试环境实测一次小版本升级。
- **回答**:结论 ___ / 依据 ___ / 确认人 ___ / 日期 ___

### Q12 计费与休眠/唤醒

- **背景/设计影响**:成本控制与 `AwakeCluster`/`HibernateCluster` 语义。
- **具体问题**:
  1. `billingMode`:0=按需、1=包周期;包周期(period)类型与周期的 API 参数?
  2. 空集群/停用集群是否计费?休眠(HibernateCluster)后计费变化?
  3. `AwakeCluster` 唤醒的触发条件与耗时?
- **建议验证**:文档确认;计费以价格页/工单为准。
- **回答**:结论 ___ / 依据 ___ / 确认人 ___ / 日期 ___

### Q13 管理集群 → CCE API Server 的网络路径

- **背景/设计影响**:管理集群(可能在不同 VPC/Region)需访问工作集群 API Server 以执行 `clusterctl get kubeconfig` 后 kubectl 操作;kubeconfig 的 server 地址可达性决定部署形态。
- **具体问题**:
  1. 公网 endpoint:创建集群时开启公网访问(publicAccess)后,API Server 地址与端口(5443?)是否公网可达?是否有访问控制(白名单)?
  2. 私网 endpoint:`internal` 地址是否仅 VPC 内可达?跨 VPC(对等连接/云专线/CCE 集群跨 VPC 通信)的推荐方案?
  3. 管理集群与工作集群在同一 VPC/不同 VPC/不同 Region 三种场景下,推荐哪种接入方式?
- **建议验证**:文档确认 + 实测公网/内网 kubeconfig 连通性。
- **回答**:结论 ___ / 依据 ___ / 确认人 ___ / 日期 ___

### Q14 API 限流与错误码全集

- **背景/设计影响**:大规模管理集群下,Provider 频繁调用 CCE/ECS/VPC API;需知限流阈值以设计退避。
- **具体问题**:
  1. CCE、ECS、VPC 各服务的 API 调用频率限制(每分钟/每秒)?不同接口是否不同?
  2. 触发限流时返回的错误码/HTTP 状态码(429?)、响应头是否含 Retry-After?
  3. 常用错误码全集(至少:集群/节点池/节点不存在、冲突、配额不足、权限不足、集群状态不允许)能否提供一份对照表?
  4. 是否有批量接口(如 ListClusters/ListNodes 的分页上限)与配额查询接口?
- **建议验证**:文档/工单确认;实测高频调用观察限流。
- **回答**:结论 ___ / 依据 ___ / 确认人 ___ / 日期 ___

---

## 四、汇总清单(官方检索确认版 — 详见 [确认结果](cce-verification-findings.md))

| # | 主题 | 官方检索结论 | 剩余需实测/咨询 | 依据 |
|---|---|---|---|---|
| Q1 | 空集群创建/计费/配额 | ✅ 官方原文"创建空集群(只有 Master 无 Node)";空集群照常计费;flavor 超限 CCE.01400014 | 空集群最终 phase;Available 前调节点池错误码 | cce_02_0236 + price-cce |
| Q2 | kubeconfig 有效期与 external/internal | ✅ 有效期 -1/[1,1827];external/internal 按 publicIp;**重新签发即时生效(实测)** | 不传 duration 行为;授权项两处不一致 | cce_02_0248 + SDK + 冒烟 |
| Q3 | ScaleNodePool 语义与扩缩容冲突 | ✅ **绝对值(期望总数)**;scaleGroups 必填 default;UpdateNodePool 不填 initialNodeCount 会缩到 0 | 绝对值/增量实测关闭;伸缩中再伸缩错误码 | ScaleNodePool.html + cce_02_0356 |
| Q4 | Turbo(eni)网络硬性要求 | ✅ eni 容器子网可取 VPC 子网(可与节点子网重叠);硬约束=服务网段不重叠;eni 可增不可改 | "覆盖 2 AZ"类说法无官方依据 | cce_10_0284 + bestpractice |
| Q5 | 安全组创建/绑定/修改 | ✅ 自动建 node/control/eni SG;podSG 仅 Turbo 每池≤5;改 SG 只对新建节点生效;5443 白名单=改 control SG;**Standard 接受 customSecurityGroups(实测)** | — | cce_faq_00265 + cce_10_0784 + 冒烟 |
| Q6 | IAM 最小权限与凭证约束 | ✅ 细粒度 action 表;FullAccess 不含生成证书;委托代行 ECS/VPC;联邦用户无永久 AK/SK | 跨 project;最小策略隐式依赖 | cce_10_0187 + cce_10_0556 |
| Q7 | 默认配额与超限错误码 | ✅ CCE 只限集群数;约束页 50/Region(API 页写 5,矛盾);错误码 CCE.01400007 系 | 以控制台实测值为准 | cce_faq_00154 + productdesc |
| Q8 | 删除语义与资源残留 | ✅ 异步 1~3 分钟;delete_evs 默认残留、delete_eni/net 默认删;ondemand_node_policy 默认删按需节点;休眠中不可删 | 有节点池时直接删集群;删不存在集群错误码 | cce_02_0241 + cce_10_0212 |
| Q9 | AddNode 单节点路径可行性 | ✅ AddNode=重装 ECS(清数据)+严格前置(≥2C4G/单网卡/数据盘);CCE 自动安装;DefaultPool 无弹性 | CAPI Machine 路径取舍 | AddNode.html + cce_10_0198 |
| Q10 | Autopilot 对接(远期) | ✅ Serverless 无节点 API;50 集群/区域;按 CPU/内存计费 | CAPI 对接方式(远期) | 集群类型对比 + autopilot 约束 |
| Q11 | 集群升级工作流 | ✅ 仅原地升级;TargetVersion 必填;升级 API 可用(实测);**实测定论:ShowClusterUpgradeInfo 返回 offered 升级目标=空(平台当前不提供跨版本路径,"无可用目标"需作为正常状态处理)** | 升级耗时(无路径不可实测);升级策略需咨询华为云 | 升级 API + 冒烟 |
| Q12 | 计费与休眠/唤醒 | ✅ billingMode 默认按需;休眠停控制节点费用、节点/EIP 照常;唤醒 3~5 分钟 | 包周期是否禁休眠;唤醒失败错误码 | cce_02_0374/0375 + price |
| Q13 | 管理集群→API Server 网络路径 | ✅ 公网 https://EIP:5443;**实测:EIP 绑定后公网可达(reachable=true)**;私网 VPC 内 IP/VIP;跨 VPC=对等/专线/VPN | 跨 Region 方案 | cce_10_0864 + bestpractice + 冒烟 |
| Q14 | API 限流与错误码全集 | ✅ 错误码表公开;限流 APIGW.0308;**实测:持续 ~71 req/s 即大量限流(703/1000),阈值远低于 APIGW 默认 200 req/s → 轮询/重试必须退避+抖动** | Retry-After | ErrorCode.html + 冒烟 |

## 五、参考来源(问题依据)

- 华为云 CCE 官方 Go SDK `huaweicloud-sdk-go-v3/services/cce/v3`(CreateCluster/ScaleNodePool/CreateKubernetesClusterCert 等模型与注释,含 `CertDuration` 1~1827 天、`ScaleNodePoolSpec` 增量语义等)。
- 华为云官方文档:创建集群 API(cce_02_0236)、创建节点池 API(cce_02_0354)、集群类型对比(cce_10_0342)、网络模型、CCE 权限概述(cce_10_0187)、系统委托说明(cce_10_0556)。
- 完整清单与抓取存档:见 [research-sources.md](research-sources.md)。
