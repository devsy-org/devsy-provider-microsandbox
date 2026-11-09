# AGENTS.md

---

## Environment Setup

This repository implements Devsy's external MicroSandbox Runtime v1 provider. It is a single Go module that builds the `devsy-runtime-microsandbox` executable and the `package-provider` release tool.

### Prerequisites and Tooling

Toolchain is managed by mise (Go, golangci-lint, prek, pinned in `mise.toml`). Install mise using `curl https://mise.run | sh`. Install toolchain dependencies with `mise install`.

Building requires CGO and a C compiler. Guest execution uses the official MicroSandbox Go SDK and its embedded native library. Devsy core must not acquire this native dependency.

MicroSandbox `msb` 0.7.13 or newer is required at runtime.

---

## Common Developer Commands

`mise tasks` shows developer commands. The most common tasks:

- **Set up**: `mise run setup` (downloads Go modules with retries and installs the prek git hooks)
- **Run unit tests**: `mise run test` (race-enabled `go test` and `go vet`)
- **Lint**: `mise run lint` (golangci-lint checks and a formatting diff)
- **Format**: `mise run format`
- **Run all pre-commit hooks**: `mise run pre-commit`
- **Build development binary**: `mise run build` (output: `dist/devsy-runtime-microsandbox`)
- **Generate release metadata**: `go run ./cmd/package-provider <vSEMVER> <artifact-directory>` (needs the complete set of native executables in the directory)
- **Validate workflow changes**: `actionlint .github/workflows/*.yml` when `actionlint` is installed

### Local Development: Agent Binary URL (`DEVSY_AGENT_URL`)

Not relevant here. This provider does not build or serve the Devsy agent binary.

---

## Testing

Unit tests use `testify/suite` and run on Linux, macOS, and Windows in CI. They need no `msb` runtime.

### Local provider workflow

Set `MSB_PATH` and `MSB_LIBKRUNFW_PATH` to an existing matching pair of absolute paths. Run `unset DEVSY_HOME` and `mise run build:provider:dev`. Follow the printed commands to install `microsandbox-local`, then run a workspace as shown in the README. Rebuild and follow the refresh commands after changes. The helper supports the host platform only and keeps its runtime home under `dist/m`. Use a short checkout path for Unix sockets. Core's `--disable-daemon` strips provider options, including these paths.

### Relocated pinned runtime test

`TestRelocatedPinnedRuntime` in `cmd/devsy-runtime-microsandbox` runs on linux/amd64 only. It downloads its own pinned runtime and firmware, so it needs no `msb` install. It needs `DEVSY_QUALIFY_RUNTIME=1` and `DEVSY_ADAPTER_BINARY`, the absolute path of a built provider executable (`mise run build`). CI runs it in the `Pinned runtime relocation` job.

### Real VM tests

These tests skip unless `DEVSY_MSB_CONFORMANCE=1` or `DEVSY_QUALIFY_RUNTIME=1` is set. They need the matching `msb` 0.7.13 executable and firmware, with absolute `MSB_PATH` and `MSB_LIBKRUNFW_PATH`, plus Hypervisor.framework on a Mac or accessible KVM on Linux. Hosted CI runners lack KVM, so CI does not run them.

Run real VM conformance locally before a release and when the msb runtime changes:

```sh
unset DEVSY_HOME
DEVSY_MSB_CONFORMANCE=1 mise exec -- go test -v -count=1 -timeout 35m ./internal/server -run '^TestRealRuntimeConformance$'
```

The conformance test isolates its runtime home under `/tmp` and pulls Alpine 3.22 from Docker Hub. Signal reporting is skipped because the SDK has no signal events.

`TestRealGuestEOF` in `internal/msb` also needs `DEVSY_QUALIFY_RUNTIME=1`, `DEVSY_QUALIFY_SANDBOX`, `MSB_PATH`, and `MSB_LIBKRUNFW_PATH`, with an isolated `MSB_HOME` and an existing running Alpine VM. With the official darwin arm64 0.7.13 pair, normal `cat` EOF and 51 cancelled execs left 2 goroutines before and after settling. No native stdin Close leak reproduced. Guest stdin cleanup is joined after Kill and handle teardown. Preserve Kill before joining stdin cleanup.

### Distribution checks

