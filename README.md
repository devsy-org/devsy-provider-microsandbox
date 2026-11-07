# MicroSandbox Provider for Devsy

The MicroSandbox provider runs Devsy workspaces in hardware isolated MicroSandbox microVMs.

MicroSandbox `msb` 0.7.13 or newer is required. The SDK upgrades the `MSB_HOME` database, which older `msb` releases refuse to use. After `msb` 0.7.13 upgrades the database, providers built with an older SDK cannot run commands. Update `msb` and the provider together.

Devsy v1.24.0 or newer is required for the short name.

```sh
devsy provider add microsandbox
```

If `microsandbox` is already configured as the deprecated built-in provider, add this provider with `--name microsandbox-external` and use that name for workspaces. Existing built-in configurations are not migrated automatically.

Supported platforms: linux amd64, linux arm64, darwin arm64, windows amd64.

See the [website documentation](https://devsy.sh/docs/managing-providers/microsandbox) for details and [AGENTS.md](AGENTS.md) for contributor guidance.

## License

[MPL-2.0](LICENSE).
