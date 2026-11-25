FROM golang:1.26-alpine AS download
RUN arch="$(go env GOARCH)" && \
    case "$arch" in \
      arm64) checksum=ced87c09a2a5e87c7c94fd25e36c421670b97fded064026738a69f13ee8c7b96 ;; \
      amd64) checksum=f156c609eeaf9e5fcfeea33fd70411222005f83760aa95893c9d58eff006b45c ;; \
      *) echo "Unsupported architecture: $arch" >&2; exit 1 ;; \
    esac && \
    wget -q -O /tmp/weed.tar.gz "https://github.com/seaweedfs/seaweedfs/releases/download/4.55/linux_${arch}.tar.gz" && \
    echo "$checksum  /tmp/weed.tar.gz" | sha256sum -c - && \
    tar -xzf /tmp/weed.tar.gz -C /usr/local/bin weed && \
    rm -f /tmp/weed.tar.gz

FROM alpine:3.22
LABEL org.opencontainers.image.source="https://github.com/seaweedfs/seaweedfs"
RUN adduser -D -H app && mkdir -p /data && chown app:app /data
COPY --from=download /usr/local/bin/weed /usr/local/bin/weed
USER app
WORKDIR /data
ENTRYPOINT ["/usr/local/bin/weed"]
