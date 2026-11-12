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

This host-only task uses `dist/m` as its isolated runtime home. Use a short checkout path for Unix sockets. After rebuilding, refresh:

```sh
mise run build:provider:dev
devsy provider set-source --use=false microsandbox-local "$PWD/dist/local/provider.yaml"
devsy provider init microsandbox-local
```

Core strips provider options with `devsy up --disable-daemon`. Real VM tests need Hypervisor.framework or KVM. Follow [AGENTS.md](AGENTS.md#real-vm-tests) before releases or runtime changes.

Creation errors redact request environment, label and bind source values. SDK errors pass through unchanged; runtime 0.7.13 failure probes found no environment or label values. The msb catalog stores these values on disk, including in `MSB_HOME/db/msb.db-wal` after deletion. See [AGENTS.md](AGENTS.md#real-vm-tests) for probe coverage.

## License

[MPL-2.0](LICENSE).
