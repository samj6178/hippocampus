.PHONY: build test lint run db-up db-down migrate clean web web-install

APP_NAME := hippocampus
VERSION  := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
DB_URL := postgres://mos:mos@localhost:5432/hippocampus?sslmode=disable

## Frontend
web-install:
	cd web && npm install

web: web-install
	cd web && npx vite build
	rm -rf cmd/hippocampus/web_dist
	cp -r web/dist cmd/hippocampus/web_dist

## Build (includes frontend)
build: web
	go build -ldflags "-X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)" \
		-o bin/$(APP_NAME) ./cmd/hippocampus/

build-go:
	go build -ldflags "-X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)" \
		-o bin/$(APP_NAME) ./cmd/hippocampus/

## Test
test:
	go test -race -short ./...

test-integration:
	go test -race -run Integration ./...

test-coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

## Lint
lint:
	go vet ./...

## Run locally
run: build
	./bin/$(APP_NAME) -config config.json

## Database
db-up:
	docker compose up -d timescaledb

db-down:
	docker compose down

migrate:
	@echo "Running migrations against $(DB_URL)"
	@for f in migrations/*.up.sql; do \
		echo "Applying $$f..."; \
		psql "$(DB_URL)" -f "$$f"; \
	done

migrate-down:
	@echo "Rolling back migrations..."
	@for f in $$(ls -r migrations/*.down.sql); do \
		echo "Rolling back $$f..."; \
		psql "$(DB_URL)" -f "$$f"; \
	done

## Docker
docker-build:
	docker compose build

docker-up:
	docker compose up -d

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f hippocampus

## Clean
clean:
	rm -rf bin/ coverage.out
