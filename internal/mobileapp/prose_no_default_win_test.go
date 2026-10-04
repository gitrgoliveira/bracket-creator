package mobileapp

// Gate for the operator rule (2026-10-04): the term "default win" does not
// exist in kendo and must never appear in anything a PERSON reads. Every
// finished match has a result -- a scoreline, or a registered decision that
// names the winner (kiken, fusenpai, fusensho, hantei) -- and every sentence
// naming one must say which, never reach for a generic "default win" label
// (domain.DecisionWord is the one owner of that word). Code identifiers
// (IsDefaultWinDecisionStr, DefaultWinIppons, the wire code
// "default_win_stands", ...) and Go comments are exempt: this gate parses
// only STRING LITERALS, which is exactly what a person-facing message, an
// error sentence, a history reason or an openapi description is built from.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var defaultWinLiteralPattern = regexp.MustCompile(`(?i)default[ -]win`)

// repoRootForDefaultWinGate walks up from the package's own directory to the
// module root (the directory holding go.mod), so the gate does not depend on
// the test's working directory or on this file's location within the repo.
func repoRootForDefaultWinGate(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find go.mod above %s", dir)
		}
		dir = parent
	}
}

// TestNoDefaultWinInStringLiterals parses every non-test .go file under
// internal/ and cmd/ and fails on any string literal (basic lit, not a
// comment, not an identifier) matching /default[ -]win/i, naming the
// file:line of each. A comment or an identifier never lands in this list:
// go/ast's BasicLit nodes are the parsed tokens of the source, which do not
// include comments, and an identifier (IsDefaultWinDecisionStr,
// DefaultWinIppons, HoldReasonDefaultWinStands as a name) is an *ast.Ident,
// not a *ast.BasicLit of kind STRING.
func TestNoDefaultWinInStringLiterals(t *testing.T) {
	root := repoRootForDefaultWinGate(t)
	var violations []string
	for _, sub := range []string{"internal", "cmd"} {
		start := filepath.Join(root, sub)
		err := filepath.Walk(start, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				if info.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			fset := token.NewFileSet()
			file, perr := parser.ParseFile(fset, path, nil, 0)
			if perr != nil {
				return fmt.Errorf("parsing %s: %w", path, perr)
			}
			ast.Inspect(file, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				value := lit.Value
				if unquoted, uerr := strconv.Unquote(lit.Value); uerr == nil {
					value = unquoted
				}
				if defaultWinLiteralPattern.MatchString(value) {
					pos := fset.Position(lit.Pos())
					violations = append(violations, fmt.Sprintf("%s:%d: %s", pos.Filename, pos.Line, lit.Value))
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", start, err)
		}
	}
	if len(violations) > 0 {
		t.Fatalf("found %d string literal(s) naming \"default win\" (operator ruling 2026-10-04: kendo has no such term; name the decision -- kiken, fusenpai, or fusensho -- instead, via domain.DecisionWord):\n%s",
			len(violations), strings.Join(violations, "\n"))
	}
}
