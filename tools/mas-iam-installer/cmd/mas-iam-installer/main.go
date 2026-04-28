package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/app"
)

func main() {
	command := app.NewRootCommand()
	if name := filepath.Base(os.Args[0]); name != "" {
		command.Use = name
	}

	if err := command.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
