.PHONY: lint format format-fix test test-privileged build build-tools build-images toolchain-image proto ci hooks

# The toolchain floor every go command below runs under: `go install tool@version`
# and `go run tool@version` take the toolchain from the tool's own `go.mod` rather
# than from this module, so a tool whose directive the machine's base toolchain
# already satisfies is built by that base toolchain, and a binary built below this
# module's `go` directive refuses to load its packages at package-load time
# (stack-go.md §Verification commands). `+auto` makes the value a minimum rather
# than a pin, so a tool needing more still gets more. The version is read out of
# go.mod, which owns it, rather than written here a second time (#705): a `go`
# line this cannot parse yields `go+auto`, which every go command refuses by
# name, so a reshaped go.mod stops the run instead of dropping the floor to
# whatever is installed.
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
PRIVILEGED_TEST_BIN ?= /tmp/sherlock-privileged.test
PRIVILEGED_BINARY_TEST_BIN ?= /tmp/sherlock-privileged-binary.test
PRIVILEGED_SHERLOCK_BIN ?= /tmp/sherlock-privileged

# golangci-lint's cache is keyed on file content, not path, and replays a result
# with the paths of the checkout that recorded it. A cache shared between
# checkouts therefore reports findings at files that may no longer exist, and a
# `//nolint` it cannot read there does not suppress them (#1000). Inside the
# tree, the cache is only ever this checkout's and goes when the checkout does.
# Assigned rather than defaulted, so an inherited shared value cannot return.
export GOLANGCI_LINT_CACHE = $(CURDIR)/.cache/golangci-lint

# The build and test entry points, and the vet run inside lint, each run
# `go list ./...` first, print the number of packages it lists beside the
# selector, and fail where that number is zero (stack-go.md §Verification commands). `go list` exits 0 when it
# lists nothing, so the recipe compares the number itself, and a module go list
# cannot read fails here with its own error rather than as an empty tree. The
# number counts what `./...` selected, not what the command after it built,
# tested or vetted, and the message says so. `$$selected` is set for the rest of
# the recipe line that expands this.
go_packages = packages=$$(go list ./...) || exit $$?; \
	selected=$$(printf '%s' "$$packages" | awk 'END{print NR}'); \
	test "$$selected" -gt 0 || { echo "$@: ./... selected no packages under $(CURDIR); the run covered nothing"; exit 1; }; \
	echo "$@: ./... selected $$selected packages"

lint:
	@$(go_packages); \
	echo "go vet ./..."; \
	go vet ./...
	golangci-lint run ./...

# The Go files a commit could carry: tracked, plus untracked files git does not
# ignore, leaving out what .gitignore, .git/info/exclude and the global
# excludes remove. `gofmt -l .` walks the filesystem instead, ignored paths
# included, so a checkout holding an ignored tree such as .local/ failed where
# CI and a clean worktree pass, and `gofmt -w .` rewrote that tree (#879).
# Untracked files stay in, so a new file not yet added fails here and not first
# in CI. A tracked file deleted from the tree is dropped, since gofmt cannot open
# it. NUL-separated, so no name is split or quoted on the way to gofmt; -z
# changes only the separator, so the output names the selection without it.
go_selection = --cached --others --exclude-standard -- '*.go'
go_files = git ls-files -z $(go_selection) | xargs -0 -r sh -c 'for f; do test ! -e "$$f" || printf "%s\0" "$$f"; done' sh

# The guard keeps a checkout git cannot read from reporting as one with no Go
# files.
git_guard = git rev-parse --is-inside-work-tree >/dev/null || { echo "$@: git cannot read $(CURDIR), and the Go files $@ reads are the ones git selects"; exit 1; }

# A file list holding no Go files prints nothing and exits zero, exactly like a
# clean run (#653). The count is what tells the two apart, and a count of zero
# is a failure. It counts the names go_files hands gofmt, so the number
# reported is the number gofmt read, and the output names how they were selected.
format:
	@$(git_guard); \
	checked=$$($(go_files) | tr -cd '\0' | wc -c | tr -d ' '); \
	test "$$checked" -gt 0 || { echo "format: no Go files in $(CURDIR), selected by: git ls-files $(go_selection); the check covered nothing"; exit 1; }; \
	unformatted=$$($(go_files) | xargs -0 -r gofmt -l); \
	test -z "$$unformatted" || { echo "$$unformatted"; echo "format: $$checked Go files checked, selected by: git ls-files $(go_selection); the files above are not formatted, run 'make format-fix'"; exit 1; }; \
	echo "format: $$checked Go files checked, selected by: git ls-files $(go_selection); all formatted"

format-fix:
	@$(git_guard); \
	$(go_files) | xargs -0 -r gofmt -w

test:
	@$(go_packages); \
	echo "go test ./..."; \
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

