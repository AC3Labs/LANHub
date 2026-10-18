#!/bin/sh
# Builds the static binary here and a scratch image context in ./dist.
set -e
cd "$(dirname "$0")"
mkdir -p dist
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/lanhub-hub .
cp /etc/ssl/certs/ca-certificates.crt dist/
cp Dockerfile.prebuilt dist/Dockerfile
ls -la dist
