# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go generate ./internal/api && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/marketplace-api ./cmd/api

FROM scratch

COPY --from=build /out/marketplace-api /marketplace-api

ENV PORT=8080
EXPOSE 8080
USER 65532:65532

HEALTHCHECK --interval=5s --timeout=3s --start-period=3s --retries=10 \
    CMD ["/marketplace-api", "-healthcheck"]

ENTRYPOINT ["/marketplace-api"]

