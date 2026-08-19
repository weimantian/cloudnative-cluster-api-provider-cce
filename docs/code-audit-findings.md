# 全量代码审计记录(2026-08-19)

> 审计方式:静态扫描(go vet / gofmt / 危险模式 grep)+ 4 路并行语义审计(controller 正确性 / 服务层与错误处理 / API·CRD·RBAC 一致性 / 安全与测试覆盖)。规模约 4600 行 Go。
> 修复提交:`22aabaa`(高危)、`bd390bc`(中危)、`6c7c584`(仓库卫生)。

## 一、汇总

| 严重度 | 已修 | 记录在案 |
|---|---|---|
| 高 | 6 | 0 |
| 中 | 10 | 4 |
| 低 | 3 | 12 |

## 二、已修复

### 高危(提交 22aabaa)
| # | 问题 | 位置 | 修复 |
|---|---|---|---|
| H1 | **端点类型契约不一致**:controller 匹配 `public`/`private`,但 CCE 返回 `Internal`/`External` → ControlPlaneEndpoint 永不回填 | `ccemanagedcontrolplane_controller.go` | 按 Internal/External 匹配 + 从 URL 解析 host/port |
| H2 | 带外删除 404 分支只改内存不清持久化 → 永不重建(死循环) | 同上 | 先 Status().Update 再返回 |
| H3 | 删除路径把 ShowCluster 任意瞬时错误当"已删除"→ finalizer 被移除、云集群孤儿化 | 同上 | 仅 IsNotFound 视为已删;DeleteCluster 404 幂等 |
| H4 | CreateNodePool 非幂等:409(限流丢失响应)永久卡死 | `cce.go` | adopt-by-name(ListNodePools) |
| H5 | 凭证 Secret 缺失静默回退全局 env(跨租户) | `scope/scope.go` | 显式引用缺失=报错,仅空引用才 env |
| H6 | 网络校验 fail-open:凭证错误跳过校验仍 MarkTrue | `ccecluster_controller.go` | 凭证失败 → MarkFalse + persist + requeue |

### 中危(提交 bd390bc)
| # | 问题 | 修复 |
|---|---|---|
| M1 | availableReplicas 用 NodeCount(含创建/删除中),replicas 用目标数 | replicas=currentNode、availableReplicas=activeNode;池缺失则重置重建 |
| M2 | billing.mode=1 CRD 合法但必然失败(period 字段未暴露) | webhook 显式拒绝 mode=1 |
| M3 | StartUpgrade 吞掉 ShowClusterUpgradeInfo 错误 | 查询失败直接返回 |
| M4 | 升级 Success 用陈旧 info.Version 再次触发升级 | Success 后 persist+requeue(下轮用新版本) |
| M5 | 升级 Failed 永久死锁(保留 taskID 无清除路径) | Failed 清 taskID,下轮重评(声明式重试) |
| M6 | 容器网段未校验与 VPC/子网重叠 | validator 补 4b 检查 |
| M7 | ScaleNodePool 负值未校验 | 负值报错 |
| M8 | RBAC 注释缺口(ccecluster 读 cp、machinepool 读 ccecluster) | 补注释 + 重新生成 manifests |
| M9 | 升级任务 404 后死循环 | 服务层返回"任务不存在"语义 |
| M10 | CreateNodePool 空 Flavor/0 盘直接发送 | fast-fail |

### 低危(提交 6c7c584)
| # | 问题 | 修复 |
|---|---|---|
| L1 | 12MB Mach-O 二进制 `smoke-setup` 被提交 | 移除 + .gitignore |
| L2 | (随 M9)升级任务 404 | 见 M9 |
| L3 | (随 M10)CreateNodePool 校验 | 见 M10 |

## 三、记录在案(未修,待排期)

### 中危
1. **错误路径 conditions 不持久化(系统性)**:三个 controller 的多数 `MarkFalse` 失败分支只改内存、返回前未 `Status().Update`,失败原因对用户与 CAPI 不可见(违反 v1beta2 conditions 契约)。已修:CECCluster 网络校验、CP ShowCluster/凭证路径;剩余 ~20 处(MachinePool 凭证/scale/attrs 失败、CP 凭证/kubeconfig 失败等)。建议:引入统一 patch helper 或逐分支补齐。
2. **kubeconfig Secret 无 ownerRef**:同名 Secret 会被整份覆盖;CP 状态丢失时删除路径找不到它(孤儿)。建议 SetControllerReference + 归属校验。
3. **MachinePool.spec.replicas 未从拥有者同步**:声明 `machinepools get;list;watch` 却从不 Get,`kubectl scale machinepool` 不生效。建议 reconcile 中 Get 拥有者同步 replicas。
4. **CreateCluster adopt-by-name 无所有权/状态校验**:冲突后按名收养,不校验 phase/VPC 归属,可能收养异参集群。建议 ShowCluster 校验 phase + 关键参数。

### 设计取舍(审计建议但有意保留)
- **containerNetwork.mode 默认 eni**:审计建议改 overlay_l2(避免"只填 clusterName 默认即失败"),但架构定位为 Turbo+eni 默认(与 EKS 托管模式对齐),保留现状;用户需显式给 mode/category。

### 低危
- `cmd/main.go` zap Development:true(生产应 false)
- metrics :8080 无鉴权、缺 seccompProfile/runAsUser
- 死字段:`Spec.ProjectID`、`AdditionalTags`、`FailureReason/FailureMessage`、`CCECluster.Status.ControlPlaneEndpoint`、scope 的 ClusterScope/PatchHelper 死代码、Tags(服务层声明未用)
- ValidateUpdate 无不可变字段保护(cidr/eniSubnets/category/flavor/nodePoolName 变更静默忽略)
- DataVolumes 仅取 `[0]`
- 类型 `+optional` 与 webhook 必填矛盾(os/az/rootVolume 注释未同步)
- parseTaints 静默纠错(非法 key/effect 不报错)、effect 含冒号时截断
- AgencyName 空串发送
- ScaleNodePool 的 ScaleGroups 硬编码 "default"
- kubeconfig 轮换依赖 10h resync、无 Secret watch
- secret-scan 对华为云 AK/SK 覆盖有限(gitleaks 默认规则抓不到,建议自定义 toml)
- go.mod 含 mongo-driver indirect CVE(go mod tidy 移除)

## 四、测试覆盖缺口(未修,待补)

- **scope 层零测试**(ResolveCredentials 正是 H5 风险点)
- **错误分类零测试**(errors.go 的 Is* 函数)
- **assembleKubeconfig 零测试**
- MachinePool reconcileDelete 全路径无测试
- kubeconfig 轮换的"需刷新"分支未实测
- CP 的凭证失败/kubeconfig 轮换/endpoint 优先级无测试
- webhook(CCECluster/CCEManagedControlPlane)校验无测试
- e2e 为 t.Skip 占位

## 五、审计方法(可复现)

```bash
go vet ./... && gofmt -l cmd api controllers internal
grep -rn "panic(\|TODO\|FIXME\|log.*AK\|_ =" cmd api controllers internal --include="*.go" | grep -v _test
# 4 路并行 subagent:controller 正确性 / 服务层 / API·CRD·RBAC / 安全·测试
```
