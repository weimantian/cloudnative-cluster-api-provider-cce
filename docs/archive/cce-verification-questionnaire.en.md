# Huawei Cloud CCE Alignment Questionnaire (pre-implementation)

> 中文版 / Chinese: [cce-verification-questionnaire.md](cce-verification-questionnaire.md)
>
> Background docs: [Research sources & verification checklist](research-sources.md) §4 (14 items), [Architecture design](architecture-design.md), [Requirements design](requirements-design.md)

## 1. Context & Purpose

We are developing `cloudnative-cluster-api-provider-cce` — a Cluster API Infrastructure Provider that manages **Huawei Cloud CCE managed clusters** (aligned with the CAPI + AWS EKS managed-mode experience). Before implementation, we need confirmation on the **14 items** below regarding CCE API behavior and constraints, to avoid rework caused by assumptions. **PoC coding will not start until all items are confirmed/verified.**

## 2. How to Fill In

- For each item, please provide: **Conclusion** (supported / not supported / constraints) + **Evidence** (official doc link / test result / ticket conclusion) + **Confirmer + Date**.
- Suggested verification methods are provided (console steps or API calls; all APIs can be exercised with the official Go SDK `huaweicloud-sdk-go-v3/services/cce/v3` or KooCLI).
- If an item must be verified by us, please provide a test account/sub-project; otherwise a documented answer is sufficient.

## 3. Questionnaire

### Q1 Creating an empty cluster (0 nodes)

- **Context / design impact**: Our primary path is "create CCE cluster (control plane) first → create node pools afterwards". Official docs show the `CreateCluster` request body contains no node parameters; empty-cluster behavior must be confirmed.
- **Questions**:
  1. Is creating a cluster **without any nodes** via `POST /api/v3/projects/{project_id}/clusters` supported (both Standard and Turbo)?
  2. Is an empty cluster billed? What is the billing basis (cluster itself vs nodes)?
  3. Does an empty cluster consume cluster quota? Is it subject to the flavor's max node count?
  4. Is an empty cluster's status `Available`? What error code is returned if node pool APIs are called before `Available`?
- **Suggested verification**: create an "empty cluster" in console or via SDK `CreateCluster` (no nodes) → observe status and cost.
- **Answer**: Conclusion ___ / Evidence ___ / Confirmer ___ / Date ___

### Q2 kubeconfig certificate retrieval & validity

- **Context / design impact**: The provider fetches kubeconfig via `CreateKubernetesClusterCert` and stores it in a Secret; refresh policy depends on the answer.
- **Questions**:
  1. `CertDuration.duration` is documented as **-1 or [1,1827]** (max 5 years). Does `-1` mean 5 years? What is the default? Any shorter upper bound (ticket/compliance)?
  2. After expiry, does the kubeconfig from `clusterctl get kubeconfig` become invalid? Does re-calling `CreateKubernetesClusterCert` take effect immediately (is revoking the old cert required)?
  3. Switching between `current-context` `external` (public) and `internal` (private): does a cluster without a public IP necessarily return `internal`? What form is the `internal` server address (VIP/domain)?
  4. Behavior of the revoke API `clustercertrevoke` (does it affect already-issued kubeconfigs)?
- **Suggested verification**: SDK `CreateKubernetesClusterCert` with `duration=1` and `duration=-1`; inspect returned kubeconfig and certificate validity.
- **Answer**: Conclusion ___ / Evidence ___ / Confirmer ___ / Date ___

### Q3 Node pool scaling semantics (`ScaleNodePool` / `UpdateNodePool`)

- **Context / design impact**: CAPI `MachinePool.spec.replicas` changes → provider calls the scaling API. The SDK comment indicates **`ScaleNodePoolSpec.desiredNodeCount` is a delta** (scale-up = current + delta; scale-down = current − delta) and **omitting it defaults to 0, which deletes all nodes in the scaling group** — a high-risk point that must be confirmed.
- **Questions**:
  1. Is `ScaleNodePool.desiredNodeCount` an **absolute target** or a **delta**? (The SDK comment "add to / subtract from current node count" implies delta, but the field name is misleading.)
  2. `scaleGroups`: is the default scaling group named `"default"`? Must it always be passed? Is scale-down limited to a single scaling group?
  3. Relationship between `UpdateNodePool` (changing `initialNodeCount`) and `ScaleNodePool`: can both change node count? Which is recommended for "align to desired count"?
  4. When a node pool has `autoscaling` enabled, does an external `ScaleNodePool` call conflict? Which wins?
  5. During ScalingUp/ScalingDown, do further scale calls error? What error code?
