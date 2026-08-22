# CAPA 对齐修复设计文档（10 项）

> 日期：2026-08-21（设计）/ 2026-08-22（实施完成）
> 对标基准：CAPA `kubernetes-sigs/cluster-api-provider-aws` @ `67de5c2`（v2.10 主干，2026-08-12）
> 本项目：cloudnative-cluster-api-provider-cce
> 前置文档：`docs/capa-parity-gap-analysis.md`（差距总账）、`docs/capa-comparison-review-2026-08.md`（实现级审视）
> 方向变更：本文推翻 gap-analysis 中「BYO-only 网络对 CCE 场景合理」的旧结论，对齐 CAPA managed 网络模式。
>
> **状态更新（2026-08-22）：P0/P1/P2 共 9 项已全部实现并通过测试**（go build / go vet / go test 全量通过）；P3（#10 超节点）为远期调研项未开始。实施过程中对标 CAPA 源码逐项核实，修正了本设计文档的若干假设——见文末「实施记录」。
>
> **二次审计（2026-08-22）**：修复 3 个类型/逻辑 bug（EIP 时序、NAT 删除 Enabled 条件、CCECluster identityRef 凭证链）；实现 #4 conditions 细化（VpcReady/SubnetsReady/NatGatewaysReady）与 #5 收养模式（tag 三态）。

---

## 一、背景与范围

截至 2026-08-21，三个 P0（扩缩容触发、删除路径 identityRef、RoleIdentity agency 传递）及 ClusterClass/v1beta2 转换/Scope patchHelper/events/并发 flag 等已全部修复（代码核实）。本文覆盖**剩余 10 个差距项**的设计与修复计划。

工程原则（延续现有代码惯例）：
- 声明式 spec 能力**不加 feature gate**（gap-analysis 阶段 2 第 10 条判定准则）；仅「provider 主动做 spec 之外的云侧操作」需要 gate（如 GC）。
- 所有新云侧行为必须有真实云冒烟验证 + envtest 单测双覆盖。
- 删除路径必须走完整凭证链（`resolveControlPlaneCredentials`），禁止回退 per-cluster Secret 硬编码。

---

## 二、修复项总览

| # | 修复项 | 优先级 | 状态 | 核心落点 |
|---|---|---|---|---|
| 1 | 网络拓扑托管（managed VPC/子网/NAT） | P0 | ✅ 已实现 | `api/common/types.go` + `controllers/ccecluster_controller.go` + `internal/services/network/manager.go` |
| 2 | 外部资源 GC 扩展（遗留 EIP/EVS 扫删） | P1 | ✅ 已实现 | `controllers/gc.go` + service 层 EIP/EVS 枚举 |
| 3 | KMS/envelope 加密（encryptionConfig 暴露） | P1 | ✅ 已实现 | `CCEManagedControlPlane.spec` + `CreateClusterInput` |
| 4 | IAM 认证模式（authenticatingProxy） | P1 | ✅ 已实现 | 同上 |
| 5 | 外部 autoscaler 反向同步 | P1 | ✅ 已实现 | `ccemanagedmachinepool_controller.go` |
| 6 | user-kubeconfig 双 Secret | P2 | ✅ 已实现 | `ccemanagedcontrolplane_controller.go` kubeconfig 段 |
| 7 | providerIDList 回填 | P2 | ✅ 已实现 | `ListNodes` service（`cce://uid` 格式）+ pool spec 回填 |
| 8 | 节点修复（ResetNode 接入） | P2 | ✅ 已实现 | `CCEManagedMachinePool.spec` + `ListNodesWithStatus`/`ResetNode` service |
| 9 | Ginkgo + flavors e2e | P2 | ✅ 已实现 | `test/e2e/`（build tag `e2e`）+ flavors 数据 |
| 10 | Fargate/Autopilot（超节点） | P3 | ⏳ 未开始 | 新 CRD 面（远期调研） |

---

## 三、逐项设计

### 修复项 1：网络拓扑托管（P0，核心差距）

**现状与差距**
- `api/common/types.go` 的 `VPC`/`Subnet` 类型已预留双模式字段（`Name`/`CIDR`/`ResourceID`，学 ACK 设计），但 `clusterNetwork()` 只读 `VPC.ID`/`Subnets[0].ID`，ID 为空即传空串。
- 全 provider 零 `CreateVpc`/`CreateSubnet`/`CreateNatGateway` 调用（仅存在于 `hack/` 运维工具）。
- NAT 出网完全在 provider 语义之外，用户必须手动跑 `hack/nat-egress`。

**CAPA 参考**
- 三态模型：`vpc.id == ""` → 创建；`vpc.id != ""` + owned tag → 收养托管（含删除）；`vpc.id != ""` + 无 tag → BYO 引用（只 describe，下游全跳过）。
- Reconcile 顺序：VPC → 子网 → IGW → NAT → 路由表 → endpoints。
- Delete 严格依赖倒序 + 错误聚合：endpoints → 路由表 → NAT → EIP → IGW → 子网 → VPC。
- 子网默认生成：子网列表为空时按 VPC CIDR 自动切分（每 AZ 1 公 1 私）。
- `SubnetSpec.ID` 双语义：managed 模式当名字用，云侧 id 回填 `ResourceID`。

