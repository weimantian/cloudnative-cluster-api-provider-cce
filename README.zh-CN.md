# cloudnative-cluster-api-provider-cce

[![License: MIT-0](https://img.shields.io/badge/License-MIT--0-brightgreen.svg)](LICENSE)
[![Huawei Cloud](https://img.shields.io/badge/HuaweiCloud-CCE-orange)](https://www.huaweicloud.com/product/cce.html)
[![Build Status](https://img.shields.io/badge/build-passing-brightgreen)]()
[![Lifecycle: incubating](https://img.shields.io/badge/lifecycle-incubating-blue)]()

> 英文版 / English: [README.md](README.md)

一个用于管理**华为云 CCE(云容器引擎)托管集群**的 Cluster API(CAPI)基础设施 Provider——通过标准的 Cluster API 资源以声明式方式创建、扩缩容和删除 CCE 集群与节点池,对标 `CAPI + AWS EKS 托管模式` 的使用体验。

本项目面向平台工程师与 SRE 团队,帮助他们用 Kubernetes 原生、适合 GitOps 的工具链(`kubectl` / `clusterctl` / ArgoCD / Flux)管理华为云 CCE 集群,就像今天管理 AWS EKS 集群一样。

> **状态:incubating(PoC 已验证)。** 架构设计与需求设计文档已完成;可编译的 PoC(CRD、控制器、服务层、webhook、部署清单)已就位。云侧行为已在真实华为云 CCE 账号上验证通过(空集群创建→Available、节点池绝对值扩缩容、kubeconfig 轮换、带清理的删除、公网 EIP 绑定、限流行为——详见 [docs/cce-verification-findings.md](docs/cce-verification-findings.md))。单元测试与 envtest 控制器测试全部通过。参见 [docs/](docs/) 与[需求文档](docs/requirements-design.md)中的路线图。

## 目录

- [概述](#概述)
- [架构](#架构)
- [方案亮点](#方案亮点)
- [涉及云服务与费用](#涉及云服务与费用)
- [前置条件](#前置条件)
- [快速开始](#快速开始)
- [分步部署](#分步部署)
- [使用方法 / 验证](#使用方法--验证)
- [清理资源](#清理资源)
- [详细文档](#详细文档)
- [依赖与致谢](#依赖与致谢)
- [FAQ / 故障排除](#faq--故障排除)
- [贡献指南](#贡献指南)
- [许可证](#许可证)
- [联系方式 / 维护者](#联系方式--维护者)

## 概述

`cloudnative-cluster-api-provider-cce` 将 Cluster API 对象(`Cluster`、`MachinePool`)翻译为华为云 CCE API 调用,使您可以:

- 创建 CCE 托管集群(控制面由华为云托管)——支持 **CCE Standard** 与 **CCE Turbo**;
- 通过 `MachinePool` 管理 CCE 节点池(修改 `replicas` 即可扩缩容);
- 通过 `clusterctl get kubeconfig` 获取工作集群的 kubeconfig。

它遵循 CAPI Provider 合约(namespace 级 CRD、版本标签、`status.conditions`、finalizer、`clusterctl` 打包),并按照华为云解决方案开发者套件治理规范发布(见 [CONTRIBUTING.md](CONTRIBUTING.md) 与 [docs/](docs/))。

## 架构

```mermaid
flowchart LR
    subgraph MGMT["管理集群"]
        CAPI["cluster-api (core)<br/>Cluster / MachinePool"]
        P["cce-provider-for-cluster-api<br/>(本 Provider)"]
        CAPI -->|infrastructureRef / controlPlaneRef| P
    end
    P -->|华为云 Go SDK| HW["华为云(目标项目)"]
    HW --> CCE["CCE 托管集群"]
    HW --> NP["节点池(ECS 节点)"]
    CCE -->|kubeconfig| P
```

设计细节:参见 [docs/architecture-design.md](docs/architecture-design.md) 与 [docs/research-sources.md](docs/research-sources.md)(每个设计决策背后的事实依据)。

## 方案亮点

- **声明式管理托管集群**——CCE 控制面完全由华为云托管,Provider 只负责翻译与调谐。
- **CCE Standard + CCE Turbo 双支持**——两者都支持(默认推荐 Turbo,与 EKS 托管定位对齐)。
- **MachinePool ↔ 节点池**——通过 `MachinePool.spec.replicas` 扩缩容;托管节点池无需 bootstrap provider。
- **兼容 `clusterctl`**——`metadata.yaml` + `infrastructure-components.yaml` 打包(进行中),支持 `clusterctl describe cluster` / `get kubeconfig`。
- **GitOps 就绪**——通过 ArgoCD/Flux 从 Git 全流程驱动。

## 涉及云服务与费用

| 服务 | 用途 | 费用说明 |
|---|---|---|
| CCE(云容器引擎) | 托管 Kubernetes 集群(Standard/Turbo) | 默认按需计费(`billingMode: 0`);空集群也可能产生费用,请以价格页为准 |
| ECS(弹性云服务器) | 工作节点(由 CCE 节点池管理) | 按节点计费,见节点池 `flavor` / `billingMode` |
| VPC / 子网 | 集群与节点的网络 | 由用户提供/引用;出网可能需要 NAT/EIP |
| (可选)EIP / ELB | 公网访问 API Server | 仅当开启 `endpointAccess.public` 时 |

> 测试集群使用后务必清理(见[清理资源](#清理资源)),避免持续计费。

## 前置条件

- 华为云账号,且 IAM 用户具备 Provider 所需的 CCE 权限(action 清单见 [docs/smoke-test-checklist.md](docs/smoke-test-checklist.md) §2;对应问卷 [docs/cce-verification-questionnaire.md](docs/cce-verification-questionnaire.md) Q6)。
- 已有 VPC 和子网(CCE 创建集群前必须存在 VPC)。
- 一个 Kubernetes 管理集群(`kind` 或专用集群),已安装 `clusterctl` v1.14+ 并配置好 `kubectl`。
- Webhook TLS 证书:管理集群上安装 [cert-manager](https://cert-manager.io)(生产推荐),或预创建 `webhook-service-cert` TLS Secret(见下方步骤 3)。

## 快速开始

> 以下完整流程已用 `clusterctl v1.14.0` + `kind` + 真实华为云 CCE 账号端到端验证通过(详见 [docs/clusterctl-deployment-validation.md](docs/clusterctl-deployment-validation.md))。凭证**只能**通过 Secret / 环境变量提供——切勿硬编码。

```bash
# 1. 让 clusterctl 找到 Provider(发布前使用本地源;目录布局见"分步部署")
mkdir -p ~/.cluster-api
cat > ~/.cluster-api/clusterctl.yaml <<'EOF'
providers:
  - name: "cce"
    url: "file:///tmp/cce/infrastructure-cce/v0.1.0/infrastructure-components.yaml"
    type: "InfrastructureProvider"
EOF

# 2. 在管理集群上安装 Provider(会同时安装 cert-manager、CAPI 核心、
#    bootstrap/control-plane 与 infrastructure-cce)
clusterctl init --infrastructure cce --wait-providers

# 3. 提供每集群凭证 Secret
kubectl create secret generic my-cce-cluster-credentials \
  --namespace default \
  --from-literal=accessKey="$CCE_ACCESS_KEY" \
  --from-literal=secretKey="$CCE_SECRET_KEY"

# 4. 应用工作集群并观察(真实创建 CCE 集群)
kubectl apply -f cluster-template.yaml
kubectl get ccemanagedcontrolplane --watch
```

## 分步部署

1. **构建 Provider 组件**(每个版本一次;正式发布会直接附带 `infrastructure-components.yaml`):

   ```bash
   make docker-build docker-push   # IMG=registry/org/cce-provider-controller:vX.Y.Z
   kubectl kustomize config/default > infrastructure-components.yaml
   # 6 个 webhook 需要 clientConfig.caBundle = base64(CA 证书);cert-manager 会自动注入,
   # 否则需手动填充(见演练文档)。
   ```

2. **配置 clusterctl** 定位 Provider。发布前的本地目录布局:

   ```bash
   mkdir -p /tmp/cce/infrastructure-cce/v0.1.0
   cp infrastructure-components.yaml metadata.yaml /tmp/cce/infrastructure-cce/v0.1.0/
   # ~/.cluster-api/clusterctl.yaml 同"快速开始"
   ```

   > 组件中的镜像名必须是三段式规范名(`registry/org/repo:tag`),否则 clusterctl 的镜像 override 会解析失败。

3. **Webhook TLS 证书**。不使用 cert-manager 时,预创建 manager 挂载到 `/tmp/k8s-webhook-server/serving-certs` 的 Secret:

   ```bash
   # CN 必须匹配 webhook 服务:webhook-service.cce-provider-system.svc
   # (SAN: webhook-service、webhook-service.cce-provider-system.svc(.cluster.local))
   kubectl -n cce-provider-system create secret tls webhook-service-cert \
     --cert=server.crt --key=server.key
   ```

   > RBAC 注意:leader-election RoleBinding 的 subject.namespace 必须是真实命名空间(`cce-provider-system`);kustomize 不会改写 RoleBinding 的 subjects。

4. **安装:**

   ```bash
   clusterctl init --infrastructure cce --wait-providers
   clusterctl get providers          # 四个 Provider 均 Available
   ```

5. **创建工作集群**(Cluster + CCECluster + CCEManagedControlPlane + MachinePool + CCEManagedMachinePool;样例见 `config/samples/cluster-template.yaml`,填入你的 VPC/子网 ID):

   ```bash
   kubectl create secret generic my-cce-cluster-credentials \
     --namespace default \
     --from-literal=accessKey="$CCE_ACCESS_KEY" \
     --from-literal=secretKey="$CCE_SECRET_KEY"
   kubectl apply -f workload-cluster.yaml
   kubectl get ccemanagedcontrolplane my-cce-cluster-control-plane -o yaml
   ```

   预期条件:`CCEClusterReady=True(ClusterAvailable)`、`CredentialsReady=True`、`KubeconfigReady=True`、`UpgradeReady=True`。

6. **验证与清理:**

   ```bash
   clusterctl get kubeconfig my-cce-cluster > my-cce-cluster.kubeconfig
   kubectl --kubeconfig my-cce-cluster.kubeconfig get nodes   # == replicas,全部 Ready

   kubectl delete cluster my-cce-cluster   # 异步:CCE 集群 → kubeconfig Secret → finalizer
   ```

## 使用方法 / 验证

```bash
# 验证集群健康
clusterctl describe cluster my-cluster

# 获取工作集群 kubeconfig
clusterctl get kubeconfig my-cluster > my-cluster.kubeconfig
kubectl --kubeconfig my-cluster.kubeconfig get nodes
```

预期结果:`kubectl get nodes` 显示的节点数等于 `MachinePool.spec.replicas`,且全部 `Ready`。

### 集群升级(FR-1.7)

将 `CCEManagedControlPlane.spec.version` 改为更高的 Kubernetes 版本,Provider 将驱动 CCE 升级工作流(升级前检查 → 原地滚动升级 → 升级后检查),并通过 `UpgradeReady` 条件报告进度。注意:平台决定可升级的目标版本——当无可升级目标时,条件报告 `UpgradeNotOffered`(属正常状态而非错误;详见 `docs/cce-verification-findings.md` Q11)。

### 节点池自动伸缩(Alpha,功能开关)

`CCEManagedMachinePool.spec.autoscaling`(enable/min/max)仅在开启 `NodePoolAutoscaling` 功能开关时生效:

```bash
manager --feature-gates=NodePoolAutoscaling=true
```

### 规格白名单(webhook)

`CCEManagedMachinePool.spec.flavor` 会按 ECS 规格命名规则做格式校验;可按部署(分 region)注入可选白名单:

```bash
manager --valid-flavors=c6.large.2,c7.large.2
```

## 清理资源

```bash
# 删除工作集群(删除 CCE 集群、节点池与 Provider 持有的资源)
kubectl delete cluster my-cluster

# 可选:从管理集群卸载 Provider
clusterctl delete --infrastructure cce
```

> 删除 `Cluster` 对象会触发依赖删除(节点池 → 集群 → kubeconfig Secret)。删除后在 CCE 控制台确认无 EIP/EVS/ELB 残留。

## 详细文档

- [架构设计文档](docs/architecture-design.md)
- [需求设计文档](docs/requirements-design.md)
- [调研依据与事实清单(含验证清单)](docs/research-sources.md)
- [华为云 CCE 对齐问卷](docs/cce-verification-questionnaire.md) · [验证结论记录](docs/cce-verification-findings.md)
- [clusterctl 部署演练记录(kind + 真实 CCE)](docs/clusterctl-deployment-validation.md)
- [官方 API 参考文档审查记录](docs/api-review-findings.md)
- [全量代码审计记录](docs/code-audit-findings.md)
- [CAPA 能力对标差距分析](docs/capa-parity-gap-analysis.md)
- [CAPA 源码分析报告](docs/CAPA架构分析报告.md) · [阿里云 ACK Provider 源码分析报告](docs/ACKProvider架构分析报告.md) · [CAPHW 源码分析报告](docs/CAPHW架构分析报告.md)

## 依赖与致谢

- [Cluster API](https://cluster-api.sigs.k8s.io/)(`sigs.k8s.io/cluster-api`)——核心合约与控制器。
- [controller-runtime](https://github.com/kubernetes-sigs/controller-runtime)——Reconciler 框架。
- [华为云 Go SDK](https://github.com/huaweicloud/huaweicloud-sdk-go-v3)——CCE/ECS/VPC 客户端。
- 参考实现:[cluster-api-provider-aws](https://github.com/kubernetes-sigs/cluster-api-provider-aws)、[alibabacloud-provider-for-Cluster-API](https://github.com/AliyunContainerService/alibabacloud-provider-for-Cluster-API)、[cluster-api-provider-huawei](https://github.com/huaweicloud-samples/cluster-api-provider-huawei)。

## FAQ / 故障排除

- **集群创建报网络错误**——CCE 要求先有 VPC,且容器/服务网段不能冲突;请检查 `spec.network` 与网段规划(见 [docs/architecture-design.md](docs/architecture-design.md) §6)。
- **节点池不扩缩容**——确认控制面已 `Ready`(节点池只有在集群 `Available` 后才能创建),且 IAM 用户具备 `cce:nodepool:scale` 权限。
- **`clusterctl get kubeconfig` 返回的 server 不可达**——私网集群的 kubeconfig server 是内网地址,请确保管理集群能访问。
- 更多:[docs/requirements-design.md](docs/requirements-design.md) §8(注意事项)与[验证清单](docs/research-sources.md) §4。

## 贡献指南

参见 [CONTRIBUTING.zh-CN.md](CONTRIBUTING.zh-CN.md)。所有提交必须带 DCO 签名(`git commit -s`)。

## 许可证

本项目采用 [MIT No Attribution(MIT-0)](LICENSE) 许可证。

## 联系方式 / 维护者

- 维护者:<your-team@huaweicloud.com>(占位——仓库创建时更新)
- 交流渠道:GitHub Issues / [华为云开发者社区](https://developer.huaweicloud.com/)
