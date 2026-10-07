# Devsy MicroSandbox Provider

External MicroSandbox runtime provider for [Devsy](https://github.com/devsy-org/devsy).

## Status

This repository currently contains the project and quality-control tooling.
The executable supports `--version` and deliberately fails other invocations;
it does not yet serve Runtime Protocol v1 or create workspaces. There is no
installable external provider manifest or runtime release asset yet.

Use Devsy's built-in `microsandbox` provider for workspaces. It remains supported
while the MicroSandbox client is extracted and the external runtime is tested
for parity. This repository does not change existing provider configurations.

The next stages migrate the MicroSandbox client, implement the Runtime Protocol
v1 adapter using the [Devsy Runtime SDK](https://github.com/devsy-org/devsy-runtime-sdk),
and test an external provider alias before changing the built-in manifest.
Image builds, tags, and publication remain Devsy image-backend responsibilities.

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
