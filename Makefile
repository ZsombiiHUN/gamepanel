.PHONY: all build web test run-dev
all: web build

web:
	cd web && pnpm install --frozen-lockfile && pnpm build

build:
	mkdir -p bin
	go build -o bin/panel ./cmd/panel
	go build -o bin/daemon ./cmd/daemon

test:
	go test -race -count=1 ./...
