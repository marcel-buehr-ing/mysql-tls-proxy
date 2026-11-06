FROM golang:1.27.3-alpine@sha256:fa3d596898d9f126af7c01d14ee7597bab3f39cccf392ef36b69fe77cd681f18 AS build
WORKDIR /src
COPY go.mod main.go proxy.go proxy_test.go certificate.go certificate_test.go shutdown_test.go LICENSE ./
ENV CGO_ENABLED=0
RUN go test ./... && \
    go build -trimpath -ldflags="-s -w" -o /out/mysql-tls-proxy .

FROM scratch
LABEL org.opencontainers.image.title="mysql-tls-proxy" \
      org.opencontainers.image.description="TLS-only bridge for the classic MySQL/MariaDB protocol" \
      org.opencontainers.image.licenses="MIT"
COPY --from=build /out/mysql-tls-proxy /mysql-tls-proxy
COPY --from=build /src/LICENSE /LICENSE
USER 65532:65532
EXPOSE 3306
ENTRYPOINT ["/mysql-tls-proxy"]
