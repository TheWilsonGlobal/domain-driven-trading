FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/bin/gateway ./cmd/gateway
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/bin/engine ./cmd/engine
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/bin/settlement ./cmd/settlement

FROM alpine:3.19 AS base
RUN apk --no-cache add ca-certificates tzdata

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
