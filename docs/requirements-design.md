# cce-provider-for-cluster-api 需求设计文档

- 版本:v0.2(需求+PoC 验证版)
- 状态:需求定稿(全部 P0/P1 项已实现,经真实 CCE 冒烟与 clusterctl 部署验证;P2 项为远期)
- 配套文档:[调研依据与事实清单](research-sources.md)、[架构设计文档](architecture-design.md)、[华为云 CCE 对齐问卷](cce-verification-questionnaire.md)、[验证结论记录](cce-verification-findings.md)、[clusterctl 部署演练记录](clusterctl-deployment-validation.md)、[CAPA 架构分析报告](CAPA架构分析报告.md)、[CAPHW 架构分析报告](CAPHW架构分析报告.md)、[ACKProvider 架构分析报告](ACKProvider架构分析报告.md)

> **事实基准声明**:同架构文档,所有需求项的依据均来自真实来源(华为云官方 SDK/文档、CAPA 与阿里云 ACK Provider 源码);无法从公开资料确认处标注 **[需验证]**,需对接真实华为云 CCE 确认(完整清单见 [research-sources.md §4](research-sources.md))。
>
> **验证状态(2026-08-19)**:各 FR 中引用的 [需验证] 项已逐项确认——Q1/Q2/Q3/Q5/Q7/Q8/Q13/Q14 真实冒烟实测、Q4/Q6/Q9/Q10/Q12 官方文档确认、Q11 实测定论(无跨版本目标为正常状态);落地状态逐项见 [验证结论记录](cce-verification-findings.md) 与 [落地跟踪](poc-implementation-tracker.md)。**P0/P1 全部实现;仅 Q11 升级耗时、Q14 Retry-After 需华为云/工单补充。**

---

## 1. 项目目标与范围

### 1.1 目标

为华为云 CCE 托管集群提供符合 Cluster API 规范的 Infrastructure Provider,实现"声明式创建、扩缩容、删除"CCE 集群与节点池,对标 `CAPI + AWS EKS 托管模式`,并兼容 `clusterctl` 工具链。

### 1.2 非目标(首版明确排除)

- 不在 ECS 上自建 Kubernetes(那是 CAPHW 的领域,本项目只对接 CCE 托管服务);
- 不管理 CCE Autopilot(Serverless)集群(单独评估);
- 不自动创建 VPC/子网(首版"引用+校验";自动创建列为二期);
- 不合并 kubernetes-sigs 上游(发布到华为云官方组织,对标阿里云模式)。

### 1.3 术语

- CAPI:Cluster API;CP:控制面;MP:MachinePool;CABPK:Kubeadm Bootstrap Provider;Contract:Provider 合约。

---

## 2. 用户画像与核心使用场景

| 场景 | 用户 | 说明 |
|---|---|---|
| S1 创建托管集群 | 平台工程师 | 在管理集群 apply `Cluster`+`CCECluster`+`CCEManagedControlPlane`+`MachinePool`+`CCEManagedMachinePool`,自动获得可用 CCE 集群 |
| S2 扩缩容 | 平台工程师/运维 | 改 `MachinePool.spec.replicas` 触发节点池扩缩容 |
| S3 取 kubeconfig | 开发/运维 | `clusterctl get kubeconfig <cluster>` 获得工作集群访问凭据 |
| S4 删除集群 | 运维 | 删除 Cluster 对象,CCE 集群与节点池按依赖顺序清理 |
| S5 GitOps 管理 | 平台团队 | ArgoCD/Flux 同步集群定义(用户聊天中已明确的交付方式) |
| S6 集群升级(二期) | 平台工程师 | 改 `CCEManagedControlPlane.spec.version` 触发 CCE 升级编排 |

---

## 3. 功能需求(FR)

优先级标记:**P0**(首版必须)/ **P1**(二期)/ **P2**(远期)。

### 3.1 集群生命周期(核心)

