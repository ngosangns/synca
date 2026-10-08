package main

import (
	"fmt"
	"os"

	"github.com/ngosangns/synca/go/internal/cli"
	"github.com/ngosangns/synca/go/internal/tui"
)

func main() {
	if err := cli.Run(os.Args[1:], tui.Run); err != nil {
		fmt.Fprintf(os.Stderr, "synca failed: %v\n", err)
		os.Exit(1)
	}
}
