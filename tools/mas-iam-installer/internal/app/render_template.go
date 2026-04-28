package app

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

type renderTemplateOptions struct {
	vars string
}

func newRenderTemplateCommand() *cobra.Command {
	opts := &renderTemplateOptions{}

	command := &cobra.Command{
		Use:    "render-template [input] [output]",
		Short:  "Render a template by replacing environment variable placeholders",
		Hidden: true,
		Args:   cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.run(args[0], args[1])
		},
	}

	command.Flags().StringVar(&opts.vars, "vars", "", "Comma-separated allow list of variables to replace")
	return command
}

func (o *renderTemplateOptions) run(inputPath, outputPath string) error {
	raw, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("read template %s: %w", inputPath, err)
	}

	allowed := parseAllowedVars(o.vars)
	rendered := os.Expand(string(raw), func(name string) string {
		if len(allowed) > 0 {
			if _, ok := allowed[name]; !ok {
				return "${" + name + "}"
			}
		}
		return os.Getenv(name)
	})

	if err := os.WriteFile(outputPath, []byte(rendered), 0o644); err != nil {
		return fmt.Errorf("write rendered template %s: %w", outputPath, err)
	}
	return nil
}

func parseAllowedVars(raw string) map[string]struct{} {
	allowed := map[string]struct{}{}
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		allowed[item] = struct{}{}
	}
	return allowed
}