| 编号 | 需求 | 优先级 | 依据/说明 |
|---|---|---|---|
| FR-1.1 | 通过 `CCECluster` + `CCEManagedControlPlane` 声明式创建 CCE 集群(Standard/Turbo) | P0 | CCE API `CreateCluster`(官方文档 cce_02_0236);字段含 category/flavor/version/containerNetwork/serviceNetwork/authentication/customSan 等 |
| FR-1.2 | 集群创建幂等:重复 reconcile 不重复创建(固定命名 + 创建冲突时按名称接管 adopt-by-name,实测确认) | P0 | ACK Provider 固定名 Get 判存在模式;实测补充:创建成功但响应丢失(限流边界)时按名称接管已有集群 |
| FR-1.3 | 等待集群 phase=Available 后回写 `status.clusterID`、`status.controlPlaneEndpoint` | P0 | 官方 phase 枚举(Available/Unavailable/ScalingUp/ScalingDown);`ShowClusterEndpoints` 返回 url+type |
| FR-1.4 | 集群更新:支持可变更字段(描述、标签、日志、插件等)对齐;网段等不可变字段变更由 webhook 拒绝 | P0 | 官方:隧道模式网段创建后不可改,vpc-router/eni 可增不可改 |
| FR-1.5 | 集群删除:先删依赖(节点池→插件→集群),容忍 404,轮询至消失后移除 finalizer | P0 | 参照 CAPA reconcileDelete 的依赖计数 + 错误聚合模式([CAPA 架构分析报告](CAPA架构分析报告.md) §3.1) |
| FR-1.6 | `Unavailable` 等异常状态集群的状态上报与失败条件 | P0 | 官方 phase:Unavailable 需手动删除 |
| FR-1.7 | 集群升级(改 version 触发 CCE 升级工作流) | P1(已实现) | CCE API `CreateUpgradeWorkFlow/CreatePreCheck/CreatePostCheck`(SDK 事实);升级行为细节 **[需验证] 11 → 已定论:平台当前无跨版本目标,空目标为正常状态** |
| FR-1.8 | 集群休眠/唤醒(AwakeCluster) | P2 | SDK 有 `AwakeCluster`;运维策略,优先级低 |
| FR-1.9 | 集群标签(AdditionalTags)同步 `BatchCreateClusterTags` | P1 | SDK 事实;用于资源归属识别 |

### 3.2 节点池管理(核心)

| 编号 | 需求 | 优先级 | 依据/说明 |
|---|---|---|---|
| FR-2.1 | `CCEManagedMachinePool` ↔ CCE 节点池:创建(flavor/rootVolume/dataVolumes/os/sshKey/taints/labels/replicas) | P0 | 官方节点池文档 cce_02_0354(nodeTemplate/initialNodeCount 等) |
| FR-2.2 | 扩缩容:`MachinePool.spec.replicas` 变更 → 节点池伸缩 API(`ScaleNodePool`,SDK 事实;IAM `cce:nodepool:scale`)或 `UpdateNodePool(initialNodeCount)` | P0 | 伸缩语义 **[需验证] 3** |
| FR-2.3 | 状态回写:`status.replicas/availableReplicas`(按节点 `Active` 数)、`status.nodePoolID` | P0 | 参照 ACK Provider AliyunManagedMachinePoolStatus;节点状态枚举见 SDK model_node_status.go |
| FR-2.4 | 节点池删除 → `DeleteNodePool` → 轮询至不存在 → 移除 finalizer | P0 | SDK 事实 |
| FR-2.5 | 控制面未就绪时节点池等待(WaitingForControlPlane 条件) | P0 | CAPA 模式:MachinePool 控制器须等 ControlPlane.Status.Ready([CAPA 架构分析报告](CAPA架构分析报告.md) §3.3) |
| FR-2.6 | 节点池自动扩缩容(autoscaling enable/min/max)映射 | P1(已实现,Alpha gate 默认关) | 与 CAPI 扩缩容双驱动语义冲突处理 **[需验证] 3 → 已实测:并存不冲突(B3)**;`--feature-gates=NodePoolAutoscaling=true` 启用 |
| FR-2.7 | 节点池安全组绑定(Turbo ≥1.21,≤5 个) | P1 | 官方文档 cce_02_0354 |
| FR-2.8 | 单节点路径(`CCEMachine` ↔ CCE CreateNode/AddNode) | P2 | 引导语义 **[需验证] 9**;若不可行则移除 |
| FR-2.9 | 节点滚动替换(节点异常自动重建) | P2 | CCE 节点池自身能力评估后映射 |

### 3.3 kubeconfig 管理

