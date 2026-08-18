# PoC VERIFY/TODO → 华为云对齐问卷 逐项落地跟踪清单

> 关联文档:[华为云 CCE 对齐问卷](cce-verification-questionnaire.md)(14 项,待华为云确认)、[调研依据与事实清单](research-sources.md)、[架构设计文档](architecture-design.md)、[需求设计文档](requirements-design.md)
>
> **用法**:华为云确认问卷后,按本清单"确认后的实现动作"逐项落地;每项完成后回填"落地状态/提交"列,并跑对应验收。**在对应问卷项确认前,不实现依赖其结论的云侧行为。**

## 一、代码 VERIFY/TODO 与问卷项映射

### A 组:集群创建与网络(阻塞主链路)

| # | 代码位置 | 问卷项 | 依赖的确认结论(待华为云填写) | 确认后的实现动作 | 验收方式 | 落地状态 |
|---|---|---|---|---|---|---|
| A1 | `controllers/ccecluster_controller.go:89-90` TODO(P0) 网络校验服务 | **Q4**(eni VPC/子网硬性要求)、Q5 | eni 对 VPC/子网要求:ENI 子网、网段不重叠、AZ 覆盖;校验失败的错误码 | 实现 `internal/services/network` 校验服务:子网存在性、CIDR 与容器网段不重叠、eni 子网覆盖;接入 CCECluster reconcile(失败→`NetworkReady=False`+信息) | 单测(校验矩阵)+ 真实 CCE 冒烟(不满足条件→拒绝并给出明确错误) | 待确认 |
| A2 | `internal/services/cce/cce.go:128-129` TODO(P0) hostNetwork/authentication 映射 | Q4、Q5 | hostNetwork 子网、认证方式(认证模式/证书)的 API 参数 | `CreateClusterInput` 增加 `hostNetwork.subnetId`、`authentication` 字段并映射 SDK 模型 | 单测(映射)+ 冒烟(创建的集群参数与确认一致) | 待确认 |
| A3 | `internal/services/cce/cce.go:149-150` + `controllers/ccemanagedcontrolplane_controller.go:223-224` TODO(P0) 删除语义 | **Q8** | DeleteCluster 级联行为、时长、EIP/EVS/ELB 残留、Unavailable 可删性、删除中状态 | 实现删除编排:先删节点池→删集群→轮询消失;按确认处理残留与 Unavailable;删除卡死兜底 | e2e(删除后核对无残留)+ 单元(状态机) | 待确认 |
| A4 | `controllers/ccemanagedcontrolplane_controller.go` 主流程(等 infra → 建集群 → 等 Available) | **Q1** | 空集群(0 节点)创建可行性与计费/配额 | 若空集群不可建或受限:调整主流程(创建集群时附带首节点池);补充计费/配额预检 | 冒烟(空集群创建)+ 文档 | 待确认 |

### B 组:节点池与扩缩容(阻塞主链路)

| # | 代码位置 | 问卷项 | 依赖的确认结论 | 确认后的实现动作 | 验收方式 | 落地状态 |
|---|---|---|---|---|---|---|
| B1 | `internal/services/cce/cce.go:221-224` WARNING(ScaleNodePool 增量语义) | **Q3** | `desiredNodeCount` 是增量还是绝对值;`scaleGroups` 默认值;缩容限制 | 若=增量:保持现状+补强注释与日志;若=绝对值:改为直接传 `replicas`;统一控制器侧 delta 计算 | 单测(两种语义)+ 真实扩缩容冒烟 | 待确认 |
| B2 | `controllers/ccemanagedmachinepool_controller.go:136,151` 扩缩容与状态同步 | Q3 | 扩缩容期间状态;availableReplicas 口径(节点 Active 数来源) | 实现 replicas 对齐算法(含并发/伸缩中重试);`availableReplicas` 用 ListNodes 节点 Active 计数回填 | e2e(3→5→3)+ 状态一致性断言 | 待确认 |
| B3 | `internal/features/features.go:21` NodePoolAutoscaling gate | Q3 | 节点池 autoscaling 与外部 ScaleNodePool 的冲突/优先级 | 实现 FR-2.6:gate 开启时映射 autoscaling.enable/min/max;与 CAPI replicas 协调策略按确认定 | 单测 + 冒烟 | 待确认 |
| B4 | `config/samples/cluster-template.yaml:90` flavor `c7.large.2` | Q6/Q7 | 规格可用性(region 差异) | 样例按确认的规格表更新;webhook 增加 flavor 白名单校验(按 region) | webhook 单测 | 待确认 |

