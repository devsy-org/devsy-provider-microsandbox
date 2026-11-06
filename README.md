# MicroSandbox Provider for Devsy

The MicroSandbox provider runs Devsy workspaces in hardware isolated MicroSandbox microVMs.

MicroSandbox `msb` 0.7.13 or newer is required. The SDK upgrades the `MSB_HOME` database, which older `msb` releases refuse to use. After `msb` 0.7.13 upgrades the database, providers built with an older SDK cannot run commands. Update `msb` and the provider together.

```sh
devsy provider add github.com/devsy-org/devsy-provider-microsandbox
```

If `microsandbox` already exists, adding it fails; add `--name microsandbox-external` to the command and use that name for workspaces.

Supported platforms: linux amd64, linux arm64, darwin arm64, windows amd64.

See the [website documentation](https://devsy.sh/docs/managing-providers/microsandbox) for details and [AGENTS.md](AGENTS.md) for contributor guidance.

## License

[MPL-2.0](LICENSE).
