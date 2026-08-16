.PHONY: lint format format-fix test build build-tools generate proto ci hooks

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

build-tools:
	@for tool in files shell_exec message_send github; do \
		CGO_ENABLED=0 go build -o tools/$$tool/$$tool ./tools/$$tool/; \
	done

generate:
	mockery

ci: lint format test build

proto:
	buf generate

hooks:
	@lefthook install