**CCE 能力映射（与 AWS 的差异点）**
| 概念 | AWS | 华为云 CCE | 设计取舍 |
|---|---|---|---|
| 公/私子网 | `isPublic` + 路由表推导 | 子网无公私概念，NAT/ELB 决定出网 | 不引入 `isPublic`；NAT 按「每子网一条 SNAT」建模 |
| NAT 网关 | per-AZ 自动铺 | NAT 网关必须挂子网（`InternalNetworkId`）+ SNAT 规则按 `network_id` | 显式 `natGateway` spec（enabled + 规格），不做 per-AZ 自动推导 |
| ENI 子网 | 无 | Turbo 需要 `neutron_subnet_id`（ENI 子网） | `Subnet` 增加 `Type` 字段（node/eni），创建时回填 neutron id |
| VPC 删除 | 删子网后可删 | 路由必须清空才能删子网（实测） | 删除顺序加入「清路由」步骤 |
| 容器网段 | 无 VPC 级唯一约束 | vpc-router 容器 CIDR **每 VPC 唯一** | 保留现有创建前预检（加分项），托管模式下校验同 VPC 已有集群 CIDR |

**设计方案**

1. **API 变更**（`api/common/types.go`，复用已预留字段）：

```go
type NetworkSpec struct {
    VPC      VPC      `json:"vpc,omitempty"`
    Subnets  []Subnet `json:"subnets,omitempty"`
    // 新增：出网托管（v1beta2 + v1beta1 转换同步）
    // +optional
    NatGateway *NatGatewaySpec `json:"natGateway,omitempty"`
}

type NatGatewaySpec struct {
    // Enabled triggers managed NAT + SNAT creation for every managed subnet.
    // +optional
    Enabled bool `json:"enabled,omitempty"`
    // Spec of the NAT gateway: "1" (small, default), "2", "3", "4".
    // +kubebuilder:default="1"
    // +optional
    Spec string `json:"spec,omitempty"`
}

type Subnet struct {
    // ... 现有字段不变 ...
    // 新增：子网类型（默认 node；Turbo ENI 子网标 eni）
    // +optional
    Type SubnetType `json:"type,omitempty"`
}
```

2. **三态判定**（学 CAPA，落在 `CCECluster` controller）：
```go
func (n *NetworkSpec) IsUnmanaged(clusterName string) bool {
    return n.VPC.ID != "" && !hasOwnedTag(n.VPC.Tags, clusterName)
}
```
- `vpc.id == ""` + `cidr` → 创建 VPC/子网，回填 `ResourceID`；子网列表空 → 按 VPC CIDR 自动切分（node 子网 + 可选 eni 子网）。
- `vpc.id != ""` + owned tag → 收养托管（含删除）。
- `vpc.id != ""` + 无 tag → 现状 BYO（预检保留）。

3. **Reconcile 流程**（`ccecluster_controller.go` 由薄壳升级为网络 reconciler）：
```
VPC → (子网创建/回填) → NAT 网关 → SNAT 规则 → Ready
```
- 每步一个 condition（`VpcReady`/`SubnetsReady`/`NatGatewayReady`），复用现有 `NetworkReadyCondition` 细化 reason。
- service 层新增（从 `hack/smoke-setup`/`hack/nat-egress` 搬运已验证逻辑）：`CreateVpc`/`CreateSubnet`/`CreateNatGateway`/`CreateSnatRule` + 对应 `Delete*` + `WaitFor*`。
- 幂等：按 name 查找已存在资源（学 `findGatewayByName`）。

4. **Delete 流程**（严格倒序 + 错误聚合，学 CAPA `DeleteNetwork`）：
```
SNAT 规则 → NAT 网关 → (清 VPC 路由) → 子网 → VPC
```
- 仅在 managed/收养态执行；BYO 态全跳过。
- CCECluster finalizer 等待 CCEManagedControlPlane 删除完成后再拆网络（复用现有「依赖计数」模式：pool 先删、CP 后删、网络最后删）。

**CCEManagedControlPlane 侧配合**：`clusterNetwork()` 改读 `ResourceID`（回填后的真 id）；Turbo 模式 `ENISubnets` 从 `Type == eni` 的子网取 neutron_subnet_id。

**验收标准**
- envtest：三态（创建/收养/BYO）+ 删除倒序 + 错误聚合 + 幂等重入，全覆盖。
- 真实云冒烟：`vpc.id` 留空 + `natGateway.enabled=true` → 一键建出 VPC/子网/NAT/SNAT + 集群 Ready；删除后 0 残留（survey-hw 核查）。
- BYO 路径回归：现有 e2e（引用已有 VPC）不受影响。

