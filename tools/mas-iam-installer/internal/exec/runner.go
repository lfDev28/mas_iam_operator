package exec

import (
	"context"
	"fmt"
	"io"
	"os"
	osexec "os/exec"
	"strings"
)

type Options struct {
	Name string
	Args []string
	Dir  string
	Env  map[string]string
}

type Runner struct{}

func NewRunner() *Runner {
	return &Runner{}
}

func (r *Runner) LookPath(name string) (string, error) {
	return osexec.LookPath(name)
}

func (r *Runner) Output(ctx context.Context, options Options) (string, error) {
	cmd := commandContext(ctx, options)
	output, err := cmd.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(output))
		if trimmed == "" {
			return "", fmt.Errorf("%s failed: %w", commandString(options), err)
		}
		return "", fmt.Errorf("%s failed: %w: %s", commandString(options), err, trimmed)
	}
	return string(output), nil
}

func (r *Runner) Stream(ctx context.Context, options Options, stdout, stderr io.Writer) error {
	cmd := commandContext(ctx, options)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %w", commandString(options), err)
	}
	return nil
}

func (r *Runner) Interactive(ctx context.Context, options Options, stdin io.Reader, stdout, stderr io.Writer) error {
	cmd := commandContext(ctx, options)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %w", commandString(options), err)
	}
	return nil
}

func commandContext(ctx context.Context, options Options) *osexec.Cmd {
	cmd := osexec.CommandContext(ctx, options.Name, options.Args...)
	cmd.Dir = options.Dir
	cmd.Env = mergedEnv(options.Env)
	return cmd
}

func commandString(options Options) string {
	if len(options.Args) == 0 {
		return options.Name
	}
	return fmt.Sprintf("%s %s", options.Name, strings.Join(options.Args, " "))
}

func mergedEnv(overrides map[string]string) []string {
	env := os.Environ()
	if len(overrides) == 0 {
		return env
	}
	for key, value := range overrides {
		env = append(env, fmt.Sprintf("%s=%s", key, value))
	}
	return env
}
