package main

import (
	"context"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: subsing <input-directory> <output-directory>")
		os.Exit(2)
	}
	result, err := Run(context.Background(), os.Args[1], os.Args[2])
	if err != nil {
		fmt.Fprintln(os.Stderr, "subsing:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stdout, "generated %d configuration file(s)\n", result.Files)
}
