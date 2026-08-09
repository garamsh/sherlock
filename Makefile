.PHONY: lint format format-fix test build generate proto ci hooks

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

proto:
	protoc --go_out=. --go-grpc_out=. --go_opt=paths=source_relative --go-grpc_opt=paths=source_relative internal/workspace/workspace.proto


hooks:
	@lefthook install
