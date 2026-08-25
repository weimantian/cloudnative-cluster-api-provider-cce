# 云上管云上部署指导

> 在华为云 CCE 管理集群上安装 CCE Provider,通过 Cluster API 声明式管理华为云 CCE 工作负载集群的完整生命周期(create → scale → delete)。

## 架构概述

```
┌─────────────────────────────────────────────────────────┐
│  华为云 cn-north-4                                       │
│                                                         │
│  ┌───────────────────────────┐    ┌───────────────────┐ │
│  │  管理集群 (CCE Standard)   │    │  工作负载集群      │ │
│  │  capi-mgmt-<timestamp>    │    │  (CCE, 私有 API)  │ │
│  │                           │    │                   │ │
│  │  · CAPI core              │    │  · 控制面(华为托管) │ │
│  │  · CCE Provider (本仓库)  │───>│  · 节点池          │ │
│  │  · cert-manager           │    │                   │ │
│  └─────────────┬─────────────┘    └───────────────────┘ │
│                │                                       │
│                │ NAT 网关 (出网拉镜像)                   │
│                ▼                                       │
│         quay.io / registry.k8s.io                      │
│         swr.cn-north-4 (Provider 镜像)                  │
└─────────────────────────────────────────────────────────┘
```

**核心要点**:管理集群的 CCE 节点需要出网能力(NAT 网关)来拉取 `quay.io`/`registry.k8s.io` 的镜像;Provider 自定义镜像需推送到 SWR 私有仓库,通过 imagePullSecret 拉取。

---

## 前置条件

### 本地工具

```bash
# macOS
brew install docker kubectl go

# clusterctl v1.14.0 (必须精确匹配)
curl -L https://github.com/kubernetes-sigs/cluster-api/releases/download/v1.14.0/clusterctl-$(uname -s | tr '[:upper:]' '[:lower:]')-$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/') -o clusterctl
chmod +x clusterctl && sudo mv clusterctl /usr/local/bin/
clusterctl version  # 应输出 v1.14.0
```

### 华为云资源

| 项目 | 要求 |
|---|---|
| IAM AK/SK | 有 CCE/VPC/EIP/NAT/ECS 操作权限 |
| 账户余额 | 充足(CCE 集群 + 节点按需计费) |
| VPC + 子网 | 已存在,或用 `hack/deploy-network` 一键创建 |
| SSH 密钥对 | 已存在,或用 `hack/deploy-network` 创建 |
| SWR 仓库 | 需要一个组织/命名空间(如 `capi_cce`) |

### 代理剥离(重要)

所有 `go`/`kubectl`/`clusterctl`/`docker` 命令都需要剥离本地代理环境变量,否则会导致各种连接失败:

```bash
# 在每个命令前加,或设为 alias
alias nocloud='env -u http_proxy -u https_proxy -u HTTP_PROXY -u HTTPS_PROXY -u SOCKS_PROXY -u all_proxy -u ALL_PROXY'
```

---

## 步骤总览

| 步骤 | 操作 | 耗时 |
|---|---|---|
| 1 | 准备网络与密钥 | 2 min |
| 2 | 创建管理集群 | 10-15 min |
| 3 | 配置 NAT 出网 | 2 min |
| 4 | 构建并推送 Provider 镜像到 SWR | 5 min |
| 5 | 生成 infrastructure-components.yaml | 1 min |
| 6 | 注册 clusterctl | 1 min |
| 7 | 安装 Provider (clusterctl init) | 3 min |
| 8 | 创建 Secrets | 2 min |
| 9 | (可选) 搬运核心 CAPI 镜像到 SWR | 10 min |
| 10 | 验证 Provider 运行 | 1 min |
| 11 | 创建工作负载集群 | 10-20 min |
| 12 | 自动化 E2E 测试 | 20-30 min |
| 清理 | 删除所有资源 | 10 min |

---