# The shell and shipped-binary privileged families assert the drop to the
# exec UID and the fresh network namespace, neither of which a process
# without privilege can reach (ADR 0008, ADR 0010). It is not one of the
# checks `make ci` runs, because `make ci` must run for a contributor who is
# not root; CI invokes this name in a job that is.
#
# The binary is built rather than run through `go test` because the child
# execs it: an untrusted child runs the platform binary as its resource-limit
# shim, and `go test` puts that binary in a private directory the child's own
# account cannot enter. SHERLOCK_PRIVILEGED_TESTS then makes every guard in the
# family report an absent privilege instead of skipping past it, so a run
# that cannot reach the boundary says so.
#
# setpriv narrows the run to the deployment's capability set before a single
# case executes. A check that holds more privilege than the deployment cannot
# fail the way the deployment fails, which is how #612 was true in production
# and green in CI at once (#620). The narrowing only ever drops, so whatever
# the runner granted, what the tests see is the deployment's set — and
# SHERLOCK_DEPLOYED_CAPS names it, so a run that skipped this line is a failure
# rather than a quieter pass.
#
# The workspace binary is built as build/workspace.Dockerfile builds it. That
# context holds no .git and stamps no VCS state; -buildvcs=false matches it
# here, where root's git refuses a checkout another account owns.
test-privileged:
	@test "$(words $(DEPLOYED_CAP_ADD)) $(words $(DEPLOYED_CAP_DROP))" = "1 1" || { echo "$(DEPLOYED_COMPOSE): want one cap_add and one cap_drop in the flow-sequence form, read $(words $(DEPLOYED_CAP_ADD)) and $(words $(DEPLOYED_CAP_DROP))"; exit 1; }
	@test "$(DEPLOYED_CAP_DROP)" = all || { echo "$(DEPLOYED_COMPOSE): cap_drop is [$(DEPLOYED_CAP_DROP)], not [ALL]: the capability set is only a whitelist while everything else is dropped"; exit 1; }
	@test "$$(id -u)" = 0 || { echo "test-privileged needs root: the drop to the exec UID is what it exercises (ADR 0008)"; exit 1; }
	go test -c -o $(PRIVILEGED_TEST_BIN) ./internal/workspace/shell/
	go test -c -o $(PRIVILEGED_BINARY_TEST_BIN) ./tests/
	CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags="-s -w" -o $(PRIVILEGED_SHERLOCK_BIN) ./cmd/sherlock/
	@chmod 0755 $(PRIVILEGED_TEST_BIN) $(PRIVILEGED_BINARY_TEST_BIN) $(PRIVILEGED_SHERLOCK_BIN)
	SHERLOCK_PRIVILEGED_TESTS=1 SHERLOCK_DEPLOYED_CAPS=$(DEPLOYED_CAP_ADD) \
		setpriv --bounding-set=$(DEPLOYED_CAP_SET) $(PRIVILEGED_TEST_BIN) -test.v
	SHERLOCK_PRIVILEGED_TESTS=1 SHERLOCK_DEPLOYED_CAPS=$(DEPLOYED_CAP_ADD) \
		SHERLOCK_TEST_BINARY=$(PRIVILEGED_SHERLOCK_BIN) \
		setpriv --bounding-set=$(DEPLOYED_CAP_SET) $(PRIVILEGED_BINARY_TEST_BIN) \
		-test.v -test.run='^TestEndToEnd_PrivilegedWorkspaceExec$$'

# `go build ./...` prints nothing on success and reports an empty match as a
# stderr warning rather than an error, so a module holding no packages exits
# zero exactly like a run that compiled every one (#715, #653). The count
# `go_packages` prints is what tells the two apart.
build:
	@$(go_packages); \
	echo "go build ./..."; \
	go build ./...

# A `for` loop's status is its last iteration's, so a tool that failed to
# compile left the target at exit 0 having written fewer executables than it
# names (#720). Each build exits the recipe on failure now, whatever its
# position, and what it wrote is checked for the execute bit, because
# `go build -o` also returns zero for a package that is not `main` and writes
# an archive the platform cannot spawn. The counter advances past both, so the
# number printed is the number of executables written (#653, #715). The set is
# the loop's list, not what tools/ holds, so a tool directory missing from it is
# never built, and the message names that list as the selection (#1095).
build-tools:
	@built=0; \
	for tool in file_read file_write file_list shell_exec message_send github_push github_open_issue github_open_pull_request github_comment image_generate; do \
		CGO_ENABLED=0 go build -o tools/$$tool/$$tool ./tools/$$tool/ || exit $$?; \
		test -x tools/$$tool/$$tool || { echo "build-tools: go build wrote tools/$$tool/$$tool without the execute bit; ./tools/$$tool/ must be a main package"; exit 1; }; \
		built=$$((built + 1)); \
	done; \
	echo "build-tools: $$built tools built, selected by: the tool list in the build-tools recipe"