---

### 修复项 2：外部资源 GC 扩展（P1）

**现状与差距**
- `controllers/gc.go` 已有孤儿集群清扫器（owned tag + Cluster CR 不存在 → 级联删集群）。
- 差距：DeleteCluster 级联选项之外的**独立遗留资源**（用户手动建的 EIP、EVS 云盘、ELB）不在扫描范围，泄漏后持续计费。

**CAPA 参考**
- `ExternalResourceGC` gate + `gc.NewService`：按集群 tag 扫全部资源类型并聚合删除，每资源错误不阻断整体。

**设计方案**
1. service 层新增 `ListEips`/`DeleteEip`（EIP v2/v3 SDK）与 `ListVolumes`/`DeleteVolume`（EVS SDK）；tag 过滤直接用资源自带 `tags` 字段（EIP v3 `ListPublicips` 响应含 `tags[]`，EVS `VolumeDetail.Tags` 为 map）——**无需 TMS**（见实施记录 5.4 的调查修正）。
2. `sweep()` 扩展为两阶段：先删孤儿集群（现状），再扫 owned-tag 遗留 EIP/EVS：`孤儿集群 → ListEips(按 tag) → ListVolumes(按 tag) → 聚合删除`。
3. 保持现有 gate 语义（`ExternalResourceGC` + `--gc-region`），新增 `--gc-resource-types=eip,evs`（默认 `eip,evs`，ELB 二期）。
4. 白名单保护：tag 匹配 `cluster-api-provider-cce.cluster.<name>=owned` 才删，且跳过正被现有 Cluster CR 引用的 VPC 内资源（防误删用户 VPC 的共享资源）。

**验收标准**
- 单测：tag 过滤、白名单、错误聚合。
- 真实云：手工制造带 owned tag 的孤立 EIP/EVS → 开启 GC → 一个 interval 内删除；无 tag 资源不动。

---

### 修复项 3：KMS/envelope 加密（P1）

**现状与差距**
- `CreateClusterInput` 无 `encryptionConfig`（grep 证实零引用）；集群创建走平台默认 `{"mode":"Default"}`（survey 实测）。
- CCE CreateCluster API 支持 `spec.encryptionConfig.mode`（`Default`/`KMS`）。

**CAPA 参考**
- EKS `EncryptionConfig`（KMS provider 映射 secrets envelope 加密），声明式字段直透。

**设计方案**
1. `CCEManagedControlPlaneSpec` 新增：
```go
// +optional
EncryptionConfig *EncryptionConfigSpec `json:"encryptionConfig,omitempty"`

type EncryptionConfigSpec struct {
    // Mode: "Default" | "KMS".
    // +kubebuilder:validation:Enum=Default;KMS
    Mode string `json:"mode"`
    // KmsKeyID of the KMS key (required when Mode=KMS).
    // +optional
    KmsKeyID string `json:"kmsKeyId,omitempty"`
}
```
2. `CreateClusterInput` 加同名字段透传；`toCreateClusterInput` 映射。
3. webhook 校验：`Mode=KMS` 时 `KmsKeyID` 必填；不可变（创建后禁止改 mode）。
4. 条件上报：`EncryptionConfigured`（可并入现有 Addons 段或独立 condition）。

**验收标准**：单测（校验/透传/不可变）+ 真实云建 KMS 模式集群（有 KMS 密钥的前提下；无密钥则验证 API 拒绝路径）。

---

### 修复项 4：IAM 认证模式（P1）

**现状与差距**
- 集群创建走默认 `"authentication":{"mode":"rbac","authenticatingProxy":{}}`（survey 实测）；spec 无暴露。
- CCE 支持 `authenticating_proxy` 模式（认证代理：自定义 CA + proxy 地址，IAM 用户经代理鉴权）。

**CAPA 参考**
- EKS `AccessConfig.AuthenticationMode`（API/CONFIG_MAP/AND），声明式直透 + condition 上报。

**设计方案**
1. `CCEManagedControlPlaneSpec` 新增：
```go
// +optional
Authentication *AuthenticationSpec `json:"authentication,omitempty"`

type AuthenticationSpec struct {
    // Mode: "rbac" (default) | "authenticating_proxy".
    // +kubebuilder:validation:Enum=rbac;authenticating_proxy
    Mode string `json:"mode"`
    // AuthenticatingProxy config (required when Mode=authenticating_proxy).
    // +optional
    AuthenticatingProxy *AuthenticatingProxySpec `json:"authenticatingProxy,omitempty"`
}

type AuthenticatingProxySpec struct {
    // CA of the proxy (PEM). 
    CA string `json:"ca"`
    // Concurrency limit of concurrent logins (default 1000).
    // +optional
    Concurrency *int32 `json:"concurrency,omitempty"`
}
```
2. `CreateClusterInput` 透传 + `toCreateClusterInput` 映射（SDK `Authentication`/`AuthenticatingProxy` 结构）。
3. webhook：mode 不可变；`authenticating_proxy` 时 `ca` 必填。
4. 与现有 `AccessPolicy`（access entry 对标）正交：认证模式管「谁能连 API」，访问策略管「连上后有什么权限」。