## 步骤 1:准备网络与密钥

### 方式 A:使用已有 VPC/子网/密钥对

从华为云控制台获取 VPC ID、子网 ID、密钥对名称。

### 方式 B:一键创建(推荐首次测试)

```bash
nocloud CLOUD_SDK_AK=<AK> CLOUD_SDK_SK=<SK> CCE_SMOKE_REGION=cn-north-4 \
  go run ./hack/deploy-network
```

输出示例:
```
VPC: capi-smoke-vpc (9c4c6207-...)
Node subnet: capi-smoke-subnet-node (..., id=..., neutron=...)
ENI  subnet: capi-smoke-subnet-eni  (..., id=..., neutron=...)
Keypair: capi-smoke-key (created)

--- export for scripts/smoke-cce.sh ---
export CCE_SMOKE_REGION="cn-north-4"
export CCE_SMOKE_VPC="9c4c6207-..."
export CCE_SMOKE_SUBNET="..."
export CCE_SMOKE_ENI_SUBNET="..."  # neutron_subnet_id
export CCE_SMOKE_KEYPAIR="capi-smoke-key"
export CCE_SMOKE_CASES='cluster,pool,scale,delete'
```

### 统一环境变量

```bash
export CCE_SMOKE_AK=<你的AK>
export CCE_SMOKE_SK=<你的SK>
export CCE_SMOKE_REGION=cn-north-4
export CCE_SMOKE_VPC=<VPC-ID>
export CCE_SMOKE_SUBNET=<子网-ID>
export CCE_SMOKE_KEYPAIR=<密钥对名称>
export CCE_SMOKE_AZ=cn-north-4a
```

---

## 步骤 2:创建管理集群

```bash
nocloud go run ./hack/deploy-mgmt-cluster
```

**做什么**:
- 创建 CCE Standard 集群 `capi-mgmt-<timestamp>`(flavor `cce.s1.small`,vpc-router 模式,公开 API)
- 创建节点池 `mgmt-pool-0`(默认 2 节点,`c6.large.2`,Huawei Cloud EulerOS 2.0)
- 等待集群 Available + 节点 Active(超时 30 min)
- 下载 kubeconfig 到 `capi-mgmt.kubeconfig`

**输出示例**:
```
creating management cluster "capi-mgmt-98282" ...
cluster created: 2f0c3941-9d34-11f1-9003-0255ac10024c
  phase=Creating (want Available)
  ...
cluster Available: version=v1.36
  endpoint type=External url=https://120.46.211.3:5443
node pool created: ... (flavor c6.large.2, nodes 2)
  activeNodes=2 (want 2)
kubeconfig written to capi-mgmt.kubeconfig (12345 bytes)

MGMT_CLUSTER_ID=2f0c3941-9d34-11f1-9003-0255ac10024c
MGMT_POOL_ID=...
```

**验证**:
```bash
nocloud kubectl --kubeconfig capi-mgmt.kubeconfig get nodes
# 应看到 2 个 Ready 节点
```

---

## 步骤 3:配置 NAT 出网

管理集群节点需要出网能力来拉取 `quay.io`(cert-manager)和 `registry.k8s.io`(CAPI core)镜像。

```bash
nocloud go run ./hack/nat-egress -mode create
```

**做什么**:
- 创建 EIP `capi-egress-eip`(5 Mbit/s bandwidth)
- 创建 NAT 网关 `capi-egress-nat`(规格 spec=1,小型)
- 创建 SNAT 规则,将节点子网映射到 EIP
- 幂等:已存在则复用

**输出示例**:
```
created EIP id=7a8c4ce4-... addr=114.116.228.222
created NAT gateway id=48c7bfc0-... - waiting for ACTIVE…
NAT gateway id=48c7bfc0-... ACTIVE
created SNAT rule id=0873fdf8-... subnet=2aa8f43c-... eip=114.116.228.222
---- summary ----
NAT_GATEWAY_ID=48c7bfc0-...
EIP_ID=7a8c4ce4-...
EIP_ADDRESS=114.116.228.222
SUBNET_ID=2aa8f43c-...
```

