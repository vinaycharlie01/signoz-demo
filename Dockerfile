# Multi-stage build. modernc.org/sqlite is pure Go (no cgo), so this stays a
# simple static build with no C toolchain in either stage.
FROM golang:1.25-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/loadgen ./cmd/loadgen

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app

COPY --from=build /out/api /app/api
COPY --from=build /out/loadgen /app/loadgen

ENV HTTP_ADDR=:8090 \
    DB_PATH=/data/orders.db \
    OTEL_SERVICE_NAME=signoz-demo-order-service \
    DEPLOYMENT_ENVIRONMENT=docker-compose \
    OTEL_EXPORTER_OTLP_ENDPOINT=signoz-otel-collector:4317 \
    OTEL_EXPORTER_OTLP_INSECURE=true

VOLUME ["/data"]
EXPOSE 8090

ENTRYPOINT ["/app/api"]
