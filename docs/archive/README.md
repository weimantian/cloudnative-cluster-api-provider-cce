# 归档文档说明（docs/archive/）

> 归档日期：2026-08-24
> 本目录存放开发过程中产生的**历史文档、研究资料与过程记录**。当前活跃文档见 `docs/` 根目录，最新对标状态见 [`capa-alignment-final-summary.md`](../capa-alignment-final-summary.md)。

## 归档内容

| 文档 | 类型 | 归档原因 |
|---|---|---|
| `capa-alignment-remediation-design-2026-08.md` | 修复设计 + 实施记录 | 已被 `capa-alignment-final-summary.md` 合并取代 |
| `capa-comparison-review-2026-08.md` | 实现级审视 | 同上（其 3 个 P0 已修复，正文未回改） |
| `capa-parity-gap-analysis.md` | 差距总账 | 同上（状态更新行已标注补齐，正文部分行未回改） |
| `CAPA架构分析报告.md` | 源码分析 | 开发阶段范本研究，实现已完成 |
| `ACKProvider架构分析报告.md` | 源码分析 | 同上 |
| `CAPHW架构分析报告.md` | 源码分析 | 同上 |
| `api-review-findings.md` | API 审查记录 | 一次性审计过程（含修复 commit） |
| `code-audit-findings.md` | 代码审计记录 | 同上 |
| `poc-implementation-tracker.md` | 实现跟踪清单 | 过程文档，P0/P1/P2 已完成 |
| `cce-verification-questionnaire.md` | 对齐问卷（中文） | 实现前确认用，14 项已全部核对 |
| `cce-verification-questionnaire.en.md` | 对齐问卷（英文） | 同上 |
| `云容器引擎 CCE API参考.pdf` | 官方 API 参考 | `api-review-findings.md` 的原始依据 |
| `capa-alignment-summary-2026-08-22.md` | 对标 CAPA 汇总报告（v1） | 已被 `capa-alignment-final-summary.md` 合并取代 |
| `audit/`（v1 审计，7 文件） | 逐字段代码审计 | 同上 |
| `audit-v2/`（v2 增量审计） | 增量审计（对标 CAPA v2.13.0） | 同上 |

## 已知内容错误（已被后续修正，此处仅留档）

以下历史文档中的结论在后续调查中被推翻，**以 `capa-alignment-final-summary.md` 为准**，请勿直接引用：

1. **`capa-parity-gap-analysis.md`**：「GC 按 tag 扫描清理遗留 EIP **依赖 TMS 标签服务**」——❌ 错误。EIP 有专属 `CreatePublicipTag`/`BatchCreatePublicipTags` API，**无需 TMS**。
2. **`capa-alignment-remediation-design-2026-08.md`** 风险表：「EIP tag key **36 字符**限制 vs owned tag key 超限」——❌ 错误。EIP/NAT tag key 实为 **128 字符**（SDK 旧注释误导，官方 2026-08-05 已更新），owned tag key 无需缩短。
3. **三份 CAPA 对标文档中隐含的「SWR 无 VPC endpoint，CCE 更依赖 NAT」**——❌ 错误。SWR **支持 VPCEP**，基础版对同区域 ECS/CCE 节点**默认内网访问**（免 NAT），与 CAPA ECR VPC endpoint 对等。

## 当前活跃文档（docs/ 根目录）

- `capa-alignment-final-summary.md` —— **对标 CAPA 最终审计报告（合并归纳版）**（含变更记录、能力全景、剩余差距、调查结论、待决策项；对标 CAPA v2.13.0 / CAPI v1.14.0）
- `architecture-design.md` —— 架构设计
- `requirements-design.md` —— 需求设计
- `research-sources.md` —— 调研依据与事实清单（事实底座）
- `cce-verification-findings.md` —— 验证结论记录
- `cloud-to-cloud-deployment-guide.md` —— 云上管云上部署指导
- `clusterctl-deployment-validation.md` —— clusterctl 部署演练记录
- `smoke-test-checklist.md` —— 真实 CCE 冒烟测试清单