**验证**:
```bash
nocloud go run ./hack/nat-egress -mode list
# 应看到 gateway status=ACTIVE + snat-rule status=ACTIVE
```

> **注意**:NAT 网关和 EIP 会持续计费。测试完成后记得清理(见清理章节)。

---

## 步骤 4:构建并推送 Provider 镜像到 SWR

### 4.1 获取 SWR 登录凭据

华为云控制台 → 容器镜像服务 SWR → `capi_cce` 命名空间 → 右上角"登录指令"。

### 4.2 登录 SWR

```bash
docker login swr.cn-north-4.myhuaweicloud.com \
  -u 'cn-north-4@<USER_ID>' -p '<TOKEN>'
```

### 4.3 构建镜像

```bash
# Makefile 默认 IMG = swr.cn-north-4.myhuaweicloud.com/$(IMAGE_ORG)/cce-provider-controller:latest
nocloud IMAGE_ORG=capi_cce make docker-build
```

### 4.4 推送到 SWR

```bash
nocloud IMAGE_ORG=capi_cce make docker-push
```

**验证**:
```bash
nocloud docker manifest inspect swr.cn-north-4.myhuaweicloud.com/capi_cce/cce-provider-controller:latest
# 应输出 manifest JSON,无报错
```

---

## 步骤 5:生成 infrastructure-components.yaml

需要:(1) 把镜像名覆盖为 SWR 地址;(2) 生成 webhook 自签证书并注入 caBundle。

```bash
ARTIFACTS=_artifacts/cceswr
mkdir -p "$ARTIFACTS"

# 5.1 Kustomize 配置(覆盖镜像为 SWR 地址)
cat > "$ARTIFACTS/kustomization.yaml" <<'EOF'
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - ../../config/default
images:
  - name: swr.cn-north-4.myhuaweicloud.com/cce-provider/controller
    newName: swr.cn-north-4.myhuaweicloud.com/capi_cce/cce-provider-controller
    newTag: latest
EOF

nocloud kubectl kustomize "$ARTIFACTS" > "$ARTIFACTS/infrastructure-components-raw.yaml"

# 5.2 生成自签 CA + webhook 服务器证书
cd "$ARTIFACTS"
openssl genrsa -out ca.key 2048 2>/dev/null
openssl req -x509 -new -nodes -key ca.key -sha256 -days 365 \
  -subj "/CN=cce-provider-ca" -out ca.crt 2>/dev/null
openssl genrsa -out server.key 2048 2>/dev/null
cat > server.conf <<'EOF'
[req]
distinguished_name = dn
req_extensions = ext
prompt = no
[dn]
CN = webhook-service.cce-provider-system.svc
[ext]
subjectAltName = @alt_names
[alt_names]
DNS.1 = webhook-service
DNS.2 = webhook-service.cce-provider-system
DNS.3 = webhook-service.cce-provider-system.svc
DNS.4 = webhook-service.cce-provider-system.svc.cluster.local
EOF
openssl req -new -key server.key -out server.csr -config server.conf 2>/dev/null
openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out server.crt -days 365 -sha256 -extfile server.conf 2>/dev/null
cd ../..

# 5.3 把 CA bundle 注入所有 webhook 的 clientConfig
CABUNDLE="$(base64 < "$ARTIFACTS/ca.crt" | tr -d '\n')"
awk -v cab="$CABUNDLE" '
  { print }
  $0 == "  clientConfig:" { print "    caBundle: " cab }
' "$ARTIFACTS/infrastructure-components-raw.yaml" > "$ARTIFACTS/infrastructure-components.yaml"

echo "已注入 caBundle 到 $(grep -c caBundle "$ARTIFACTS/infrastructure-components.yaml") 个 webhook"
```

