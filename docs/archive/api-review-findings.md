# 官方 CCE API 参考文档审查记录(2026-08-19)

> 依据:`docs/云容器引擎 CCE API参考.pdf`(2518 页,版本 01,2026-08-18)与 `docs/云容器引擎 CCE SDK参考_副本.pdf`(17 页)。提取代码用到的 30 个 API 章节文本(`/tmp/cce-doc/*.txt`),4 路并行对照实现(`internal/services/cce/`、`controllers/`、`internal/services/errors/`、webhook)。
> 修复提交:`0646156`(集群管理+错误码主批)、`b8d8ace`(IAM 权限措辞)、`234c10a`(操作冲突码+命名映射)、`fd655cf`(节点池余项)。
> 状态图例:✅ 已修复 · 📋 记录在案(能力缺口/取舍,不改代码)

## 一、汇总

| 模块 | 高(阻塞) | 中 | 低 | 一致项 |
|---|---|---|---|---|
| 集群管理 | 2 | 7 | 2 | 6 |
| 节点池/节点 | 1 | 3 | 3 | 4 |
| 集群升级 | 0 | 3 | 2 | 5 |
| 错误码/IAM | 0 | 2 | 2 | 6 |
| **合计** | **3** | **15** | **9** | **21** |

---

## 二、集群管理服务(对照 CreateCluster/ShowCluster/ListClusters/DeleteCluster/CreateKubernetesClusterCert/ShowClusterEndpoints/GetClusterQuota/ShowQuotas)

| # | 位置 | 问题 | 文档依据 | 状态 |
|---|---|---|---|---|
| A1 | `cce.go` DeleteCluster | **periodic_node_policy 默认值颠倒**:空值时代码发 `reset`(清数据),官方默认 `retain`(保留数据) | DeleteCluster.txt 表4-144 "默认取值: retain" | ✅ 空值不再传参,平台默认生效;`DeleteOBS/DeleteSFS/DeleteSFS30` 透传(官方默认 skip→残留) |
| A2 | `interfaces.go` DeleteClusterInput | 缺 `delete_obs/delete_sfs/delete_sfs30` 透传(官方默认 skip=残留) | DeleteCluster.txt 表4-144 "delete_obs…默认取值: false" | ✅ 新增三字段并映射 |
| A3 | `cce.go` CreateCluster | **hostNetwork(VPC+子网)官方必填,代码可整体省略** | CreateCluster.txt 表4-3/4-6 "VPC是集群内节点之间的通信依赖,所以是必选的参数集" | ✅ 任一为空即返回参数错误(fail fast) |
| A4 | `cce.go` CreateCluster | category 空值时 eni 模式应默认 Turbo,代码一律发 CCE | CreateCluster.txt 表4-5 "容器网络参数设置为eni模式时,默认为Turbo" | ✅ `clusterCategory(category, mode)` 按 mode 推导 |
| A5 | `cce.go` CreateCluster + Input | **包周期(billingMode=1)缺必填 periodType/periodNum** | CreateCluster.txt 表4-18 "billingMode为1(包周期)时生效,且为必选" | ✅ 新增 PeriodType/PeriodNum/IsAutoRenew/IsAutoPay 并校验必填 |
| A6 | `cce.go` GetClusterKubeconfig | **duration=-1 被钳成 1 天**(官方 -1=5 年/1827 天),>1827 无钳制 | CreateKubernetesClusterCert.txt 表4-177 "若填-1则为最大值5年" | ✅ -1→1827,越界钳制 |
| A7 | `interfaces.go` Endpoint | Type 注释错误(应为 Internal/External,来源是 ShowCluster 非 ShowClusterEndpoints) | ShowCluster.txt 表4-76 "Internal:用户子网内访问的地址 / External:公网访问的地址" | ✅ 注释修正 |
| A8 | `cce.go` CreateCluster | flavor/version 空串也被发送(官方默认仅"不配置"时生效) | CreateCluster.txt 表4-5 flavor "默认取值: cce.s1.small";version "若不配置,默认创建最新版本" | ✅ 空值省略字段 |
| A9 | `interfaces.go` | CreateClusterInput.Tags / CreateNodePoolInput.BillingMode·Tags 声明但未映射 | CreateCluster.txt 表4-5 clusterTags | ✅ BillingMode 已映射(见 B3);Tags 未实现,记录 📋 |
| A10 | `errors.go` | IsQuotaExceeded 漏 01400009/10/12/19(CPU/内存/EIP/租户配额) | ErrorCodes.txt 四码 "Insufficient … quota" | ✅ 补齐常量并纳入 |
| A11 | `errors.go`/`cce.go` | 容器网段冲突:官方 01400005 + 实测 CCE_CM.0410 未纳入 IsConflict(靠字符串硬编码) | ErrorCodes.txt "CCE.01400005 Container network CIDR blocks conflict";CM 族官方表未收录(实测) | ✅ 两码入 IsConflict,替换硬编码 |
| — | 一致项 | ShowCluster 映射、DeleteCluster 已实现选项枚举、证书请求体(duration/expire_at)、ShowQuotas(quotaKey=="cluster")、404/409/429 分类 | — | ✅ |

