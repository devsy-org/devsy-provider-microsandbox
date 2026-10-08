# AGENTS.md

## Environment and commands

This repository implements Devsy's external MicroSandbox Runtime v1 provider.
Use the toolchain pinned in `mise.toml`; run `mise install` to install it.
Building requires CGO and a C compiler because guest execution uses the official
MicroSandbox Go SDK and its embedded native library. Devsy core must not acquire
this native dependency.

Run commands through `mise exec --`:

- `task test`: race-enabled Go tests and `go vet`.
- `task lint`: strict golangci-lint checks and a formatting diff.
- `task format`: apply Go formatting.
- `task pre-commit`: all `prek.toml` hooks.
- `task build`: build `dist/devsy-runtime-microsandbox`.
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
- Preserve validation before mutation, provisioning-only version gates, mount
  ownership, image snapshot identity, and secret redaction.
- Installation and the plugin handshake must not create or start a VM. Keep
  stdout reserved for the plugin protocol; diagnostics belong on stderr.
- Do not change the built-in Devsy provider or migrate existing provider sources
  until real VM parity is demonstrated. Fixture tests and alias-installation
  smoke tests do not establish VM parity.
- Release manifests must pin checksums of the actual executable bytes and list
  only platforms that were built and tested natively.

## Validation and pull requests

Start from current `origin/main`, inspect overlapping work, and preserve unrelated
changes. Run relevant tests, `task lint`, and `task pre-commit` before pushing.
Workflow and distribution changes must pass the native packaging and real Devsy
installer checks in CI. Keep README instructions standalone and accurate.

Use scoped branches, signed Conventional Commits, and concise subjects. Satisfy
any repository CLA requirements. Open draft PRs for review. Verify validation,
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
