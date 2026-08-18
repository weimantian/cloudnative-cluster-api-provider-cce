# deploy/variables.md — 环境变量参考

本仓库所有脚本**只能**通过环境变量读取配置,不硬编码任何值。运行脚本前请在 shell(或 CI 密钥库)中导出它们。

> English version: [variables.md](variables.md)

## Provider 部署

| 变量 | 必填 | 说明 | 示例 |
|---|---|---|---|
| `CCE_ACCESS_KEY` | 是(部署) | 华为云访问密钥(AK),用于管理 CCE 资源 | `ABCDEFGHIJKLMNOPQRST` |
| `CCE_SECRET_KEY` | 是(部署) | 与 AK 配对的华为云秘密访问密钥(SK) | `secret-value` |
| `CCE_REGION` | 是(部署) | 目标区域,如 `cn-north-4` | `cn-north-4` |
| `CCE_PROJECT_ID` | 否 | 目标账号的项目 ID;为空时从凭证推断 | `0a1b2c3d4e5f...` |
| `MANAGEMENT_CLUSTER_KUBECONFIG` | 否 | 管理集群 kubeconfig 路径;默认 `~/.kube/config` | `/tmp/mgmt.kubeconfig` |
| `CLUSTERCTL_VERSION` | 否 | 要检查的 `clusterctl` 版本;默认最新 v1.x | `v1.9.0` |
| `WORKLOAD_CLUSTER_MANIFEST` | 否 | 工作集群清单路径(Cluster + CceCluster + CceManagedControlPlane + MachinePool + CceManagedMachinePool) | `config/samples/workload-cluster.yaml` |

## 工作集群清单(引用变量)

工作集群清单本身必须通过 `deploy-provider.sh` 创建的每集群 Secret 引用凭证;严禁在清单中内嵌密钥。

| 清单字段 | 来源 |
|---|---|
| `CCECluster.spec.credentialsSecretName` | 由 `deploy-provider.sh` 创建的 Secret `<cluster>-credentials` |
| `CCEManagedMachinePool.spec.replicas` | 期望节点数(如 `3`) |
