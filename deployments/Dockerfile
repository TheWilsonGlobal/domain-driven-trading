# Base images are pinned by digest (tag kept for readability). Bump the Go tag
# together with the `go` directive in go.mod.
FROM golang:1.27.3-alpine@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb AS builder
WORKDIR /app
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /app/bin/gateway ./cmd/gateway
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /app/bin/engine ./cmd/engine
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /app/bin/settlement ./cmd/settlement

FROM alpine:3.22@sha256:2e0b81d60597f449baa56dc25d355367869d2dca4cd57e866f1e2b52af3eb55f AS base
RUN apk --no-cache add ca-certificates tzdata \
 && addgroup -S -g 10001 ddt \
 && adduser -S -u 10001 -G ddt -H -s /sbin/nologin ddt
USER 10001:10001

FROM base AS gateway
COPY --from=builder /app/bin/gateway /usr/local/bin/gateway
EXPOSE 5470
ENTRYPOINT ["/usr/local/bin/gateway"]

FROM base AS engine
COPY --from=builder /app/bin/engine /usr/local/bin/engine
ENTRYPOINT ["/usr/local/bin/engine"]

FROM base AS settlement
COPY --from=builder /app/bin/settlement /usr/local/bin/settlement
ENTRYPOINT ["/usr/local/bin/settlement"]
