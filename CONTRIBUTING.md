# Contributing to cloudnative-cluster-api-provider-cce

Thanks for your interest in contributing! This repository follows the [Huawei Cloud solution developer kit governance](https://developer.huaweicloud.com/) and the [Cluster API](https://cluster-api.sigs.k8s.io/) community conventions.

> 中文版 / Chinese: [CONTRIBUTING.zh-CN.md](CONTRIBUTING.zh-CN.md)

## Code of Conduct

Please read and follow our [Code of Conduct](CODE_OF_CONDUCT.md). Participation in this project means you agree to abide by its terms.

## Contribution Workflow

1. **Fork** this repository to your own GitHub account.
2. **Create a branch** from `main` for your change (e.g. `feat/ccemanagedcontrolplane-kubeconfig`).
3. **Develop** — follow the coding standards below.
4. **Commit with DCO signature**: every commit must include a `Signed-off-by:` trailer. Use `git commit -s`.
   The DCO (Developer Certificate of Origin) full text: <https://developercertificate.org/>. By signing, you certify that you have the right to submit the contribution.
5. **Open a Pull Request** against `main`, using the [PR template](.github/PULL_REQUEST_TEMPLATE.md) and linking the related issue number.
6. **Review**: maintainers will respond within **5 business days** and may request changes. A merge requires approval from **at least one maintainer**.
7. After approval, the maintainer merges your PR.

## Coding Standards

- Follow the Kubernetes SIG Go conventions (formatting with `gofmt`/`goimports`, lint clean, unit tests for new logic).
- Follow the security red lines in this repository's governance:
  - Never commit real credentials, tokens, or keys (see the `secret-scan` workflow);
  - Never log secrets — structured logging only, with redaction;
  - All sensitive deployment inputs are read from environment variables or interactive prompts only.
- Add or update tests for the code you change (service-layer unit tests, envtest controller tests, webhook tests).

## Issue / PR Templates

- Bug reports and feature requests: use the templates in [.github/ISSUE_TEMPLATE/](.github/ISSUE_TEMPLATE/).
- To request a change, open an issue first (or link an existing one) so the maintainers and community can discuss it.

## Review Process

- Maintainers will respond within **5 business days**.
- Reviews may request changes; multiple rounds are normal.
- A PR is merged after **at least one maintainer approval** and all CI checks (DCO, markdown lint, secret scan, IaC validation) pass.

## License Declaration

By contributing to this repository, you agree that your contributions will be licensed under the repository's license ([MIT-0](LICENSE)) and distributed accordingly. If the repository later switches to another approved license (e.g., Apache-2.0), your contribution will be distributed under the then-current license.

## Getting Help

- Open an issue with the `question` label.
- Reach the maintainers at <your-team@huaweicloud.com> (placeholder — update at repo creation).
