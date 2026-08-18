# app — 示例应用代码

本目录预留给**示例应用**,用于演示如何使用 `cloudnative-cluster-api-provider-cce`(例如:验证已创建 CCE 集群的示例工作负载)。

> English version: [README.md](README.md)

## 规范(依据华为云解决方案开发者套件治理规范 §4.6)

- **依赖清单**:示例代码必须包含锁定版本的依赖清单(如 `requirements.txt`、`pom.xml`、`go.mod`),确保能在华为云标准运行时环境直接执行。
- **环境变量配置**:所有连接信息与密钥必须从环境变量读取——与 `deploy/variables.md` 保持一致;严禁硬编码凭证。
- **代码注释**:关键逻辑需有中/英文注释,函数/类必须有文档字符串。

## 状态

当前为空——示例应用将在后续里程碑添加(路线图见 [docs/requirements-design.md](../docs/requirements-design.md))。
