.PHONY: lint format format-fix test build generate ci hooks

lint:
	@if [ -f go.mod ]; then \
		go vet ./...; \
		golangci-lint run ./...; \
	else echo "no go.mod yet; skipping go vet"; fi

format:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; exit 1; }

format-fix:
	@gofmt -w .

test:
	@if [ -f go.mod ]; then go test ./...; else echo "no go.mod yet; skipping go test"; fi

build:
	@if [ -f go.mod ]; then go build ./...; else echo "no go.mod yet; skipping go build"; fi

ci: lint format test build

generate:
	@mockery

hooks:
	@lefthook install
