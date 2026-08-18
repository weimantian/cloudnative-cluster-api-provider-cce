---
name: Pull Request
about: 提交对 cloudnative-cluster-api-provider-cce 的修改
title: ""
labels: []
assignees: []
---

> English template: [PULL_REQUEST_TEMPLATE.md](PULL_REQUEST_TEMPLATE.md)

## 关联 Issue

Closes #<!-- issue 编号 -->

## 变更说明

本 PR 做了什么、为什么。

## DCO 确认

- [ ] 我同意 [Developer Certificate of Origin](https://developercertificate.org/) 的全部条款,并确认我的所有提交都带有 `Signed-off-by:` 尾注(`git commit -s`)。

## 检查清单

- [ ] 代码可编译且 `make lint` 通过
- [ ] 新增或更新单元/envtest 测试并通过(`make test`)
- [ ] Webhook / API 变更已记录
- [ ] 面向用户的变化已更新 README / 文档(含中文版)
- [ ] 未提交任何密钥或凭证(secret-scan workflow)
- [ ] 若改动部署脚本(`deploy/scripts/*`),已通过 iac-validate workflow 校验
- [ ] Markdown lint 通过

## 测试证据

描述测试方式(单元、envtest、手工、e2e),如有请附 `clusterctl describe cluster` 输出。
