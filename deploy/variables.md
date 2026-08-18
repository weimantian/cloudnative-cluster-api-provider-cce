# deploy/variables.md — Environment Variables Reference

All scripts in this repository read configuration **only** from environment
variables. No value is hardcoded. Export them in your shell (or a CI secret
store) before running the scripts.

> 中文版 / Chinese: [variables.zh-CN.md](variables.zh-CN.md)

## Provider deployment

| Variable | Required | Description | Example |
|---|---|---|---|
| `CCE_ACCESS_KEY` | yes (deploy) | Huawei Cloud Access Key (AK) used to manage CCE resources | `ABCDEFGHIJKLMNOPQRST` |
| `CCE_SECRET_KEY` | yes (deploy) | Huawei Cloud Secret Key (SK) paired with the AK | `secret-value` |
| `CCE_REGION` | yes (deploy) | Target region for the management setup, e.g. `cn-north-4` | `cn-north-4` |
| `CCE_PROJECT_ID` | no | Project ID of the target account; inferred from credentials if empty | `0a1b2c3d4e5f...` |
| `MANAGEMENT_CLUSTER_KUBECONFIG` | no | Path to the management-cluster kubeconfig; defaults to `~/.kube/config` | `/tmp/mgmt.kubeconfig` |
| `CLUSTERCTL_VERSION` | no | `clusterctl` version to check; defaults to latest v1.x | `v1.9.0` |
| `WORKLOAD_CLUSTER_MANIFEST` | no | Path to the workload cluster YAML (Cluster + CceCluster + CceManagedControlPlane + MachinePool + CceManagedMachinePool) | `config/samples/workload-cluster.yaml` |

## Workload cluster manifest (referenced variables)

The workload cluster manifest itself must reference credentials through the
per-cluster Secret created by `deploy-provider.sh`; never embed keys in the
manifest.

| Manifest field | Source |
|---|---|
| `CCECluster.spec.credentialsSecretName` | Secret `<cluster>-credentials` created by `deploy-provider.sh` |
| `CCEManagedMachinePool.spec.replicas` | Desired node count (e.g. `3`) |
