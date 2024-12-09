FROM golang:1.23.4-alpine AS builder
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT=""

ENV GO111MODULE=on \
    CGO_ENABLED=0 \
    GOOS=${TARGETOS} \
    GOARCH=${TARGETARCH} \
    GOARM=${TARGETVARIANT}

RUN apk add --no-cache ca-certificates tini-static \
    && update-ca-certificates

WORKDIR /build
COPY . .
RUN go build -o aws_batch_exporter /build/cmd/aws_batch_exporter.go

FROM gcr.io/distroless/static:nonroot
USER nonroot:nonroot
COPY --from=builder --chown=nonroot:nonroot /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder --chown=nonroot:nonroot /sbin/tini-static /tini
COPY --from=builder --chown=nonroot:nonroot /build/aws_batch_exporter /aws_batch_exporter
ENTRYPOINT [ "/tini", "--", "/aws_batch_exporter" ]
LABEL \
    org.opencontainers.image.title="aws-batch-exporter" \
    org.opencontainers.image.source="https://github.com/montblu/aws-batch-exporter"
