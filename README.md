# Devsy MicroSandbox Provider

External MicroSandbox runtime provider for [Devsy](https://github.com/devsy-org/devsy).

## Status

This repository contains the project tooling and the extracted MicroSandbox CLI
client in `internal/msb`. The client covers lifecycle commands, non-PTY byte
streams, mount encoding, version parsing, and image loading.
The executable supports `--version` and deliberately fails other invocations;
it does not yet serve Runtime Protocol v1 or create workspaces. There is no
installable external provider manifest or runtime release asset yet.

Use Devsy's built-in `microsandbox` provider for workspaces. It remains supported
while the external runtime is implemented and tested for parity. This repository
does not change existing provider configurations.

Image snapshot preparation follows the current built-in driver; workspace owner
resolution and the server mapping remain to be implemented. The next stages
implement the Runtime Protocol v1 adapter using the
[Devsy Runtime SDK](https://github.com/devsy-org/devsy-runtime-sdk),
and test an external provider alias before changing the built-in manifest.
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

Client tests use subprocess fixtures and a local OCI registry; they do not
require an installed MicroSandbox runtime or Docker daemon. Real runtime parity
validation will accompany the external provider adapter.

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
