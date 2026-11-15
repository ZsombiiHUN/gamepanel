.PHONY: all build web test e2e run-dev
all: web build

web:
	cd web && pnpm install --frozen-lockfile && pnpm build

build:
	mkdir -p bin
	go build -o bin/panel ./cmd/panel
	go build -o bin/daemon ./cmd/daemon

test:
	go test -race -count=1 ./...

# Browser tests: builds the UI, then runs web/e2e against a throwaway panel with a fake daemon.
# First time on a machine: cd web && pnpm e2e:install
e2e:
	cd web && pnpm install --frozen-lockfile && pnpm e2e