## 三、节点池/节点管理(对照 CreateNodePool/ListNodePools/UpdateNodePool/DeleteNodePool/ScaleNodePool/UpgradeNodePool/CreateNode/ListNodes/AddNode)

| # | 位置 | 问题 | 文档依据 | 状态 |
|---|---|---|---|---|
| B1 | `controllers/ccemanagedmachinepool_controller.go` reconcileDelete | **删除死锁:DeleteNodePool 后无条件 requeue,NodePoolID 永不清空 → finalizer 永不移除,对象删不掉** | DeleteNodePool.txt(异步删除语义) | ✅ 轮询 ListNodePools 确认消失后清 NodePoolID |
| B2 | `cce.go` ListNodePools + controller | **NodeCount 从不赋值 → AvailableReplicas 恒 0;Replicas 用目标数(initialNodeCount)而非实际** | ListNodePools.txt status: currentNode "当前节点池中所有节点数量"、activeNode "就绪的节点数量" | ✅ 映射 currentNode/activeNode,新增 ActiveNodeCount |
| B3 | `cce.go` CreateNodePool | **BillingMode 静默丢弃**(CR/Input 已赋值,构造 NodeTemplate 未引用) | CreateNodePool.txt 表 "billingMode 0:按需付费;1:包周期" | ✅ 映射 `NodeTemplateBillingMode` E_0/E_1 |
| B4 | webhook validate() | **az/os/rootVolume 官方必填被当可选**(实测:空 AZ 报 "Az [] is not in available az list"、空 OS 报 "should not be empty"、非本地盘须数据盘) | CreateNodePool.txt "az 是…通过api创建节点不支持随机可用区";"rootVolume 是…";"未指定私有镜像时 os 为必选" | ✅ webhook 强校验(az/os/rootVolume 必填,rootVolume size [40,1024]) |
| B5 | `interfaces.go`/`cce.go` UpdateNodePool | 遗漏 taintPolicy/labelPolicy/userTagsPolicy/extensionScaleGroups/nodeManagementUpdate 等官方可更新字段 | UpdateNodePool.txt NodePoolSpecUpdate | ✅ 新增 3 个 policy 字段;controller 在 spec 有 taints/labels 时设 refresh(存量节点收敛,呼应 Q11b) |
| B6 | `cce.go` UpdateNodePool | customSecurityGroups 无法重置为空(恢复默认安全组) | UpdateNodePool.txt "未指定安全组ID,新建节点将添加Node节点默认安全组" | ✅ 无条件发送(空数组=重置) |
| B7 | `cce.go` toNodePoolAutoscaling | 注释与代码矛盾(nil 处理)、无 nil 防护 | — | ✅ 注释修正 + nil guard |
| B8 | Service 接口 | UpgradeNodePool(同步节点池)未实现 | UpgradeNodePool.txt "同步节点池中已有节点的配置" | 📋 能力缺口;部分可用 update 的 taint/label refresh 替代 |
| — | 一致项 | ScaleNodePool 绝对值语义+scaleGroups=default、DeleteNodePool 404 幂等、ListNodePools uid/name/initialNodeCount、AddNode/CreateNode 取舍(与 Q9 一致:重装清数据+严格前置、不支持接入节点池)、taints 解析/≤20、k8sTags 约束 | — | ✅ |

