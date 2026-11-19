.PHONY: all build web test cover lint fmt vuln check e2e run-dev openapi
all: web build

# Tool versions, pinned so every machine and CI get the same results.
GOLANGCI_LINT := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0
GOVULNCHECK   := go run golang.org/x/vuln/cmd/govulncheck@v1.10.0

# Statement coverage of ./internal and ./cmd across all tests (the e2e tests
# cover the API from another package). Raise it when coverage rises; never lower it.
COVER_MIN := 72

web:
	cd web && pnpm install --frozen-lockfile && pnpm build

build:
	mkdir -p bin
	go build -o bin/panel ./cmd/panel
	go build -o bin/daemon ./cmd/daemon

test:
	go test -race -count=1 ./...

# Tests with coverage; fails below COVER_MIN.
cover:
	go test -race -count=1 -coverpkg=./internal/...,./cmd/... -coverprofile=coverage.out ./...
	@go tool cover -func=coverage.out | awk -v min=$(COVER_MIN) '/^total:/ { sub("%", "", $$3); printf "coverage %.1f%% (floor %d%%)\n", $$3, min; if ($$3 + 0 < min) { print "coverage fell below the floor"; exit 1 } }'

# Go linters and formatting (.golangci.yml).
lint:
	$(GOLANGCI_LINT) run ./...

# Rewrites Go files with gofumpt and goimports.
fmt:
	$(GOLANGCI_LINT) fmt ./...

# Known vulnerabilities in the code paths the binaries actually use.
vuln:
	$(GOVULNCHECK) ./...

# Everything CI checks, short of the browser and Docker tests.
check: lint vuln cover
	cd web && pnpm install --frozen-lockfile && pnpm check

# Browser tests: builds the UI, then runs web/e2e against a throwaway panel with a fake daemon.
# First time on a machine: cd web && pnpm e2e:install
e2e:
	cd web && pnpm install --frozen-lockfile && pnpm e2e

# Regenerate web/openapi.json and the UI's API types after changing the API.
openapi:
	go run ./cmd/panel openapi > web/openapi.json
	cd web && pnpm gen:api
