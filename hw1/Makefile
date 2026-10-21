UNAME_S := $(shell uname -s 2>/dev/null)
SERVICE_IMAGE ?= marketplace-user-service:local
SERVICE_CONTAINER ?= marketplace-user-service
DOCKER_COMPOSE := $(shell if docker compose version >/dev/null 2>&1; then printf 'docker compose'; elif command -v docker-compose >/dev/null 2>&1; then printf 'docker-compose'; fi)

ifeq ($(OS),Windows_NT)
C4_RUNNER := powershell.exe -NoProfile -ExecutionPolicy Bypass -File scripts/render-c4.ps1
else
C4_RUNNER := bash scripts/render-c4.sh
endif

.PHONY: help install ensure-c4-deps ensure-docker install-macos install-linux install-windows c4 c4-svg c4-png test run docker-build docker-run docker-up docker-down docker-logs health

help:
	@echo "make install          - install PlantUML and Graphviz for the current OS"
	@echo "make ensure-c4-deps   - install PlantUML and Graphviz if they are missing"
	@echo "make install-macos    - install dependencies with Homebrew"
	@echo "make install-linux    - install dependencies with apt, dnf or pacman"
	@echo "make install-windows  - install dependencies with Chocolatey"
	@echo "make c4               - render the C4 diagram as SVG"
	@echo "make c4-png           - render the C4 diagram as PNG"
	@echo "make test             - run service tests"
	@echo "make run              - run User Service locally"
	@echo "make docker-build     - build the User Service image"
	@echo "make docker-run       - build and run the container in the foreground"
	@echo "make docker-up        - start the service with Docker Compose or plain Docker"
	@echo "make docker-down      - stop the Docker Compose project or plain Docker container"
	@echo "make health           - call http://localhost:8080/health"

install:
ifeq ($(OS),Windows_NT)
	@$(MAKE) --no-print-directory install-windows
else ifeq ($(UNAME_S),Darwin)
	@$(MAKE) --no-print-directory install-macos
else ifeq ($(UNAME_S),Linux)
	@$(MAKE) --no-print-directory install-linux
else
	@echo "Unsupported operating system. Use install-macos, install-linux or install-windows."
	@exit 1
endif

ensure-c4-deps:
	@command -v plantuml >/dev/null 2>&1 && command -v dot >/dev/null 2>&1 || $(MAKE) --no-print-directory install

ensure-docker:
	@command -v docker >/dev/null 2>&1 || { \
		echo "Docker was not found. Install Docker Desktop or Docker Engine."; \
		exit 1; \
	}

install-macos:
	@command -v brew >/dev/null 2>&1 || { echo "Homebrew is required: https://brew.sh"; exit 1; }
	brew install plantuml graphviz

install-linux:
	@set -e; \
	if [ "$$(id -u)" -eq 0 ]; then \
		elevate=""; \
	elif command -v sudo >/dev/null 2>&1; then \
		elevate="sudo"; \
	else \
		echo "Run this target as root or install sudo."; \
		exit 1; \
	fi; \
	if command -v apt-get >/dev/null 2>&1; then \
		$$elevate apt-get update && $$elevate apt-get install -y plantuml graphviz; \
	elif command -v dnf >/dev/null 2>&1; then \
		$$elevate dnf install -y plantuml graphviz; \
	elif command -v pacman >/dev/null 2>&1; then \
		$$elevate pacman -S --needed plantuml graphviz; \
	else \
		echo "Supported package managers: apt, dnf and pacman."; \
		exit 1; \
	fi

install-windows:
	@powershell.exe -NoProfile -ExecutionPolicy Bypass -Command "if (-not (Get-Command choco -ErrorAction SilentlyContinue)) { Write-Error 'Chocolatey is required: https://chocolatey.org/install'; exit 1 }; choco install plantuml graphviz -y --no-progress; if ($$LASTEXITCODE -ne 0) { exit $$LASTEXITCODE }"

c4: c4-svg

c4-svg: ensure-c4-deps
	$(C4_RUNNER) svg

c4-png: ensure-c4-deps
	$(C4_RUNNER) png

test:
	go test ./...

run:
	go run ./cmd/user-service

docker-build: ensure-docker
	docker build --tag $(SERVICE_IMAGE) .

docker-run: docker-build
	docker run --rm --name $(SERVICE_CONTAINER) --publish 8080:8080 $(SERVICE_IMAGE)

docker-up: ensure-docker
	@if [ -n "$(DOCKER_COMPOSE)" ]; then \
		$(DOCKER_COMPOSE) up --build --detach; \
	else \
		docker build --tag $(SERVICE_IMAGE) .; \
		docker rm --force $(SERVICE_CONTAINER) >/dev/null 2>&1 || true; \
		docker run --detach --name $(SERVICE_CONTAINER) --publish 8080:8080 --env PORT=8080 $(SERVICE_IMAGE); \
	fi

docker-down: ensure-docker
	@if [ -n "$(DOCKER_COMPOSE)" ]; then \
		$(DOCKER_COMPOSE) down; \
	else \
		docker rm --force $(SERVICE_CONTAINER) >/dev/null 2>&1 || true; \
	fi

docker-logs: ensure-docker
	@if [ -n "$(DOCKER_COMPOSE)" ]; then \
		$(DOCKER_COMPOSE) logs --follow user-service; \
	else \
		docker logs --follow $(SERVICE_CONTAINER); \
	fi

health:
	@if command -v curl >/dev/null 2>&1; then \
		curl --fail --silent --show-error http://localhost:8080/health; \
		echo; \
	elif command -v powershell.exe >/dev/null 2>&1; then \
		powershell.exe -NoProfile -Command "Invoke-RestMethod http://localhost:8080/health | ConvertTo-Json -Compress"; \
	else \
		echo "curl or PowerShell is required to call /health."; \
		exit 1; \
	fi
