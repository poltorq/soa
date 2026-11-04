FROM golang:1.26-alpine AS build

RUN apk add --no-cache protobuf protobuf-dev
RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.10 && \
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN protoc -I proto -I /usr/include \
    --go_out=. --go_opt=module=flight-booking-hw3 \
    --go-grpc_out=. --go-grpc_opt=module=flight-booking-hw3 \
    proto/flight/v1/flight.proto

ARG SERVICE
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/service ./cmd/${SERVICE}

FROM alpine:3.23
RUN adduser -D -u 65532 app
COPY --from=build /out/service /service
USER app
ENTRYPOINT ["/service"]

