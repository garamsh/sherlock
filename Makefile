.PHONY: lint format format-fix test test-privileged build build-tools build-images proto ci hooks

# The revision the images are labelled with. CI passes the commit it built;
# the default is what a contributor's checkout is sitting on.
REVISION ?= $(shell git rev-parse HEAD)

# Where `test-privileged` builds the test binary. Not under the repository:
# the file is written and run by root, and the worktree keeps no root-owned
# artifacts.
PRIVILEGED_TEST_BIN ?= /tmp/gagent-privileged.test

lint:
	go vet ./...
	golangci-lint run ./...

format:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; exit 1; }

format-fix:
	@gofmt -w .

test:
	go test ./...

# The capability list the tests below run under is read out of the deployment
# rather than repeated here (#620): it has one owner, `cap_add` in
# $(DEPLOYED_COMPOSE), and a second copy in this file or in the workflow would
# be the shape #591 and #589 record — a claim in one file about a value another
# file owns.
#
# `caps` reads one key's flow sequence and lowercases the names. It matches one
# line per occurrence, so `$(words …)` counts them: two services share that file
# and only the workspace drops capabilities, so a key found twice means the
# wrong service's list could be read. The guards are in the recipe, not here, so
# no other target pays for them.
DEPLOYED_COMPOSE = deploy/local/docker-compose.yml
COMMA := ,
caps = $(shell sed -n "s/^[[:space:]]*$(1):[[:space:]]*\[\(.*\)\][[:space:]]*$$/\1/p" $(DEPLOYED_COMPOSE) | tr -d " \"'" | tr 'A-Z' 'a-z')
DEPLOYED_CAP_ADD = $(call caps,cap_add)
DEPLOYED_CAP_DROP = $(call caps,cap_drop)
DEPLOYED_CAP_SET = -all,+$(subst $(COMMA),$(COMMA)+,$(DEPLOYED_CAP_ADD))

# The executor's privileged family: the cases that assert the drop to the
# exec UID and the fresh network namespace, neither of which a process
# without privilege can reach (ADR 0008, ADR 0010). It is not one of the
# checks `make ci` runs, because `make ci` must run for a contributor who is
# not root; CI invokes this name in a job that is.
#
# The binary is built rather than run through `go test` because the child
# execs it: an untrusted child runs the platform binary as its resource-limit
# shim, and `go test` puts that binary in a private directory the child's own
# account cannot enter. GAGENT_PRIVILEGED_TESTS then makes every guard in the
# family report an absent privilege instead of skipping past it, so a run
# that cannot reach the boundary says so.
#
# setpriv narrows the run to the deployment's capability set before a single
# case executes. A check that holds more privilege than the deployment cannot
# fail the way the deployment fails, which is how #612 was true in production
# and green in CI at once (#620). The narrowing only ever drops, so whatever
# the runner granted, what the tests see is the deployment's set — and
# GAGENT_DEPLOYED_CAPS names it, so a run that skipped this line is a failure
# rather than a quieter pass.
test-privileged:
	@test "$(words $(DEPLOYED_CAP_ADD)) $(words $(DEPLOYED_CAP_DROP))" = "1 1" || { echo "$(DEPLOYED_COMPOSE): want one cap_add and one cap_drop in the flow-sequence form, read $(words $(DEPLOYED_CAP_ADD)) and $(words $(DEPLOYED_CAP_DROP))"; exit 1; }
	@test "$(DEPLOYED_CAP_DROP)" = all || { echo "$(DEPLOYED_COMPOSE): cap_drop is [$(DEPLOYED_CAP_DROP)], not [ALL]: the capability set is only a whitelist while everything else is dropped"; exit 1; }
	@test "$$(id -u)" = 0 || { echo "test-privileged needs root: the drop to the exec UID is what it exercises (ADR 0008)"; exit 1; }
	go test -c -o $(PRIVILEGED_TEST_BIN) ./internal/workspace/shell/
	@chmod 0755 $(PRIVILEGED_TEST_BIN)
	GAGENT_PRIVILEGED_TESTS=1 GAGENT_DEPLOYED_CAPS=$(DEPLOYED_CAP_ADD) \
		setpriv --bounding-set=$(DEPLOYED_CAP_SET) $(PRIVILEGED_TEST_BIN)

build:
	go build ./...

build-tools:
	@for tool in files shell_exec message_send github; do \
		CGO_ENABLED=0 go build -o tools/$$tool/$$tool ./tools/$$tool/; \
	done

# Builds both platform images (ADR 0017); nothing is pushed and no registry is
# named here. The build context is the repository root because the build stage
# copies the module.
build-images:
	docker build -f build/agent.Dockerfile --build-arg REVISION="$(REVISION)" -t gagent-agent .
	docker build -f build/workspace.Dockerfile --build-arg REVISION="$(REVISION)" -t gagent-workspace .

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
