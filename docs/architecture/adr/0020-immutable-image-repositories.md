# ADR 0020: The image repositories are immutable, and a consumer references an image by digest

> Status: accepted
> Date: 2026-08-24

## Context

gagent publishes two images — `garam/gagent-agent` and `garam/gagent-workspace`, built from `build/agent.Dockerfile` and `build/workspace.Dockerfile` — into ECR repositories in an AWS account this repository does not own and cannot read. What that account holds is `garamsh/infra`'s to set. What gagent requires of it is this project's to state, and `deployment.md` had been stating it as a description of three settings — immutability, scan-on-push and a lifecycle policy — rather than as a requirement. One of the three was already false when the description was read: `garamsh/infra` declares no lifecycle policy on any repository in that account and records the absence as a deliberate open question rather than an oversight (reported by the garam PM, 2026-08-23).

That description was load-bearing while it lasted. `garamsh/infra` planned a mutability flip across five ECR repositories, read `IMMUTABLE` here, and restructured the change so each application states its own repository setting instead (2026-08-23). It did that work by being accurate on the day it was read, which is what a description of another project's account cannot be relied on to stay.

What makes immutability a requirement rather than a preference is in this repository. `Makefile` derives `REVISION` from the checked-out commit and does not mark a dirty tree, and both Dockerfiles carry that value into `org.opencontainers.image.revision`. A build of a dirty tree is therefore labelled with a clean commit whose tree was not what was built.

## Decision

**Every gagent image repository is created `IMMUTABLE` and is never made mutable; a tag in one names a single commit and never moves; gagent requires neither a lifecycle policy nor image scanning; and a consumer references an image by digest with `imagePullPolicy: Always`.**

### Immutable, bring-up and production alike

Push a dirty-tree build under a tag some clean build already holds, and a mutable repository takes it: the overwrite is silent, and the tag now names a commit whose tree was not what was built. An immutable repository refuses it: ECR's `PutImage` answers `ImageTagAlreadyExistsException`. Immutability converts a silent mislabel into a refused push, which is the shape ADR 0010 requires of the executor — it never silently upgrades, because a capability reported unavailable is better than a change nobody sees.

The second reason is that the setting is retroactive. Tag immutability is a property of the repository and not of a tag, and ECR records nothing about the policy a tag was pushed under, so a repository that is ever mutable leaves every tag it holds a readable label rather than an identifier — the ones pushed while it was immutable included (from the gitops PM, 2026-08-23). A tag identifies a build here only because these repositories have never been mutable, which makes this a requirement on their creation and not only on their present setting.

### A tag names one commit and never moves

This follows from immutability rather than standing beside it: a moving tag cannot exist in a repository that refuses a second push of a name. There is no `dev` and no `latest`. It is stated anyway because garam chose the opposite for its own bring-up repositories, and a reader comparing the two should see a deliberate difference rather than an oversight.

The release scheme `deployment.md` §Images and tags records is such a tag — a `v`-prefixed git tag becomes the image tag without the `v` — so re-pointing that git tag and releasing again is refused rather than silently overwritten.

### Neither a lifecycle policy nor image scanning is required

The clause claiming a lifecycle policy is removed rather than made true. Bring-up repositories are emptied and removed once a release path publishes, and production repositories hold released versions, where expiry is the wrong behaviour. Scan-on-push goes the same way and for the same reason rather than for a judgement about scanning: gagent has no requirement to state about it, and what it had was a description of the account. Either control the account runs for its own hygiene is `garamsh/infra`'s to decide; gagent depends on neither and describes neither.

### A consumer references the digest and pulls always

A tag's stability rests on a repository setting that can be changed retroactively, and a consumer cannot check from outside that it never was. So the reference is the digest and the tag is what a human reads. That is not in tension with requiring the tag to identify a build: gagent requires it of its own repositories, and a consumer who cannot verify the requirement was kept does not rest on it.

`imagePullPolicy: Always` for a reason specific to these repositories rather than as a general preference. They are emptied when a release path publishes, so the failure to be loud about is a reference that stops resolving. The default `IfNotPresent` turns that into a per-node stale-cache fault that presents as the node's; `Always` makes it uniform and immediate.

## Consequences

- **A reader who cannot see the account can act on `deployment.md` §Images and tags, and a reviewer can check it against the account.** The section states what the repositories owe this project; nothing in it goes stale when a setting there changes, because it describes no setting.
- **Immutability bounds the dirty-tree failure without preventing it.** The refusal fires on the second push of a name, so the first push of a dirty build succeeds and is indistinguishable from a clean one — nothing marks a dirty tree, `REVISION` being the checked-out commit either way.
- **Nothing produces a tag this rule can be applied to yet.** `make build-images` names what it builds `gagent-agent` and `gagent-workspace`, which is `:latest` — the moving tag this record forbids — so the pushed tag would be invented at the command line. #692 owns that, and the release workflow and its IAM role still do not exist (`deployment.md` §Images and tags).
- **Nothing here enforces any of this**, which is why it is stated as a requirement. The obligations bind whoever creates the repositories and whoever pushes to them; this repository cannot read the setting and does not try.
- **A bad image at a taken tag is replaced by a new tag, never overwritten.** That cost is accepted: it is the same refusal the record is chosen for.
- **The account's own hygiene is unconstrained by this record.** It states what gagent requires, not what the account may hold, so a lifecycle or scanning policy `garamsh/infra` adds for reasons of its own contradicts nothing here and needs no change to this document.

## Rejected alternatives

- **Making the description true** — adding the lifecycle policy so the sentence holds. It leaves a description of another project's account in this document, free to go stale again with nothing here able to notice. The false clause was the visible instance; the form was the defect (#690).
- **`MUTABLE` repositories with digest-pinned consumers.** A consumer that references the digest is unharmed by a moved tag, so mutability would cost it nothing. Rejected for what it costs elsewhere: the tag stops naming a build for anyone who reads one, and the silent overwrite of a dirty-tree build is a failure nothing else on the path notices.
- **A moving bring-up tag (`dev`), as garam uses for its own repositories.** It cannot exist in an immutable repository, and the argument for immutability is strongest exactly there, where dirty-tree builds are most likely.
- **`imagePullPolicy: IfNotPresent`, the orchestrator's default.** It turns a reference that stopped resolving into a per-node stale-cache fault that presents as the node's.
- **Querying the account to settle what it currently holds.** The required form does not depend on what is there, so the answer would change nothing — and it is not this repository's account to inspect.
