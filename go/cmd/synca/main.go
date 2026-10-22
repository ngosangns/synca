package main

import (
	"os"

	"github.com/ngosangns/synca/go/internal/cli"
)

func main() {
	if code := cli.Finish(cli.Run(os.Args[1:]), os.Stderr); code != 0 {
		os.Exit(code)
	}
}
