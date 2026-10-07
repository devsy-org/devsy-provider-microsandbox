// Command devsy-runtime-microsandbox is the external MicroSandbox runtime entry point.
package main

import (
	"fmt"
	"os"
)

var version = "dev"

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		if _, err := fmt.Fprintln(os.Stdout, "devsy-runtime-microsandbox", version); err != nil {
			os.Exit(1)
		}
		return
	}
	fmt.Fprintln(
		os.Stderr,
		"MicroSandbox runtime serving is not available yet; use Devsy's built-in provider",
	)
	os.Exit(1)
}