# Builds the two platform images (ADR 0017, ADR 0021); nothing is pushed and no
# registry is named here. Each build context is the repository root because
# each build stage copies the module; what each context holds is $(dockerignores).
#
# The tag is $(REVISION), the same value the revision label carries, so an
# image cannot be tagged one commit and labelled another. It is not `:latest`,
# which the immutable repositories these images require (deployment.md §Images
# and tags) take once and never again — leaving the tag to be invented at a
# push command line, which is where a wrong one enters (#692).
#
# The guard fails closed: an empty REVISION builds `sherlock-agent:`, and docker
# reports the reference it was handed rather than the metadata that is missing.
#
# The second guard is the tree the label claims. `REVISION` is the checked-out
# commit and marks nothing, so a dirty tree is labelled and tagged with a
# commit whose tree was not what was built, and an immutable repository takes
# that first push rather than refusing it (ADR 0020, #695).
#
# What the tree is measured over is what docker reads: the build contexts — the
# source the images are built from — together with the Dockerfiles it is handed
# and the ignore files that decide what each context holds. An edit to a
# document, a test, or this file does not fail the build. This file is the
# exception on purpose: it invokes docker rather than being read by it, and a
# guard that refused the build of its own repair is the override-in-a-shell-
# profile shape #695 names.
#
# A context is an allow-list, `*` and then re-includes, so the paths it admits
# are the paths its ignore file re-includes: read out of the file that owns
# that list rather than restated here (#591). A file of any other shape reads
# as no paths at all, and the recipe fails on that rather than checking a
# pathspec that is not the context.
#
# There is one ignore file, because the two images share one context: the
# tools are baked into the agent image at build time (ADR 0021), so a path
# the tool sources need is a path the agent image's build reads.
DOCKERIGNORE = .dockerignore
dockerignores = $(DOCKERIGNORE)
ignore_patterns = $(shell sed -e '/^[[:space:]]*\#/d' -e '/^[[:space:]]*$$/d' $(1) 2>/dev/null)
context_paths_of = $(if $(filter *,$(firstword $(call ignore_patterns,$(1)))),$(patsubst !%,%,$(filter !%,$(call ignore_patterns,$(1)))))
context_paths = $(sort $(foreach file,$(dockerignores),$(call context_paths_of,$(file))))
shapeless_ignores = $(strip $(foreach file,$(dockerignores),$(if $(call context_paths_of,$(file)),,$(file))))

# The inputs docker reads that no context holds. A Dockerfile is handed to
# `-f` rather than copied, and an ignore file decides what the context is, so
# an edit to either changes the image while leaving the context clean (#698).
# The Dockerfile names are the values the recipe passes to `-f`, so the guard
# and the build read one owner rather than two copies (#591), and a name the
# tree does not hold fails `docker build -f` immediately — which is why these
# are not in $(missing_paths) beside the context's paths, where a pathspec
# matching nothing would report clean instead.
#
# $(dockerignores) are checked for a second reason: the guard derives its own
# scope from those files, so leaving one out would let an uncommitted edit
# redefine what a context is for a build the guard then reports clean.
AGENT_DOCKERFILE = build/agent.Dockerfile
WORKSPACE_DOCKERFILE = build/workspace.Dockerfile
build_inputs = $(AGENT_DOCKERFILE) $(WORKSPACE_DOCKERFILE) $(dockerignores)
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
# metadata and no readable ignore file to build.
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
	@test -z "$(revision_is_default)" || test -z "$(shapeless_ignores)" || { echo "[$(shapeless_ignores)]: want '*' and then one '!<path>' per admitted path: the tree is checked over the paths these files re-include, which are the build contexts only while everything is excluded first"; exit 1; }
	@test -z "$(revision_is_default)" || test -z "$(missing_paths)" || { echo "[$(dockerignores)]: re-include [$(missing_paths)], which $(CURDIR) does not hold: every admitted path is read as a git pathspec and one that matches nothing reports a clean tree"; exit 1; }
	@test -z "$(dirty_inputs)" || { echo "build-images: what docker reads differs from $(REVISION), which every image built here is labelled and tagged with, at [$(dirty_inputs)]. Commit those paths, or name what you are building: make build-images REVISION=<commit>"; exit 1; }
	@echo "build-images: $(if $(revision_is_default),what docker reads [$(checked_paths)] matches $(REVISION),REVISION=$(REVISION) was passed in and what docker reads is not checked against it)"
	$(BUILD_ENV) docker build -f $(AGENT_DOCKERFILE) --build-arg REVISION="$(REVISION)" -t sherlock-agent:$(REVISION) .
	$(BUILD_ENV) docker build -f $(WORKSPACE_DOCKERFILE) --build-arg REVISION="$(REVISION)" -t sherlock-workspace:$(REVISION) .

# The toolchain image the privileged CI job runs in, read out of the
# Dockerfile that owns the pin rather than restated in the workflow (#633).
# It is read out of $(WORKSPACE_DOCKERFILE) rather than a second literal
# naming that file (#705), because the family that job runs is the
# workspace's boundary (#620) and the toolchain it is checked under should
# be the toolchain that image is built with.
#
# One owner means one thing to move: Dependabot's docker ecosystem covers
# /build, so the digest bump it proposes carries the workflow with it and
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
