.PHONY: lint format format-fix test test-privileged build build-tools build-images toolchain-image proto ci hooks

# The revision the images are labelled and tagged with. CI passes the commit it
# built; the default is what a contributor's checkout is sitting on, and empty
# where there is no checkout to read. `build-images` reports that case itself,
# so git's message is dropped rather than repeated once per expansion.
REVISION ?= $(shell git rev-parse HEAD 2>/dev/null)

# Where `test-privileged` builds the test binary. Not under the repository:
# the file is written and run by root, and the worktree keeps no root-owned
# artifacts.
PRIVILEGED_TEST_BIN ?= /tmp/gagent-privileged.test

lint:
	go vet ./...
	golangci-lint run ./...

# gofmt walks the filesystem rather than the module, so a tree holding no Go
# files prints nothing and exits zero, exactly like a clean run (#653). The
# count is what tells the two apart, and a count of zero is a failure. The
# find expression restates gofmt's own rule for what a walk picks up — a
# non-directory whose name ends in `.go` and does not begin with `.` — so the
# number reported is the number `gofmt -l .` read.
format:
	@checked=$$(find . ! -type d -name '*.go' ! -name '.*' | awk 'END{print NR}'); \
	test "$$checked" -gt 0 || { echo "format: no Go files under $(CURDIR); the check covered nothing"; exit 1; }; \
	unformatted=$$(gofmt -l .); \
	test -z "$$unformatted" || { echo "$$unformatted"; echo "format: $$checked files checked; the files above are not formatted, run 'make format-fix'"; exit 1; }; \
	echo "format: $$checked files checked, all formatted"

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
#
# The tag is $(REVISION), the same value the revision label carries, so an
# image cannot be tagged one commit and labelled another. It is not `:latest`,
# which the immutable repositories these images require (deployment.md §Images
# and tags) take once and never again — leaving the tag to be invented at a
# push command line, which is where a wrong one enters (#692).
#
# The guard fails closed: an empty REVISION builds `gagent-agent:`, and docker
# reports the reference it was handed rather than the metadata that is missing.
build-images:
	@test -n "$(REVISION)" || { echo "build-images: REVISION is empty, and it is both the tag and the label; the default reads 'git rev-parse HEAD', which a checkout with no git metadata does not answer - pass one: make build-images REVISION=<commit>"; exit 1; }
	docker build -f build/agent.Dockerfile --build-arg REVISION="$(REVISION)" -t gagent-agent:$(REVISION) .
	docker build -f build/workspace.Dockerfile --build-arg REVISION="$(REVISION)" -t gagent-workspace:$(REVISION) .

# The toolchain image the privileged CI job runs in, read out of the
# Dockerfile that owns the pin rather than restated in the workflow (#633).
# The workspace image is the one it reads, because the family that job runs
# is the workspace's boundary (#620) and the toolchain it is checked under
# should be the toolchain that image is built with.
#
# One owner means one thing to move: Dependabot's docker ecosystem is scoped
# to /build, so the digest bump it proposes carries the workflow with it and
# there is no second copy left behind (#589, #575).
#
# `=` rather than `:=` so no other target pays for the sed, and the guard
# sits in the recipe for the same reason. It fails closed: an empty read
# would otherwise hand the job an image field that resolves to something
# else, and a doubled one would hand it two names.
TOOLCHAIN_DOCKERFILE = build/workspace.Dockerfile
toolchain_image = $(shell sed -n 's/^FROM \([^ ]*\) AS build$$/\1/p' $(TOOLCHAIN_DOCKERFILE))

toolchain-image:
	@test "$(words $(toolchain_image))" = 1 || { echo "$(TOOLCHAIN_DOCKERFILE): want exactly one 'FROM <image> AS build' line, read [$(toolchain_image)]"; exit 1; }
	@echo "$(toolchain_image)"

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
