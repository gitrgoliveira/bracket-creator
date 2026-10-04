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
// Two patterns are asked (defaultWinMatches), the same pair the client-side
// gates use: "default win" spelled with whitespace/braces/quotes/"+" between
// the words (a struct or template fragment such as `{word} win` included),
// and the sentence the other way round, "wins ... by default" within 40
// characters. A string built from adjacent literals joined by "+" is read as
// the one sentence a person sees (flattenStringConcat), not as two literals
// neither of which matches alone.

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

var defaultWinLiteralPatterns = []*regexp.Regexp{
	// "default win", "default-win", "DefaultWin" rendered as prose (braces,
	// quotes, "+" and a literal hyphen cover a value built from a Go string
	// concatenation or a struct/template literal fragment such as `{word}
	// win`). The separator class requires ONE OR MORE characters, matching
	// the client gates (web-mobile/check-write-result.mjs,
	// docs/check_prose.py): a zero-or-more class would also match camelCase
	// identifier text with no separator at all ("isDefaultWin",
	// "DefaultWinStands"), which is an identifier, not prose. \x{00a0} is
	// U+00A0 NO-BREAK SPACE: Go's RE2 \s is ASCII-only, so without it a
	// non-breaking space between the words would pass this gate while the JS
	// and Python \s (both Unicode-aware) would still catch it.
	regexp.MustCompile(`(?i)default[\s\x{00a0}{}"'+-]+win`),
	// "wins ... by default" (and "win ... by default") the other way round,
	// e.g. "The opponent wins 2-0 by default."
	regexp.MustCompile(`(?i)\bwins?\b[^.\n]{0,40}\bby default\b`),
}

// defaultWinMatches reports whether value contains either agreed phrasing
// for the forbidden term (see prose_no_default_win_test.go's package
// comment); it is the one place both patterns are asked.
func defaultWinMatches(value string) bool {
	for _, re := range defaultWinLiteralPatterns {
		if re.MatchString(value) {
			return true
		}
	}
	return false
}

// TestDefaultWinMatches pins the separator class in
// defaultWinLiteralPatterns[0] directly against defaultWinMatches, the same
// function TestNoDefaultWinInStringLiterals asks for every string literal in
// the tree. It covers the one-or-more-separator fix (bc-cse): a plain space,
// a hyphen, a non-breaking space (U+00A0, which Go's ASCII-only \s would
// otherwise miss), and quote/plus characters left behind by flattening a "+"
// chain of adjacent string literals into the one sentence a person reads
// (flattenStringConcat) all count as prose and must match; camelCase and
// snake_case identifier text, which carries no separator this class
// recognizes, must not.
func TestDefaultWinMatches(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "plain space", value: "default win", want: true},
		{name: "hyphen", value: "default-win", want: true},
		{name: "non-breaking space", value: "default win", want: true},
		{name: "quote-plus-quote joined text", value: `default" + "win`, want: true},
		{name: "wins by default sentence", value: "the opponent wins 2–0 by default", want: true},
		{name: "camelCase identifier", value: "isDefaultWin", want: false},
		{name: "PascalCase identifier", value: "DefaultWinStands", want: false},
		{name: "snake_case identifier", value: "default_win_stands", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := defaultWinMatches(tt.value)
			if got != tt.want {
				t.Errorf("defaultWinMatches(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

// flattenStringConcat reports the compile-time value of e when e is built
// entirely from string literals joined by "+" (optionally parenthesized),
// so a sentence split across adjacent literals (e.g. `"the default " +
// "win"`) is read as the one string a person actually sees, rather than as
// two literals neither of which matches alone. It reports ok=false as soon
// as any operand is not itself a string literal or such a chain (a variable
// operand, a non-ADD operator, ...), since that is not a compile-time
// sentence this gate can read.
func flattenStringConcat(e ast.Expr) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(v.Value)
		if err != nil {
			return "", false
		}
		return s, true
	case *ast.ParenExpr:
		return flattenStringConcat(v.X)
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", false
		}
		left, ok := flattenStringConcat(v.X)
		if !ok {
			return "", false
		}
		right, ok := flattenStringConcat(v.Y)
		if !ok {
			return "", false
		}
		return left + right, true
	default:
		return "", false
	}
}

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
				switch x := n.(type) {
				case *ast.BinaryExpr:
					// A "+" chain of string literals is the sentence a
					// person actually reads (e.g. "the default " + "win"),
					// which neither half matches alone. Flatten the whole
					// chain and stop descending into it: its own literals
					// are covered by this check, not by the BasicLit arm
					// below, which would otherwise test each half in
					// isolation and silently miss the join.
					if x.Op == token.ADD {
						if value, ok := flattenStringConcat(x); ok {
							if defaultWinMatches(value) {
								pos := fset.Position(x.Pos())
								violations = append(violations, fmt.Sprintf("%s:%d: %q (joined from adjacent string literals)", pos.Filename, pos.Line, value))
							}
							return false
						}
					}
				case *ast.BasicLit:
					if x.Kind != token.STRING {
						return true
					}
					value := x.Value
					if unquoted, uerr := strconv.Unquote(x.Value); uerr == nil {
						value = unquoted
					}
					if defaultWinMatches(value) {
						pos := fset.Position(x.Pos())
						violations = append(violations, fmt.Sprintf("%s:%d: %s", pos.Filename, pos.Line, x.Value))
					}
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
