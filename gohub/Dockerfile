# LANHub Go hub. Two stages: build a static binary, then ship it alone.
# Build from the repo's root (the hub carries the agent builds it hands out):
#   docker build -f gohub/Dockerfile .
FROM golang:1.27.3 AS build
WORKDIR /src/gohub
COPY gohub/go.mod gohub/go.sum ./
RUN go mod download
COPY agent/ /src/agent/
RUN cd /src/agent && go mod download
COPY gohub/ ./
# The agents for every platform, signed with LANHub's release key when the
# build is given it (the agent_signing_key secret; see cmd/agentbundle).
# Without it they're bundled unsigned: installable, but not pushed as updates.
RUN --mount=type=secret,id=agent_signing_key,required=false \
    LANHUB_AGENT_SIGNING_KEY_FILE=/run/secrets/agent_signing_key go run ./cmd/agentbundle -agent /src/agent -out agentbuilds
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /lanhub-hub . && mkdir -p /empty

FROM scratch
ARG LANHUB_VERSION=dev
ARG LANHUB_REVISION=
# The hub's Settings → Updates reads these to see what's published.
LABEL org.opencontainers.image.version=$LANHUB_VERSION \
      org.opencontainers.image.revision=$LANHUB_REVISION \
      org.opencontainers.image.source=https://github.com/AC3Labs/LANHub
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /lanhub-hub /lanhub-hub
# Owned by the hub's user so a new volume mounted here is writable:
# /data holds the database and the generated key, /media-key the secret
# shared with LANHub Media.
COPY --from=build --chown=65532:65532 /empty /data
COPY --from=build --chown=65532:65532 /empty /media-key
USER 65532:65532
ENV LANHUB_DB=/data/database.sqlite LANHUB_ENV_FILE=/app/hub.env
EXPOSE 8000
ENTRYPOINT ["/lanhub-hub"]