**验收标准**：单测 + 真实云 rbac 模式回归（默认路径不变）；authenticating_proxy 视测试环境可用性做冒烟或 API 拒绝路径验证。

---

### 修复项 5：外部 autoscaler 反向同步（P1）

**现状与差距**
- `autoscaling`（Alpha gate）仅正向：spec.min/max → CCE 节点池弹性配置。
- 外部 autoscaler（如集群 autoscaler 直接调 CCE API 改节点数）后，CR spec 不同步，下次 reconcile 会被打回。

**CAPA 参考**
- `machinepool.autoscaling.annotations` 的 `cluster.x-k8s.io/replicas-managed-by: "external"` 注解：检测到注解时，`spec.replicas` 改为**从云侧 status 反向同步**而非下发。

**设计方案**
1. `CCEManagedMachinePool` 支持 `cluster.x-k8s.io/replicas-managed-by: "external"` 注解（CAPI 标准注解，MP controller 识别）。
2. 注解存在时：`pool.Spec.Replicas = cloudSideCurrentNode`（status 反向写 spec，走 patchHelper）；删除注解恢复正向。
3. 仅当 `autoscaling.enabled=true`（弹性组存在）时生效，否则 webhook 警告。
4. 事件：`ReplicasManagedExternally` normal event，用户可感知谁在管。

**验收标准**：envtest（注解切换正/反向 + 事件）；真实云手动改 CCE 节点数 → CR spec 反向同步。

---

### 修复项 6：user-kubeconfig 双 Secret（P2）

**现状与差距**
- 仅 `<cluster>-kubeconfig`（CAPI 消费）。用户拿不到独立于 CAPI 轮换周期的凭据。

**CAPA 参考**
- CAPA 双 Secret：`<cluster>-kubeconfig`（CAPI 内部用）+ `<cluster>-user-kubeconfig`（用户用，短期 token）。

**设计方案**
1. CP controller kubeconfig 段扩展：创建/轮换 `<cluster>-user-kubeconfig`（同样 controller-reference 挂 CP 下，删除自动清理）。
2. 用户 Secret 独立有效期（如 24h，可配 `spec.kubeconfig.userValidityDays`，默认短于 CAPI 的 365d），到期前自动刷新。
3. `clusterctl get kubeconfig` 默认仍取 CAPI Secret（契约不变）；文档标注用户 Secret 用途。

**验收标准**：envtest（双 Secret 创建/独立轮换/随删除清理）。

---

### 修复项 7：providerIDList 回填（P2）

**现状与差距**
- `CCEManagedMachinePool` 无 `Spec.ProviderIDList`（CAPI v1beta2 MachinePool 契约字段）；`clusterctl describe` 无法展示逐节点 provider ID，MHC（machine health check）等生态无法按节点定位。

**CAPA 参考**
- `AWSManagedMachinePool.Spec.ProviderIDList`：每节点 instance ID 列表，由 controller 从 ASG 实例同步。

**设计方案**
1. service 层新增 `ListNodePoolNodes(ctx, clusterID, nodePoolID)`（CCE `ListClusterNodes` API，过滤 nodePoolID）。
2. MP controller reconcile：`pool.Spec.ProviderIDList = ["cce://<node-uid>", ...]`（`Status.Replicas` 对齐 currentNode）。
3. providerID 格式 `cce://<uid>`（uid = CCE 节点 metadata.uid，全局唯一；实施时核对 `NodePoolInfo` 补充 serverId 备选）。
4. 排序稳定（按节点名）避免 spec 抖动；节点的 ECS serverId 写入 `Status.ProviderIDList` 之外的扩展字段（可选，二期）。

**验收标准**：envtest（fake service 回填 + 排序稳定）+ 真实云 `clusterctl describe` 可见逐节点。

---

### 修复项 8：节点修复（P2）

**现状与差距**
- CCE 节点异常（NotReady/损坏）只能整池重建或人工处理；CAPA 有 `NodeRepairConfig`（自动修复托管节点组内坏节点）。

**CAPA 参考**
- `AWSManagedNodeGroup` `NodeRepair`：检测 ASG 健康信号，对 Unhealthy 节点触发替换。

**CCE 能力映射**
- CCE 有 `ResetNode`（重置节点，重装系统盘回归初始状态）与节点「重装操作系统」API；节点池维度有健康状态上报。

