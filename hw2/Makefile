.PHONY: generate fmt vet test race cover check build integration up down reset logs ps health db e2e

generate:
	go generate ./internal/api

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './internal/api/generated.gen.go')

vet: generate
	go vet ./...

test: generate
	go test ./...

race: generate
	go test -race ./...

cover: generate
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

check: fmt generate vet test race

integration: generate
	@test -n "$$TEST_DATABASE_URL" || (echo "Set TEST_DATABASE_URL to a dedicated database migrated by Flyway"; exit 1)
	go test -race ./internal/store -run Integration -count=1

build: generate
	mkdir -p bin
	go build -o bin/marketplace-api ./cmd/api

up:
	docker compose up --build -d

down:
	docker compose down

reset:
	docker compose down -v --remove-orphans
	docker compose up --build -d

logs:
	docker compose logs -f api

ps:
	docker compose ps

health:
	curl --fail --silent --show-error http://localhost:8080/health | jq .

db:
	docker compose exec postgres psql -U marketplace -d marketplace

e2e:
	./scripts/e2e.sh
