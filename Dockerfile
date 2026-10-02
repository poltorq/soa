# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS build

WORKDIR /src

COPY go.mod ./
COPY cmd/user-service ./cmd/user-service

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/user-service \
    ./cmd/user-service

FROM scratch

COPY --from=build /out/user-service /user-service

ENV PORT=8080
EXPOSE 8080

USER 65532:65532

HEALTHCHECK --interval=10s --timeout=3s --start-period=2s --retries=3 \
    CMD ["/user-service", "-healthcheck"]

ENTRYPOINT ["/user-service"]
