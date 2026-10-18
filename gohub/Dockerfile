# LANHub Go hub. Two stages: build a static binary, then ship it alone.
FROM golang:1.27.1 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /lanhub-hub .

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /lanhub-hub /lanhub-hub
USER 65532:65532
ENV LANHUB_DB=/data/database.sqlite LANHUB_ENV_FILE=/app/hub.env LANHUB_LISTEN=:8000
EXPOSE 8000
ENTRYPOINT ["/lanhub-hub"]
