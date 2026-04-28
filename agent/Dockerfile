# syntax=docker/dockerfile:1
FROM golang:1.25.1-alpine AS builder

# Support cross-compilation (docker buildx --platform linux/arm64, etc.)
ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /app

# --- dependency layer (cached unless go.mod/go.sum change) ---
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/root/go/pkg/mod \
    go mod download

# --- build layer (cached unless source changes) ---
COPY . .
RUN --mount=type=cache,target=/root/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o chowkidar-agent .

# --- certs layer (only CA bundle, no shell or package manager) ---
FROM alpine:3.21 AS certs
RUN apk --no-cache add ca-certificates

# --- final image: scratch = zero OS overhead ---
FROM scratch

COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/chowkidar-agent /chowkidar-agent

ENTRYPOINT ["/chowkidar-agent"]
