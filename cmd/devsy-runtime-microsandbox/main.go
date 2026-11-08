// Command devsy-runtime-microsandbox serves the external MicroSandbox runtime.
package main

import (
	"fmt"
	"os"

	"github.com/devsy-org/devsy-provider-microsandbox/internal/config"
	"github.com/devsy-org/devsy-provider-microsandbox/internal/msb"
	"github.com/devsy-org/devsy-provider-microsandbox/internal/server"
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
	driver, err := newDriver()
	if err != nil {
		// The message goes to stderr for logs and to every RPC so Devsy shows it.
		fmt.Fprintln(os.Stderr, err)
		driver = server.NewConfigError(err)
	}
	sdkserver.Serve(driver)
	return 0
}

func newDriver() (runtimev1.RuntimeDriverServer, error) {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return nil, err
	}
	if cfg.MSBPath != "" {
		if _, err := resolvePair(cfg, microsandbox.ResolveRuntime); err != nil {
			return nil, err
		}
	}
	return server.New(msb.Client{Binary: cfg.MSBPath}, cfg, version)
}
