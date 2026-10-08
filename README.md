# Devsy MicroSandbox Provider

External MicroSandbox runtime provider for [Devsy](https://github.com/devsy-org/devsy).

## Status

This repository contains the project tooling and the extracted MicroSandbox CLI
client in `internal/msb`. The client covers lifecycle commands, non-PTY byte
streams, mount encoding, version parsing, and image loading.
The executable supports `--version` and `serve` through the Runtime SDK plugin
handshake. It implements lifecycle RPCs, binary non-PTY Exec, and finite merged
Logs. Releases following this change will include native executables, SHA-256
checksums, and an installable `provider.yaml`.

Use Devsy's built-in `microsandbox` provider for workspaces. It remains supported
while the external runtime is implemented and tested for parity. This repository
does not change existing provider configurations.

Image snapshot preparation follows the current built-in driver; workspace owner
resolution is available. The Runtime v1 adapter maps configuration,
preflight, capability reporting, image creation, inspection, and lifecycle calls.
Binary Exec/Logs transport and the serving entry point are implemented.
The adapter uses the
[Devsy Runtime SDK](https://github.com/devsy-org/devsy-runtime-sdk),
with external-provider alias testing planned before changing the built-in manifest.
Image builds, tags, and publication remain Devsy image-backend responsibilities.
The client prepares one immutable image snapshot before validation or import.
Locally built images must be saved by the configured Docker-compatible CLI; they
never fall back to a registry. Other images use a cached Docker image when
available, otherwise a Linux image for the host architecture from an OCI registry.
The same snapshot is imported under a content-derived tag after validation, so a
moving source tag cannot change the image used to create the VM. Callers keep the
snapshot open until validation and import finish, then close it to remove its
private archive. Preparation captures registry layers before returning, so import
and inspection can use a different context and no longer need registry access. Registry access uses the host's Docker configuration and
credential helpers. Kubernetes service-account registry authentication is not
part of this local-runtime client.

Workspace ownership uses the developer identity (`remoteUser`, then workload
`user`, then root) without changing the workload execution user. The resolver
reads `/etc/passwd` and `/etc/group` from the prepared image, respecting layer
replacements, whiteouts, and opaque directories without running image code.
Account-file and account-directory links are rejected rather than exposing stale
metadata from lower layers. Explicit numeric
UID:GID and root identities need no account lookup. Missing or invalid accounts
fail validation before runtime mutation. Only the primary workspace bind mount
needs an owner; named volumes, tmpfs, and `stat-virt=off` skip resolution. A
non-root Dockerless identity still requires a prebuilt developer image or disabled
stat virtualization with private host permissions, because it cannot be resolved
from the runner image before VM creation. Resolved IDs control guest mount
ownership and do not change host inode ownership. The runtime adapter applies
these IDs and validates mount configuration before import or creation. `RunImage`
rejects an existing VM with `AlreadyExists`; deletion is an explicit lifecycle
operation that stops a running VM before removing it. It never replaces a VM
as a side effect of image creation. Invalid operator values and unsupported
Docker-specific options return errors instead of being silently ignored.
Additional bind mounts use runtime defaults; only the primary workspace
mount receives the configured permission policy and resolved owner.

Client tests use subprocess fixtures and a local OCI registry; they do not
require an installed MicroSandbox runtime or Docker daemon. Executable tests
perform the real plugin handshake, stream binary logs through a CLI fixture,
and verify native SDK loading and backend-failure propagation in an isolated
catalog. Structured execution-event fixtures cover binary duplex streams,
guest exits, and cleanup. These tests do not certify real VM runtime parity,
which will accompany the external provider alias.

Exec uses the official MicroSandbox Go SDK v0.7.7 for structured, non-PTY guest
execution. Only a guest `Exited` event produces a terminal exit frame, including
nonzero guest exit codes. Backend failures, missing completion, cancellation,
and output failures remain RPC errors. The CLI still handles lifecycle and logs.
Exec requires msb 0.7.7 or newer; older installations receive an explicit
unsupported error before SDK access. Lifecycle operations retain their existing
version policy.

Exec preserves literal argv and the requested user and emits separate bounded
stdout/stderr frames. TTY, workdir, and environment overrides are rejected
explicitly. CloseStdin closes command input without canceling the command;
RPC cancellation kills the guest execution and releases its SDK handles. Commands
may finish before stdin closes. Connecting for execution does not start a stopped
VM or acquire ownership of its lifecycle. Logs returns finite merged output.

Building this external provider now requires CGO and a C compiler. The SDK embeds
its released native library for Linux amd64/arm64, macOS arm64, and Windows
amd64/arm64. Devsy core does not gain a native dependency. Runtime parity with a
real MicroSandbox VM remains a separate gate before provider cutover.

## Development

Install [mise](https://mise.jdx.dev), then run:

```sh
mise install
mise exec -- task lint
mise exec -- task test
mise exec -- task pre-commit
mise exec -- task build
./dist/devsy-runtime-microsandbox --version
```

`prek.toml` defines pre-commit hooks. CI runs pre-commit and lint as separate jobs,
plus race-enabled Go checks and executable smoke tests on Linux, macOS, and Windows.
Commits must be signed and use Conventional Commit subjects.

## Versioning

Release Please manages semantic versions and changelogs after main-branch checks
pass, following Devsy provider conventions. Repository setup requires the Devsy
GitHub App installation, its organization secrets (`DEVSY_GITHUB_APP_ID` and
`DEVSY_GITHUB_APP_PRIVATE_KEY`), and auto-merge enabled for release PRs. CI will
fail the release job when these are unavailable rather than silently skip it.
The release workflow builds and tests native executables on Linux amd64/arm64,
macOS arm64, and Windows amd64. It generates `provider.yaml` from those exact
executables, verifies downloads using Devsy's checksum resolver, and installs an
alias in an isolated configuration before publishing the complete asset set.
Windows arm64 and macOS amd64 are not distributed by this workflow.

## Experimental external installation

Use a Devsy build containing external runtime support (commit
`017e389afcd23132ce45277c387a57440a880e7f` or later), an installed MicroSandbox
CLI 0.7.7 or newer for Exec, and Docker for image builds. Once a release containing
`provider.yaml` is available:

```sh
devsy provider add --use=false --name microsandbox-external \
  github.com/devsy-org/devsy-provider-microsandbox
```

This installs a separate provider configuration without activating it. Initialize
it, then explicitly select it when starting a workspace:

```sh
devsy provider init microsandbox-external
devsy workspace up . --provider microsandbox-external
```

Existing built-in
`microsandbox` configurations continue to use the built-in driver. Options retain
their existing names and defaults. Real VM parity is still required before the
built-in provider can switch to the external implementation.

For local packaging, place all four native executables in `dist/`, then run
`mise exec -- go run ./cmd/package-provider v0.1.3 dist`. The generator fails if
any executable is missing or empty. Install the generated `dist/provider.yaml`
under the same alias when testing local builds; its download URLs refer to the
specified GitHub release, so that release must contain the corresponding assets.

## License

[MPL-2.0](LICENSE), matching Devsy and its Runtime SDK.
