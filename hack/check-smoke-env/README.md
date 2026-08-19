# check-smoke-env

Lists the current VPCs, subnets (with neutron IDs) and CCE clusters in the
region, so a smoke-test run can pick existing network resources
(`docs/smoke-test-checklist.md`).

```bash
AK=... SK=... go run ./hack/check-smoke-env
```
