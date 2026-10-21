#!/bin/sh
# Builds the static binary here and a scratch image context in ./dist.
set -e
cd "$(dirname "$0")"
mkdir -p dist
# The agents the hub hands out; signed if LANHUB_AGENT_SIGNING_KEY(_FILE) is set.
go run ./cmd/agentbundle -agent ../agent -out agentbuilds
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/lanhub-hub .
cp /etc/ssl/certs/ca-certificates.crt dist/
V=$(cat ../VERSION); R=$(git rev-parse HEAD 2>/dev/null || true)
sed -e "s/^ARG LANHUB_VERSION=.*/ARG LANHUB_VERSION=$V/" -e "s/^ARG LANHUB_REVISION=.*/ARG LANHUB_REVISION=$R/" Dockerfile.prebuilt > dist/Dockerfile
mkdir -p dist/empty
ls -la dist