| 编号 | 需求 | 优先级 | 依据/说明 |
|---|---|---|---|
| FR-3.1 | 集群就绪后调用 `CreateKubernetesClusterCert` 获取 kubeconfig,写入 `<cluster>-kubeconfig` Secret | P0 | SDK 事实;对标 CAPA `<cluster>-kubeconfig` 与 ACK Provider controller_kubeconfig.go |
| FR-3.2 | 支持 `clusterctl get kubeconfig` | P0 | Secret key `value` 规范(CAPI 约定) |
| FR-3.3 | 证书轮换(有效期到期前自动刷新) | P1(已实现) | `ClusterCertDuration` 有效期上限 **[需验证] 2 → 已确认:1~1827 天,重新签发即时生效** |
| FR-3.4 | 私网/公网 kubeconfig 选择(current-context external/internal) | P1 | 官方响应字段;网络可达性 **[需验证] 13 → 已确认:公网 https://EIP:5443 实测可达** |

### 3.4 凭证与安全

| 编号 | 需求 | 优先级 | 依据/说明 |
|---|---|---|---|
| FR-4.1 | 每集群凭证 Secret(推荐)+ 全局兜底(env/默认 Secret) | P0 | 修正 CAPHW 全局单凭证/明文打印反例([CAPHW 架构分析报告](CAPHW架构分析报告.md) §7) |
| FR-4.2 | 凭证缺失/非法 → CredentialsReady=False + 事件提示 | P0 | ACK Provider:凭证缺失程序拒绝启动(README) |
| FR-4.3 | 凭证不写入日志/镜像/userdata;日志脱敏 | P0 | 安全要求;CAPHW 把 AK/SK 写入 cloud-config 为反例 |
| FR-4.4 | 支持 CCE 集群委托 agencyName(cce_cluster_agency) | P1 | 官方委托说明(系统委托 cce_admin_trust 全量权限 vs cce_cluster_agency 最小权限) |
| FR-4.5 | IAM 临时凭证/委托链支持 | P2 | CAPA identity 三层模型([CAPA 架构分析报告](CAPA架构分析报告.md) §6)为远期参考 |

### 3.5 网络

| 编号 | 需求 | 优先级 | 依据/说明 |
|---|---|---|---|
| FR-5.1 | 引用已有 VPC/子网,创建前校验存在性与 CIDR 冲突 | P0 | 官方:创建集群前必须先有 VPC |
| FR-5.2 | 容器网段/服务网段规划校验(不与 VPC 冲突;服务网段默认 10.247.0.0/16) | P0 | 官方文档 cce_02_0236 |
| FR-5.3 | Turbo(eni)模式 ENI 子网配置 | P0 | 官方:eni 需 ENI 子网;VPC 与 Pod 网段不重叠规则 **[需验证] 4** |
| FR-5.4 | 公网访问控制(endpointAccess.public / 私网) | P0 | 对标 ACK Provider EndpointAccess.Public |
| FR-5.5 | 自动创建 VPC/子网(可选) | P1 | 对标 ACK Provider VPC/VSwitch MR |
| FR-5.6 | 安全组策略:节点池绑安全组、控制面访问白名单 | P1 | 官方:节点池 ≤5 个安全组;行为 **[需验证] 5** |

### 3.6 插件(Addon)

| 编号 | 需求 | 优先级 | 依据/说明 |
|---|---|---|---|
| FR-6.1 | `CCEManagedControlPlane.spec.addons` 声明式管理 CCE 插件(coredns/vpc-cni/autoscaler 等) | P1 | SDK `CreateAddonInstance/UpdateAddonInstance/DeleteAddonInstance`;对标 CAPA EKS Addons(失败不阻塞集群就绪) |
| FR-6.2 | 创建集群时附装插件(annotations 方式) | P1 | 官方:创建集群请求体 annotations 可装插件(如 icagent) |

### 3.7 集群类型支持

| 编号 | 需求 | 优先级 | 依据/说明 |
|---|---|---|---|
| FR-7.1 | CCE Standard(overlay_l2 / vpc-router)与 CCE Turbo(eni)均支持 | P0 | 官方集群类型对比;同一 API 不同参数 |
| FR-7.2 | 默认值:Turbo + eni(对标 EKS 定位);webhook 强校验 category 与 mode 一致性 | P0 | SDK:eni 模式默认 Turbo;Turbo 默认 2000 节点 |
| FR-7.3 | CCE Autopilot 支持评估 | P2 | 独立 API 族(SDK 事实);无节点概念,语义不同 |