## 四、集群升级(对照 UpgradeCluster/ShowUpgradeClusterTask/ListUpgradeClusterTasks/PreCheck/PostCheck/ClusterMasterSnapshot/GetClusterUpgradeInfo/GetClusterUpgradePaths/CreateUpgradeWorkFlow)

| # | 位置 | 问题 | 文档依据 | 状态 |
|---|---|---|---|---|
| C1 | `cce.go` StartUpgrade | userDefinedStep 注释数值错误("up to 120" 应为 [1-60]);文档标"可选"但实测必填 | UpgradeCluster.txt "取值范围:[1-60] 默认取值:20" | ✅ 注释修正;值 20 同时满足文档默认与平台必填 |
| C2 | `interfaces.go`/`cce.go` UpgradeInfo | **suggestPatch 未暴露**,controller 无法给出"先升补丁"的可执行建议;smoke 需绕过接口直调 SDK | GetClusterUpgradeInfo.txt "suggestPatch 推荐升级的目标补丁版本号,如r0" | ✅ UpgradeInfo 增加 Patch/SuggestPatch;controller 空目标提示引用 |
| C3 | `cce.go` StartUpgrade | 创建 PreCheck 后未等待 Success 即发 UpgradeCluster(流程缺口) | CreateUpgradeWorkFlow.txt "升级前检查 → 集群升级 → 升级后确认" | 📋 建议补轮询(待实现) |
| C4 | Service | Pause/Retry/Continue 未实现(RetryUpgradeClusterTask 建议补);ShowUpgradeWorkFlow 未用(工作流被 Cancel 无感知) | ListUpgradeClusterTasks.txt(Retry 示例) | 📋 建议补(受控次数重试 + 工作流状态) |
| C5 | `smoke_test.go` | Q11 用例旧请求体:kind "PreCheck"/"PostCheck"(应为 PreCheckTask/PostCheckTask)、缺 clusterVersion、InPlaceRollingUpdate 空对象——与修复后实现/文档矛盾,失去回归价值 | PreCheck.txt kind "PreCheckTask";PostCheck.txt kind "PostCheckTask" | ✅ 已对齐修复后请求体 |
| C6 | controller 版本匹配 | `slices.Contains(targetVersions, spec.version)` 逐字符相等;平台允许大版本写法(自动选最新补丁),用户写 v1.35 会被误判不可用 | PreCheck.txt "升级目标版本,如果填写大版本,则自动选择最新补丁版本" | ✅ 前缀归一化匹配(containsVersion) |
| C7 | `interfaces.go` | 仅定义 UpgradeTaskPhaseSuccess/Failed 两常量,Init/Queuing/Running/Pause 靠 default 分支兜底 | ShowUpgradeClusterTask.txt phase 枚举 | 📋 建议补齐 6 常量 |
| — | 一致项 | clusterID 必填(修复)、版本完整格式(修复)、WorkFlowSpec/PrecheckSpec 字段、ShowUpgradeClusterTask 请求/响应、GetClusterUpgradeInfo release/targetVersions 映射 | — | ✅ |

## 五、错误码分类与 IAM 权限(对照 ErrorCodes/StatusCodes/IAMActions + SDK 参考)

