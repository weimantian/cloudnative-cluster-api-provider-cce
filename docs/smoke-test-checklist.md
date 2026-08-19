# 真实 CCE 冒烟测试 — 所需环境与准备清单

> 运行:`scripts/smoke-cce.sh`(驱动 `internal/services/cce/smoke_test.go`,`smoke` build tag)。
> 目的:验证官方文档无法确认的 14 项问卷中剩余行为(Q1–Q8)。**会创建真实计费资源(CCE Turbo 集群 + 节点池),测试结束自动删除,但请运行后到控制台核对残留。**

## 一、你需要提供的环境信息(运行前导出)

| 环境变量 | 必填 | 说明 | 如何获取 |
|---|---|---|---|
| `CCE_SMOKE_AK` / `CCE_SMOKE_SK` | ✅ | 华为云访问密钥对 | IAM → 访问密钥 → 新建;**必须是账号或实体 IAM 用户的永久密钥(联邦用户不行,Q6)**;权限见下 |
| `CCE_SMOKE_REGION` | 默认 `cn-north-4` | 测试区域 | 需该区域有配额 |
| `CCE_SMOKE_VPC` | ✅ | 已有 VPC ID | 控制台 VPC → 详情;CCE 创建前置必须已有 VPC(Q1) |
| `CCE_SMOKE_SUBNET` | ✅ | 节点子网 ID | VPC → 子网;需与集群同 VPC |
| `CCE_SMOKE_ENI_SUBNET` | ✅ | ENI/容器子网 ID(Turbo/eni 用) | VPC → 子网;eni 模式下容器子网可取 VPC 子网(Q4) |
| `CCE_SMOKE_KEYPAIR` | ✅ | SSH 密钥对名称 | ECS → 密钥对;节点池 `sshKey` 必填 |
| `CCE_SMOKE_FLAVOR` | 默认 `c7.large.2` | 节点 ECS 规格 | 需该区域有货且 **sub-ENI 配额**(Turbo,`CCE.01400025` 提示即无) |
| `CCE_SMOKE_CLUSTER_FLAVOR` | 默认 `cce.s2.medium` | 集群规格 | 控制面 3 节点、最大 200 节点 |
| `CCE_SMOKE_K8S_VERSION` | 默认空(CCE 最新) | Kubernetes 版本 | 空 = CCE 最新(Q11 版本策略) |
| `CCE_SMOKE_AZ` | 可选 | 节点可用区 | 空 = CCE 自动 |
| `CCE_SMOKE_CASES` | 默认全跑 | 用例开关:`cluster,pool,scale,delete`(可加 `quota,kubeconfig`) | — |

## 二、账号权限与配额(运行前确认)

1. **IAM 权限(最小集合,Q6)**:以下 action 的自定义策略即可(CCE 依赖的 ECS/VPC 资源操作由系统委托代行,AK/SK 无需 ECS/VPC 权限):
   - `cce:cluster:list/get/create/update/delete` + `cce:cluster:get`(证书)
   - `cce:nodepool:list/get/create/update/delete/scale`
   - `cce:node:list/get/create/delete`
   - `cce:job:get/list`
   - (企业项目授权下创建节点需全局 `evs:quotas:get`、`evs:types:get`)
2. **系统委托**:账号下需已存在 `cce_admin_trust` 或 `cce_cluster_agency`(控制台首次创建集群会提示自动创建;缺失则集群创建失败)。
3. **配额(Q7,控制台"资源 > 我的配额")**:
   - 集群配额 ≥ 2(测试 + 容错);
   - ECS 配额:节点池 2 节点 × 所选规格 + 系统盘 40G × 2;
   - VPC/子网/EIP(测试用私网访问,不需要 EIP;若要测公网再加)。
4. **安全组**:私网测试无需额外配置;若 `endpointAccess.public=true`,需 `cce-control` 安全组 5443 放通管理端 IP(Q5/Q13)。

## 三、用例 ↔ 问卷项映射(冒烟会输出什么)

| 用例 | 问卷项 | 冒烟输出/验证点 |
|---|---|---|
| `cluster` | Q1/Q4 | 创建**空集群**(Turbo/eni)是否成功;phase 最终为 Available;版本/endpoint 回读 |
| `kubeconfig` | Q2 | duration=30 天证书获取是否成功(解析有效期) |
| `pool` | Q3/Q5 | initialNodeCount=2 是否生效;节点达到 2 |
| `scale` | **Q3 关键** | **2 节点池上 ScaleNodePool(desiredNodeCount=2)→ 观察节点数:仍为 2 = 绝对值;变 4 = 增量**;UpdateNodePool(ignore=true)是否保持数量不变 |
| `delete` | Q8 | DeleteNodePool → DeleteCluster(delete_evs/eni/net=true)→ 集群消失;**运行后请到控制台核对无 EVS/ELB/EIP 残留** |
| `quota` | Q7 | ShowQuotas 返回集群配额 limit/used(实测值,解决文档 5 vs 50 矛盾) |
| `autoscaling` | Q3 + B3(FR-2.6) | **节点池带 autoscaling {enable=true,min=1,max=4} 创建是否被接受;ListNodePools 回读是否一致;autoscaling 开启时手动 ScaleNodePool 是否并存不冲突** |
| `upgrade` | Q11 + E3(FR-1.7) | **GetUpgradeInfo 返回的平台目标版本列表(空 = 平台无路径,Q11);有目标 → StartUpgrade→ShowUpgradeTask 轮询到 Success/Failed 并计时;无目标 → 记录 StartUpgrade 被平台拒绝的错误(controller 走 UpgradeNotOffered)** |

> 说明:`autoscaling`、`upgrade` 用例需在 `CCE_SMOKE_CASES` 中显式开启(如 `CCE_SMOKE_CASES=autoscaling,upgrade`)。`upgrade` 用例参数:`CCE_SMOKE_UPGRADE_FROM`(起始版本,默认 v1.34;实测 v1.33 目标已开放、v1.34 未开放);`CCE_SMOKE_UPGRADE_MODE`(vpc-router=Standard 默认 / eni=Turbo,eni 需 `CCE_SMOKE_ENI_SUBNET` 传 neutron 子网 ID);`CCE_SMOKE_UPGRADE_WITH_POOL=1`(创建 1 节点节点池后再升级,官方流程需升用户节点;空集群与带节点池实测均在升级前检查失败)。升级成功时测试会自动输出耗时。

## 四、安全与成本提醒

- **只使用一次性测试账号/子项目**;凭证仅经环境变量传入,**禁止写入仓库/日志**;
- 冒烟期间集群约计费 30–60 分钟(创建 + 节点就绪 + 伸缩观察 + 删除);删除后核对残留资源并退订;
- 若测试中途失败(如资源不足),用控制台手动删除 `capi-smoke-*` 集群。

## 五、预期结果回填

运行后把输出(尤其 Q3 的节点数结果、Q7 配额值)回填到 [问卷汇总表](cce-verification-questionnaire.md) 与 [落地跟踪](poc-implementation-tracker.md),关闭对应待实测项。