**设计方案**
1. `CCEManagedMachinePoolSpec` 新增：
```go
// +optional
NodeRepair *NodeRepairSpec `json:"nodeRepair,omitempty"`

type NodeRepairSpec struct {
    // Enabled auto-repairs unhealthy nodes via CCE ResetNode.
    Enabled bool `json:"enabled"`
    // MaxParallel limits concurrent repairs (default 1).
    // +kubebuilder:validation:Minimum=1
    // +optional
    MaxParallel *int32 `json:"maxParallel,omitempty"`
}
```
2. MP controller：`ListNodePoolNodes` 检测 `status.phase` 异常节点（具体异常码实施时以 SDK 核实）→ 并发受限地调 `ResetNode` → 上报 `NodeRepairing` condition + event。
3. **需要 feature gate**（`NodeRepair`，Alpha）：这是 provider 主动做 spec 之外的破坏性云侧操作（重置节点 = 数据丢失风险），符合 gate 判定准则第 ② 条。
4. 与 CAPI MHC 集成留二期（MHC remediation → 触发本 provider 修复的通道）。

**验收标准**：单测（异常检测/并发限制/gate 关闭时无操作）+ 真实云手动构造坏节点（可选）。

---

### 修复项 9：Ginkgo + clusterctl flavors e2e（P2）

**现状与差距**
- 现有 e2e 是 go test 原生 env 门控（`test/e2e/e2e_test.go`），覆盖同流程但形态与 CAPI 生态惯例（Ginkgo + clusterctl test framework + flavors）不一致；无法复用社区 test framework 的断言/事件采集/clusterctl 操作。

**CAPA 参考**
- `test/e2e/` Ginkgo 套件 + `sigs.k8s.io/cluster-api/test/framework`：`clusterctl.ApplyClusterTemplateAndWait`（flavor 参数化）+ 生命周期断言 + 自定义 matcher。

**设计方案**
1. 引入 `sigs.k8s.io/cluster-api/test/framework`（Go module 依赖，版本对齐 cluster-api v1.14.0）。
2. `test/e2e/suites/unmanaged/`（按 CAPA 目录惯例）：`cluster_lifecycle_test.go`（flavor=default：建→Ready→删）+ `scale_test.go`（flavor=default + pool 扩缩）。
3. flavors 放 `test/e2e/data/infrastructure-cce/`（cluster-template.yaml 现有 sample 迁移 + `clusterctl` 变量注入 `CCE_*`）。
4. 保留现有 go test 冒烟（真实云回归更快），Ginkgo 套件作为 CI 标准形态。
5. 环境门控不变（无 `E2E_MANAGEMENT_KUBECONFIG` 即 skip）。

**验收标准**：`make test-e2e`（新目标）在真实管理集群上跑通建/删 + 扩缩三场景。

---

### 修复项 10：Fargate/Autopilot（超节点）远期（P3）

**现状与差距**
- CAPA 有 `FargateProfile`（Serverless 容器节点池）；CCE 对应能力是 CCE Autopilot / 超节点（`ListHyperNodes`），当前 provider 完全未建模。

**设计方向（调研立项，本期不实现）**
1. 评估 `CCEManagedHyperNodePool` 新 CRD 面（Serverless 节点池：无 ECS 实例语义，只有算力单元）。
2. 关键差异：无 flavor/OS/磁盘/sshKey 字段语义（Serverless 化），但保留 label/taint/可用区。
3. 按 gate 判定准则第 ① 条：新 CRD 面 + 新 controller 需要 feature gate（如 `HyperNodePool`，Alpha）。
4. 先出调研文档（CCE 超节点 API 全集 + CAPI MachinePool 契约匹配度分析）再排期。

**验收标准（本期限）**：调研文档 `docs/hypernode-research.md` 产出，含 API 匹配矩阵与可行性结论。

---

## 四、修复计划

### 4.1 依赖关系

```
B1（独立快赢，无互相依赖）
  #3 KMS ─┐
  #4 IAM ─┼─ 均改 CP spec + CreateClusterInput + webhook（同文件序列化提交）
  #6 user-kubeconfig ─┘
  #7 providerIDList ── 独立（MP controller + service）

B2（核心）
  #1 网络拓扑托管
      内部三步：1a managed VPC/子网 → 1b NAT/SNAT → 1c tag 收养
      #1 完成后 #2 的 GC 需覆盖 VPC/NAT 遗留（B3 吸收）

B3（运维增强）
  #5 反向同步（依赖现有 autoscaling gate，无硬依赖）
  #2 GC 扩展（建议在 #1 后做，一并覆盖 VPC/NAT 泄漏面）

B4（工程化 + 远期）
  #8 节点修复（独立）
  #9 Ginkgo e2e（建议在功能项稳定后收口）
  #10 超节点调研（随时可启动，独立）
```

### 4.2 批次排期

