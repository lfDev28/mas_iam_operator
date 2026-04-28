package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRenderTemplateReplacesAllVariablesWhenNoAllowList(t *testing.T) {
	t.Setenv("MAS_TEST_FOO", "foo")
	t.Setenv("MAS_TEST_BAR", "bar")

	inputPath, outputPath := writeRenderTemplateFixture(t, "foo=${MAS_TEST_FOO}\nbar=${MAS_TEST_BAR}\n")

	opts := &renderTemplateOptions{}
	if err := opts.run(inputPath, outputPath); err != nil {
		t.Fatalf("render template: %v", err)
	}

	assertRenderedTemplate(t, outputPath, "foo=foo\nbar=bar\n")
}

func TestRenderTemplateOnlyReplacesAllowedVariables(t *testing.T) {
	t.Setenv("MAS_TEST_FOO", "foo")
	t.Setenv("MAS_TEST_BAR", "bar")

	inputPath, outputPath := writeRenderTemplateFixture(t, "foo=${MAS_TEST_FOO}\nbar=${MAS_TEST_BAR}\n")

	opts := &renderTemplateOptions{vars: "MAS_TEST_FOO"}
	if err := opts.run(inputPath, outputPath); err != nil {
		t.Fatalf("render template: %v", err)
	}

	assertRenderedTemplate(t, outputPath, "foo=foo\nbar=${MAS_TEST_BAR}\n")
}

func writeRenderTemplateFixture(t *testing.T, template string) (string, string) {
	t.Helper()

	dir := t.TempDir()
	inputPath := filepath.Join(dir, "template.yaml")
	outputPath := filepath.Join(dir, "rendered.yaml")
	if err := os.WriteFile(inputPath, []byte(template), 0o644); err != nil {
		t.Fatalf("write template fixture: %v", err)
	}
	return inputPath, outputPath
}

func assertRenderedTemplate(t *testing.T, outputPath, expected string) {
	t.Helper()

	raw, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read rendered template: %v", err)
	}
	if string(raw) != expected {
		t.Fatalf("rendered template mismatch\nexpected:\n%s\ngot:\n%s", expected, string(raw))
	}
}
