// Command gendocs generates the lifecycle hooks reference table from hooks.go
// godoc and splices it into HOOKS.md and README files.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	tableStart = "<!-- testrig:gendocs:table -->"
	tableEnd   = "<!-- /testrig:gendocs:table -->"
)

var spliceTargets = []string{
	"HOOKS.md",
	"README.md",
	"tools/test/README.md",
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "gendocs: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	root, err := moduleRoot()
	if err != nil {
		return err
	}

	table, err := buildHooksTable(filepath.Join(root, "hooks.go"))
	if err != nil {
		return err
	}

	for _, rel := range spliceTargets {
		path := filepath.Join(root, rel)
		if err := spliceFile(path, table); err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
	}
	return nil
}

func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.mod not found")
		}
		dir = parent
	}
}

func spliceFile(path, table string) error {
	content, err := os.ReadFile(path) //nolint:gosec // G304: fixed repo paths
	if err != nil {
		return err
	}

	updated, err := splice(string(content), table)
	if err != nil {
		return err
	}
	if updated == string(content) {
		return nil
	}
	return os.WriteFile(path, []byte(updated), 0o644) //nolint:gosec // G306: markdown docs
}

func splice(content, table string) (string, error) {
	start := strings.Index(content, tableStart)
	if start < 0 {
		return "", fmt.Errorf("missing %s", tableStart)
	}
	end := strings.Index(content[start:], tableEnd)
	if end < 0 {
		return "", fmt.Errorf("missing %s", tableEnd)
	}
	end += start

	var b strings.Builder
	b.WriteString(content[:start])
	b.WriteString(tableStart)
	b.WriteByte('\n')
	b.WriteString(table)
	b.WriteByte('\n')
	b.WriteString(tableEnd)
	b.WriteString(content[end+len(tableEnd):])
	return b.String(), nil
}