| 批次 | 内容 | 预估工作量 | 交付物 |
|---|---|---|---|
| **B1** | #3 KMS、#4 IAM 认证、#6 user-kubeconfig、#7 providerIDList | 3~4 人日 | 4 个独立 PR；每项 envtest + 真实云回归 |
| **B2** | #1 网络拓扑托管（1a VPC/子网 → 1b NAT → 1c tag 收养，三步序列化） | 5~7 人日 | 3 个 PR（1a/1b/1c）；含 service 层 VPC/NAT 模块 + 三态单测 + 真实云一键建删冒烟 |
| **B3** | #2 GC 扩展、#5 反向同步 | 3~4 人日 | 2 个 PR；EIP/EVS 扫删冒烟 + 反向同步 envtest |
| **B4** | #8 节点修复、#9 Ginkgo e2e、#10 超节点调研 | 6~10 人日 | #8 PR（含 gate）；#9 测试框架迁移 + flavors；#10 调研文档 |

总计：**17~25 人日**，B1→B2→B3 串行推进，B4 内部并行。

### 4.3 每批次统一验收门禁

1. `make test`（envtest 全量）+ `go vet ./...` 通过。
2. 新增行为必须有用例覆盖：正常路径 + 一个失败路径（错误分类/退避符合现有惯例）。
3. 涉及真实云的批次（B1/B2/B3）在华为云测试账号执行冒烟，`hack/survey-hw` 核查 **0 残留资源**。
4. 文档同步：`docs/capa-parity-gap-analysis.md` 对应行状态更新（本次新增「修复项 #N」锚点）。
5. 每项完成时在 gap-analysis 的差距总账中标记 ✅ 并注明 commit。

### 4.4 风险与缓解

| 风险 | 影响 | 缓解 |
|---|---|---|
| #1 托管网络删除顺序在 CCE 上有未知坑（路由/安全组残留阻塞 VPC 删除） | 删除卡 finalizer | 1a 先只做 VPC/子网托管 + 删除轮询容错；错误聚合不阻断；实测后补 1b |
| #2 EIP/EVS tag 过滤准确性（EIP tag key 36 字符限制 vs owned tag key 长度） | GC 扫不到资源 | 资源自带 tags 字段直连过滤（EIP v3 tags、EVS map）；owned tag key 长度问题见实施记录 5.4 |
| #7 providerID 格式与生态消费方（MHC）不兼容 | 二期集成困难 | 格式在 PR 评审时与 CAPI 社区惯例对齐（参考 providerID 规范），保留 serverId 备选 |
| #9 引入 cluster-api test framework 拉大依赖树 | 模块冲突 | 版本严格对齐 v1.14.0；先行 spike 验证 go.mod 兼容再迁移 |
| KMS/authenticating_proxy 无测试资源 | 冒烟受阻 | 验证 API 拒绝路径 + 单测覆盖映射逻辑；有资源后再补正向冒烟 |

### 4.5 里程碑

| 里程碑 | 完成标志 |
|---|---|
| M1（B1 完成） | 4 个快赢项合入；gap-analysis 4 行更新 ✅ |
| M2（B2 完成） | `vpc.id` 留空 + `natGateway.enabled=true` 一键建出完整环境并删除干净；真实云冒烟记录 |
| M3（B3 完成） | GC 全资源类型扫删 + 外部 autoscaler 共存验证 |
| M4（B4 完成） | Ginkgo e2e 进 CI；超节点调研文档评审通过 |

---

## 五、实施记录（2026-08-22）

P0/P1/P2 共 9 项全部实现。实施过程中对标 CAPA 源码（commit `67de5c2`，本地 `/tmp/capa`）逐项核实，修正了本设计文档的若干假设：

### 5.1 设计假设修正（以 CAPA 源码为准）

| # | 设计文档原假设 | CAPA 源码事实 | 最终实现 |
|---|---|---|---|
| 3 | EncryptionConfig 有 `Mode + KmsKeyID` | `model.EncryptionConfig` 只有 `Mode`（Default/KMS），KMS 密钥由账户级配置 | 去掉 KmsKeyID，只保留 Mode |
| 4 | AuthenticatingProxy 有 `CA + Concurrency` | `model.AuthenticatingProxy` 是 `Ca/Cert/PrivateKey`（base64 PEM），无 Concurrency | 用 Ca/Cert/PrivateKey 三字段，service 层 base64 编码 |
| 5 | 反向同步写 infra pool spec；仅 autoscaling 生效 | CAPA `eks/nodegroup.go:545` 反向写 **CAPI MachinePool.spec.replicas**，注解即信号（无 autoscaling 条件） | 反向写 `MachinePool.spec.replicas`（patch），无 autoscaling 条件 |
| 6 | user Secret 独立短周期 | CAPA user kubeconfig 用 exec 插件（`aws-iam-authenticator`）只创建一次；CCE 无 exec，用证书 | 命名对齐 `<cluster>-user-kubeconfig`，CCE 用证书 + 独立轮换（云能力差异） |
| 7 | providerID 格式未定 | CAPA 用 `aws:///az/instance-id` | `cce://<uid>`（scheme-qualified） |
| 8 | NodeRepair 有 `MaxParallel`；需 feature gate | CAPA `NodeRepairConfig` 只有 `Enabled *bool`，无 gate；CCE 无对应开关（SDK 零 repair 字段） | 只保留 `Enabled`，无 gate（默认 false）；CCE 用 `ResetNode` 替代实现 |
| 9 | 引入完整 cluster-api test framework | CAPA e2e 基建（E2EContext + kind + clusterctl bootstrap）是另一量级工程 | 对标 Ginkgo 形态 + build tag `e2e` + flavors 目录；未引入 kind/clusterctl 完整基建 |