### 3.8 Provider Contract 与工具链

| 编号 | 需求 | 优先级 | 依据/说明 |
|---|---|---|---|
| FR-8.1 | CRD namespace-scoped;TypeMeta/ObjectMeta;`cluster.x-k8s.io/v1beta1(+v1beta2)` 版本标签 | P0 | Contract 硬性要求(用户聊天已确认清单) |
| FR-8.2 | status.conditions 标准 Conditions + finalizer + paused 支持 | P0 | Contract + CAPA/CAPHW 模式 |
| FR-8.3 | `metadata.yaml` + `infrastructure-components.yaml` + `cluster-template.yaml`,支持 `clusterctl init --infrastructure cce` | P0 | clusterctl 合约;ACK Provider 仅做到 describe/get kubeconfig 兼容、未做 init 打包([ACKProvider 架构分析报告](ACKProvider架构分析报告.md) §11),我们补齐;打包模式参照 CAPA |
| FR-8.4 | webhook:defaulting + validating(网段不可变、category/mode 一致性、flavor 白名单、taints≤20、SG≤5) | P0 | 官方约束 + 对齐 ACK Provider webhook |
| FR-8.5 | 发布到华为云官方组织与开发者社区;文档结构对齐 CAPA/CAPHW(docs/book) | P0 | 用户已确认的发布策略 |

### 3.9 可观测性

| 编号 | 需求 | 优先级 | 依据/说明 |
|---|---|---|---|
| FR-9.1 | Kubernetes 事件(创建/删除/扩缩容/凭证失败) | P0 | CAPA pkg/record 模式 |
| FR-9.2 | 结构化日志(logr)+ 凭证脱敏 | P0 | controller-runtime |
| FR-9.3 | Prometheus 指标(API 调用/错误/限流计数) | P1 | config/prometheus 对齐 CAPHW |
| FR-9.4 | OpenTelemetry 追踪(可选) | P2 | — |

### 3.10 测试

| 编号 | 需求 | 优先级 | 依据/说明 |
|---|---|---|---|
| FR-10.1 | Service 层单元测试(SDK mock) | P0 | CAPA interfaces.go 注入模式 |
| FR-10.2 | Controller 层 envtest 测试(调谐顺序/conditions/finalizer) | P0 | CAPHW 有 envtest 骨架但无断言(反例,需真实断言) |
| FR-10.3 | Webhook 单测 | P0 | — |
| FR-10.4 | e2e:真实华为云账号,CAPI 官方 e2e framework(创建→就绪→扩缩容→删除) | P0 | CAPA test/e2e;ACK Provider/CAPHW 的 e2e 不触达真实云(反例,本项目作为发布门槛) |
| FR-10.5 | 升级 e2e(二期) | P1 | — |

### 3.11 仓库治理与发布规范(华为云解决方案开发者套件)

> 依据:华为云代码仓库治理规范(仓库命名、标准套件结构、生命周期、README/LICENSE/CONTRIBUTING/CODE_OF_CONDUCT/deploy/scripts/.github 各规范)。已与用户确认:**仓库名 `cloudnative-cluster-api-provider-cce`、许可证 MIT-0**。