---

## 步骤 6:注册 clusterctl

```bash
CCE_PROVIDER_VERSION=v0.1.0
CLUSTERCTL_SRC="/tmp/cce/infrastructure-cce/${CCE_PROVIDER_VERSION}"
mkdir -p "$CLUSTERCTL_SRC" "$HOME/.cluster-api"

cp "$ARTIFACTS/infrastructure-components.yaml" metadata.yaml "$CLUSTERCTL_SRC/"

cat > "$HOME/.cluster-api/clusterctl.yaml" <<EOF
providers:
  - name: "cce"
    url: "file://$CLUSTERCTL_SRC/infrastructure-components.yaml"
    type: "InfrastructureProvider"
EOF
```

---

## 步骤 7:安装 Provider

```bash
export KUBECONFIG=$(pwd)/capi-mgmt.kubeconfig

# 不要加 --wait-providers:webhook 证书 Secret 还没创建,provider pod 会卡在
# ContainerCreating,--wait-providers 会一直等到超时
nocloud clusterctl init --infrastructure cce
```

**做什么**:安装 cert-manager + CAPI core + kubeadm-bootstrap + kubeadm-control-plane + cce provider。

**此时状态**:
- cert-manager pods:从 quay.io 拉镜像(经 NAT),应该能 Running
- CAPI core pods:从 registry.k8s.io 拉镜像(经 NAT),应该能 Running
- **CCE Provider pod:卡在 ContainerCreating** — 因为 SWR 是私有仓库,缺 imagePullSecret;且 webhook-service-cert Secret 不存在

```bash
nocloud kubectl get pods -A
# cert-manager-* : Running
# capi-controller-manager-* : Running
# capi-kubeadm-*-controller-manager-* : Running
# cce-provider-controller-manager-* : ContainerCreating ← 正常,下一步修复
```

---

## 步骤 8:创建 Secrets

### 8.1 SWR 拉镜像 Secret(cce-provider-system)

```bash
SWR_USER='cn-north-4@<USER_ID>'
SWR_TOKEN='<TOKEN>'

nocloud kubectl create secret docker-registry cce-provider-swr-secret \
  --namespace cce-provider-system \
  --docker-server=swr.cn-north-4.myhuaweicloud.com \
  --docker-username="$SWR_USER" \
  --docker-password="$SWR_TOKEN" \
  --docker-email='noreply@huawei.cloud'
```

### 8.2 Webhook TLS 证书

```bash
nocloud kubectl -n cce-provider-system create secret tls webhook-service-cert \
  --cert="$ARTIFACTS/server.crt" --key="$ARTIFACTS/server.key"
```

### 8.3 CCE 凭据 Secret

```bash
nocloud kubectl -n cce-provider-system create secret generic cce-provider-credentials \
  --from-literal=accessKey="$CCE_SMOKE_AK" \
  --from-literal=secretKey="$CCE_SMOKE_SK"
```

### 8.4 给 Provider Deployment 添加 imagePullSecrets 并重启

```bash
nocloud kubectl -n cce-provider-system patch deployment cce-provider-controller-manager \
  --type='json' \
  -p='[{"op":"add","path":"/spec/template/spec/imagePullSecrets","value":[{"name":"cce-provider-swr-secret"}]}]'

nocloud kubectl -n cce-provider-system rollout restart deployment/cce-provider-controller-manager
```

---

## 步骤 9:(可选)搬运核心 CAPI 镜像到 SWR

> 如果 NAT 出网稳定,可以跳过此步。此步将核心 CAPI 镜像也搬到 SWR,消除对公网 registry 的依赖。