### 5.2 实现要点

- **#1 网络拓扑**：`internal/services/network/manager.go` 新增 `Manager`（VPC/子网/NAT/EIP/SNAT 的 Reconcile/Delete，三态判定 `IsManaged` = `vpc.id` 空 + cidr/resourceID 非空，删除依赖倒序 + 错误聚合 + NotFound 容忍）。托管 vs BYO 用 `ResourceID` 判定（CAPA 的 tag 收养 1c 阶段未做，留待后续）。
- **#2 GC**：`sweep()` 两阶段（孤儿集群 → owned-tag 遗留 EIP/EVS），`--gc-resource-types=eip,evs` flag；EIP tags 为 `[]string`("k=v")、EVS tags 为 `map[string]string`，统一转 map 后按 `OwnedTagPrefix` 过滤。
- **#8 节点修复**：`NodeStatusPhase` 异常态 = `Abnormal`/`Error`；`ResetNode` 对节点池内节点省略 spec（官方：以节点池配置重装）；`ListNodesWithStatus` 目前集群级，节点池级过滤待 CCE `ListNodePoolNodes` API 接入。
- **#9 Ginkgo**：`go get` 时曾误降级 cluster-api v1.14.0→v1.11.11，已恢复并锁定 `ginkgo v2.32.0`；e2e 包全 build-tag 化后，无 `-tags e2e` 时 `go test ./...` 跳过该包（warning，非致命）。

### 5.3 验证

`go build ./...`、`go vet ./...`、`go vet -tags e2e ./test/e2e/...`、`go test ./...`（全量通过，零 FAIL）、`go test -tags e2e ./test/e2e/...`（env 门控下 skip）。

### 5.4 二次审计修复（2026-08-22）

**修复的 3 个 bug**：

1. `manager.go` `findEipBySnatRules` 时序错误：EIP ID 找回在 `deleteSnatRules` 之后，SNAT 已删找不到 → 移到删 SNAT 之前。
2. `manager.go` `DeleteNetwork` 依赖 `ng.Enabled`：disable 后删除泄漏 NAT → 改为 `ResourceID != "" || EIPResourceID != ""`（有记录即删）。
3. `ccecluster_controller.go` 用 `scope.ResolveCredentials`（只认 per-cluster Secret）而非 identityRef 链 → identityRef 集群在网络校验/托管/删除三处卡死；新增 `resolveClusterCredentials`（读 CP 的 identityRef 链）。

**#4 conditions 细化（对标 CAPA）**：

- `internal/conditions` 加 `VpcReady`/`SubnetsReady`/`NatGatewaysReady`。
- `ManagerInterface` 由原子 `ReconcileNetwork` 拆分为 `ReconcileVpc`/`ReconcileSubnets`/`ReconcileNatGateway`。
- controller 新增 `reconcileManagedNetwork` 分步调用，每步 Mark 独立 condition。

**#5 收养模式（对标 CAPA 三态）**：

- `common.VPC` 加 `Tags` 字段。
- `network.IsManaged(spec, clusterName)` 三态：`vpc.id` 空 = 创建；`vpc.id` 非空 + owned tag = 收养（托管含删除）；`vpc.id` 非空 + 无 tag = BYO。
- `ReconcileVpc` 收养态回填 `ResourceID = vpc.id` 并继续托管子网/NAT；`DeleteNetwork` 收养态也删除（用 `vpcID` 变量统一 managed/adopted）。

**#6 tag 打标设计（事实核查后）**：

| 资源 | 打 tag 方式 | key 限制 | tag 格式 |
|---|---|---|---|
| VPC | `CreateVpcOption.Tags *[]string`（创建时） | 未明（统一标签体系） | `key*value`（星号，**实测确认**） |
| NAT 网关 | `CreateNatGatewayOption.Tags *[]string`（创建时）或 `CreateNatGatewayTag`（v3，创建后） | **128**（`TagBody.Key` 注释） | 创建时 `[]string`（推断星号，**余额冻结无法实测**）；独立 API `{Key,Value}` |
| EIP | `CreatePublicipTag`（v2.0，创建后） | **128**（官方 2026-08-05 更新；SDK 注释「36」过时） | `ResourceTagOption{Key,Value}`（文档明确，无需实测） |

**EIP API 面（不是迭代版本）**：SDK 的 `eip/v2` 与 `eip/v3` 是两个并存的 API 面——`eip/v2` 包装生命周期（`CreatePublicip` HTTP `/v1/`、`CreatePublicipTag` HTTP `/v2.0/.../tags`），`eip/v3` 是查询/绑定面（`ListPublicips` HTTP `/v3/` 含 tags）。创建和打 tag 在 `eip/v2`，查询（含 tags）可用 `eip/v3`。

