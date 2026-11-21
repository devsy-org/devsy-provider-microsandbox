// Command devsy-runtime-microsandbox serves the external MicroSandbox runtime.
package main

import (
	"fmt"
	"os"

	"github.com/devsy-org/devsy-provider-microsandbox/internal/config"
	"github.com/devsy-org/devsy-provider-microsandbox/internal/msb"
	"github.com/devsy-org/devsy-provider-microsandbox/internal/server"
	sdklog "github.com/devsy-org/devsy-runtime-sdk/log"
	"github.com/devsy-org/devsy-runtime-sdk/runtimev1"
	sdkserver "github.com/devsy-org/devsy-runtime-sdk/server"
	microsandbox "github.com/superradcompany/microsandbox/sdk/go"
)

var version = "dev"

const runtimeUsage = "usage: devsy-runtime-microsandbox --version|serve|runtime-check"

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, runtimeUsage)
		return 1
	}
	switch args[0] {
	case "--version":
		if _, err := fmt.Fprintln(os.Stdout, "devsy-runtime-microsandbox", version); err != nil {
			return 1
		}
		return 0
	case "serve":
		return serve()
	case "runtime-check":
		return runtimeCheck()
	default:
		fmt.Fprintln(os.Stderr, runtimeUsage)
		return 1
	}
}

func serve() int {
	driver, err := startupDriver()
	if err != nil {
		driver = server.NewConfigError(err)
	}
	sdkserver.Serve(driver)
	return 0
}

func startupDriver() (runtimev1.RuntimeDriverServer, error) {
	logger := sdklog.NewPluginLogger("microsandbox")
	logger.Info("starting MicroSandbox provider")
	driver, err := newDriver()
	if err != nil {
		logger.Error("MicroSandbox provider initialization failed; see the RPC error for details")
	}
	return driver, err
}

func newDriver() (runtimev1.RuntimeDriverServer, error) {
	cfg, err := config.LoadEnvironment()
	if err != nil {
		return nil, err
	}
	if cfg.MSBPath != "" {
		if _, err := resolvePair(cfg, microsandbox.ResolveRuntime); err != nil {
			return nil, err
		}
	}
	return server.New(msb.Client{
		Binary:            cfg.MSBPath,
		ImageCLI:          cfg.ImageBuilder,
		StrictImageLookup: cfg.StrictImageLookup,
	}, cfg, version)
}