- **Suggested verification**: create a 2-node pool → call `ScaleNodePool` with `desiredNodeCount=2` and `3`; observe whether the count becomes 4/5 or 2/3.
- **Answer**: Conclusion ___ / Evidence ___ / Confirmer ___ / Date ___

### Q4 Turbo (eni) network model — VPC/subnet hard requirements

- **Context / design impact**: Turbo clusters with `containerNetwork.mode=eni` have strong network constraints; misconfiguration causes repeated creation failures. The provider needs validation-layer checks.
- **Questions**:
  1. For eni mode, what are the hard VPC/subnet requirements? (Must subnets support ENI? Subnet count and AZ requirements? Must VPC CIDR and Pod CIDR (`eniNetwork`) not overlap?)
  2. Relationship between `eniNetwork.subnets` (ENI subnets) and node subnets: must they be explicitly specified? Minimum count? (Some docs mention "ENI subnets covering 2 AZs" — please confirm.)
  3. When a Turbo cluster is created with non-compliant VPC/subnets, what error code/message is returned?
  4. For Standard `vpc-router` mode ("add CIDRs after creation, but existing ones cannot be modified"): exact boundary and any limit on added subnets/CIDRs?
- **Suggested verification**: create a Turbo (eni) cluster with non-compliant subnets and record the error; compare with the console creation wizard's validation rules.
- **Answer**: Conclusion ___ / Evidence ___ / Confirmer ___ / Date ___

### Q5 Security groups (master / node / eni)

- **Context / design impact**: The provider plans to support node pool security group binding (Turbo ≥1.21, ≤5 per pool) and cluster-level security group policy.
- **Questions**:
  1. Does CCE auto-create master/node security groups at cluster creation? Can custom security groups be specified? Difference and constraints between `customSecurityGroups` (node pool) and `podSecurityGroups` (Pod level)?
  2. Confirm the node pool security group limit: max 5 per pool? Is binding unsupported for Standard clusters?
  3. Can security groups of an existing cluster/node pool be modified? How does the change take effect (existing vs new nodes)?
  4. API Server access control: is public 5443 access restricted via security group/network ACL? Whitelist mechanism of `publicAccess`?
- **Suggested verification**: create cluster/node pool in console and inspect auto-created security groups and rules; test `customSecurityGroups` via SDK.
- **Answer**: Conclusion ___ / Evidence ___ / Confirmer ___ / Date ___

### Q6 IAM permissions & credential constraints

- **Context / design impact**: The provider calls CCE APIs with AK/SK; the minimum permission set and account constraints must be determined.
- **Questions**:
  1. Minimum IAM permissions for managing CCE clusters/node pools/nodes (per the official permissions table: cluster `cce:cluster:list/get/create/update/delete/upgrade/start/stop/resize`; node pool `cce:nodepool:*`; node `cce:node:*`; cert `cce:cluster:get`) — is a **custom policy with only these actions** sufficient? Are VPC/ECS/EVS permissions needed as well (node creation depends on ECS quota; enterprise-project authorization requires global `evs:quotas:get` and `evs:types:get`)?
  2. Must the AK/SK account be the project (Project) main account? Are sub-accounts/agencies supported? Purpose and version requirement of `agencyName` (`cce_cluster_agency`, 1.27+)?
  3. Relationship between `CCE FullAccess` and the fine-grained actions above (is FullAccess necessary)?
  4. Can one AK/SK pair manage resources in multiple projects? Extra configuration for cross-project calls?
- **Suggested verification**: test create/delete cluster and node pools with a minimal custom-policy account; cross-check the official "CCE permissions overview" doc.
- **Answer**: Conclusion ___ / Evidence ___ / Confirmer ___ / Date ___

### Q7 Default quotas & over-limit errors