| 编号 | 需求 | 优先级 | 说明/状态 |
|---|---|---|---|
| FR-GOV-1 | 仓库命名 `<领域前缀>-<场景描述>`(全小写英文、连字符) | P0 | 已定:`cloudnative-cluster-api-provider-cce`(技术领域:云原生) |
| FR-GOV-2 | 标准套件结构:README.md、LICENSE、CONTRIBUTING.md、CODE_OF_CONDUCT.md、deploy/、app/、docs/、scripts/、.github/(ISSUE_TEMPLATE、PULL_REQUEST_TEMPLATE、workflows) | P0 | 已按规范搭建并 git init 提交:README 17 节、LICENSE MIT-0、DCO 流程、CoC 标准模板、deploy 脚本+变量说明+销毁命令、app/ 预留示例应用、scripts 头部注释/幂等、.github 模板+4 workflow;`.gitignore` 排除环境产物(.codeartsdoer/、.merkle-snapshot.json) |
| FR-GOV-3 | LICENSE 默认 MIT-0;Apache 2.0 为例外需申请并经工作组批准;严禁 GPL/AGPL 依赖 | P0 | MIT-0 已定;依赖引入时须做许可证扫描(禁止 GPL/AGPL) |
| FR-GOV-4 | README 17 节结构,CRITICAL 章节(标题徽章/简介/前置条件/快速开始/使用方法验证/许可证/联系方式)不可缺失;非中国限定场景主 README 用英文,**并提供中文版(README.zh-CN.md 等,全仓库 MD 均中英双语)** | P0 | README 已按 17 节编写(英文主版 + 中文版,互链);发布前复核各 CRITICAL 节 |
| FR-GOV-5 | CONTRIBUTING:DCO 签名(`git commit -s`)、Fork 流程、5 个工作日内响应、至少 1 名维护者批准、贡献按仓库许可分发 | P0 | 已编写 |
| FR-GOV-6 | CODE_OF_CONDUCT:标准模板(不得修改条款)+ 官方维护者联络方式 | P0 | 已编写(联络邮箱占位,仓库创建时替换) |
| FR-GOV-7 | deploy 规范:密钥仅环境变量/输入提示、变量说明(variables.md)、CI 语法验证、销毁命令 | P0 | 已提供 deploy/scripts/{deploy-provider,destroy}.sh + variables.md;iac-validate workflow 已接 |
| FR-GOV-8 | scripts 规范:破坏性操作需确认、头部注释(功能/用法/依赖)、幂等 | P0 | check-prerequisites.sh 已满足;新增脚本须遵守 |
| FR-GOV-9 | .github:bug/feature Issue 模板、PR 模板(关联 Issue/DCO 复选框/检查清单)、4 个 workflow(dco-check/markdown-lint/secret-scan/iac-validate) | P0 | 已创建 |
| FR-GOV-10 | 仓库生命周期:起步 incubating;季度巡检,6 个月无更新且无活跃 Issue 进入归档评估 | P0 | 当前状态 incubating;README 徽章已标注 |

---

## 4. 非功能需求(NFR)

| 编号 | 需求 | 说明 |
|---|---|---|
| NFR-1 幂等性 | 任意步骤失败可安全重试,无副作用 | 设计原则;"创建前必查" |
| NFR-2 性能 | 单集群 reconcile 收敛时间:创建 ≤ 15min(CCE 集群创建时长为主,受云侧限制)、扩缩容分钟级;控制器每秒处理 reconcile 数满足管理集群规模 | 轮询间隔默认 15-30s(CAPA WaitInfraPeriod 类比) |
| NFR-3 可靠性 | 控制面 3 副本部署 + leader election;reconcile 崩溃可恢复;finalizer 防孤儿云资源 | CAPHW main.go 有 leader election 可参考 |
| NFR-4 安全 | 凭证最小暴露;RBAC 最小权限;镜像无真实凭证;密钥不进日志 | §3.4 |
| NFR-5 兼容性 | 遵循 CAPI 版本矩阵(metadata.yaml 声明);K8s/CAPI 版本升级 CI 验证 | 用户聊天确认的社区对齐要求 |
| NFR-6 可维护性 | 分层(controller/scope/service)可单测;代码规范(Go lint/boilerplate);docs/book 文档 | 对齐 SIG 规范 |
| NFR-7 限流友好 | CCE/ECS/VPC API 限流阈值内工作,退避,不风暴 | 限流阈值 **[需验证] 14 → 已实测**:读 ~70 req/s、写 10 次/分钟(APIGW.0308);实现为限流/配额错误返回延迟 requeue+nil error(`resultAfterError`),不产生 error 风暴 |
| NFR-8 成本可控 | 空集群计费与资源回收(删除不留 EIP/EVS/ELB 残留) | 残留行为 **[需验证] 8** |

---

## 5. 里程碑与需求优先级汇总(MoSCoW)

