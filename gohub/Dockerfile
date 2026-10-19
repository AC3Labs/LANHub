# LANHub Go hub. Two stages: build a static binary, then ship it alone.
FROM golang:1.27.1 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
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
