#!/bin/bash
set -e
cd "$(dirname "$0")"
if [ -e "release/fusion" ]; then
  rm -rf "release/fusion"
fi
mkdir -p release
go build -o release/fusion ./cmd/fusion
echo "built release/fusion"
