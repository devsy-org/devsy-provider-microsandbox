// Command devsy-runtime-microsandbox serves the external MicroSandbox runtime.
package main

import (
	"fmt"
	"os"

	"github.com/devsy-org/devsy-provider-microsandbox/internal/config"
	"github.com/devsy-org/devsy-provider-microsandbox/internal/msb"
	"github.com/devsy-org/devsy-provider-microsandbox/internal/server"
	sdkserver "github.com/devsy-org/devsy-runtime-sdk/server"
)

var version = "dev"

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: devsy-runtime-microsandbox --version|serve")
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
	default:
		fmt.Fprintln(os.Stderr, "usage: devsy-runtime-microsandbox --version|serve")
		return 1
	}
}

func serve() int {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	driver, err := server.New(msb.Client{}, cfg, version)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	sdkserver.Serve(driver)
	return 0
}
