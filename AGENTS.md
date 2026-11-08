# AGENTS.md

## Environment and commands

This repository implements Devsy's external MicroSandbox Runtime v1 provider.
Use the toolchain pinned in `mise.toml`; run `mise install` to install it.
Building requires CGO and a C compiler because guest execution uses the official
MicroSandbox Go SDK and its embedded native library. Devsy core must not acquire
this native dependency.

Run commands through `mise run`:

- `mise run test`: race-enabled Go tests and `go vet`.
- `mise run lint`: strict golangci-lint checks and a formatting diff.
- `mise run format`: apply Go formatting.
- `mise run pre-commit`: all `prek.toml` hooks.
- `mise run build`: build `dist/devsy-runtime-microsandbox`.
- `go run ./cmd/package-provider <vSEMVER> <artifact-directory>`: generate release
  metadata from the complete native executable set.
- `actionlint .github/workflows/*.yml`: validate workflow changes when available.

## Go design and behavior

Follow idiomatic Go and the [Uber Go style guide](https://github.com/uber-go/guide/blob/master/style.md).
Prefer simple functions, small interfaces at real capability boundaries, explicit
errors, and clear ownership of processes, streams, SDK handles, and temporary
files. Refactor when it improves correctness or clarity, not to introduce a
pattern. Prefer `testify/suite` for unit tests. Keep comments for non-obvious
invariants and rationale rather than restating code.

- Keep protocol adaptation in `internal/server`, backend operations in
  `internal/msb`, environment configuration in `internal/config`, and release
  metadata in `internal/distribution`.
- Preserve binary stdout/stderr, literal argv, cancellation, stdin EOF, and
  cleanup behavior. Only an authoritative guest exit event supplies an exit code;
  backend failures remain errors.
- Keep guest environment and sandbox label values off the `msb` command line: they
  go through `--runtime-conf` references to host variables (`internal/msb/envconf.go`).
- Preserve validation before mutation, provisioning-only version gates, mount
  ownership, image snapshot identity, and secret redaction.
- Keep guest environment values off the `msb run` command line, where other
  local users can read them with `ps`. `internal/msb` passes them through host
  variables referenced by a temporary `--runtime-conf` file (msb 0.7.13 and
  newer). The file holds no values and is removed after `msb run` returns. The
  values stay in the msb child's environment, readable only by the same user.
- Info must not change capabilities based on the runtime version probe: the host needs preflight and may select destructive recreation.
- Installation and the plugin handshake must not create or start a VM. Keep
  stdout reserved for the plugin protocol; diagnostics belong on stderr.
- Do not change the built-in Devsy provider or migrate existing provider sources
  until real VM parity is demonstrated. Fixture tests and alias-installation
  smoke tests do not establish VM parity.
- Release manifests must pin checksums of the actual executable bytes and list
  only platforms that were built and tested natively.

## Validation and pull requests

Start from current `origin/main`, inspect overlapping work, and preserve unrelated
changes. Run relevant tests, `mise run lint`, and `mise run pre-commit` before pushing.
Workflow and distribution changes must pass the native packaging and real Devsy
installer checks in CI. Keep README instructions standalone and accurate.

Use scoped branches, signed Conventional Commits, and concise subjects. Satisfy
any repository CLA requirements. Contributors sign the CLA by commenting when asked; bots are allowlisted. Open draft PRs for review. Verify validation,
applicable CI checks, and completed Greptile and CodeRabbit reviews against the
final head; resolve significant valid findings before merging. A skipped or
rate-limited review is pending, not a completed review. Merge only with human
authorization covering the change.

Generated release PRs containing only version metadata and changelog updates
auto-merge after applicable CI passes. Do not request Greptile or CodeRabbit
reviews for these PRs or treat skipped reviews as blockers. Code and workflow
changes retain the review requirements above.

**Merged commits must contain only a single-line Conventional Commit subject,
with an empty body.** When authorized to merge, squash with an explicit subject
and an explicitly empty body; never copy the PR description or commit list into
the merged commit. Apply the same rule to automated release PR merges and verify
the resulting commit message.

Release Please creates a draft release and tag. Build and verify all native assets from the tagged commit before publishing. Publish a release behind the current latest without marking it latest. Rerun the Release assets workflow with the tag to recover a failure. For an already published release, download its assets and verify their checksums and regenerated provider manifest without uploading or publishing again. Never publish a draft with incomplete assets.

Guest stdin cleanup is joined after Kill and handle teardown. The gated
`TestRealGuestEOF` uses `DEVSY_QUALIFY_RUNTIME=1`, `DEVSY_QUALIFY_SANDBOX`,
`MSB_PATH`, and `MSB_LIBKRUNFW_PATH` with an isolated `MSB_HOME` and an existing
running Alpine VM. With the official darwin arm64 0.7.13 pair, normal cat EOF
and 51 cancelled execs left 2 goroutines before and after settling. No native
stdin Close leak reproduced. Preserve Kill before joining stdin cleanup.

Real VM conformance requires the matching msb 0.7.13 executable and firmware,
with absolute `MSB_PATH` and `MSB_LIBKRUNFW_PATH`, plus Hypervisor.framework on
Mac or accessible KVM on Linux. Run:

```sh
unset DEVSY_HOME
DEVSY_MSB_CONFORMANCE=1 mise exec -- go test -v -count=1 -timeout 35m ./internal/server -run '^TestRealRuntimeConformance$'
```

The test isolates its runtime home under /tmp and pulls Alpine 3.22 from Docker
Hub. Signal reporting is explicitly skipped because the SDK lacks signal events.
Hosted runners lack KVM, so CI does not run real VM conformance.