```bash
# 9.1 搬运三个核心 CAPI 镜像
for img in \
  registry.k8s.io/cluster-api/cluster-api-controller:v1.14.0 \
  registry.k8s.io/cluster-api/kubeadm-bootstrap-controller:v1.14.0 \
  registry.k8s.io/cluster-api/kubeadm-control-plane-controller:v1.14.0; do
  nocloud docker pull "$img"
  dst="swr.cn-north-4.myhuaweicloud.com/capi_cce/$(basename "$img")"
  nocloud docker tag "$img" "$dst"
  nocloud docker push "$dst"
done

# 9.2 在 CAPI 命名空间创建 SWR pull secret
for ns in capi-system capi-kubeadm-bootstrap-system capi-kubeadm-control-plane-system; do
  nocloud kubectl create secret docker-registry cce-provider-swr-secret \
    --namespace "$ns" \
    --docker-server=swr.cn-north-4.myhuaweicloud.com \
    --docker-username="$SWR_USER" \
    --docker-password="$SWR_TOKEN" \
    --docker-email='noreply@huawei.cloud'
done

# 9.3 打补丁三个 CAPI Deployment:换 SWR 镜像 + 加 imagePullSecrets
nocloud kubectl -n capi-system patch deployment capi-controller-manager \
  --type='json' -p='[
    {"op":"replace","path":"/spec/template/spec/containers/0/image","value":"swr.cn-north-4.myhuaweicloud.com/capi_cce/cluster-api-controller:v1.14.0"},
    {"op":"add","path":"/spec/template/spec/imagePullSecrets","value":[{"name":"cce-provider-swr-secret"}]}
  ]'

nocloud kubectl -n capi-kubeadm-bootstrap-system patch deployment capi-kubeadm-bootstrap-controller-manager \
  --type='json' -p='[
    {"op":"replace","path":"/spec/template/spec/containers/0/image","value":"swr.cn-north-4.myhuaweicloud.com/capi_cce/kubeadm-bootstrap-controller:v1.14.0"},
    {"op":"add","path":"/spec/template/spec/imagePullSecrets","value":[{"name":"cce-provider-swr-secret"}]}
  ]'

nocloud kubectl -n capi-kubeadm-control-plane-system patch deployment capi-kubeadm-control-plane-controller-manager \
  --type='json' -p='[
    {"op":"replace","path":"/spec/template/spec/containers/0/image","value":"swr.cn-north-4.myhuaweicloud.com/capi_cce/kubeadm-control-plane-controller:v1.14.0"},
    {"op":"add","path":"/spec/template/spec/imagePullSecrets","value":[{"name":"cce-provider-swr-secret"}]}
  ]'
```

---

## 步骤 10:验证 Provider 运行

```bash
nocloud kubectl get pods -A | grep -E 'capi-|cert-manager|cce-provider'
```

**期望输出**(全部 `1/1 Running`):
```
capi-kubeadm-bootstrap-system        capi-kubeadm-bootstrap-controller-manager-xxx    1/1 Running
capi-kubeadm-control-plane-system    capi-kubeadm-control-plane-controller-manager-xxx 1/1 Running
capi-system                          capi-controller-manager-xxx                      1/1 Running
cce-provider-system                  cce-provider-controller-manager-xxx              1/1 Running
cert-manager                         cert-manager-xxx                                 1/1 Running
cert-manager                         cert-manager-cainjector-xxx                      1/1 Running
cert-manager                         cert-manager-webhook-xxx                         1/1 Running
```

**检查 Provider 日志**:
```bash
nocloud kubectl -n cce-provider-system logs deploy/cce-provider-controller-manager --tail=30
```

应看到:
```
Starting Controller
Starting workers
Started Controller (CCECluster, CCEManagedControlPlane, CCEManagedMachinePool ...)
```

**检查 CRD**:
```bash
nocloud kubectl get crd | grep cce
# cceclusters.infrastructure.cluster.x-k8s.io
# ccemanagedcontrolplanes.controlplane.cluster.x-k8s.io
# ccemanagedmachinepools.infrastructure.cluster.x-k8s.io
```

---

