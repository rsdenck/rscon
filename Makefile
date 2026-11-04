# RSCON Makefile
# =============
# make build       - build all binaries
# make test        - run all tests
# make vet         - run go vet
# make fmt         - run gofmt
# make migrate     - apply database migrations
# make migrate-down-N - roll back N migrations
# make clean       - remove build artifacts
# make help        - show this help

.PHONY: all build test vet fmt migrate clean help

all: build test

build:
	go build ./...
	go build -o rscond ./cmd/rscond/
	go build -o rscon ./cmd/rscon/

test:
	go test -count=1 ./...

vet:
	go vet ./...

fmt:
	gofmt -w ./cmd ./internal ./pkg
	gofmt -w web/src/App.tsx

migrate:
	go run ./cmd/rscond/ -config examples/config.yaml -migrate

migrate-down:
	@if [ -z "$(MIGRATE_N)" ]; then \
		echo "Usage: make migrate-down MIGRATE_N=2"; \
		exit 1; \
	fi
	go run ./cmd/rscond/ -config examples/config.yaml -migrate-down $(MIGRATE_N)

clean:
	rm -f rscond rscon
	rm -rf internal/web/dist

help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "} {printf "\033[36mmake %-20s\033[0m %s\n", $$1, $$2}'
