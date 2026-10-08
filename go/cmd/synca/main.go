package main

import (
	"fmt"
	"os"

	"github.com/ngosangns/synca/go/internal/cli"
)

func main() {
	if err := cli.Run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "synca failed: %v\n", err)
		os.Exit(1)
	}
}