## 步骤 11:创建工作负载集群(手动)

### 11.1 准备集群模板

```bash
cp config/samples/cluster-template.yaml /tmp/my-cluster.yaml
```

编辑 `/tmp/my-cluster.yaml`,替换三个占位符:
- `VERIFY-VPC-ID` → 你的 VPC ID(`$CCE_SMOKE_VPC`)
- `VERIFY-SUBNET-ID` → 你的子网 ID(`$CCE_SMOKE_SUBNET`)
- `VERIFY-KEYPAIR-NAME` → 你的密钥对名称(`$CCE_SMOKE_KEYPAIR`)

```bash
sed -i.bak \
  -e "s/VERIFY-VPC-ID/$CCE_SMOKE_VPC/" \
  -e "s/VERIFY-SUBNET-ID/$CCE_SMOKE_SUBNET/" \
  -e "s/VERIFY-KEYPAIR-NAME/$CCE_SMOKE_KEYPAIR/" \
  /tmp/my-cluster.yaml
```

### 11.2 创建凭据 Secret

```bash
nocloud kubectl create secret generic my-cce-cluster-credentials \
  --namespace default \
  --from-literal=accessKey="$CCE_SMOKE_AK" \
  --from-literal=secretKey="$CCE_SMOKE_SK"

# CAPI v1.14 MachinePool 合约要求 bootstrap 引用存在(管理节点池无需 bootstrap 数据)
nocloud kubectl create secret generic my-cce-cluster-bootstrap \
  --namespace default --from-literal=value=""
```

### 11.3 应用集群模板

```bash
nocloud kubectl apply -f /tmp/my-cluster.yaml
```

### 11.4 观察创建过程

```bash
# 控制面状态
nocloud kubectl get ccemanagedcontrolplane my-cce-cluster-control-plane -w
# 等待 Ready=True(约 5-15 分钟)

# 节点池状态
nocloud kubectl get ccemanagedmachinepool my-cce-cluster-pool-0 -w
# 等待 Ready=True, Replicas=1

# 整体集群状态
nocloud clusterctl describe cluster my-cce-cluster
```

### 11.5 获取工作负载集群 kubeconfig

```bash
nocloud clusterctl get kubeconfig my-cce-cluster > my-cce-cluster.kubeconfig
```

> **注意**:工作负载集群默认 `endpointAccess.public: false`,kubeconfig 中的 server 地址是 VPC 内网 IP,只能从 VPC 内部访问(如管理集群节点上)。

### 11.6 删除工作负载集群

```bash
nocloud kubectl delete cluster my-cce-cluster
# 异步删除:节点池 → CCE 集群 → kubeconfig Secret → finalizers
```

---

## 步骤 12:自动化 E2E 测试

### 方式 A:Provider 驱动 E2E(推荐,测试完整 Provider 链路)

```bash
export E2E_MANAGEMENT_KUBECONFIG=$(pwd)/capi-mgmt.kubeconfig
export CCE_ACCESS_KEY="$CCE_SMOKE_AK"
export CCE_SECRET_KEY="$CCE_SMOKE_SK"
export CCE_E2E_VPC_ID="$CCE_SMOKE_VPC"
export CCE_E2E_SUBNET_ID="$CCE_SMOKE_SUBNET"
export CCE_E2E_KEYPAIR="$CCE_SMOKE_KEYPAIR"
export CCE_E2E_REGION=cn-north-4
export CCE_E2E_AZ=cn-north-4a
export CCE_E2E_NODE_FLAVOR=c6.large.2

nocloud make e2e
# 或:nocloud go test ./test/e2e/... -timeout 60m -v
```

**测试流程**(`test/e2e/e2e_test.go`):
1. 创建凭据 Secret + bootstrap Secret
2. 创建 Cluster + CCECluster + CCEManagedControlPlane + MachinePool + CCEManagedMachinePool
3. 等待控制面 Ready(超时 25 min)
4. 等待节点池 Ready(超时 25 min)
5. 删除 Cluster(超时 25 min)
6. 等待 Cluster 完全消失

