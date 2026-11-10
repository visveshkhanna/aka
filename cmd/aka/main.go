// Command aka gives any local dev server a stable named URL.
package main

import (
	"os"

	"aka/internal/cli"
)

func main() { os.Exit(cli.Main(os.Args[1:])) }
