FROM golang:1.26-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG SERVICE
RUN CGO_ENABLED=0 go build -p=1 -trimpath -ldflags="-s -w" -o /service ./cmd/${SERVICE}

FROM alpine:3.22
RUN adduser -D -H app
USER app
WORKDIR /app
COPY --from=builder /service /service
COPY schema /app/schema
ENTRYPOINT ["/service"]
