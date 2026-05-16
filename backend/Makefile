.PHONY: build test test-int run lint migrate-up migrate-down clean

ifneq (,$(wildcard .env))
include .env
export
endif

BINARY=device-platform
CMD=./cmd/server

build:
	go build -o bin/$(BINARY) $(CMD)

run:
	go run $(CMD)

test:
	go test ./... -count=1 -short

test-int:
	go test ./... -count=1 -tags=integration -timeout=120s

lint:
	go vet ./...
	@if command -v staticcheck >/dev/null 2>&1; then \
		staticcheck ./...; \
	else \
		echo "staticcheck not found; skipping staticcheck"; \
	fi

migrate-up:
	migrate -path internal/storage/migrations -database "$(DATABASE_URL)" up

migrate-down:
	migrate -path internal/storage/migrations -database "$(DATABASE_URL)" down 1

clean:
	rm -rf bin/
