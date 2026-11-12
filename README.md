# MicroSandbox Provider for Devsy

Run Devsy workspaces in MicroSandbox microVMs.

External provider requires msb 0.7.13+. Built-in Devsy driver requires 0.7.2+ for provisioning, 0.7.7+ for recreate. The SDK upgrades the `MSB_HOME` database; update msb and provider together because older versions cannot use it.

Devsy 1.24.0+ supports the short name:

```sh
devsy provider add microsandbox
```

If the deprecated built-in provider already uses `microsandbox`, add `--name microsandbox-external`. Existing configurations are not migrated.

Empty/whitespace-only `MSB_PATH` and `MSB_LIBKRUNFW_PATH` use runtime discovery; select both paths together. Blank `MSB_HOME`, `MSB_CONFIG_PATH`, and `MSB_AGENTD_PATH` use MicroSandbox defaults.

Platforms: linux amd64/arm64, darwin arm64, windows amd64.

See [website documentation](https://devsy.sh/docs/managing-providers/microsandbox) and [contributor guidance](AGENTS.md).

## Local development

Use existing matching runtime/firmware with absolute paths:

```sh
export MSB_PATH=/absolute/path/to/msb
export MSB_LIBKRUNFW_PATH=/absolute/path/to/libkrunfw
unset DEVSY_HOME
mise install
mise run build:provider:dev
devsy provider add --use=false --name microsandbox-local "$PWD/dist/local/provider.yaml"
devsy provider init microsandbox-local
devsy workspace up /absolute/path/to/project --provider microsandbox-local --id local-test --ide none
devsy workspace ssh local-test --start-services=false --command 'uname -sm'
devsy workspace delete local-test --force
```

The host-only task builds for macOS arm64 or Linux amd64/arm64, keeps pinned executables, and isolates its runtime home in `dist/m`. Use short checkout paths for Unix sockets. It does not download runtimes or remove old builds. Rebuild, then refresh:

```sh
mise run build:provider:dev
devsy provider set-source --use=false microsandbox-local "$PWD/dist/local/provider.yaml"
devsy provider init microsandbox-local
```

Devsy older than v1.27.0 loses runtime paths with `--disable-daemon`.

Real VM tests need Hypervisor.framework or KVM and are not run on hosted CI. Run the conformance command in [AGENTS.md](AGENTS.md#real-vm-tests) locally before a release and when the runtime changes.

Creation errors redact request environment, label and bind source values, including devcontainer metadata and short values; SDK errors pass through unchanged. Provider generated user and workspace mount labels stay readable unless the same value also appears in a request environment variable or label.

## License

[MPL-2.0](LICENSE).
