# CLAUDE.md

This file provides guidance to AI coding agents — Claude Code (claude.ai/code) and vendor-neutral tools such as Codex, OpenCode, Cursor, and Copilot — when working with code in this repository.

## Agent instruction files

`CLAUDE.md` and `AGENTS.md` are kept **byte-identical**. `CLAUDE.md` is what Claude Code reads; `AGENTS.md` is what vendor-neutral agent tools read — Codex, OpenCode, Cursor, Copilot, and whatever follows them. Two real files, deliberately not a symlink: not every tool resolves one.

**After editing either file, copy it over the other — don't repeat the edit by hand:**

```bash
cp CLAUDE.md AGENTS.md   # or the reverse, whichever you just edited
```

Retyping a change is exactly how the two drift; one reflowed line or reworded clause is enough. `diff CLAUDE.md AGENTS.md` must print nothing. If it ever does, treat it as a defect and fix it by letting one file win wholesale — never by merging them.

## What this repo is

A first-party **OpenTofu / Terraform provider for [TCPShield](https://tcpshield.com)** (DDoS protection for Minecraft and other game servers), owned by kirchDev. It covers the complete [TCPShield REST API](https://swagger.tcpshield.com/) — networks, backend sets, domains, the IP/ASN/country firewalls, Bedrock tunnels, Sentry tunnels and their firewall rules, plus the account.

- **Provider type (HCL):** `tcpshield` → `provider "tcpshield" {}`
- **OpenTofu registry address:** `kirchdev/tcpshield`
- **Go module:** `github.com/kirchDev/terraform-provider-tcpshield`
- **SDK:** `terraform-plugin-framework` (NOT the legacy SDKv2)

The repo name format `NAMESPACE/terraform-provider-NAME` is **mandatory** for the OpenTofu/Terraform registry — it's not a style choice.

**Deliberately left out:** `PATCH /user/email` and `POST /user/password` (credentials don't belong in state), `GET /user/apikey` (the provider already holds it — exposing it copies a secret into state), `POST /user/apikey/regenerate` (invalidates the provider's own key mid-apply). Don't add them without a conversation.

## Current state

10 resources and 24 data sources, every spec endpoint mapped. Verification differs by area:

- **Live-verified (2026-10)** — `TestLiveNetworkLifecycle` runs create → update → import → destroy against the real API for networks (every setting except `webhook_url` and `verification_server`), backend sets, domains and all three firewalls, and reads every network-scoped data source plus `tcpshield_domain_preverify` / `tcpshield_domain_verification` along the way. `TestLiveUserProfile` renames the account and puts the name back; it never touches the phone (see below) and skips on an account without one.
- **Plan-gated, mock-only** (per [tcpshield.com/plans](https://tcpshield.com/plans); the test account is Pro) — Bedrock tunnels need Geyser support (Premium+; Pro answers `You cannot add any more bedrock tunnels.`), `tcpshield_analytics` needs advanced analytics (Premium+; Pro answers 403), `verification_server` is Enterprise only.
- **Mock-only, cause unknown** — Sentry tunnels + their firewall rules and the tunnel-scoped data sources: creation failed server-side on the test account (`Allocating not used IP address failed.`); the plans page does not mention Sentry at all.
- `webhook_url` needs a real Discord/Slack webhook to test.
- The README's _Test status_ table is the user-facing version of this list — keep the two in step. `TestEveryTypeIsCoveredByAMockTest` fails when a resource or data source appears in no acceptance-test configuration.

Two layers coexist:

- **Node meta layer** (from the `scaffold` template): pnpm + oxlint + oxfmt + husky + commitlint + release-please + CI/CodeQL/Dependabot + issue/PR templates. Gates config/docs (JSON/YAML/MD).
- **Go provider**: `internal/client/` (the REST client) and `internal/provider/` (resources, data sources, tests).

## API shape (verified against the live API, 2026-10)

The published OpenAPI spec is loose — several single reads are documented as arrays and most writes as "200, no body". What the API really does:

- Base `https://api.tcpshield.com`, auth header **`X-API-Key`**. Plain JSON, no envelope — except `/user/summary` (`{"data": …}`) and **creates**, which answer `{"data": {"id": n}, "message": …, "status": 0}`. `client.CreatedID` tolerates every shape; when no id comes back the resource falls back to `findNewest` (list + match, highest id).
- **Errors** are `{"error": "…"}` with a real status (404 for a missing entity). Validation errors put an **object** in `error` (`{"UpdateDomainRequest.Name": "Name is a required field"}`); `errorMessage` renders it as sorted `field: message` pairs. An empty collection may come back as `null`.
- **Backend sets:** the listing has no `backends`, the single read has `backends` but **no `id`** — `fill` takes the id from the request path, and `tcpshield_backend_sets` reads each set once more. The **PATCH is a PUT**: a field left out resets to its zero value (`load_balancing_mode` → 0) and a body without `backends` is answered "identical, no update necessary" and ignored — so create and update send the whole set.
- **Domains** and **Bedrock tunnels:** PATCH requires every field (`name`, `backend_set_id`, `bac` / the full create body) — always send all.
- **Networks:** PATCH is truly partial, and every setting in the response is patchable. But it **silently ignores zero values** for the ints and for `mitigation_message` / `antivpn_kick_msg` — sending one leaves the old value live and fails the apply with an inconsistent result. Validators (1–100, 1–3600, 1–600, non-empty) turn that into a plan-time error. Booleans and the `offline_*` texts _can_ be reset to `false`/`""`. `webhook_url` must be Discord or Slack; `verification_server` is plan-gated. `POST /networks` is limited by plan (Pro: one network).
- **Firewalls** have no single read and no update: read through the list, every attribute `RequiresReplace`. The IP firewall wants real CIDR (a bare IP is a 500 `Invalid Cidr formatting`) and keeps host bits as written; `cidrValidator` checks at plan time. The country create response carries `country_id: 0`, the list the resolved code/name — always read back from the list.
- **Domain (pre)verification** answers **403** `Failed to verify domain.` when DNS is not set up. The data sources report that as `verified = false`, not as an error.
- **Sentry tunnel** update is a PUT requiring `name`, `endpoint_public_key`, `endpoint_peer`, `bind_port`; `location` is create-only. `/tunnels/getBindPort` returns a bare number that changes on every call.
- **`/user/updateGeneralInfo`** requires `firstname`, `lastname` and `phone` on every call and refuses an empty one (`UpdateUserGeneralSettings.PhoneNumber: PhoneNumber is a required field`) — `""`, `null` and leaving it out all fail. A phone number, once set, **cannot be removed through the API**; all three attributes are Required + non-empty. Never let a test set a phone it cannot take back.
- **DELETE of a missing entity** is a 404; `client.Delete` treats it as success.
- **Plan limits** surface only at apply: networks (Pro 1, Premium 3, Enterprise 10), IP firewall entries (Pro 5, Enterprise 50), Bedrock tunnels, analytics and the verification server. The descriptions name them; the provider does not know the account's plan.
- Rate limits are undocumented; the client retries `429` (honouring `Retry-After`) and `5xx` on reads only — a write that 5xx'd may have landed.

## Commands

Go (via `GNUmakefile`; needs **Go ≥ 1.27** — `go.mod` sets `go 1.27.0`; `golang.org/x/net` and friends need at least 1.26). `go.mod` also pins `toolchain go1.27.2`: `setup-go` in CI and the release reads that line, and builds with an older 1.27 patch could ship a standard library with since-fixed vulnerabilities. Raise it when a newer Go patch fixes the standard library; keep it at a version the local `golangci-lint` was built with or newer, and one the CI `golangci-lint` (the `_ci-go.yml` default in `kirchDev/workflows`, v2.14.0 since v0.14.1) supports.

| Command        | What it does                                                                |
| :------------- | :-------------------------------------------------------------------------- |
| `make build`   | `go build -o terraform-provider-tcpshield`                                  |
| `make tidy`    | `go mod tidy`                                                               |
| `make fmt`     | `gofmt -s -w .`                                                             |
| `make vet`     | `go vet ./...`                                                              |
| `make lint`    | `golangci-lint run`                                                         |
| `make docs`    | render `docs/` from the schema (build + export + tfplugindocs)              |
| `make test`    | `go test ./...` — client unit tests, examples coverage                      |
| `make testacc` | `TF_ACC=1 go test ./...` — mock acceptance tests; needs a TF binary, no key |
| `make vuln`    | `govulncheck ./...` — reachable vulnerabilities, standard library included  |

**Live tests** (real API, real entities — a whole network is created and destroyed; needs an account with a free network slot):

```bash
TF_ACC=1 TCPSHIELD_LIVE=1 TCPSHIELD_API_KEY=… go test ./internal/provider -run TestLive -v
```

Node meta layer: `pnpm install` (wires husky hooks), `pnpm check` / `pnpm check:fix`. CI (`.github/workflows/ci.yml`) runs the central **Go job** (build·vet·gofmt·golangci-lint·test + `TF_ACC` mock acceptance tests) and the **check job** (`pnpm check`).

> [!NOTE]
> Generated files are excluded from oxfmt via `.prettierignore` (`docs/` from tfplugindocs, `CHANGELOG.md` from release-please). Don't reformat them — the next generation undoes it.

## Patterns & gotchas

- One file per entity (resource + its data sources), one `api*` wire struct and one `*Model`; constructors `New{Camel}Resource` / `New{Camel}DataSource`; TypeName `tcpshield_{snake}`; everything registered in `provider.go`. Data sources reuse the resource models.
- `firewall_resource.go` serves all three firewalls from one implementation; a `firewallKind` names what differs.
- **Ids are `Int64`**, import ids are `<parent_id>/<id>` (`parseImportID`). `tcpshield_user_profile` is a singleton: create adopts, delete forgets.
- **Optional + Computed + `UseStateForUnknown`** on every setting: left out of the config, it keeps the live value and never plans as `(known after apply)`.
- **State is never the write's answer** — every create/update reads the entity back. The write responses are inconsistent (nanosecond vs millisecond timestamps, `country_id: 0`).
- `mock_test.go` reproduces every quirk above; when the live API surprises you, teach the mock first, then fix the provider. `TestAccMockNetworkLifecycle` runs the live test's configuration against it.
- `examples_test.go` fails when a resource/data source has no example, or an example has no resource.

### Manual smoke test (loads the binary in OpenTofu)

```bash
make build
cat > /tmp/tcpshield.tfrc <<EOF
provider_installation {
  dev_overrides { "kirchdev/tcpshield" = "$(pwd)" }
  direct {}
}
EOF
TF_CLI_CONFIG_FILE=/tmp/tcpshield.tfrc tofu -chdir=path/to/example plan
```

## Release & publishing

- **release-please** (`.github/workflows/release-please.yml`, `release-type: go`) owns versioning + CHANGELOG + the tag + GitHub release.
- When it cuts a release, the **GoReleaser** job in the same workflow builds the cross-platform archives, **GPG-signs** the checksums and **appends** them to the release (`release.mode: append`).
- The registry consumes the per-platform zips + `SHA256SUMS` + detached `.sig` + `manifest.json` (protocol `6.0`, from `terraform-registry-manifest.json`).
- The GPG key in `KEYS` is the **same** key the sibling providers use — already registered, nothing to rotate.

## Conventions

- **Conventional Commits enforced** via commitlint on `git commit`. Don't `--no-verify` unless explicitly asked.
- Siblings **`../terraform-provider-laravelforge`**, **`../terraform-provider-discord`** and **`../terraform-provider-linear`** are the reference implementations of these provider conventions — check them when unsure.

## AI & skills

- **`.claude/settings.json`** ships a baseline permission policy — see _Permission policy_ below for the rules it follows. `.claude/settings.local.json` (per-machine overrides, typically `enabledMcpjsonServers`) is gitignored.
- **`.tituskirch-skills.json`** configures the [TitusKirch skills](https://github.com/TitusKirch/skills) (commit, PR, issue, release, docs …) per repo. It is the runtime **config**, not an installer. Regenerate/reconcile it with the `tituskirch-skills-config` skill.
- **Installing the skills.** The bundle is installed via the skills.sh CLI (`pnpm dlx skills add TitusKirch/skills`), not vendored into the repo. `pnpm skills:update` refreshes project-scoped skills tracked in `skills-lock.json` (only present once a repo actually installs project skills).

## Permission policy

`.claude/settings.json` is deliberately lopsided: a **long `deny` list and a short `allow` list**. The two sides answer different questions, so they follow opposite rules.

**`deny` may be generous.** A rule for a command the repo doesn't have is a no-op, it never needs maintenance, and it is never reviewed — a too-broad block only surfaces when you actually hit it. So the list covers every stack kirchDev repos might grow into (Laravel, Prisma, Terraform/OpenTofu, AWS), not just this one. `git reflog expire` and `git gc --prune=now` are in there because they destroy the rescue path that survives a `reset --hard`.

The line to draw is **the machine or something remote, not the working copy**. Blocked: anything that wrecks the OS (`dd`, `mkfs`, `chmod -R`, `rm -rf /…`), tears down remote state or resources (`terraform destroy`, `state rm`, `aws ec2 terminate-instances`, `gh repo delete`), or throws away work with no recovery path (force-push, `reset --hard`, `stash drop`). Deliberately *not* blocked, because they are ordinary local development: `rm -rf node_modules`, `docker volume rm`, `docker compose down -v`, `docker system prune`, `php artisan tinker`, deleting a remote branch. Those prompt instead — a command that is sometimes wanted belongs in the middle state, never in `deny`.

**`allow` must stay short.** Its only return is fewer prompts — no safety is gained. Every line has to be read and understood by whoever copies this file, and an unreviewed allow list is more dangerous than none. Keep what occurs many times per session (read-only git, `ls`/`grep`/`rg`, the project's own check scripts) and let everything else ask.

**Three states, not two.** A command in `allow` runs unasked; one in `deny` is impossible and has to be typed by hand; one in **neither list prompts you** — and that middle state is the right default for almost everything. Reserve `deny` for what a mistaken "yes" could not undo. A normal `git push` is not that: it is reversible, visible and the ordinary way work ships, so it sits in `allow`.

> [!IMPORTANT]
> **Never allow a rule that runs arbitrary code.** `php artisan tinker --execute`, `pnpm exec turbo run`, `find . *` (which covers `-delete` and `-exec rm`), a raw `pnpm dlx`, or an MCP tool that executes SQL (`database-query`, `run-query`) each hand back everything the `deny` list took away — a blocked `db:wipe` means nothing next to an allowed `tinker --execute 'DB::statement(...)'`. A deny list is only as strong as the weakest allow rule beside it.

Two things this file cannot do, by design: it cannot tell which branch a `git push` targets (protect release branches with **branch protection**, not permissions), and prefix rules miss flags placed before the subcommand (`docker compose -f x.yml down -v`). Treat it as lowering the odds, not as a guarantee.

Downstream repos keep the `deny` list as-is and swap the `pnpm` lines in `allow` for whatever their stack runs.

**Codex gets the same policy** in `.codex/rules/default.rules` — permission config is not portable, so the block list exists twice and **both must be changed together**. Codex uses Starlark `prefix_rule()` calls matching on argument *tokens*, which handles flags and shell chains that the `Bash(…)` prefix patterns miss, and every rule carries its own `match`/`not_match` cases. Check a rule with:

```bash
codex execpolicy check --pretty --rules .codex/rules/default.rules -- git push --force
```

**Parity between the two is machine-checked, not eyeballed.** `pnpm check:policy` (`scripts/check-policy-parity.ts`, part of `pnpm check` and of CI) expands every `prefix_rule` into its concrete argv prefixes — the cartesian product over its alternation lists — and matches the two sets in both directions, so "we changed both files" becomes a number rather than a claim. Two things it encodes are worth knowing before editing either file:

- **The languages differ, so a few gaps cannot be closed.** Claude Code matches a prefix of the command _string_; a `prefix_rule` matches whole argv _tokens_. `Bash(aws iam delete-:*)` therefore bans every delete verb AWS will ever ship, and the Codex side can only enumerate the ones it ships today. Such a difference is legal but must be **declared** — in the `DELIBERATE` list in the script and in the `.codex/rules/default.rules` header — and the check fails both on an undeclared one and on a declaration that has gone stale.
- **Neither language normalises flag order or case.** `rm -rf /` and `rm -fr /` are separate bans; `rm -r -f /` and `redis-cli FlushAll` are neither, and enumerating permutations never ends. The check proves the two files list the **same spellings** — it does not claim the set of spellings is complete. Same caveat as the two below, and for the same reason.

## Workflows are calls, not copies

Every file in `.github/workflows/` is a **stub**: a trigger and a `uses:` pointing at a body in [`kirchDev/workflows`](https://github.com/kirchDev/workflows). A repo created from this template inherits the calls, not 727 lines of workflow — and a fix made centrally reaches it on its next Dependabot bump instead of never.

What follows for a new repo:

- **Do not paste a workflow body back in.** If a stub almost fits, the answer is an input on the body or an own job beside the call — see that repository's `docs/1.guides/2.add-a-body.md`.
- **The pins are commit SHAs with the version as a trailing comment.** Dependabot raises the bumps; the `github-actions` ecosystem is already configured in `.github/dependabot.yml`.
- **A repo that publishes something** adds its own job to `release-please.yml`, gated on `needs.release-please.outputs.release-created`.
- **A repo with a compiled language** names it in `codeql.yml`'s `languages` input rather than forking the workflow.
- **Checks come from `package.json`.** `ci.yml` runs whatever the `check` script chains, so adding a check needs no workflow change at all.

## Branching model

The default here is a **`dev` integration branch**: branch off `dev`, PR into `dev`, roll `dev` up into `main`, and release-please releases from `main`. That is what most kirchDev repos run, so the template runs it too — a variant that ships switched off is a variant nobody notices is broken.

> [!IMPORTANT]
> A repo created from this template has the `dev` config but **no `dev` branch**. Create it before the first Dependabot run: with `target-branch: 'dev'` pointing at a branch that doesn't exist, Dependabot opens nothing at all. Going main-only (below) is a deliberate step too — leaving the config untouched is the one option that silently does nothing.

`.github/workflows/promotion-pr.yml` opens and updates the rolling draft promotion PR. Mark that PR ready and **merge it with a merge commit, never a squash**: squashing collapses the individual `feat:`/`fix:` commits into the PR's own `chore:` title, and release-please then cuts nothing.

It calls a central body that picks its own target: with a `stage` branch it promotes `dev` into `stage`, without one straight into `main`. The stub is therefore the same file whichever flow a repo is on.

Going **main-only** is three edits, all of them removals:

```bash
rm .github/workflows/promotion-pr.yml
# .github/dependabot.yml    — drop both `target-branch: 'dev'` lines
# .tituskirch-skills.json   — set `pr.base` to "main"
```

Nothing is vendored for this. A variant worth shipping as files is one that *adds* something — content that would otherwise be lost. A variant that only deletes has nothing to preserve, so it stays documented.

`ci.yml` and `codeql.yml` list both `main` and `dev` in their `on: branches:` filters and neither edit touches them. A filter naming a branch that doesn't exist is a no-op, so it costs a main-only repo nothing — and without `dev` in `ci.yml`, PRs into `dev` (Dependabot's included) would run no CI at all.

## House style for READMEs and meta files

`/write-readme` skill encodes the canonical structure. Key rules: hero block wrapped in `<div align="center">`, prescribed section emojis (✨ 📦 🗺️ 📚 🤝 🛣️ 📄), license footer always reads `[MIT](LICENSE) © [Titus Kirch](https://github.com/TitusKirch/) / [IT-Dienstleistungen Titus Kirch](https://kirch.dev)`. Use GitHub callouts (`> [!TIP]`, `> [!IMPORTANT]`), never plain blockquotes.