| 里程碑 | 内容 | 验收 |
|---|---|---|
| M0 调研定稿(本阶段) | 架构/需求文档、API 冒烟验证清单、华为云侧确认 | [附录 A](#附录-a需对接华为云-cce-验证事项索引) 全部给出结论 |
| M1 PoC(对标用户聊天"1-3 天 vibe coding"预期) | 项目骨架、CRD、CCECluster/CCEManagedControlPlane 创建与删除、kubeconfig Secret、单节点池创建 | 真实账号冒烟:创建→就绪→删除通过 |
| M2 生产化 | webhook、节点池扩缩容、凭证增强、错误处理/退避、clusterctl 集成、单元/envtest 全覆盖 | 全部 P0 FR 通过;e2e 通过 |
| M3 发布 | 升级(FR-1.7)、插件(FR-6.x)、文档、镜像、华为云社区发布;仓库治理合规全量复核(README 17 节 CRITICAL、LICENSE MIT-0、DCO、4 个 CI workflow 常绿、生命周期标签) | P1 项完成或明确排期;发布物齐全;治理巡检通过 |
| M4 扩展 | 自动网络、私网 endpoint、Autopilot 评估、单节点路径 | P2 项按需 |

优先级汇总:必须(P0)18 项 / 应该(P1)10 项 / 可选(P2)7 项(见 §3 各表)。

---

## 6. 验收标准(节选关键场景)

| 场景 | 验收标准 |
|---|---|
| S1 创建 | `kubectl apply` 集群定义后,`clusterctl describe cluster` 显示 Cluster/CP/MP 均 Ready;CCE 控制台可见对应集群与节点池;节点数 = replicas |
| S2 扩缩容 | replicas 3→5,节点池节点数 15 分钟内达到 5 且全部 Ready;replicas 5→3 反向同理 |
| S3 kubeconfig | `clusterctl get kubeconfig <cluster>` 可访问工作集群(能 kubectl get nodes) |
| S4 删除 | 删除 Cluster 后,CCE 集群、节点池、相关 EIP/EVS/ELB 均被清理,无残留;finalizer 不卡死 |
| 幂等 | 控制器重启/网络抖动后重放,集群状态收敛且无重复资源 |
| 失败路径 | 凭证错误→CredentialsReady=False + 事件;限流→退避重试;网段变更→webhook 拒绝 |

---

## 7. 风险与依赖

| 风险/依赖 | 等级 | 应对 |
|---|---|---|
| CCE API 行为与文档假设不符(空集群、扩缩容、kubeconfig 可达性) | 高 | M0 完成 [附录 A] 实测;PoC 先行 |
| 华为云侧配额/权限不满足(集群数、ENI 子网、委托) | 高 | 提前与华为云确认配额与最小权限;文档给出预检脚本 |
| CAPI 合约版本演进(v1beta2) | 中 | 锁定目标 CAPI 版本;metadata.yaml 矩阵;依赖升级 CI |
| 节点池扩缩容与 CAPI 双驱动冲突 | 中 | 首版关闭节点池 autoscaling,单一驱动 |
| 凭证安全(泄露/轮换) | 中 | 每集群 Secret;脱敏;e2e 用一次性测试账号 |
| 依赖上游 SDK(region endpoint 解析、API 演进) | 低 | 锁定 SDK 版本;错误码全集维护 |

---

## 8. 注意事项(开发与交付全流程)

> 本节直接响应用户需求"需要包含注意事项",与用户聊天中整理的"注意事项清单"对齐,并按本项目代码调研结果增补。

### 8.1 领域与 API 注意(来自官方文档/SDK 事实)

1. **网段不可变是"一次性决策"**:容器隧道模式创建后完全不可改;vpc-router/eni 可增不可改;误配需重建集群。→ webhook 强校验 + 默认值审慎 + 文档醒目提示(架构文档 §6.3)。
2. **创建集群前必须先有 VPC**(官方明确);Provider 首版只"引用+校验",要在文档中引导用户预置网络,避免"集群创建失败"的常见误用。
3. **CCE 集群(控制面)与节点是分离资源**:创建集群请求体不含节点;必须先建集群(等 Available)再建节点池(官方:节点池仅在集群可用/扩缩容时可调用)。对应 CAPI 依赖顺序(CP ready → MP)。
4. **eni(Turbo)对网络有硬性要求**:ENI 子网、VPC 与 Pod 网段不重叠、子网可用区覆盖——**[需验证] 4**,PoC 前必须实测,否则 Turbo 集群创建反复失败。
5. **节点池安全组 ≤5 个、taints ≤20 条**(官方约束);webhook 校验,避免 API 报错。
6. **服务网段默认 10.247.0.0/16**,若与用户 VPC/容器网段冲突需显式指定;IPv6 仅特定版本/模式支持(官方文档)。
7. **委托(agency)权限最小化**:默认 cce_admin_trust 权限过大;1.27+ 用 `agencyName` 指定 cce_cluster_agency(官方委托说明);不要给 CCE 组件超出所需的权限。
8. **kubeconfig 有有效期**(ClusterCertDuration),需要轮换逻辑,不能"取一次用永久";私网集群的 kubeconfig server 地址可达性是部署前提 **[需验证] 2/13**。
9. **删除有依赖顺序与残留风险**:节点池须先于集群删除;EIP/EVS/ELB 残留行为需实测 **[需验证] 8**;finalizer 卡死是 CAPI provider 最常见事故,删除路径必须完整 e2e。
10. **API 限流**:CCE/ECS/VPC 各自限流,批量 reconcile 时易触发;必须有错误分类 + 指数退避,否则大规模管理集群会抖动 **[需验证] 14**。

### 8.2 工程与社区对齐注意(来自三仓库代码调研)

11. **不要照抄 CAPHW 的"自建集群"实现**:其 VPC/子网/SG/NAT/EIP 自动创建、kubeadm userdata、cloud-config 明文 AK/SK 注入等全部不适用于 CCE 托管模式(可复用部分见 [CAPHW 架构分析报告](CAPHW架构分析报告.md) §12)。
12. **不要照抄 ACK Provider 的 Crossplane/Upjet + Terraform 内嵌架构**:其 API 类型由 Terraform provider 生成、内嵌 terraform 运行时,复杂度高;本项目直接用华为云官方 Go SDK(cce/v3)更简单可控(见 [ACKProvider 架构分析报告](ACKProvider架构分析报告.md))。
13. **不要照抄 ACK/CAPHW 的"假 e2e"**:两者 e2e 均不触达真实云,是脚手架;本项目把"真实 CCE 冒烟"设为发布门槛。
14. **凭证处理对标 CAPA 而非 CAPHW**:CAPHW 全局单凭证 + 明文打印 AK 是反例;每集群 Secret + 脱敏 + 最小 RBAC。
15. **Contract 合规是"发布即验收"**:版本标签、conditions、finalizer、clusterctl 发布物缺一不可(用户聊天已确认清单);建议用 `clusterctl generate provider` 校验本地发布物。
16. **实验性功能与主路径隔离**:节点池/MachinePool 属 CAPI 实验 API,需 feature gate(对标 CAPA exp/)与文档声明;主路径(Cluster + CP + MachinePool)优先稳定。
17. **资源识别不要依赖命名**:必须用 ID 回写 + Tag 双机制(Contract 要求);CAPHW 靠 name 匹配、无 tagging 是反例。
18. **版本矩阵与升级 CI**:SDK、CAPI、K8s 三者的版本组合要在 metadata.yaml 声明并进 CI,防止"能编译但合约不兼容"。
19. **e2e 与真实凭证隔离**:CI 用一次性测试账号/独立项目,凭证经 secret 注入,禁止入库;生产账号与测试账号分离。
20. **文档即交付物**:docs/book(用户指南/开发指南/参考)+ 华为云开发者社区博客 + 云商店上架素材,对标阿里云发布模式。

### 8.3 需对接华为云 CCE 确认(索引)

见 [附录 A](#附录-a需对接华为云-cce-验证事项索引) 与 [research-sources.md §4](research-sources.md)(14 项)。**正式问卷(中英双语,可直接发送):[华为云 CCE 对齐问卷](cce-verification-questionnaire.md)。在 M0 完成前,不进入 PoC 编码**。

### 8.4 华为云仓库治理注意(2025 新增硬性要求)

21. **命名即合规门槛**:仓库名必须为 `<领域前缀>-<场景描述>`(已定 `cloudnative-cluster-api-provider-cce`),不得用拼音;仓库创建前先与华为云工作组确认前缀归类(技术领域:云原生)。
22. **许可证默认 MIT-0**:不要再对标阿里云 ACK Provider 用 Apache 2.0;若确需 Apache 2.0,须在仓库创建时向工作组申请并注明理由。引入依赖前做许可证扫描,严禁 GPL/AGPL。
23. **DCO 是强制项**:所有提交必须 `git commit -s`;dco-check workflow 在 CI 拦截;协作团队需在 CONTRIBUTING 中明确。
24. **README 语言规则**:若面向非中国客户(开源 provider 一般属此类)必须用英文,不能因为团队习惯写中文;中文资料放 docs/ 子目录(如 docs/zh)。
25. **deploy/ 与 docs/ 职责分离**:deploy/ 只放部署脚本/IaC(密钥环境变量化 + variables.md + 销毁命令);架构/需求深度文档放 docs/,README 不得与 docs 重复。
26. **生命周期标签**:起步 incubating;若 6 个月无更新进入归档评估——发布节奏(如季度小版本)要写进团队计划,避免仓库"冷启动"后被归档。
27. **CI 四件套常绿**:dco-check / markdown-lint / secret-scan(gitleaks)/ iac-validate 是合并前置条件;新增脚本必须过 shellcheck,新增 IaC 必须过语法验证。

---

## 附录 A:需对接华为云 CCE 验证事项索引

> 状态说明:**实测确认** = 真实 CCE 冒烟验证;文档确认 = 官方文档/SDK 可确证;待华为云 = 需工单/抓包补充。逐项证据见 [cce-verification-findings.md](cce-verification-findings.md)。

| # | 事项 | 当前状态(2026-08-19) |
|---|---|---|
| 1 | 空集群(0 节点)创建、计费、配额影响 | ✅ 实测确认(Q1):空集群创建成功至 Available |
| 2 | 证书有效期上限;external/internal kubeconfig 切换与可达性 | ✅ 实测确认(Q2):重新签发即时生效,有效期 1~1827 天 |
| 3 | UpdateNodePool(initialNodeCount)扩缩容语义;与节点池 autoscaling 协调 | ✅ 实测确认(Q3/B3):`desiredNodeCount`/`initialNodeCount` 均为**绝对值**;autoscaling 与手动伸缩并存 |
| 4 | Turbo(eni)对 VPC/子网硬性要求(ENI 子网、网段不重叠) | ✅ 文档确认+实测(Q4):eni 子网需 neutron_subnet_id;同 VPC 容器网段不可重叠(CCE_CM.0410) |
| 5 | 安全组创建/关联行为(master/node/eni;每池 ≤5 边界) | ✅ 实测确认(Q5):Standard 接受 customSecurityGroups;每池 ≤5 |
| 6 | 管理 CCE 最小 IAM 权限与 AK/SK 账号约束(项目/委托) | ✅ 文档确认(Q6):细粒度 action 表;证书生成依赖 cluster:get |
| 7 | 默认配额(集群/节点池/节点/VPC/子网/ENI)与超配错误码 | ✅ 实测确认(Q7):集群配额 limit=50/区域 |
| 8 | DeleteCluster 删除时长/依赖与残留(EIP/EVS/ELB;Unavailable 可删性) | ✅ 实测确认(Q8):异步删除;delete_evs/eni/net 传参防残留;复核无残留 |
| 9 | AddNode/AddNodesToNodePool 对已有 ECS 的引导要求(决定单节点路径) | ✅ 文档确认(Q9):重装 ECS 不可免干预,首版只走节点池 |
| 10 | Autopilot 在 CAPI 模型中的表达(远期) | ✅ 文档确认(Q10):远期评估 |
| 11 | CreateUpgradeWorkFlow 参数与升级状态 | ✅ 实测定论(Q11):API 全可用;**平台当前无跨版本目标(空列表),耗时需华为云** |
| 12 | 计费模式(按需/包周期;空集群成本;休眠唤醒) | ✅ 文档确认(Q12) |
| 13 | 管理集群访问 CCE API Server 的网络路径(公网/对等/专线) | ✅ 实测确认(Q13):EIP 绑定后 https://EIP:5443 可达 |
| 14 | CCE/ECS/VPC API 限流阈值与错误码全集 | ✅ 实测确认(Q14):错误码表落地;**读 ~70 req/s、写 10 次/分钟触发限流;Retry-After 待抓包** |