- **Context / design impact**: Large-scale management requires knowing quotas in advance; over-limit error codes determine the provider's error classification and messaging.
- **Questions**:
  1. Default per-project quotas: max CCE clusters, node pools, nodes, VPC/subnet/ENI counts? (The official "constraints & limits" page does not publish specific numbers — please provide or point us to them.)
  2. Error code and message format when quota is exceeded? (e.g. CCE.xxxx / VPC.xxxx / Ecs.xxxx)
  3. Can quotas be raised? Process and lead time?
- **Suggested verification**: console "resource quota" page; trigger over-quota via SDK and record error codes.
- **Answer**: Conclusion ___ / Evidence ___ / Confirmer ___ / Date ___

### Q8 Cluster deletion semantics & leftovers

- **Context / design impact**: Deletion is where providers most often fail (stuck finalizers, orphaned cloud resources).
- **Questions**:
  1. `DeleteCluster` flow: does it cascade-delete node pools/nodes automatically? Order of magnitude of duration (minutes)? Cluster status during deletion?
  2. When deleting a cluster, are attached EIP/EVS/ELB resources released automatically? Which ones remain (require manual cleanup)?
  3. Must node pools/nodes be deleted first? If a cluster with node pools is deleted directly, what happens (rejected/cascaded/orphaned)?
  4. Can an `Unavailable` (abnormal) cluster be deleted? Fallback for stuck deletion (forced delete)?
  5. Idempotency of delete: what is returned for repeated delete / deleting a non-existent cluster?
- **Suggested verification**: delete a real cluster once; record duration, leftover resources, and per-phase status.
- **Answer**: Conclusion ___ / Evidence ___ / Confirmer ___ / Date ___

### Q9 Single-node path (`AddNode` / `AddNodesToNodePool`) feasibility

- **Context / design impact**: Phase 2 considers the CAPI `Machine` (non-MachinePool) path, onboarding existing ECS into CCE. Its bootstrap semantics determine feasibility.
- **Questions**:
  1. Prerequisites of `AddNode` / `AddNodesToNodePool` (adding existing ECS to a cluster/node pool): must the ECS pre-install a node agent/script? Is "SSH-free, automatic bootstrap" supported?
  2. After onboarding, how is kubelet initialized and registered? (Done by CCE automatically, or does the user need to run a script on the ECS?)
  3. Lifecycle differences between "managed ECS onboarding" and "node pool auto-created ECS" (update/replace/delete semantics)?
  4. If the onboarding path relies on manual steps, would you recommend **node-pool-only** for CAPI scenarios?
- **Suggested verification**: console "add/admit node" wizard; test `AddNodesToNodePool` via SDK.
- **Answer**: Conclusion ___ / Evidence ___ / Confirmer ___ / Date ___

### Q10 CCE Autopilot (Serverless) & CAPI integration (long-term)

- **Context / design impact**: Autopilot has no node concept (like AWS Fargate); how the CAPI model (Cluster + MachinePool) maps is TBD.
- **Questions**:
  1. Differences between Autopilot APIs and Standard/Turbo (separate API family `CreateAutopilotCluster` etc.)? Support for node-less clusters with workload scheduling only?
  2. If supported long-term, does Huawei have a recommended CAPI integration approach (e.g. Autopilot cluster + no MachinePool)?
  3. Autopilot quotas, billing, and region constraints?
- **Suggested verification**: doc confirmation; this item does not block v1 — a preliminary answer suffices.
- **Answer**: Conclusion ___ / Evidence ___ / Confirmer ___ / Date ___

### Q11 Cluster upgrade (`CreateUpgradeWorkFlow`)

- **Context / design impact**: P1 supports changing `version` to trigger an upgrade. Orchestration parameters and states must be confirmed.
- **Questions**:
  1. Call order and required parameters of `CreateUpgradeWorkFlow` / `CreatePreCheck` / `CreatePostCheck` (target version, cross-minor support, upgrade type)?
  2. Cluster status during upgrade (`Upgrading`?) and API availability (can node pools be created/scaled during upgrade)?
  3. Upgrade failure handling and rollback? Order of magnitude of duration?
  4. Is platform-version-only upgrade (platformVersion, not Kubernetes version) supported?
  5. After upgrade, do node pools/nodes need rolling (handled by CCE automatically, or must nodes be rebuilt)?
