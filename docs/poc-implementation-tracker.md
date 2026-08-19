# PoC VERIFY/TODO → 华为云对齐问卷 逐项落地跟踪清单

> 关联文档:[华为云 CCE 对齐问卷](cce-verification-questionnaire.md)(14 项,待华为云确认)、[调研依据与事实清单](research-sources.md)、[架构设计文档](architecture-design.md)、[需求设计文档](requirements-design.md)
>
> **用法**:华为云确认问卷后,按本清单"确认后的实现动作"逐项落地;每项完成后回填"落地状态/提交"列,并跑对应验收。**在对应问卷项确认前,不实现依赖其结论的云侧行为。**

## 一、代码 VERIFY/TODO 与问卷项映射

### A 组:集群创建与网络(阻塞主链路)

| # | 代码位置 | 问卷项 | 依赖的确认结论(待华为云填写) | 确认后的实现动作 | 验收方式 | 落地状态 |
|---|---|---|---|---|---|---|
| A1 | `controllers/ccecluster_controller.go:89-90` TODO(P0) 网络校验服务 | **Q4**(eni VPC/子网硬性要求)、Q5 | eni 对 VPC/子网要求:ENI 子网、网段不重叠、AZ 覆盖;校验失败的错误码 | 实现 `internal/services/network` 校验服务:子网存在性、CIDR 与容器网段不重叠、eni 子网覆盖;接入 CCECluster reconcile(失败→`NetworkReady=False`+信息) | 单测(校验矩阵)+ 真实 CCE 冒烟(不满足条件→拒绝并给出明确错误) | ✅已实现(网络校验服务接入) |
| A2 | `internal/services/cce/cce.go:128-129` TODO(P0) hostNetwork/authentication 映射 | Q4、Q5 | hostNetwork 子网、认证方式(认证模式/证书)的 API 参数 | `CreateClusterInput` 增加 `hostNetwork.subnetId`、`authentication` 字段并映射 SDK 模型 | 单测(映射)+ 冒烟(创建的集群参数与确认一致) | ✅已确认(Q4/Q5) |
| A3 | `internal/services/cce/cce.go:149-150` + `controllers/ccemanagedcontrolplane_controller.go:223-224` TODO(P0) 删除语义 | **Q8** | ✅ 已确认(官方 cce_02_0241):DeleteCluster 带删除选项 `delete_evs`(默认 false→残留!)/`delete_eni`(默认 block)/`delete_net`(默认 block)/`delete_efs`/`delete_obs`/`delete_sfs`;删除为异步;实测:删除时长、Unavailable 可删性、节点池先行 | 实现删除编排:先删节点池→删集群(显式传 `delete_evs=true/block` 等选项防残留)→轮询消失;按确认处理 Unavailable | e2e(删除后核对无 EVS/ELB 残留)+ 单元(状态机) | ✅已实现(删除选项传参 delete_evs/eni/net=true),待实测 |
| A4 | `controllers/ccemanagedcontrolplane_controller.go` 主流程(等 infra → 建集群 → 等 Available) | **Q1/Q12** | ✅ **实测确认**:空集群(Turbo/eni 与 Standard/vpc-router 均成功)创建并到 Available;Q12:包周期创建响应不返回集群 ID → 按名称查询 | 主流程实测可行;实现注意:包周期后按名称查询集群 ID | 冒烟已通过 | ✅已确认+冒烟通过 |

### B 组:节点池与扩缩容(阻塞主链路)

