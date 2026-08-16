.PHONY: lint format format-fix test build build-tools generate proto ci hooks

lint:
	go vet ./...
	golangci-lint run ./...

format:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; exit 1; }

format-fix:
	@gofmt -w .

test:
	go test ./...
# Which entry point carries a documentation check is open in #397. Until it is
# settled these sit behind `test` rather than under a fifth name; moving them is
# moving these two lines.
	@python3 citations/reference_check.py
	@python3 citations/reference_check_fixtures.py

build:
	go build ./...

build-tools:
	@for tool in files shell_exec message_send github; do \
		CGO_ENABLED=0 go build -o tools/$$tool/$$tool ./tools/$$tool/; \
	done

generate:
	mockery

ci: lint format test build

proto:
	buf lint
	buf generate
# Scoped to the generated stubs, so unrelated uncommitted work does not fail the
# target, and read from status rather than diff, so a stub that appeared counts as
# one that moved. A stub named here is not committed; committing it clears this.
	@test -z "$$(git status --porcelain -- '*.pb.go')" || { git status --porcelain -- '*.pb.go'; exit 1; }

hooks:
	@lefthook install
