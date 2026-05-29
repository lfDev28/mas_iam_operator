package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func OpenRunLog(repoRoot, command string) (*os.File, string, error) {
	logDir := filepath.Join(repoRoot, ".mas-est-installer", "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, "", fmt.Errorf("create log directory: %w", err)
	}

	filename := fmt.Sprintf("%s-%s.log", time.Now().UTC().Format("20060102T150405Z"), command)
	path := filepath.Join(logDir, filename)

	file, err := os.Create(path)
	if err != nil {
		return nil, "", fmt.Errorf("create log file: %w", err)
	}

	return file, path, nil
}
