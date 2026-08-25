# clusterctl 部署演练记录(kind + 真实 CCE 全链路验证)

> 演练时间:2026-08-19 · 管理集群:kind v0.27.0(k8s v1.32.2)· clusterctl v1.14.0
> 目的:验证 Provider 通过标准 `clusterctl init` 安装后,与 CAPI 核心控制器和真实华为云 CCE 的端到端集成。

## 一、演练结论(全部通过)

| 验证点 | 结果 |
|---|---|
| `clusterctl init --infrastructure cce` | ✅ 安装 cert-manager + CAPI 核心 + bootstrap/control-plane + infrastructure-cce v0.1.0 |
| CRD 安装 | ✅ `cceclusters` / `ccemanagedcontrolplanes` / `ccemanagedmachinepools` 就绪 |
| Webhook(mutating/validating 各 3 个) | ✅ 创建 workload Cluster 时全部调用成功(Default/Validate) |
| CCECluster 网络校验 | ✅ `NetworkReady=True(NetworkValidated)`,校验真实 VPC/子网引用 |
| CAPI 契约衔接 | ✅ `status.initialization.provisioned=true` → Cluster `InfrastructureProvisioned=true` |
| 控制面创建(真实 CCE) | ✅ 凭证解析 → CreateCluster → 集群 Available → endpoint 回填 → kubeconfig Secret 生成(`https://<内网IP>:5443`) |
| 幂等接管 | ✅ 限流边界下创建成功但响应丢失时,按名称接管已有集群(CCE_CM.0410 不再永久失败) |
| 429 退避 | ✅ APIGW.0308(写操作 10 次/分钟)触发后按 1 分钟退避重试,不产生 error 风暴 |
| 删除编排 | ✅ 删除 Cluster → 控制面删 CCE 集群(带 delete 选项)→ 删 kubeconfig Secret → finalizer 移除;云侧无残留 |

## 二、演练中发现并修复的真实问题(提交 f6cd1c6)

1. **webhook 服务地址指向 `system` 命名空间**:`config/webhook` 未纳入 kustomize,webhook 配置里的 `service.namespace: system` 占位符未被替换 → 创建对象时 webhook 调不通("service webhook-service not found")。
   **修复**:补 `config/webhook/kustomization.yaml` + `service.yaml`,并在 `config/default` 启用 `../webhook`,让 namespace transformer 生效。
2. **manager 无 webhook 证书卷**:`/tmp/k8s-webhook-server/serving-certs/tls.crt` 不存在 → manager 启动即崩溃。
   **修复**:`config/manager/manager.yaml` 挂载 `webhook-service-cert` Secret(证书由部署方预创建或 cert-manager 注入)。
3. **缺 leader election RBAC**:ServiceAccount 无 `coordination.k8s.io/leases` 权限 → controller 无法选主。
   **修复**:新增 `config/rbac/leader_election_role.yaml` + `leader_election_role_binding.yaml`;**注意 kustomize 不会改写 RoleBinding 的 subject.namespace**,必须直接写真实命名空间 `capi-cce-system`(kubebuilder 模板的 `system` 占位会失效)。
4. **CCECluster 缺 CAPI 契约字段**:CAPI Cluster controller 依据 infra cluster 的 **`status.initialization.provisioned`**(contract 路径)设置 `Cluster.Status.Initialization.InfrastructureProvisioned`,而非 Ready 条件。仅设 `status.ready`/Ready 条件不够。
   **修复**:`CCEClusterStatus` 增加 `Initialization.Provisioned`,controller 置位 + 补充 `Ready` 条件。
5. **CreateCluster 非幂等**:限流(APIGW.0308,写操作实测 10 次/分钟)边界下创建成功但响应丢失,重试报 `CCE_CM.0410` 网段冲突且永久失败。
   **修复**:创建冲突(已存在/CIDR 冲突)时按名称查回集群 ID 接管(幂等 adopt-by-name)。
6. **429 处理返回 (Result, err)**:controller-runtime 忽略 error 时的 RequeueAfter,退避由框架默认控制,限流窗口被快速重试打满。
   **修复**:`resultAfterError()` — 限流/配额错误返回延迟 requeue + nil error(见 `controllers/requeue.go`)。

## 三、演练环境与命令