### C 组:凭证、kubeconfig 与安全组

| # | 代码位置 | 问卷项 | 依赖的确认结论 | 确认后的实现动作 | 验收方式 | 落地状态 |
|---|---|---|---|---|---|---|
| C1 | `controllers/ccemanagedcontrolplane_controller.go:35-36` kubeconfig 有效期(365 天) | **Q2** | 有效期上限(1~1827,`-1`=5 年?默认值);过期失效语义;external/internal 切换 | 有效期参数化(可配);实现到期前自动刷新(FR-3.3,reconcile 检查 Secret 有效期) | 单测(轮换)+ 冒烟(降级/恢复) | 待确认 |
| C2 | `controllers/ccemanagedcontrolplane_controller.go` kubeconfig server 地址 | Q2/Q13 | internal 地址形态;跨 VPC/Region 网络路径 | 按确认选择 endpoint 回填策略(public/private)与 kubeconfig current-context;补充网络路径文档 | 冒烟(管理集群可达性) | 待确认 |
| C3 | `internal/services/cce/cce.go` CreateNodePool 安全组绑定 | Q5 | 节点池安全组上限(≤5)行为、Standard 是否支持、修改生效方式 | 按确认完善 securityGroups 映射与校验;集群级安全组策略(如需) | webhook 单测 + 冒烟 | 待确认 |

### D 组:错误处理与运维

| # | 代码位置 | 问卷项 | 依赖的确认结论 | 确认后的实现动作 | 验收方式 | 落地状态 |
|---|---|---|---|---|---|---|
| D1 | `internal/services/errors/errors.go:18-21,44` 错误码全集与限流 | **Q14** | 常用错误码表;限流阈值;429 是否含 Retry-After | 扩展错误分类(NotFound/Conflict/Throttle/配额不足/权限不足/状态不允许);控制器按分类退避(指数 + Retry-After) | 单测(分类矩阵)+ 冒烟(高频调用) | 待确认 |
| D2 | `config/samples/cluster-template.yaml:38-57` VPC/子网/ENI 子网/版本占位 | Q4/Q11/Q13 | 各 VERIFY 占位的确切值 | 样例改为变量说明 + `scripts/check-prerequisites.sh` 增加 VPC/子网/配额/权限预检(FR-GOV/部署规范) | 脚本单测 | 待确认 |
| D3 | IAM 最小权限与委托(文档/凭证) | **Q6** | 最小权限集合;AK/SK 账号约束;agencyName 语义 | 更新 README/部署文档的权限清单;预检脚本校验;`agencyName` 默认值按确认调整 | 文档审查 + 冒烟 | 待确认 |
| D4 | 计费与休眠/唤醒 | Q12 | billingMode 语义;空集群计费;Awake/Hibernate | 按确认补充 billing 文档与(可选)休眠唤醒支持;样例计费提示更新 | 文档审查 | 待确认 |

### E 组:远期(不阻塞 PoC)

| # | 代码位置 | 问卷项 | 依赖的确认结论 | 确认后的实现动作 | 验收方式 | 落地状态 |
|---|---|---|---|---|---|---|
| E1 | (无代码)单节点路径 | **Q9** | AddNode/AddNodesToNodePool 引导要求 | 若可行:新增 `CCEMachine`(P2,FR-2.8);若不可行:从 roadmap 移除并文档说明 | — | 待确认 |
| E2 | (无代码)Autopilot | Q10 | Autopilot 与 CAPI 对接方式 | P2 评估(Cluster + 无 MachinePool) | — | 待确认 |
| E3 | (无代码)集群升级 | Q11 | CreateUpgradeWorkFlow 参数与升级状态 | P1 实现 FR-1.7(改 version 触发升级工作流 + conditions) | e2e(升级) | 待确认(实现前) |

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
