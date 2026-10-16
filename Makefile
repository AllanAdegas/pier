# Pier — build and development tasks.
# The web UI is compiled by Vite and copied into internal/webui/dist, which the
# Go binary embeds; the result is a single self-contained executable.

BINARY := bin/pier
WEBUI  := internal/webui/dist

.PHONY: all web build test vet run dev-api dev-web docker-build docker-up docker-down clean

all: build

## web: compile the SolidJS interface and stage it for embedding
web:
	cd web && deno install && deno task build
	find $(WEBUI) -mindepth 1 ! -name .gitkeep -delete
	cp -R web/dist/. $(WEBUI)/

## build: compile the interface and the single binary
build: web
	go build -o $(BINARY) ./cmd/pier

## test: run Go tests and type-check the interface
test:
	go vet ./...
	go test ./...
	cd web && deno task typecheck && deno task test

## run: build everything and start Pier on :8700 with data in ./.data
run: build
	PIER_DATA_DIR=./.data ./$(BINARY)

## dev-api: run the backend only (pair with `make dev-web`)
dev-api:
	PIER_DATA_DIR=./.data PIER_DEV=1 go run ./cmd/pier

## dev-web: run Vite with hot reload on :5173, proxying /api to :8700
dev-web:
	cd web && deno task dev

## docker-build: build the container image (pier:latest)
docker-build:
	docker compose build

## docker-up: build the image and start Pier in a container (settings in .env)
docker-up:
	docker compose up --detach --build

## docker-down: stop the Pier container; what it deployed keeps running
docker-down:
	docker compose down

clean:
	rm -rf bin web/dist
	find $(WEBUI) -mindepth 1 ! -name .gitkeep -delete
