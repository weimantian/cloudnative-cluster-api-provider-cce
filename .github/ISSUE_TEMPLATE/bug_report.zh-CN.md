---
name: Bug 报告
about: 报告 cloudnative-cluster-api-provider-cce 的缺陷
title: "[Bug] "
labels: bug
assignees: ''
---

> English template: [bug_report.md](bug_report.md)

## 问题描述

清晰、简洁地描述问题。

## 预期行为

应该发生什么。

## 实际行为

实际发生了什么(包括错误信息、控制器日志、conditions)。

## 复现步骤

1. ...(例如 `kubectl apply -f workload-cluster.yaml`)
2. ...
3. ...

## 环境信息

- Provider 版本 / 提交:
- Cluster API 版本:
- Kubernetes 版本(管理集群):
- 华为云 CCE 类型:`CCE` / `Turbo`
- 区域:
- 管理集群类型(`kind` / 云上集群):

## 日志 / 工件

相关控制器日志(请脱敏任何凭证)、`clusterctl describe cluster` 输出、CR 的 `status.conditions`。

## 补充说明

任何可能有帮助的信息,例如[验证清单](https://github.com/ORG/cloudnative-cluster-api-provider-cce/blob/main/docs/research-sources.md)中的相关条目。
