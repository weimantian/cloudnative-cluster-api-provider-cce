# 为 cloudnative-cluster-api-provider-cce 做贡献

感谢您有意参与贡献!本仓库遵循[华为云解决方案开发者套件治理规范](https://developer.huaweicloud.com/)与 [Cluster API](https://cluster-api.sigs.k8s.io/) 社区约定。

> English version: [CONTRIBUTING.md](CONTRIBUTING.md)

## 行为准则

请阅读并遵守我们的[行为准则](CODE_OF_CONDUCT.zh-CN.md)。参与本项目即表示您同意遵守其条款。

## 贡献流程

1. **Fork** 本仓库到您自己的 GitHub 账号。
2. 从 `main` **创建分支**(例如 `feat/ccemanagedcontrolplane-kubeconfig`)。
3. **开发**——遵循下列代码规范。
4. **使用 DCO 签名提交**:每个提交都必须包含 `Signed-off-by:` 尾注,使用 `git commit -s` 提交。
   DCO(Developer Certificate of Origin)全文:https://developercertificate.org/ 。签名即表明您有权提交该贡献。
5. 针对 `main` **发起 Pull Request**,使用 [PR 模板](.github/PULL_REQUEST_TEMPLATE.zh-CN.md)并关联相关 Issue 编号。
6. **评审**:维护者将在 **5 个工作日内**响应,可能要求修改。合并需**至少一名维护者**批准。
7. 批准后由维护者合并您的 PR。

## 代码规范

- 遵循 Kubernetes SIG Go 约定(使用 `gofmt`/`goimports` 格式化、lint 通过、新逻辑配套单元测试)。
- 遵循本仓库治理中的安全红线:
  - 绝不提交真实凭证、令牌或密钥(参见 `secret-scan` workflow);
  - 绝不记录密钥——只使用结构化日志,并做脱敏;
  - 所有敏感部署输入只能通过环境变量或交互提示读取。
- 为您的改动新增或更新测试(service 层单元测试、envtest 控制器测试、webhook 测试)。

## Issue / PR 模板

- Bug 报告与功能需求:使用 [.github/ISSUE_TEMPLATE/](.github/ISSUE_TEMPLATE/) 中的模板。
- 请先创建 Issue(或关联已有 Issue)以便维护者与社区讨论。

## 评审过程

- 维护者将在 **5 个工作日内**响应。
- 评审可能要求修改,多轮是正常的。
- 通过 **至少一名维护者批准** 且所有 CI 检查(DCO、markdown lint、密钥扫描、IaC 校验)通过后合并。

## 许可声明

向本仓库贡献即表示您同意您的贡献将在仓库许可证([MIT-0](LICENSE))下分发。若仓库后续切换为其他已批准的许可证(如 Apache-2.0),您的贡献将按届时有效的许可证分发。

## 获取帮助

- 以 `question` 标签创建 Issue。
- 联系维护者:<your-team@huaweicloud.com>(占位——仓库创建时更新)。
