# Runtime-only image, mirroring the convention from this org's other Go CLIs
# (see vinaycharlie01/sh-mcp-go's Dockerfile): the binary is cross-compiled
# ahead of time by `mage go:crossBuild` (nirantaraai/nava), not inside this
# Dockerfile — there is no `go build` / golang base image / multi-stage
# compile step here. Run `mage go:crossBuild` (or `mage docker:build`, which
# depends on it) before building this image.
FROM registry.access.redhat.com/ubi9/ubi-minimal:latest

RUN microdnf install -y ca-certificates && microdnf clean all

ARG TARGETARCH=amd64
COPY dist/linux_${TARGETARCH}/api /usr/local/bin/signoz-demo-api

ENV HTTP_ADDR=:8090 \
    DB_PATH=/data/orders.db \
    OTEL_SERVICE_NAME=signoz-demo-order-service \
    DEPLOYMENT_ENVIRONMENT=docker-compose \
    OTEL_EXPORTER_OTLP_ENDPOINT=signoz-otel-collector:4317 \
    OTEL_EXPORTER_OTLP_INSECURE=true

RUN mkdir -p /data && chown -R 1000:1000 /data
VOLUME ["/data"]
EXPOSE 8090

USER 1000:1000

ENTRYPOINT ["/usr/local/bin/signoz-demo-api"]
