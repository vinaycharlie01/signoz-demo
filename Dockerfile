# Runtime-only image — the binary is cross-compiled ahead of time by
# `mage go:crossBuild` (CGO disabled, pure Go). There is no `go build` step
# here. Run `mage go:crossBuild` (or `mage docker:build`, which depends on it)
# before building this image.
#
# Base image: alpine:3 — small (~8 MB), ships ca-certificates, no microdnf.
# Using alpine avoids the UBI9 microdnf/Red Hat CDN SSL issues that appear
# in air-gapped or corporate proxy environments.
FROM alpine:3

# ca-certificates is needed for any TLS calls the binary makes (e.g. OTLP/TLS).
# Alpine's apk works without Red Hat CDN so this is reliable everywhere.
RUN apk add --no-cache ca-certificates

ARG TARGETARCH=amd64
COPY dist/linux_${TARGETARCH}/api /usr/local/bin/signoz-demo-api

ENV HTTP_ADDR=:8090 \
    DB_PATH=/data/orders.db \
    OTEL_SERVICE_NAME=signoz-demo-order-service \
    DEPLOYMENT_ENVIRONMENT=docker-compose \
    OTEL_EXPORTER_OTLP_ENDPOINT=signoz-ingester:4317 \
    OTEL_EXPORTER_OTLP_INSECURE=true

RUN mkdir -p /data && chown -R 1000:1000 /data
VOLUME ["/data"]
EXPOSE 8090

USER 1000:1000

ENTRYPOINT ["/usr/local/bin/signoz-demo-api"]
