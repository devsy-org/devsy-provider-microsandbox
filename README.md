# MicroSandbox Provider for Devsy

Run Devsy workspaces in MicroSandbox microVMs.

MicroSandbox `msb` 0.7.13 or newer is required. The SDK upgrades the `MSB_HOME` database. Update `msb` and the provider together because older versions cannot use the upgraded database.

Devsy v1.24.0 or newer is required for the short name.

```sh
devsy provider add microsandbox
```

If the deprecated built-in provider already uses `microsandbox`, add this provider with `--name microsandbox-external`. Existing configurations are not migrated.

Empty or whitespace only `MSB_PATH` and `MSB_LIBKRUNFW_PATH` options use runtime discovery. Set both paths when selecting a runtime pair. Blank `MSB_HOME`, `MSB_CONFIG_PATH`, and `MSB_AGENTD_PATH` overrides also use MicroSandbox defaults.

Supported platforms: linux amd64, linux arm64, darwin arm64, windows amd64.

See the [website documentation](https://devsy.sh/docs/managing-providers/microsandbox) for details and [AGENTS.md](AGENTS.md) for contributor guidance.

## Local development

Use an existing matching runtime and firmware pair with absolute paths. On the host, run:

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

The task builds for the current host only: macOS arm64 or Linux amd64 or arm64. It keeps pinned executables and uses `dist/m` as an isolated runtime home. Use a short checkout path for Unix sockets. It does not download runtimes or remove old builds. After rebuilding, refresh:

```sh
mise run build:provider:dev
devsy provider set-source --use=false microsandbox-local "$PWD/dist/local/provider.yaml"
devsy provider init microsandbox-local
```

This external provider requires msb 0.7.13 or newer. The built-in driver in Devsy core requires 0.7.2 for provisioning and 0.7.7 for recreate. Core currently strips provider options with `devsy up --disable-daemon`, so runtime paths are lost with that flag. Real VM tests need Hypervisor.framework or KVM and are not run on hosted CI. Run the conformance command in [AGENTS.md](AGENTS.md#real-vm-tests) locally before a release and when the runtime changes.

Creation errors redact request environment and label values, including devcontainer metadata and short values. Provider generated user and workspace mount labels stay readable unless the same value also appears in a request environment variable or label.

## License

[MPL-2.0](LICENSE).