```bash
# 1. 本地 provider 源(未发布时的 clusterctl 用法)
mkdir -p /tmp/cce/infrastructure-cce/v0.1.0
cp infrastructure-components.yaml metadata.yaml /tmp/cce/infrastructure-cce/v0.1.0/
# ~/.cluster-api/clusterctl.yaml:
# providers:
#   - name: "cce"
#     url: "file:///tmp/cce/infrastructure-cce/v0.1.0/infrastructure-components.yaml"
#     type: "InfrastructureProvider"

# 2. 管理集群 + 安装
kind create cluster --name capi-demo
clusterctl init --infrastructure cce --wait-providers

# 3. webhook 证书(未用 cert-manager 注入时的自签方案)
openssl genrsa -out ca.key 2048 && openssl req -x509 -new -key ca.key -sha256 -days 30 -subj "/CN=cce-ca" -out ca.crt
# ... 签发 server.crt(SAN 含 webhook-service.capi-cce-system.svc)...
kubectl -n capi-cce-system create secret tls webhook-service-cert --cert=server.crt --key=server.key
# components 中 6 个 webhook 的 clientConfig.caBundle = base64(ca.crt)

# 4. 工作负载集群(Standard/vpc-router 示例)
kubectl create secret generic my-cce-cluster-credentials \
  --from-literal=accessKey=$AK --from-literal=secretKey=$SK
kubectl apply -f <workload-cluster.yaml>
kubectl get ccemanagedcontrolplane my-cce-cluster-control-plane -o yaml  # 观察条件
```

## 四、验证时的真实平台数据(Q14 补充)

- **写操作限流**:`APIGW.0308 ... policy user over ratelimit, limit:10, time:1 minute` — 华为云对当前账号写类 API(如 CreateCluster)限流 **10 次/分钟**(与之前读操作 ~70 req/s 触发的测量互补)。
- 容器网段冲突 `CCE_CM.0410` 在同 VPC 内是硬约束(与第二/三/四轮冒烟一致)。

## 五、后续发布前的注意

- 正式发布时镜像名应为三段式规范名(`registry/org/repo:tag`),否则 clusterctl 的镜像 override 解析失败("repository name must be canonical")。
- 生产环境推荐 cert-manager 注入 webhook 证书(本演练用自签证书 + 预创建 Secret 方案)。
- `infrastructure-components.yaml` 由 `kubectl kustomize config/default` + caBundle 注入生成(演练脚本见上),发布流水线应固化为 Makefile target。

---

## 六、第二次全量演练(2026-08-20,kind + clusterctl v1.14.0)

> 目的:按 README 从头复现"小白部署路径",并顺带验证 phase-2 新增的 addons/pod-identity/logging/滚动更新能力。

### 演练结论

| 验证点 | 结果 |
|---|---|
| `clusterctl init --infrastructure cce`(kind v0.32 + clusterctl v1.14.0) | ✅ 安装 cert-manager v1.21.1 + CAPI v1.14.0 + bootstrap/control-plane + infrastructure-cce v0.1.0 |
| provider 控制器启动(leader election + 3 控制器) | ✅ 全部 Running |
| 真实 CCE 接管(adopt-by-name,余额不足无法新建) | ✅ 7 条件全 True:`CredentialsReady/CCEClusterReady/KubeconfigReady/AddonsConfigured/PodIdentityAssociationsConfigured/LoggingConfigured/UpgradeReady` |
| `clusterctl get kubeconfig` + CA 校验 | ✅ kubeconfig 结构正确(修复双编码后) |
| 新建计费集群 | ⛔ 被账户余额阻断(`CCE.01429004 Insufficient account balance`) |

### 本次发现并修复的真实问题

1. **owned tag key 非法(`CCE_CM.0004 "Tag's parameters is invalid"`)**:CAPA 风格的 owned tag key(`sigs.k8s.io/.../cluster/<name>`)含 `/`,而 CCE `ResourceTag` key 不允许 `/`(官方字符集 `_.:=+-@` 等,128 字符)。改为点分 `cluster-api-provider-cce.cluster.<name>`。
2. **kubeconfig CA 双编码**:CCE `CreateKubernetesClusterCert` 返回的 `certificate-authority-data`/`client-certificate-data`/`client-key-data` 是 **base64(PEM)**,原实现把 base64 字符串当原始字节再 base64 一次,导致 `kubectl` 报 "unable to parse bytes as PEM block"。修复:写回前先 base64 解码。
3. **MachinePool 缺 bootstrap 被拒**:CAPI v1.14 的 MachinePool webhook 强制要求 `spec.template.spec.bootstrap`(configRef 或 dataSecretName)。托管节点池无引导数据,用空 Secret + `dataSecretName` 满足合约(控制器对该字段不做校验,见 CAPI `reconcileBootstrap` line 239-244)。
4. **版本格式**:CCE 版本用 `v1.33`(无 patch),而 MachinePool 的 `version` 字段要求完整语义化版本 `v1.33.0`;样例模板 OS 字符串应为 `Huawei Cloud EulerOS 2.0`(带空格,实测验证),非 `HuaweiCloudEulerOS2.0`。
5. **kind 节点镜像拉取被本地代理阻断**:宿主 shell 的 `HTTP_PROXY=127.0.0.1:7890`(失效代理)被 kind 节点 containerd 继承,导致 quay.io 拉取失败。去代理变量重建集群即可。

### 对小白路径的产物

- 新增一键脚本 `scripts/deploy-kind.sh`(镜像构建 → kind → 证书/组件 → clusterctl init),README(中英)重写为"一条命令 + 少量 kubectl"。