> 注意:E2E 测试不包含 scale 用例。

### 方式 B:SDK 层 Smoke 测试(含 scale,不走 Provider)

```bash
# 需要 ENI 子网的 neutron_subnet_id
export CCE_SMOKE_ENI_SUBNET=<neutron_subnet_id>
export CCE_SMOKE_CASES=cluster,pool,scale,delete
export CCE_SMOKE_MODE=eni

nocloud scripts/smoke-cce.sh
# 等价于:nocloud go test -tags smoke -v -timeout 90m ./internal/services/cce/ -run TestSmoke
```

**测试用例**:
- `cluster`:创建 CCE 集群 → Available
- `pool`:创建节点池 → Active
- `scale`:扩容节点池 1→2 → Active
- `delete`:删除集群 → gone

---

## 清理

### 删除工作负载集群

```bash
nocloud kubectl --kubeconfig capi-mgmt.kubeconfig delete cluster --all
# 或针对单个:kubectl delete cluster my-cce-cluster
```

### 从管理集群卸载 Provider

```bash
nocloud clusterctl --kubeconfig capi-mgmt.kubeconfig delete --infrastructure cce
```

### 删除管理集群

```bash
nocloud go run ./hack/deploy-mgmt-cluster -delete -cluster <MGMT_CLUSTER_ID>

# 或删除区域内所有集群(危险!):
# nocloud go run ./hack/deploy-mgmt-cluster -delete-all
```

### 删除 NAT 出网资源

```bash
# 删除所有 capi-egress-* NAT 网关 + SNAT 规则 + EIP
nocloud go run ./hack/nat-egress -mode delete-all

# 或删除指定网关:
# nocloud go run ./hack/nat-egress -mode delete -id <NAT_GATEWAY_ID>
```

### 删除孤儿 EIP(如有)

```bash
# 列出所有 EIP
nocloud go run ./hack/survey-hw

# 删除指定 EIP
nocloud go run ./hack/nat-egress -mode delete-eip -id <EIP_ID>
```

### 完整盘点

```bash
# 列出所有华为云资源(CCE 集群、EIP、VPC、子网、密钥对)
nocloud go run ./hack/survey-hw
```

---

## 故障排查

### Provider pod 卡在 ContainerCreating

```bash
nocloud kubectl -n cce-provider-system describe pod -l control-plane=controller-manager
```

常见原因:
- `secret cce-provider-swr-secret not found` → 步骤 8.1 未执行
- `secret webhook-service-cert not found` → 步骤 8.2 未执行
- `ImagePullBackOff` → SWR 凭据错误,检查 docker login + Secret

### cert-manager / CAPI pod ImagePullBackOff

NAT 出网未配置或失效。检查:
```bash
nocloud go run ./hack/nat-egress -mode list
# 确认 NAT 网关 ACTIVE + SNAT 规则 ACTIVE
```

或执行步骤 9 搬运镜像到 SWR。

### `clusterctl init` 报 "repository name must be canonical"

`infrastructure-components.yaml` 中的 manager 镜像名不是三段式 `registry/org/repo:tag`。确保步骤 5 的 kustomize images: 覆盖正确。

### Webhook 报 "failed to call webhook" / TLS 错误

webhook caBundle 未正确注入,或 webhook-service-cert Secret 证书过期。重新执行步骤 5.2-5.3 和步骤 8.2。

### 集群创建失败 `CCE.01429004 Insufficient account balance`

华为云账户余额不足,充值后重试。

### 集群创建失败 `APIGW.0308` (429 throttling)

华为云写 API 限流(约 10 次/分钟)。Controller 会自动退避重试,等待即可。

### 节点池创建失败 `OS: should not be empty`

