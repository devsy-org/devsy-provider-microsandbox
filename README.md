# MicroSandbox Provider for Devsy

Run [Devsy](https://github.com/devsy-org/devsy) workspaces in hardware-isolated
[MicroSandbox](https://github.com/superradcompany/microsandbox) microVMs.

## Getting started

Releases include provider binaries for Linux amd64/arm64, macOS arm64
(Apple Silicon), and Windows amd64.

Install Devsy and the official MicroSandbox CLI, version 0.7.13 or later.
Follow MicroSandbox's installation instructions for your host and ensure
`msb --version` works in the environment running Devsy. Docker is needed for
image builds.

The provider's embedded MicroSandbox SDK upgrades the MicroSandbox database
(`MSB_HOME`, by default `~/.microsandbox`) when it first starts a workspace.
Older `msb` releases refuse the upgraded database, so the provider rejects
`msb` versions below 0.7.13 before creating a workspace or running commands
in one. Conversely, provider releases built against an older SDK cannot run
commands once `msb` 0.7.13 has upgraded the database, so update `msb` and the
provider together.

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

### Relocated runtime pair

Set `MSB_PATH` and `MSB_LIBKRUNFW_PATH` together when the MicroSandbox
executable and matching firmware are outside the standard runtime home:

```sh
devsy provider set microsandbox --skip-init \
  -o MSB_PATH=/opt/microsandbox/msb \
  -o MSB_LIBKRUNFW_PATH=/opt/microsandbox/libkrunfw.so.5.6.1
```

The adapter uses the selected executable for lifecycle commands and passes
the same pair to the MicroSandbox Go SDK for guest execution. A partial pair
fails instead of falling back to another installation. Without either option,
the existing runtime discovery remains in effect.

The adapter executable also accepts `runtime-check`. It resolves the pair
through the SDK and runs a bounded version check without starting a VM.
Successful output is a single JSON object with `schemaVersion`, `msbPath`,
`libkrunfwPath`, `version`, and `compatible`. This check can initialize the
SDK's native library cache, so it is not a passive inspection command.

The `compatible` field is true for MicroSandbox 0.7.13 or later, the minimum
for every provider operation that creates a workspace or runs commands in it.

## Development

Building requires CGO and a C compiler. Install [mise](https://mise.jdx.dev), then:

```sh
mise install
mise run test ::: lint ::: pre-commit ::: build
```

Pull request CI verifies the pinned Linux amd64 archive digest and its exact
executable and firmware inventory. It checks a relocated pair with the CLI,
the Go SDK, and the adapter. This check does not start a VM or qualify
Devsy-managed installation and removal.

See [AGENTS.md](AGENTS.md) for development guidelines and [CHANGELOG.md](CHANGELOG.md)
for release history.

## License

[MPL-2.0](LICENSE).

Release Please creates a draft and its tag after a release PR merges. Native builds and provider manifest checks run for the tagged commit before assets are uploaded. A release behind the current latest is published without becoming latest. Rerun the Release assets workflow with the tag after a failure. For an already published release, the workflow downloads its assets and verifies their checksums and regenerated provider manifest without uploading or publishing again.
