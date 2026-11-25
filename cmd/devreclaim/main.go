package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/LoicRU/devreclaim/internal/cli"
)

func main() {
	if err := cli.NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)

		var exit *cli.ExitError
		if errors.As(err, &exit) {
			os.Exit(exit.Code)
		}

		os.Exit(1)
	}
}