| # | 代码位置 | 问卷项 | 依赖的确认结论 | 确认后的实现动作 | 验收方式 | 落地状态 |
|---|---|---|---|---|---|---|
| B1 | `internal/services/cce/cce.go`(ScaleNodePool 语义,已改) | **Q3** | ✅ **实测确认=绝对值**(冒烟:2 节点池 ScaleNodePool(2)→"No scale task needed"无操作;ScaleNodePool(4)→扩到 4 节点);`scaleGroups` 必填默认 `default` | ✅ 已按绝对目标实现(`desiredNodeCount = replicas`) | 冒烟已通过(PASS) | ✅已确认+已实现+冒烟通过 |
| B1b | `internal/services/cce` UpdateNodePool 对齐数量(新增) | Q3 | ✅ 已确认(官方 cce_02_0356):更新节点池**不填 initialNodeCount 时期望数默认变 0→会缩容**;`ignoreInitialNodeCount: true` 可保持原样 | 实现更新路径:不想动节点数→传 `ignoreInitialNodeCount=true`;想对齐→传 `initialNodeCount=目标值` | 单测 + 冒烟 | 已确认,待实现 |
| B2 | `controllers/ccemanagedmachinepool_controller.go:136,151` 扩缩容与状态同步 | Q3 | 扩缩容期间状态;availableReplicas 口径(节点 Active 数来源) | 实现 replicas 对齐算法(含并发/伸缩中重试);`availableReplicas` 用 ListNodes 节点 Active 计数回填 | e2e(3→5→3)+ 状态一致性断言 | ✅已确认(Q3 绝对值),按绝对目标实现 |
| B3 | `internal/features/features.go:21` NodePoolAutoscaling gate | Q3 | 节点池 autoscaling 与外部 ScaleNodePool 的冲突/优先级 | 实现 FR-2.6:gate 开启时映射 autoscaling.enable/min/max;与 CAPI replicas 协调策略按确认定 | 单测 + 冒烟 | ✅已确认(手动伸缩不受 autoscaling 范围限制,可并存) |
| B4 | `config/samples/cluster-template.yaml:90` flavor `c7.large.2` | Q6/Q7 | 规格可用性(region 差异) | 样例按确认的规格表更新;webhook 增加 flavor 白名单校验(按 region) | webhook 单测 | ⏳待实测(flavor 按 region) |

### C 组:凭证、kubeconfig 与安全组

| # | 代码位置 | 问卷项 | 依赖的确认结论 | 确认后的实现动作 | 验收方式 | 落地状态 |
|---|---|---|---|---|---|---|
| C1 | `controllers/ccemanagedcontrolplane_controller.go:35-36` kubeconfig 有效期(365 天) | **Q2** | 有效期上限(1~1827,`-1`=5 年?默认值);过期失效语义;external/internal 切换 | 有效期参数化(可配);实现到期前自动刷新(FR-3.3,reconcile 检查 Secret 有效期) | 单测(轮换)+ 冒烟(降级/恢复) | ✅已实现(kubeconfig 轮换,30 天阈值) |
| C2 | `controllers/ccemanagedcontrolplane_controller.go` kubeconfig server 地址 | Q2/Q13 | internal 地址形态;跨 VPC/Region 网络路径 | 按确认选择 endpoint 回填策略(public/private)与 kubeconfig current-context;补充网络路径文档 | 冒烟(管理集群可达性) | ✅已确认(Q2/Q13);**实测:UpdateClusterEip 绑定 EIP 后 https://EIP:5443 公网可达(reachable=true)** |
| C3 | `internal/services/cce/cce.go` CreateNodePool 安全组绑定 | Q5 | 节点池安全组上限(≤5)行为、Standard 是否支持、修改生效方式 | 按确认完善 securityGroups 映射与校验;集群级安全组策略(如需) | webhook 单测 + 冒烟 | ✅已确认(Q5),Standard 支持待实测 |

### D 组:错误处理与运维

