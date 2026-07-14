package main

import (
	"fmt"
	"miniscribe/internal/cli"
	"os"
)

func main() {
	if err := cli.RootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