- **Suggested verification**: doc confirmation + one real minor-version upgrade in a test environment.
- **Answer**: Conclusion ___ / Evidence ___ / Confirmer ___ / Date ___

### Q12 Billing & hibernate/awake

- **Context / design impact**: cost control and `AwakeCluster`/`HibernateCluster` semantics.
- **Questions**:
  1. `billingMode`: 0=on-demand, 1=subscription; API parameters for subscription period types?
  2. Are empty/stopped clusters billed? Billing change after hibernate (`HibernateCluster`)?
  3. Trigger conditions and duration of `AwakeCluster`?
- **Suggested verification**: doc confirmation; billing per pricing page/ticket.
- **Answer**: Conclusion ___ / Evidence ___ / Confirmer ___ / Date ___

### Q13 Management cluster → CCE API Server network path

- **Context / design impact**: the management cluster (possibly in a different VPC/Region) must reach the workload cluster API Server after `clusterctl get kubeconfig`; reachability of the kubeconfig server address determines the deployment shape.
- **Questions**:
  1. Public endpoint: after enabling public access (`publicAccess`), is the API Server address/port (5443?) publicly reachable? Is there access control (whitelist)?
  2. Private endpoint: is the `internal` address reachable only within the VPC? Recommended approach for cross-VPC (peering/Direct Connect/CCE cross-VPC communication)?
  3. For management cluster in same VPC / different VPC / different Region, which access method is recommended?
- **Suggested verification**: doc confirmation + real connectivity test of public/internal kubeconfig.
- **Answer**: Conclusion ___ / Evidence ___ / Confirmer ___ / Date ___

### Q14 API rate limits & error code catalog

- **Context / design impact**: under large-scale management, the provider calls CCE/ECS/VPC APIs frequently; rate-limit thresholds are needed to design backoff.
- **Questions**:
  1. API call rate limits (per minute/second) for CCE, ECS, VPC? Do different APIs differ?
  2. Error code/HTTP status (429?) on throttling? Does the response header include `Retry-After`?
  3. Can you provide an error code catalog for common cases (at least: cluster/node pool/node not found, conflict, quota exceeded, permission denied, cluster state not allowed)?
  4. Are there batch APIs (pagination limits of `ListClusters`/`ListNodes`) and quota-query APIs?
- **Suggested verification**: doc/ticket confirmation; observe throttling with high-frequency calls.
- **Answer**: Conclusion ___ / Evidence ___ / Confirmer ___ / Date ___

---

## 4. Summary Checklist

| # | Topic | Conclusion | Evidence | Confirmer | Date |
|---|---|---|---|---|---|
| Q1 | Empty cluster create/billing/quota | | | | |
| Q2 | kubeconfig validity & external/internal | | | | |
| Q3 | ScaleNodePool delta semantics & conflicts | | | | |
| Q4 | Turbo (eni) network hard requirements | | | | |
| Q5 | Security group create/bind/modify | | | | |
| Q6 | IAM minimum permissions & credentials | | | | |
| Q7 | Default quotas & over-limit error codes | | | | |
| Q8 | Deletion semantics & leftovers | | | | |
| Q9 | AddNode single-node path feasibility | | | | |
| Q10 | Autopilot integration (long-term) | | | | |
| Q11 | Cluster upgrade workflow | | | | |
| Q12 | Billing & hibernate/awake | | | | |
| Q13 | Mgmt cluster → API Server network path | | | | |
| Q14 | API rate limits & error code catalog | | | | |

## 5. References (question basis)

- Huawei Cloud official Go SDK `huaweicloud-sdk-go-v3/services/cce/v3` (CreateCluster / ScaleNodePool / CreateKubernetesClusterCert models and comments, incl. `CertDuration` 1–1827 days and `ScaleNodePoolSpec` delta semantics).
- Huawei Cloud official docs: Create Cluster API (cce_02_0236), Create Node Pool API (cce_02_0354), Cluster Type Comparison (cce_10_0342), Network Models, CCE Permissions Overview (cce_10_0187), System Agency (cce_10_0556).
- Full source list and archives: [research-sources.md](research-sources.md).
