#!/bin/bash
# CI gate (v2.5 Maturity): vet + tests + repeat-safe + fmt + vet/check apps.
# Run locally or from any CI runner: `bash ci.sh`.
# (A `.github/workflows/ci.yml` mirror of these steps is the repo gate where
#  the workflows directory is writable; this script is the source of truth.)
set -e
go vet ./...
go test ./... -count=1
go test ./internal/backend/ -run TestV23TCP -count=3
go build -o /tmp/fusion ./cmd/fusion
/tmp/fusion fmt . --check
/tmp/fusion vet ./tests/hello-app
/tmp/fusion check ./tests/hello-app
echo "CI OK"