| # | 位置 | 问题 | 文档依据 | 状态 |
|---|---|---|---|---|
| D1 | `errors.go` | IsQuotaExceeded 注释与实现不一致、漏 4 码(同 A10) | ErrorCodes.txt | ✅ 见 A10 |
| D2 | `errors.go` | **IsPermissionDenied 用 StatusCode==403 一刀切,把状态冲突码(01403003~06/09,"等待后重试")误判为权限错误 → 30 分钟误退避** | ErrorCodes.txt 各码处理措施均为"等待…后重试" | ✅ 按具体码判定(401 + 01403001/02/08);状态冲突码不再走长退避 |
| D3 | `errors.go`/`cce.go` | CCE_CM 族官方表未收录、未分类;0410 靠字符串硬编码 | ErrorCodes.txt 无 CM 码(实测所得) | ✅ 常量 + IsConflict(见 A11) |
| D4 | `errors.go` | 400 操作冲突码 01400023/24(扩容中建节点/建节点时删集群)未分类 | ErrorCodes.txt "operation conflict" | ✅ 新增常量并纳入 IsConflict |
| D5 | `docs/research-sources.md` | **clustercert 授权项记录错误**(记成别名 cce:cluster:get) | IAMActions 表7-14:授权项 `cce:cluster:generateClientCredential`,别名 `cce:cluster:get`,依赖 `-` | ✅ 更正;findings Q2/Q6 措辞统一(弱化"依赖"表述) |
| D6 | `docs/research-sources.md` | IAM action 新旧命名无映射;GetClusterQuota 授权项无官方条目 | IAMActions 表7-13/7-14 | ✅ 加新旧命名对照表;GetClusterQuota 授权项标注 [推断] |
| — | 一致项 | 其余全部常量与 ErrorCodes.txt 逐条一致;404/409/429 状态码分类与 StatusCodes.txt 语义一致;429 Retry-After 处理与 Q14 实测一致;SDK 构建模式(NewClient)为标准模式;findings 全部错误码引用与官方表相符 | — | ✅ |

## 六、未修项(记录在案,能力增强/取舍)

1. **升级前等待 PreCheck Success 再发 UpgradeCluster**(C3)— 建议 Service 增加"轮询 precheck 至 Success"环节。
2. **RetryUpgradeClusterTask / ShowUpgradeWorkFlow / UpgradeWorkFlowUpdate**(C4)— 失败自动重试(受控次数)、工作流状态跟踪(感知 Cancel)。
3. **UpgradeNodePool(同步节点池)**(B8)— 存量节点配置同步;当前用 update 的 taint/label refresh 部分替代。
4. **CreateClusterInput.Tags / CreateNodePoolInput.Tags**(A9)— 未映射 clusterTags/userTags,声明未用。
5. **phase 常量补齐 Init/Queuing/Running/Pause**(C7)— 避免 default 分支吞掉未来新增 phase。
6. **CRD 层约束补充** — taints 解析默认 effect=NoSchedule 与文档一致;k8sTags ≤20 未在 CRD 校验;nodePool 名称 1-63 小写校验可补。
7. **az/os/rootVolume 已 webhook 强校验**(B4)— CRD 注释保持 optional 但入口拒绝,如需 CRD required 可后续调整(会破坏向后兼容)。

## 七、审查方法(可复现)

```bash
# 1. 提取用到的 API 章节(PDF -> 文本)
python3 -m venv /tmp/pdfenv && /tmp/pdfenv/bin/pip install pypdf
# pypdf 按书签页码提取 30 个章节到 /tmp/cce-doc/*.txt(见上文各模块"文档依据"文件名)

# 2. 对照审查(4 路并行 subagent)
#    集群管理 / 节点池节点 / 集群升级 / 错误码+IAM,逐条 [文件:行号] + 文档原文 + 建议

# 3. 修复 + 全量验证
go build ./... && go vet ./... && go test ./...(含 envtest)
```
