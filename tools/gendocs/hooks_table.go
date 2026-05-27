package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"unicode"

	"github.com/smartcontractkit/testrig/internal/hooks"
)

type hookRow struct {
	Name string
	When string
	CLI  string
}

func buildHooksTable(hooksGoPath string) (string, error) {
	rows, err := hookRowsFromSource(hooksGoPath)
	if err != nil {
		return "", err
	}
	return renderHooksTable(rows), nil
}

func hookRowsFromSource(hooksGoPath string) ([]hookRow, error) {
	src, err := os.ReadFile(hooksGoPath) //nolint:gosec // G304: fixed repo path
	if err != nil {
		return nil, err
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, hooksGoPath, src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse hooks.go: %w", err)
	}

	docs := make(map[string]string)
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name == nil || fn.Doc == nil {
			continue
		}
		docs[fn.Name.Name] = strings.TrimSpace(fn.Doc.Text())
	}

	var rows []hookRow
	for _, e := range hooks.Catalog {
		docText, ok := docs[e.Name]
		if !ok {
			return nil, fmt.Errorf("hooks.go missing documented func %s", e.Name)
		}
		when, err := whenItRuns(docText)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name, err)
		}
		rows = append(rows, hookRow{
			Name: e.Name,
			When: when,
			CLI:  e.CLIFlag(),
		})
	}
	return rows, nil
}

// whenItRuns extracts the lifecycle timing phrase from a registrar godoc line.
// Expected form: "GlobalSetup registers a hook to run once before any tests."
func whenItRuns(docText string) (string, error) {
	docText = strings.TrimSpace(docText)
	const needle = " registers a hook to "
	if i := strings.Index(docText, needle); i >= 0 {
		phrase := strings.TrimSpace(docText[i+len(needle):])
		phrase = strings.TrimSuffix(phrase, ".")
		if phrase == "" {
			return "", fmt.Errorf("empty timing phrase in %q", docText)
		}
		return capitalizeFirst(phrase), nil
	}
	return "", fmt.Errorf("doc must contain %q: %s", needle, docText)
}

func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

func renderHooksTable(rows []hookRow) string {
	var b strings.Builder
	b.WriteString("| Option | When it runs | CLI equivalent |\n")
	b.WriteString("| ------ | ------------ | -------------- |\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "| `testrig.%s` | %s | `%s` |\n", r.Name, r.When, r.CLI)
	}
	return strings.TrimRight(b.String(), "\n")
}
