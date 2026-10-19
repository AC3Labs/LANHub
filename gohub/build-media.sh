#!/bin/sh
# Builds the lanhub-media binary and a build context in ./dist-media.
set -e
cd "$(dirname "$0")"
mkdir -p dist-media
V=$(cat ../VERSION); R=$(git rev-parse HEAD 2>/dev/null || true)
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w -X main.version=$V" -o dist-media/lanhub-media ./cmd/lanhub-media
sed -e "s/^ARG LANHUB_VERSION=.*/ARG LANHUB_VERSION=$V/" -e "s/^ARG LANHUB_REVISION=.*/ARG LANHUB_REVISION=$R/" Dockerfile.media.prebuilt > dist-media/Dockerfile
ls -la dist-media
