// Command glose is the single glose executable: command line and, later,
// the local review server (D-16, D-22).
package main

import (
	"os"

	"github.com/ptitan/glose/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
