# Devsy MicroSandbox Provider

External MicroSandbox runtime provider for [Devsy](https://github.com/devsy-org/devsy).

## Status

This repository contains the project tooling and the extracted MicroSandbox CLI
client in `internal/msb`. The client covers lifecycle commands, non-PTY byte
streams, mount encoding, version parsing, and image loading.
The executable supports `--version` and `serve` through the Runtime SDK plugin
handshake. It implements lifecycle RPCs, binary non-PTY Exec, and finite merged
Logs. There is no installable external provider manifest or runtime release asset
yet.

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
perform the real plugin handshake and stream binary data through
a subprocess CLI fixture. They do not certify MicroSandbox runtime parity, which
will accompany the external provider alias.

Exec preserves literal argv and the requested user, emits separate bounded
stdout/stderr frames, and reports the CLI process exit status in a terminal exit
frame. CLI launch, I/O, cancellation, and signal failures remain RPC errors.
TTY, workdir, and environment overrides are rejected explicitly. CloseStdin closes command input without
canceling the command; RPC cancellation stops and reaps the CLI process. Commands
may finish before stdin closes. Logs merges backend streams into bounded frames
and returns after the current log output, without following.

The current MicroSandbox CLI does not expose a separate guest completion channel.
A nonzero CLI exit can therefore mean either a guest result or a backend error.
This ambiguity must be resolved before runtime parity and provider release; the
streaming adapter does not infer error categories from stderr text.

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
Initial tags document project development; runtime binaries and a checksum-pinned
`provider.yaml` will be added when the runtime implementation is usable.

## License

[MPL-2.0](LICENSE), matching Devsy and its Runtime SDK.
