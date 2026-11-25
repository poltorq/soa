.PHONY: fmt fmt-check vet test cover race build check up down reset logs ps health e2e integration resilience db-clickhouse db-postgres

fmt:
	gofmt -w $$(find . -name '*.go')

fmt-check:
	@test -z "$$(gofmt -l $$(find . -name '*.go'))" || (echo "Run make fmt"; exit 1)

vet:
	go vet ./...

test:
	go test -count=1 ./...

cover:
	go test -count=1 -cover ./...

race:
	go test -count=1 -race ./...

build:
	go build ./...

check: fmt-check vet test race build

up:
	docker compose up --build -d --wait

down:
	docker compose down

reset:
	docker compose down -v --remove-orphans
	docker compose up --build -d --wait

logs:
	docker compose logs -f movie-service aggregation-service clickhouse

ps:
	docker compose ps -a

health:
	curl --fail --silent --show-error http://localhost:8080/health
	curl --fail --silent --show-error http://localhost:8081/health

e2e:
	docker compose up --build -d --wait
	sh ./scripts/e2e.sh

integration:
	docker compose up -d --wait postgres clickhouse
	docker compose run --rm --no-deps postgres-migrate
	docker compose run --rm --no-deps clickhouse-migrate
	go test -count=1 -tags=integration ./internal/aggregation

resilience:
	docker compose up --build -d --wait
	python3 scripts/resilience.py

db-clickhouse:
	docker compose exec clickhouse clickhouse-client --user analytics --password analytics --database movie_analytics

db-postgres:
	docker compose exec postgres psql -U analytics -d analytics