CCE 要求显式指定 OS。模板中已设 `os: Huawei Cloud EulerOS 2.0`。其他可选值:
- `EulerOS release 2.9`
- `Ubuntu 22.04`
- `Huawei Cloud EulerOS 1.1`

### 节点池创建失败 `Data volume needed for non-local-disk flavor`

非本地盘机型(如 `c6.large.2`)必须配置 dataVolume。模板已包含 `dataVolumes: [{size: 100, type: GPSSD}]`。

### `clusterctl get kubeconfig` 返回不可达地址

工作负载集群默认 `endpointAccess.public: false`,kubeconfig server 是 VPC 内网 IP。从 VPC 内部访问(如管理集群节点上 kubectl)。

### 代理导致各种连接失败

所有命令必须剥离代理环境变量。确认 `nocloud` alias 生效,或手动在命令前加 `env -u http_proxy -u https_proxy ...`。

---

## 环境变量速查

| 变量 | 用途 | 示例值 |
|---|---|---|
| `CCE_SMOKE_AK` | 华为云 AK | `HPUANX...` |
| `CCE_SMOKE_SK` | 华为云 SK | `doPPU6...` |
| `CCE_SMOKE_REGION` | 区域 | `cn-north-4` |
| `CCE_SMOKE_VPC` | VPC ID | `cb3f7bfb-...` |
| `CCE_SMOKE_SUBNET` | 节点子网 ID | `2aa8f43c-...` |
| `CCE_SMOKE_ENI_SUBNET` | ENI 子网 neutron ID (smoke 测试) | `16e607b7-...` |
| `CCE_SMOKE_KEYPAIR` | SSH 密钥对名 | `capi-smoke` |
| `CCE_SMOKE_AZ` | 可用区 | `cn-north-4a` |
| `CCE_SMOKE_CASES` | smoke 测试用例 | `cluster,pool,scale,delete` |
| `E2E_MANAGEMENT_KUBECONFIG` | 管理集群 kubeconfig 路径 | `./capi-mgmt.kubeconfig` |
| `CCE_ACCESS_KEY` | E2E 测试用 AK | 同 `CCE_SMOKE_AK` |
| `CCE_SECRET_KEY` | E2E 测试用 SK | 同 `CCE_SMOKE_SK` |
| `CCE_E2E_VPC_ID` | E2E 测试用 VPC | 同 `CCE_SMOKE_VPC` |
| `CCE_E2E_SUBNET_ID` | E2E 测试用子网 | 同 `CCE_SMOKE_SUBNET` |
| `CCE_E2E_KEYPAIR` | E2E 测试用密钥对 | 同 `CCE_SMOKE_KEYPAIR` |
| `KUBECONFIG` | 管理集群 kubeconfig | `./capi-mgmt.kubeconfig` |

---

## 工具速查

| 工具 | 位置 | 用途 |
|---|---|---|
| `hack/deploy-network` | `hack/deploy-network/main.go` | 一键创建 VPC/子网/密钥对 |
| `hack/deploy-mgmt-cluster` | `hack/deploy-mgmt-cluster/main.go` | 创建/列出/删除管理集群 |
| `hack/nat-egress` | `hack/nat-egress/main.go` | NAT 网关 create/list/delete/delete-all |
| `hack/survey-hw` | `hack/survey-hw/main.go` | 盘点所有华为云资源 |
| `hack/cleanup-hw` | `hack/cleanup-hw/main.go` | 按指定 ID 删除集群+EIP+子网+VPC |
| `hack/cleanup-smoke-clusters` | `hack/cleanup-smoke-clusters/main.go` | 删除所有 `capi-*` 前缀集群 |
| `scripts/smoke-cce.sh` | `scripts/smoke-cce.sh` | SDK 层 smoke 测试驱动 |
| `scripts/deploy-kind.sh` | `scripts/deploy-kind.sh` | 本地 kind 部署(非云上管云上) |