CI builds and tests the executable natively on linux/amd64, linux/arm64, darwin/arm64, and windows/amd64, generates the pinned manifest, and installs it with a pinned checkout of the real Devsy host. Workflow and distribution changes must pass these checks.

---

## Code Style and Quality

### Go Code Style

- **Idiomatic**: Prefer simple functions, small interfaces at real capability boundaries, explicit errors, and clear ownership of processes, streams, SDK handles, and temporary files. Refactor when it improves correctness or clarity, not to introduce a pattern.
- **Style Guide**: Use the Uber style guide https://github.com/uber-go/guide/blob/master/style.md.
- **Unit Testing**: Prefer using [testify suite](https://github.com/stretchr/testify#suite-package) (`testify/suite`) for writing unit tests.
- **Linter**: `golangci-lint` via `mise run lint`. Run it and `mise run format` before pushing changes.
- **Comments**: Keep comments for non-obvious invariants and rationale rather than restating code. The `revive` rules require doc comments on exported identifiers and packages.

### Package Layout

- Keep protocol adaptation in `internal/server`, backend operations in `internal/msb`, environment configuration in `internal/config`, and release metadata in `internal/distribution`.

### Behavior Rules

- Preserve binary stdout/stderr, literal argv, cancellation, stdin EOF, and cleanup behavior. Only an authoritative guest exit event supplies an exit code. Backend failures remain errors.
- Keep guest environment and sandbox label values off the `msb` command line, where other local users can read them with `ps`. `internal/msb/envconf.go` passes them through host variables referenced by a temporary `--runtime-conf` file (msb 0.7.13 and newer). The file holds no values and is removed after `msb run` returns. The values stay in the `msb` child's environment, readable only by the same user.
- Preserve validation before mutation, version checks that apply only during provisioning, mount ownership, image snapshot identity, and secret redaction.
- Info must not change capabilities based on the runtime version probe. The host needs preflight and may select destructive recreation.
- Installation and the plugin handshake must not create or start a VM. Keep stdout reserved for the plugin protocol. Diagnostics belong on stderr.
- Do not change the built-in Devsy provider or migrate existing provider sources until real VM parity is demonstrated. Fixture tests and alias-installation smoke tests do not establish VM parity.
- Release manifests must pin checksums of the actual executable bytes and list only platforms that were built and tested natively.

---

## Pull Request and Commit Guidelines

1. **Base**: Start from current `origin/main`, inspect overlapping work, and preserve unrelated changes.
2. **Contributor License Agreement (CLA)**: All contributors sign the CLA by commenting when asked. Bots are allowlisted.
3. **Commit messages**: Conventional Commits, with a concise subject line.
4. **Commit signing**: All commits are required to be signed.
5. **Branch name**: Branch should be named according to the task.
6. **Pre-commit checks**: Run relevant tests, `mise run lint`, and `mise run pre-commit` before pushing. `prek` manages the hooks in `prek.toml`.
7. **Draft PRs**: Open draft PRs for review. Keep README instructions standalone and accurate.
8. **Reviews**: Verify validation, applicable CI checks, and completed Greptile and CodeRabbit reviews against the final head. Resolve significant valid findings before merging. A skipped or rate-limited review is pending, not a completed review.
9. **Merging**: Merge only with human authorization covering the change. Squash merge only. The merged commit is a single-line Conventional Commit subject with an empty body. Squash with an explicit subject and an explicitly empty body. Never copy the PR description or commit list into the merged commit, and verify the resulting commit message.

### Releases

Releases use Release Please (`release-please-config.json`), run from the `Release Please` job in `.github/workflows/ci.yml` after `CI Success` passes on `main`.

- Release Please opens a release PR with only version metadata (`.release-please-manifest.json`) and changelog (`CHANGELOG.md`) updates. The `Lint` job rejects any other file in that PR.
- The bot enables auto-merge on the release PR, and it merges after applicable CI passes. This is intended. Do not request Greptile or CodeRabbit reviews for these PRs or treat skipped reviews as blockers. Verify the merged commit message like any other squash merge.
- Merging the release PR creates a draft release and tag. The `Release assets` workflow (`.github/workflows/release.yml`) builds and verifies all native assets from the tagged commit, then publishes. Never publish a draft with incomplete assets.
- A release behind the current latest is published without being marked latest.
- To recover a failure, rerun the `Release assets` workflow with the tag. For an already published release, it downloads the assets and verifies checksums and the regenerated provider manifest without uploading or publishing again.
