# deploy — 部署脚本

本目录包含 `cloudnative-cluster-api-provider-cce` 的部署工具。

> **状态说明**:Provider 正在开发中(incubating)。以下脚本描述目标部署流程,待发布版本(含 `metadata.yaml` + `infrastructure-components.yaml`)后即可完整执行。
> English version: [README.md](README.md)

## 目录结构

| 路径 | 用途 |
|---|---|
| `scripts/deploy-provider.sh` | 通过 `clusterctl init --infrastructure cce` 在管理集群上安装 Provider,并创建每集群凭证 Secret。 |
| `scripts/destroy.sh` | **破坏性操作**:删除工作集群并卸载 Provider。需要交互确认。 |
| `variables.md` | 脚本消费的环境变量完整清单(无硬编码密钥)。 |
| `scripts/`(仓库根下,即 `scripts/`) | 辅助脚本,例如 `scripts/check-prerequisites.sh`。 |

## 安全规则

- 所有敏感值(`CCE_ACCESS_KEY`、`CCE_SECRET_KEY` 等)**只能**通过环境变量或交互提示读取。**严禁硬编码默认值。**
- CI 的 `secret-scan` workflow(gitleaks)会在每次推送时扫描本目录。
- 脚本在 CI 中校验(`iac-validate` workflow:`bash -n` + `shellcheck`)。

## 清理

评估完成后务必运行 `scripts/destroy.sh`,避免持续计费(CCE 集群与节点按需计费)。
