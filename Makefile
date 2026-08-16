.PHONY: lint format format-fix test build build-tools generate proto ci hooks smoke-local

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

smoke-local:
	@deploy/local/smoke.sh

proto:
	buf generate

hooks:
	@lefthook install
