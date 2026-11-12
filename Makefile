# Pier — build and development tasks.
# The web UI is compiled by Vite and copied into internal/webui/dist, which the
# Go binary embeds; the result is a single self-contained executable.

BINARY := bin/pier
# Same release flags as the Dockerfile: static binary (no cgo; SQLite is pure
# Go), reproducible paths, no symbol tables.
GOBUILD := CGO_ENABLED=0 go build -trimpath -ldflags="-s -w"
WEBUI  := internal/webui/dist

.PHONY: all web build test vet run dev-api dev-web docker-build docker-up docker-down lint-workflows clean

all: build

## web: compile the SolidJS interface and stage it for embedding
web:
	cd web && deno install --frozen && deno task build
	find $(WEBUI) -mindepth 1 ! -name .gitkeep -delete
	cp -R web/dist/. $(WEBUI)/

## build: compile the interface and the single binary
build: web
	$(GOBUILD) -o $(BINARY) ./cmd/pier

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

## docker-build: build the container image locally (ghcr.io/allanadegas/pier:$PIER_VERSION)
docker-build:
	docker compose build

## docker-up: build the image and start Pier in a container (settings in .env)
docker-up:
	docker compose up --detach --build

## docker-down: stop the Pier container; what it deployed keeps running
docker-down:
	docker compose down

## lint-workflows: check the GitHub Actions workflows with actionlint (downloads it)
lint-workflows:
	go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12

clean:
	rm -rf bin web/dist
	find $(WEBUI) -mindepth 1 ! -name .gitkeep -delete
