#!/bin/sh
# Builds the lanhub-media binary and a build context in ./dist-media.
set -e
cd "$(dirname "$0")"
mkdir -p dist-media
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist-media/lanhub-media ./cmd/lanhub-media
cp Dockerfile.media dist-media/Dockerfile
ls -la dist-media
