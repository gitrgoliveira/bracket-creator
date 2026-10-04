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
// the words, with a word boundary required right after "win" (a struct or
// template fragment such as `{word} win` is covered; a JSX-shaped attribute
// run such as `variant="default" winner={w}` is not, since "win" there runs
// straight into "ner"); and the sentence the other way round, "wins/won/
// winning ... by default" within 40 characters, which may now cross a line
// break (it stops dead at a literal "."). A string built from adjacent
// literals joined by "+" is read as the one sentence a person sees
// (flattenStringConcat), not as two literals neither of which matches alone.
// TestNoDefaultWinInStringLiterals asks both patterns of every Go string
// literal under internal/ and cmd/; TestNoDefaultWinInOpenAPISpec asks them
// of specs/openapi.yaml as raw text, since that file is YAML, not Go, and no
// other gate reads it.

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
	// and Python \s (both Unicode-aware) would still catch it. The trailing
	// \b (bc-cse) requires a word boundary right after "win": without it, a
	// JSX attribute run such as `variant="default" winner={w}` matched too,
	// reading the "win" inside "winner" as the forbidden word.
	regexp.MustCompile(`(?i)default[\s\x{00a0}{}"'+-]+(?:wins?|loss(?:es)?)\b`),
	// "wins/won/winning ... by default" the other way round, e.g. "The
	// opponent wins 2-0 by default.", "the match was won by default." The
	// gap is [^.]{0,40}, not [^.\n]{0,40} (bc-cse): a sentence a formatter or
	// a YAML description wraps onto a second line is exactly the shape this
	// widening exists to catch (the same class of miss the JS and docs gates
	// fixed for the "default ... win" pattern above), and the 40-character
	// bound alone already keeps the match inside one sentence -- a literal
	// "." still stops it cold, so it never reaches into an unrelated "by
	// default" two sentences later.
	regexp.MustCompile(`(?i)\b(?:wins?|won|winning|winners?|loses|lost|losing)\b[^.]{0,40}\bby default\b`),
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

// TestDefaultWinMatches pins both patterns in defaultWinLiteralPatterns
// directly against defaultWinMatches, the same function
// TestNoDefaultWinInStringLiterals and TestNoDefaultWinInOpenAPISpec ask for
// every string literal / raw text in the tree. Pattern 0's separator class
// covers the one-or-more-separator fix (bc-cse): a plain space, a hyphen, a
// non-breaking space (U+00A0, which Go's ASCII-only \s would otherwise
// miss), and quote/plus characters left behind by flattening a "+" chain of
// adjacent string literals into the one sentence a person reads
// (flattenStringConcat) all count as prose and must match; camelCase and
// snake_case identifier text, which carries no separator this class
// recognizes, must not; nor must a JSX-shaped attribute run where "win" is
// not its own word (bc-cse's trailing \b). Pattern 1 now also catches "won"
// and "winning" (past/participle forms a bare "wins?" alternation missed)
// and crosses a line break (bc-cse: a formatter-wrapped sentence, or a
// wrapped YAML description line, is exactly the shape this exists to catch),
// while still stopping dead at a sentence boundary.
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
		{name: "was won by default", value: "the match was won by default", want: true},
		{name: "winning by default", value: "winning by default is not shown that way", want: true},
		{name: "wins by default across a line break", value: "the opponent wins\nby default", want: true},
		{name: "default wins plural", value: "default wins", want: true},
		{name: "default loss", value: "default loss", want: true},
		{name: "the winner by default", value: "the winner by default", want: true},
		{name: "loses by default", value: "loses by default", want: true},
		{name: "camelCase identifier", value: "isDefaultWin", want: false},
		{name: "PascalCase identifier", value: "DefaultWinStands", want: false},
		{name: "snake_case identifier", value: "default_win_stands", want: false},
		{name: "PascalCase loss identifier", value: "DefaultLossCount", want: false},
		{name: "a JSX attribute run, not the forbidden phrase", value: `variant="default" winner={w}`, want: false},
		{name: "won, then a new sentence starting with by default", value: "Kyoto won. By default the next match starts", want: false},
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
// comment, not an identifier) matching either pattern in
// defaultWinLiteralPatterns (see defaultWinMatches), naming the file:line of
// each. A comment or an identifier never lands in this list: go/ast's
// BasicLit nodes are the parsed tokens of the source, which do not include
// comments, and an identifier (IsDefaultWinDecisionStr, DefaultWinIppons,
// HoldReasonDefaultWinStands as a name) is an *ast.Ident, not a *ast.BasicLit
// of kind STRING. specs/openapi.yaml is not Go source, so it is not walked
// here; TestNoDefaultWinInOpenAPISpec below covers it instead, against the
// same two patterns.
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

// TestNoDefaultWinInOpenAPISpec scans specs/openapi.yaml as raw TEXT, not as
// Go source (it is YAML, so go/parser cannot read it and TestNoDefaultWin
// InStringLiterals never walks it), against the same two patterns
// defaultWinMatches asks of every Go string literal. It matches the whole
// file text rather than line by line, for the same reason the JS gate's
// scanWholeFile and docs/check_prose.py's paragraph check do: a YAML
// description's prose sentence commonly wraps across physical lines (the
// description renders as one block regardless of where its source line
// breaks), so "a withdrawal gives the opponent the win" ending one line and
// "by default" starting the next is one violation a line-by-line check
// cannot see. The line reported is where the MATCH starts, counted from the
// newlines in the text before it, matching the convention every other gate
// in this family uses.
func TestNoDefaultWinInOpenAPISpec(t *testing.T) {
	root := repoRootForDefaultWinGate(t)
	path := filepath.Join(root, "specs", "openapi.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	text := string(data)
	var violations []string
	for _, re := range defaultWinLiteralPatterns {
		for _, loc := range re.FindAllStringIndex(text, -1) {
			line := strings.Count(text[:loc[0]], "\n") + 1
			excerpt := strings.Join(strings.Fields(text[loc[0]:loc[1]]), " ")
			violations = append(violations, fmt.Sprintf("%s:%d: %s", path, line, excerpt))
		}
	}
	if len(violations) > 0 {
		t.Fatalf("found %d \"default win\" / \"wins ... by default\" match(es) in specs/openapi.yaml (operator ruling 2026-10-04: kendo has no such term; name the decision -- kiken, fusenpai, or fusensho -- instead):\n%s",
			len(violations), strings.Join(violations, "\n"))
	}
}
