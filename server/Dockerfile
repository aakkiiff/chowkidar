# syntax=docker/dockerfile:1

# ── Build (cgo-free, pure Go SQLite via modernc.org/sqlite) ────────────────
FROM golang:1.25.9-alpine AS builder

ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /app

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/root/go/pkg/mod go mod download

COPY . .
RUN --mount=type=cache,target=/root/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o chowkidar-server .

# ── CA certs layer (needed for outbound webhook HTTPS) ────────────────────
FROM alpine:3.21 AS certs
RUN apk --no-cache add ca-certificates

# ── Runtime: scratch + binary + certs. Nothing else. ──────────────────────
FROM scratch

COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/chowkidar-server /chowkidar-server

# DB + logs volume mount point. The server creates subdirs under this
# lazily; declaring it here just documents the expectation.
VOLUME ["/app/db"]

WORKDIR /app

EXPOSE 8080

ENTRYPOINT ["/chowkidar-server"]