| # | 代码位置 | 问卷项 | 依赖的确认结论 | 确认后的实现动作 | 验收方式 | 落地状态 |
|---|---|---|---|---|---|---|
| D1 | `internal/services/errors/errors.go:18-21,44` 错误码全集与限流 | **Q14** | ✅ 已确认(官方 ErrorCode.html):404=`CCE.01404001`;409=`CCE.01409001`;403 权限=`CCE.01403001`、状态不允许=`CCE.01403003~09`;400 配额=`CCE.01400007/08/09/10/11/12/13/19/20/25`;429=`CCE.01429002/003`、`APIGW.0308`;分页:ListNodes limit 1-2000+marker;配额查询 `ShowQuotas`;**实测:持续 ~71 req/s 即大量限流(1000 并发调用 703 次 429),阈值远低于 APIGW 默认 200 req/s → 控制器 429 退避(指数+抖动)必须实现** | ✅ 已落地(errors.go 已按官方码更新:IsNotFound/IsConflict/IsThrottled/IsQuotaExceeded);补:控制器按分类退避(429 → 指数退避) | 单测(分类矩阵)+ 冒烟(高频调用,已 PASS) | 已确认+代码已落地+冒烟通过 |
| D2 | `config/samples/cluster-template.yaml:38-57` VPC/子网/ENI 子网/版本占位 | Q4/Q11/Q13 | 各 VERIFY 占位的确切值 | 样例改为变量说明 + `scripts/check-prerequisites.sh` 增加 VPC/子网/配额/权限预检(FR-GOV/部署规范) | 脚本单测 | ✅已确认(Q4/Q11/Q13) |
| D3 | IAM 最小权限与委托(文档/凭证) | **Q6** | 最小权限集合;AK/SK 账号约束;agencyName 语义 | 更新 README/部署文档的权限清单;预检脚本校验;`agencyName` 默认值按确认调整 | 文档审查 + 冒烟 | ✅已确认(Q6) |
| D4 | 计费与休眠/唤醒 | Q12 | billingMode 语义;空集群计费;Awake/Hibernate | 按确认补充 billing 文档与(可选)休眠唤醒支持;样例计费提示更新 | 文档审查 | ✅已确认(Q12) |

### E 组:远期(不阻塞 PoC)

| # | 代码位置 | 问卷项 | 依赖的确认结论 | 确认后的实现动作 | 验收方式 | 落地状态 |
|---|---|---|---|---|---|---|
| E1 | (无代码)单节点路径 | **Q9** | AddNode/AddNodesToNodePool 引导要求 | 若可行:新增 `CCEMachine`(P2,FR-2.8);若不可行:从 roadmap 移除并文档说明 | — | ✅已确认(Q9,建议只走节点池) |
| E2 | (无代码)Autopilot | Q10 | Autopilot 与 CAPI 对接方式 | P2 评估(Cluster + 无 MachinePool) | — | ✅已确认(Q10) |
| E3 | (无代码)集群升级 | Q11 | CreateUpgradeWorkFlow 参数与升级状态 | P1 实现 FR-1.7(改 version 触发升级工作流 + conditions) | e2e(升级) | ✅已确认(Q11);**实测定论:ShowClusterUpgradeInfo 返回 offered 升级目标=空(平台当前不提供跨版本路径)→ 升级工作流实现时必须把"无可用目标"作为正常状态(文档化+日志),耗时量级在无路径时不可实测,需咨询华为云** |

## 二、执行批次与顺序建议

| 批次 | 问卷项 | 说明 |
|---|---|---|
| **第一批(阻塞主链路,优先确认)** | Q1、Q3、Q4、Q8 | 决定集群创建/扩缩容/删除三条主链路的正确性,未确认前这几处代码保持 TODO/WARNING |
| **第二批(完善)** | Q2、Q5、Q14 | kubeconfig 轮换、安全组、错误分类/退避 |
| **第三批(文档/运维)** | Q6、Q7、Q12、Q13 | 权限/配额预检脚本、网络路径文档、计费说明 |
| **远期** | Q9、Q10、Q11 | 单节点路径(P2)、Autopilot(P2)、升级(P1) |

## 三、落地完成标准(总)

1. 问卷 14 项全部有华为云结论(填写到 `cce-verification-questionnaire.md` 汇总表);
2. 本清单 A–E 组所有行"落地状态"= 已完成(附提交号);
3. `grep -rn "TODO(P0)\|VERIFY" cmd/ api/ controllers/ internal/` 无残留(P1/P2 项可保留标注);
4. 对应功能通过单测 + 真实 CCE 冒烟(创建→就绪→扩缩容→删除全链路,e2e 门槛 FR-10.4)。
