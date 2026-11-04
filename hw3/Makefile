.PHONY: generate fmt vet test race check build integration up down reset logs ps health db-booking db-flight e2e

generate:
	docker run --rm -v "$$(pwd):/workspace" -w /workspace golang:1.26-alpine sh -c 'apk add --no-cache protobuf protobuf-dev >/dev/null && go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.10 && go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1 && protoc -I proto -I /usr/include --go_out=. --go_opt=module=flight-booking-hw3 --go-grpc_out=. --go-grpc_opt=module=flight-booking-hw3 proto/flight/v1/flight.proto'

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './gen/*')

vet:
	go vet ./...

test:
	go test ./...

race:
	go test -race ./...

check: fmt vet test race

build:
	mkdir -p bin
	go build -o bin/flight-service ./cmd/flight-service
	go build -o bin/booking-service ./cmd/booking-service

integration:
	@test -n "$$TEST_FLIGHT_DATABASE_URL" || (echo "Set TEST_FLIGHT_DATABASE_URL to a migrated test database"; exit 1)
	go test -race ./internal/flight -run Integration -count=1

up:
	docker compose up --build -d

down:
	docker compose down

reset:
	docker compose down -v --remove-orphans
	docker compose up --build -d

logs:
	docker compose logs -f booking-service flight-service

ps:
	docker compose ps -a

health:
	curl --fail --silent --show-error http://localhost:8080/health

db-booking:
	docker compose exec booking-postgres psql -U booking -d booking

db-flight:
	docker compose exec flight-postgres psql -U flight -d flight

e2e:
	./scripts/e2e.sh

