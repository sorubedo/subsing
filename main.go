package main

import (
	"context"
	"fmt"
	"os"
)

func main() {
	skipExisting := false
	args := os.Args[1:]
	for len(args) > 0 && (args[0] == "--skip-existing") {
		skipExisting = true
		args = args[1:]
	}
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: subsing [--skip-existing] <input-directory> <output-directory>")
		os.Exit(2)
	}
	result, err := Run(context.Background(), args[0], args[1], skipExisting)
	if err != nil {
		fmt.Fprintln(os.Stderr, "subsing:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stdout, "generated %d configuration file(s)\n", result.Files)
}