**owned tag key 方案（无需缩短）**：统一用 `cluster-api-provider-cce.cluster.<name>=owned`。前缀 33 字符 + name（CAPI 上限 63）→ 总长 ≤96 < 128，三种资源都够用，与 CCE 集群/节点池已有的 key 一致，GC 统一识别。之前「36 字符限制需重设计 key」的结论作废（SDK 过时注释误导）。

**打 tag 实现**：
1. VPC：`ensureVpc` 创建时 `CreateVpcOption.Tags = []string{"cluster-api-provider-cce.cluster.<name>*owned"}`（key*value 星号）。
2. NAT：`createNatGateway` 创建时 `CreateNatGatewayOption.Tags`（格式待实测）；若不支持，创建后调 `CreateNatGatewayTag`（v3，`{Key,Value}`）。
3. EIP：`createEip` 创建后调 `CreatePublicipTag{Key: "cluster-api-provider-cce.cluster.<name>", Value: "owned"}`。

**GC 识别**：现有 `ownedClusterName`（前缀匹配）+ `parseKVTags`（EIP `k=v` 转 map）已覆盖 EIP；VPC/NAT 的 tag（`k*v`/`k=v`）若未来 GC 扩展需加对应解析。

**实测记录（2026-08-22，cn-north-4 真实 API）**：

1. **VPC 星号分隔符（实测确认）**：`CreateVpc` 带 `Tags=["cluster-api-provider-cce.cluster.tagprobe*owned"]` 创建成功，`ShowVpcTags` 正确拆出 key=`cluster-api-provider-cce.cluster.tagprobe`、value=`owned`；等号格式报 `VPC.1801 "Tag value can not be null"`（等号不是分隔符，整个字符串当 key、value 空被拒）。
2. **NAT 无法实测**：`CreateNatGateway` 报 `CBC.30060005 "Frozen CbcDeposit Failed!"`（账户余额冻结）。NAT `Tags *[]string` 与 VPC 同类型，华为云 `Tags *[]string` 统一约定为星号，推断 NAT 同为星号；且 NAT 有独立标签 API `CreateNatGatewayTag`（`{Key,Value}`，key 128）作备选。待账户恢复后实测确认。
3. **EIP 等号格式（实测确认）**：`CreatePublicip` 创建成功（EIP 按需计费不冻结，与 NAT 不同）、`CreatePublicipTag{Key,Value}` 成功、`ListPublicips` 返回 tags=`["cluster-api-provider-cce.cluster.tagprobe=owned"]`（**等号 `=` 分隔**）。正好匹配 `parseKVTags` 的 `=` split → GC 识别链路完整闭环。

**#6 实现（已完成）**：`ensureVpc` 创建时 `CreateVpcOption.Tags` 打 `ownedKey*owned`（星号，实测）；`createNatGateway` 创建时 `CreateNatGatewayOption.Tags` 打星号（推断）；`createEip` 创建后 `CreatePublicipTag{Key: ownedKey, Value: "owned"}`（实测闭环）。`ownedTagKey(clusterName)` = `cluster-api-provider-cce.cluster.<name>`。

**NAT 网关必要性（修正）**：

- SWR **有 VPC endpoint（VPCEP）**，且基础版 SWR 对同区域 ECS/CCE 节点**默认内网访问**（免 NAT 免配置）；企业版 SWR 需配置 VPCEP 内网访问。
- 因此 CCE 节点拉同区域 SWR 镜像免 NAT，与 CAPA 的 ECR VPC endpoint 完全对等。
- NAT 网关仅在「拉公网第三方镜像（quay.io/registry.k8s.io）」时需要；若第三方镜像也搬到 SWR（对标 CAPA 把镜像搬 ECR），则**完全不需要 NAT**。
- 之前部署管理集群时手动建 NAT，是为了拉 cert-manager/CAPI core（quay.io/registry.k8s.io），不是拉 SWR 镜像。
### 5.5 待办

- 真实云冒烟：P0（managed VPC/NAT 一键建删）、P1（GC EIP/EVS 扫删、KMS/authenticating_proxy 正向）、P2（ResetNode 正向）需在华为云测试账号执行，`hack/survey-hw` 核查 0 残留。
- #8 的节点池级过滤、#7 的 MHC 生态集成（providerID 消费验证）列为后续。
- #6 tag 打标 ✅ **已实现**：`ensureVpc`/`createNatGateway` 创建时打 owned tag（星号），`createEip` 创建后 `CreatePublicipTag`。VPC 星号、EIP 等号均实测闭环；NAT 星号格式待余额恢复后实测。GC 的 `parseKVTags` 已匹配 EIP 等号格式。
- #10 超节点调研文档（`docs/hypernode-research.md`）未启动。
