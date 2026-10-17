# MicroSandbox Provider for Devsy

Run [Devsy](https://github.com/devsy-org/devsy) workspaces in hardware-isolated
[MicroSandbox](https://github.com/superradcompany/microsandbox) microVMs.

## Getting started

Releases include provider binaries for Linux amd64/arm64, macOS arm64
(Apple Silicon), and Windows amd64.

Install Devsy and the official MicroSandbox CLI. Basic provisioning supports
0.7.2+; explicit recreation and structured guest execution require 0.7.7+.
Follow MicroSandbox's installation instructions for your host and ensure
`msb --version` works in the environment running Devsy. Docker is needed for
image builds.

Install the latest provider release and start a workspace:

```sh
devsy provider add github.com/devsy-org/devsy-provider-microsandbox
devsy workspace up . --provider microsandbox
```

Adding the provider initializes it and selects it as the default. If the name
`microsandbox` already exists, add `--name microsandbox-external` to the install
command and replace `microsandbox` with that name in subsequent commands.
Existing built-in configurations are not migrated automatically.

## Configuration

Set options in Devsy Desktop or through the CLI:

```sh
devsy provider set microsandbox -o MICROSANDBOX_CPUS=4
```

See the [provider options](internal/distribution/provider.yaml.tmpl) for resource
limits, storage, networking, and workspace mount policies.

## Development

Building requires CGO and a C compiler. Install [mise](https://mise.jdx.dev), then:

```sh
mise install
mise exec -- task test lint pre-commit build
```

See [AGENTS.md](AGENTS.md) for development guidelines and [CHANGELOG.md](CHANGELOG.md)
for release history.

## License

[MPL-2.0](LICENSE).
