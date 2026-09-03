.PHONY: lint format format-fix test test-privileged build build-tools build-images toolchain-image proto ci hooks

# The toolchain floor every go command below runs under: `go install tool@version`
# and `go run tool@version` take the toolchain from the tool's own `go.mod` rather
# than from this module, so a tool whose directive the machine's base toolchain
# already satisfies is built by that base toolchain, and a binary built below this
# module's `go` directive refuses to load its packages at package-load time
# (stack-go.md §9). `+auto` makes the value a minimum rather than a pin, so a tool
# needing more still gets more. The version is read out of go.mod, which owns it,
# rather than written here a second time (#705): a `go` line this cannot parse
# yields `go+auto`, which every go command refuses by name, so a reshaped go.mod
# stops the run instead of dropping the floor to whatever is installed.
go_directive := $(shell sed -n 's/^go \([0-9][0-9.]*\)$$/\1/p' go.mod)
export GOTOOLCHAIN := go$(go_directive)+auto

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

# `go build ./...` prints nothing on success and reports an empty match as a
# stderr warning rather than an error, so a module holding no packages exits
# zero exactly like a run that compiled every one (#715, #653). The count is
# what tells the two apart, and a count of zero is a failure. It comes from
# `go list ./...`, which resolves the same pattern in the same module, so the
# number reported is the number `go build ./...` compiled and a module go list
# cannot read fails here with its own error rather than as an empty tree.
build:
	@packages=$$(go list ./...) || exit $$?; \
	built=$$(printf '%s' "$$packages" | awk 'END{print NR}'); \
	test "$$built" -gt 0 || { echo "build: no packages under $(CURDIR); the build covered nothing"; exit 1; }; \
	go build ./... || exit $$?; \
	echo "build: $$built packages built"

# A `for` loop's status is its last iteration's, so a tool that failed to
# compile left the target at exit 0 having written fewer executables than it
# names (#720). Each build exits the recipe on failure now, whatever its
# position, and what it wrote is checked for the execute bit, because
# `go build -o` also returns zero for a package that is not `main` and writes
# an archive the platform cannot spawn. The counter advances past both, so the
# number printed is the number of executables written (#653, #715).
build-tools:
	@built=0; \
	for tool in files shell_exec message_send github; do \
		CGO_ENABLED=0 go build -o tools/$$tool/$$tool ./tools/$$tool/ || exit $$?; \
		test -x tools/$$tool/$$tool || { echo "build-tools: go build wrote tools/$$tool/$$tool without the execute bit; ./tools/$$tool/ must be a main package"; exit 1; }; \
		built=$$((built + 1)); \
	done; \
	echo "build-tools: $$built tools built"

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
#
# The second guard is the tree the label claims. `REVISION` is the checked-out
# commit and marks nothing, so a dirty tree is labelled and tagged with a
# commit whose tree was not what was built, and an immutable repository takes
# that first push rather than refusing it (ADR 0020, #695).
#
# What the tree is measured over is what docker reads: the build context — the
# source the images are built from — together with the Dockerfile it is handed
# and the $(DOCKERIGNORE) that decides what the context holds. An edit to a
# document, a test, or this file does not fail the build. This file is the
# exception on purpose: it invokes docker rather than being read by it, and a
# guard that refused the build of its own repair is the override-in-a-shell-
# profile shape #695 names.
#
# The context is an allow-list, `*` and then re-includes, so the paths it
# admits are the paths $(DOCKERIGNORE) re-includes: read out of the file that
# owns that list rather than restated here (#591). A file of any other shape
# reads as no paths at all, and the recipe fails on that rather than checking a
# pathspec that is not the context.
DOCKERIGNORE = .dockerignore
ignore_patterns = $(shell sed -e '/^[[:space:]]*\#/d' -e '/^[[:space:]]*$$/d' $(DOCKERIGNORE) 2>/dev/null)
allow_list = $(filter *,$(firstword $(ignore_patterns)))
context_paths = $(if $(allow_list),$(patsubst !%,%,$(filter !%,$(ignore_patterns))))

# The two inputs docker reads that the context does not hold. A Dockerfile is
# handed to `-f` rather than copied, and $(DOCKERIGNORE) decides what the
# context is, so an edit to either changes the image while leaving the context
# clean (#698). The Dockerfile names are the values the recipe passes to `-f`,
# so the guard and the build read one owner rather than two copies (#591), and
# a name the tree does not hold fails `docker build -f` immediately — which is
# why these are not in $(missing_paths) beside the context's paths, where a
# pathspec matching nothing would report clean instead.
#
# $(DOCKERIGNORE) is checked for a second reason: the guard derives its own
# scope from that file, so leaving it out would let an uncommitted edit
# redefine what the context is for a build the guard then reports clean.
AGENT_DOCKERFILE = build/agent.Dockerfile
WORKSPACE_DOCKERFILE = build/workspace.Dockerfile
build_inputs = $(AGENT_DOCKERFILE) $(WORKSPACE_DOCKERFILE) $(DOCKERIGNORE)
checked_paths = $(context_paths) $(build_inputs)

