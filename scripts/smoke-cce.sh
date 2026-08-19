#!/usr/bin/env bash
# -----------------------------------------------------------------------------
# smoke-cce.sh — run the real CCE smoke test (creates billed resources).
#
# Purpose  : Drive internal/services/cce's TestSmoke against a real Huawei
#            Cloud CCE account. Verifies the behaviors that documentation
#            could not confirm (questionnaire Q1-Q8).
# Usage    : scripts/smoke-cce.sh   (after exporting CCE_SMOKE_* variables)
# Depends  : bash, go, and the CCE_SMOKE_* environment variables below.
# Safety   : Creates a real CCE cluster + node pool (billed). Always cleans up
#            on success; on failure, delete leftovers in the console.
# Idempotent: each run uses a unique cluster name (timestamp suffix).
# -----------------------------------------------------------------------------
set -euo pipefail

# --- configuration (env only, see docs/smoke-test-checklist.md) ------------
: "${CCE_SMOKE_AK:?CCE_SMOKE_AK is required (Huawei Cloud Access Key)}"
: "${CCE_SMOKE_SK:?CCE_SMOKE_SK is required (Huawei Cloud Secret Key)}"
: "${CCE_SMOKE_VPC:?CCE_SMOKE_VPC is required (existing VPC id)}"
: "${CCE_SMOKE_SUBNET:?CCE_SMOKE_SUBNET is required (node subnet id)}"
: "${CCE_SMOKE_ENI_SUBNET:?CCE_SMOKE_ENI_SUBNET is required (eni/container subnet id)}"
: "${CCE_SMOKE_KEYPAIR:?CCE_SMOKE_KEYPAIR is required (SSH keypair name in region)}"

CCE_SMOKE_REGION="${CCE_SMOKE_REGION:-cn-north-4}"
CCE_SMOKE_CASES="${CCE_SMOKE_CASES:-cluster,pool,scale,delete}"
CCE_SMOKE_FLAVOR="${CCE_SMOKE_FLAVOR:-c7.large.2}"

echo "== CCE smoke test plan =="
echo "  region : ${CCE_SMOKE_REGION}"
echo "  vpc    : ${CCE_SMOKE_VPC}   subnet: ${CCE_SMOKE_SUBNET}   eni-subnet: ${CCE_SMOKE_ENI_SUBNET}"
echo "  cases  : ${CCE_SMOKE_CASES}"
echo "  flavor : ${CCE_SMOKE_FLAVOR}  (keypair: ${CCE_SMOKE_KEYPAIR})"
echo
echo "WARNING: this creates a REAL CCE cluster (Turbo/eni) and a node pool."
echo "The test deletes them afterwards, but check the console for leftovers"
echo "(EIP/EVS/ELB) after the run."
echo
read -r -p "Continue? [yes/NO]: " answer
[[ "$answer" == "yes" ]] || { echo "Aborted."; exit 1; }

set -x
go test -tags smoke -v -timeout 90m ./internal/services/cce/ -run TestSmoke