# Read paths that are not in the tree. They are handed to git as pathspecs,
# and one git cannot answer for — a leading `/`, a name that moved — matches
# nothing and reports a clean tree, so the recipe refuses them rather than
# checking less than it says it checked.
missing_paths = $(filter-out $(wildcard $(context_paths)),$(context_paths))

# The check runs on the default revision only. An explicit one is a claim its
# author made about what they are building, and CI makes it every run
# (.github/workflows/ci.yml); the default is a claim nobody made. Everything
# the check reads hangs off this, so a caller who passed a commit needs no git
# metadata and no readable $(DOCKERIGNORE) to build.
revision_is_default = $(filter file,$(origin REVISION))

# The checked paths that differ from what REVISION names, status column cut
# off so the message names paths. Untracked files are among them: the context
# admits a path whether git tracks it or not. An empty REVISION is a checkout
# git could not read at all, which the guard below reports, so nothing asks it
# a second question here.
dirty_inputs = $(if $(and $(REVISION),$(revision_is_default),$(context_paths)),$(shell git status --porcelain -- $(checked_paths) | cut -c4-))

# BUILDX_NO_DEFAULT_ATTESTATIONS turns off the provenance and SBOM attestations
# buildx attaches by default. An attestation is carried as a manifest of its own
# beside the image's and referenced from an index, so attaching one makes the
# build export an index — and a consumer pinning the digest then pins that index
# rather than the image (ADR 0020, deployment.md §Images and tags). Whether the
# default applies at all depends on the host: a daemon on the classic image store
# cannot hold an index and drops them, one on the containerd store keeps them, so
# the same command produces different objects on two contributors' machines. It
# is set as an environment variable rather than as `--provenance=false
# --sbom=false` because a builder that does not know it ignores it, where the
# flags would fail the build.
BUILD_ENV = BUILDX_NO_DEFAULT_ATTESTATIONS=1

build-images:
	@test -n "$(REVISION)" || { echo "build-images: REVISION is empty, and it is both the tag and the label; the default reads 'git rev-parse HEAD', which a checkout with no git metadata does not answer - pass one: make build-images REVISION=<commit>"; exit 1; }
	@test -z "$(revision_is_default)" || test -n "$(context_paths)" || { echo "$(DOCKERIGNORE): want '*' and then one '!<path>' per admitted path, read [$(ignore_patterns)]: the tree is checked over the paths this file re-includes, which are the build context only while everything is excluded first"; exit 1; }
	@test -z "$(revision_is_default)" || test -z "$(missing_paths)" || { echo "$(DOCKERIGNORE): re-includes [$(missing_paths)], which $(CURDIR) does not hold: every admitted path is read as a git pathspec and one that matches nothing reports a clean tree"; exit 1; }
	@test -z "$(dirty_inputs)" || { echo "build-images: what docker reads differs from $(REVISION), which every image built here is labelled and tagged with, at [$(dirty_inputs)]. Commit those paths, or name what you are building: make build-images REVISION=<commit>"; exit 1; }
	@echo "build-images: $(if $(revision_is_default),what docker reads [$(checked_paths)] matches $(REVISION),REVISION=$(REVISION) was passed in and what docker reads is not checked against it)"
	$(BUILD_ENV) docker build -f $(AGENT_DOCKERFILE) --build-arg REVISION="$(REVISION)" -t gagent-agent:$(REVISION) .
	$(BUILD_ENV) docker build -f $(WORKSPACE_DOCKERFILE) --build-arg REVISION="$(REVISION)" -t gagent-workspace:$(REVISION) .

# The toolchain image the privileged CI job runs in, read out of the
# Dockerfile that owns the pin rather than restated in the workflow (#633).
# It is read out of $(WORKSPACE_DOCKERFILE) rather than a second literal
# naming that file (#705), because the family that job runs is the
# workspace's boundary (#620) and the toolchain it is checked under should
# be the toolchain that image is built with.
#
# One owner means one thing to move: Dependabot's docker ecosystem is scoped
# to /build, so the digest bump it proposes carries the workflow with it and
# there is no second copy left behind (#589, #575).
#
# `=` rather than `:=` so no other target pays for the sed, and the guard
# sits in the recipe for the same reason. It fails closed: an empty read
# would otherwise hand the job an image field that resolves to something
# else, and a doubled one would hand it two names.
TOOLCHAIN_DOCKERFILE = $(WORKSPACE_DOCKERFILE)
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
