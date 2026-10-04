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
// Three patterns are asked (defaultWinMatches), the same set the client-side
// gates use: "default win" spelled with whitespace/braces/quotes/"+" between
// the words, with a word boundary required right after "win" (a struct or
// template fragment such as `{word} win` is covered; a JSX-shaped attribute
// run such as `variant="default" winner={w}` is not, since "win" there runs
// straight into "ner"); "default winner(s)", the noun form the first
// pattern's trailing boundary cannot reach; and the sentence the other way
// round, "wins/won/winning/loses ... by default" within 40 characters (the
// contraction "won't" is excluded), which may cross a line break on either
// side of "by default" too (it stops dead at a literal "." or a Markdown
// table-cell "|"). A string built from adjacent literals joined by "+" is
// read as the one sentence a person sees (flattenStringConcat), not as two
// literals neither of which matches alone. TestNoDefaultWinInStringLiterals
// asks all three patterns of every Go string literal under internal/ and
// cmd/; TestNoDefaultWinInPersonReadText asks them of every other tracked,
// person-read text file outside docs/ (specs/openapi.yaml, every tracked
// specs/**/*.md, running_a_kendo_tournament.md, and every other tracked root
// *.md except CLAUDE.md and AGENTS.md) as raw text, since none of those are
// Go source and no other gate reads them; docs/ itself is covered by
// docs/check_prose.py instead.

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
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
	// "DefaultWinStands"), which is an identifier, not prose. \p{Zs} is the
	// Unicode "Space_Separator" category (bc-cse FIX D): Go's RE2 \s is
	// ASCII-only, so without it a non-breaking space (U+00A0) or any other
	// Unicode space between the words (U+2007, U+2009, U+202F, U+3000, ...)
	// would pass this gate while the JS and Python \s (both Unicode-aware
	// by spec, no extra class needed on those two gates) would still catch
	// it. The trailing \b (bc-cse) requires a word boundary right after
	// "win": without it, a JSX attribute run such as `variant="default"
	// winner={w}` matched too, reading the "win" inside "winner" as the
	// forbidden word.
	regexp.MustCompile(`(?i)default[\s\p{Zs}{}"'+-]+(?:wins?|loss(?:es)?)\b`),
	// "default winner(s)" (bc-cse FIX C): the noun form the pattern above
	// cannot reach, because its trailing \b sits right after "win"/"wins"
	// and "winner" fails that boundary (the "n" in "win" and the "n" in
	// "ner" are both word characters, so there is no boundary between
	// them) -- which is exactly the fix that stopped it reading the "win"
	// inside "winner" as a false hit. The separator here is whitespace
	// only (ASCII or Unicode Zs), deliberately narrower than the pattern
	// above's: a JSX/struct attribute run such as `variant="default"
	// winner={w}` separates the two words with a quote, which this class
	// excludes, so that shape still passes here too.
	regexp.MustCompile(`(?i)default[\s\p{Zs}]+winners?\b`),
	// "wins/won/winning/loses ... by default" the other way round, e.g.
	// "The opponent wins 2-0 by default.", "the match was won by
	// default." (bc-cse FIX A/B).
	//
	// Rebalanced to the decision wordings only -- wins, won, winning,
	// loses -- after "winners?", "lost" and "losing" produced real false
	// positives on ordinary settings/connection prose ("Winners per pool
	// is 2 by default.", "If the connection is lost, by default the app
	// retries.", "Points lost (PL) are hidden by default."): none of
	// those name a kendo decision, so they must not trip this rule.
	// "winners?" naming a decision is still caught by the "default
	// winner(s)" pattern above when the two words sit next to each other,
	// which is the shape that actually occurs.
	//
	// "won" excludes its own contraction "won't" without a lookahead
	// (RE2 has none): \bwon\b alone also matches inside "won't", because
	// the word-to-apostrophe transition is itself a \b. The alternative
	// instead requires the character right after "won" to be present and
	// NOT an apostrophe (or end of string) -- "wonder"/"wondering" never
	// reach this branch at all, since \bwon\b does not match a "won" that
	// continues into more word characters. That forced character also
	// excludes "." and "|" (not just "'"): consuming either as the
	// "anything but an apostrophe" character would let the "won" branch
	// itself swallow the sentence/cell boundary the gap below exists to
	// stop at, so "Kyoto won. By default..." (a new sentence, not this
	// one's "by default") would otherwise still match.
	//
	// The gap is [^.|]{0,40}, not [^.\n]{0,40} nor [^.]{0,40} (bc-cse): a
	// sentence a formatter or a YAML description wraps onto a second line
	// is exactly the shape the FIX A widening exists to catch, so a plain
	// newline must not stop the match; a literal "." or a Markdown
	// table-cell "|" still stops it cold, the latter so an unrelated cell
	// before or after a decision word is never bridged into this one
	// (bc-cse FIX B). The 40-character bound (plus "." and "|") keeps the
	// match inside one sentence/cell, so crossing a real line break buys
	// the wrapped shape without reaching into an unrelated "by default"
	// two sentences, or two cells, later. by[\s\p{Zs}]+default requires
	// only whitespace between "by" and "default" (bc-cse FIX A): the
	// literal single space this used to require missed the case where a
	// formatter wraps the line between THOSE two words rather than before
	// "by".
	regexp.MustCompile(`(?i)(?:\b(?:wins?|winning|loses)\b|\bwon\b(?:[^'.|]|$))[^.|]{0,40}\bby[\s\p{Zs}]+default\b`),
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

// TestDefaultWinMatches pins all three patterns in defaultWinLiteralPatterns
// directly against defaultWinMatches, the same function
// TestNoDefaultWinInStringLiterals and TestNoDefaultWinInPersonReadText ask
// for every string literal / raw text in the tree. The cases live in the
// shared Go/JS fixture testdata/default_win_cases.json rather than a
// hand-copied table here, so this test and the JS gate's own
// (web-mobile/js/__tests__/rule_gates.test.jsx) agree case for case: a plain
// space, a hyphen, a non-breaking space and other Unicode spaces (U+00A0,
// U+2007, U+2009, U+202F, U+3000, which Go's ASCII-only \s would otherwise
// miss -- bc-cse FIX D), and quote/plus characters left behind by
// flattening a "+" chain of adjacent string literals into the one sentence
// a person reads (flattenStringConcat) all count as prose and must match;
// camelCase and snake_case identifier text, which carries no separator the
// first pattern's class recognizes, must not; nor must a JSX-shaped
// attribute run where "win"/"winner" is not its own word (bc-cse's trailing
// \b). "default winner(s)" must match on its own even though the first
// pattern's trailing \b after "win" cannot reach it (bc-cse FIX C). The
// third pattern catches "won" and "winning" (past/participle forms a bare
// "wins?" alternation missed) and crosses a line break on either side of
// "by default" (bc-cse FIX A: a formatter-wrapped sentence, or a wrapped
// YAML description line, is exactly the shape this exists to catch), while
// still stopping dead at a sentence boundary or a Markdown table-cell "|";
// "winners?", "lost" and "losing" no longer trip it, since real prose about
// settings and connections used exactly those words without naming a
// decision (bc-cse FIX B), and "won't" is excluded from the "won" branch
// without a lookahead, which RE2 does not support.
func TestDefaultWinMatches(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "default_win_cases.json"))
	if err != nil {
		t.Fatalf("reading shared Go/JS default-win case table: %v", err)
	}
	var table struct {
		Cases []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
			Want  bool   `json:"want"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &table); err != nil {
		t.Fatalf("parsing shared Go/JS default-win case table: %v", err)
	}
	if len(table.Cases) == 0 {
		t.Fatal("shared default-win case table parsed to no cases: it would assert nothing")
	}
	for _, tt := range table.Cases {
		t.Run(tt.Name, func(t *testing.T) {
			got := defaultWinMatches(tt.Value)
			if got != tt.Want {
				t.Errorf("defaultWinMatches(%q) = %v, want %v", tt.Value, got, tt.Want)
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
// comment, not an identifier) matching any pattern in
// defaultWinLiteralPatterns (see defaultWinMatches), naming the file:line of
// each. A comment or an identifier never lands in this list: go/ast's
// BasicLit nodes are the parsed tokens of the source, which do not include
// comments, and an identifier (IsDefaultWinDecisionStr, DefaultWinIppons,
// HoldReasonDefaultWinStands as a name) is an *ast.Ident, not a *ast.BasicLit
// of kind STRING. specs/openapi.yaml and every other tracked, person-read
// text file outside docs/ are not Go source, so none of them are walked
// here; TestNoDefaultWinInPersonReadText below covers them instead, against
// the same patterns.
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

// personReadTrackedTextFiles returns every TRACKED, person-read text file
// this gate must sweep for the default-win patterns, beyond the Go string
// literals TestNoDefaultWinInStringLiterals already walks: specs/openapi.yaml,
// every tracked specs/**/*.md, and every other tracked root *.md (which
// includes running_a_kendo_tournament.md) EXCEPT CLAUDE.md and AGENTS.md.
// Those two are agent-instruction files allowed to quote the banned phrase
// itself in order to state the rule (as this very file's own package
// comment does), so sweeping them would fail on their own documentation of
// the gate, not on a violation of it. docs/ is covered separately, by
// docs/check_prose.py, so it is excluded here too.
//
// "Tracked" is asked of git rather than assumed from a filesystem walk,
// because most of specs/ is gitignored planning scratch (see CLAUDE.md's
// Governance section): a plain directory walk would also sweep an untracked
// draft that nobody ships and that may legitimately still say "default win"
// from before this ruling existed.
func personReadTrackedTextFiles(t *testing.T, root string) []string {
	t.Helper()
	cmd := exec.Command("git", "ls-files")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	exempt := map[string]bool{"CLAUDE.md": true, "AGENTS.md": true}
	var files []string
	for _, rel := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if rel == "" || exempt[rel] {
			continue
		}
		isRootMD := !strings.Contains(rel, "/") && strings.HasSuffix(rel, ".md")
		isSpecsMD := strings.HasPrefix(rel, "specs/") && strings.HasSuffix(rel, ".md")
		isOpenAPI := rel == "specs/openapi.yaml"
		if isRootMD || isSpecsMD || isOpenAPI {
			files = append(files, rel)
		}
	}
	sort.Strings(files)
	return files
}

// TestNoDefaultWinInPersonReadText scans every file personReadTrackedTextFiles
// names as raw TEXT, not as Go source (none of them are .go, so go/parser
// cannot read them and TestNoDefaultWinInStringLiterals never walks them),
// against the same patterns defaultWinMatches asks of every Go string
// literal. It matches the whole file text rather than line by line, for the
// same reason the JS gate's scanWholeFile and docs/check_prose.py's
// paragraph check do: a Markdown or YAML description's prose sentence
// commonly wraps across physical lines (the description renders as one
// block regardless of where its source line breaks), so "a withdrawal gives
// the opponent the win" ending one line and "by default" starting the next
// is one violation a line-by-line check cannot see. The line reported is
// where the MATCH starts, counted from the newlines in the text before it,
// matching the convention every other gate in this family uses. The failure
// message, and a successful run's log, both name every file that was
// actually read, so a gap in coverage is visible rather than silent.
func TestNoDefaultWinInPersonReadText(t *testing.T) {
	root := repoRootForDefaultWinGate(t)
	files := personReadTrackedTextFiles(t, root)
	if len(files) == 0 {
		t.Fatal("personReadTrackedTextFiles found no files to scan: it would assert nothing")
	}
	t.Logf("scanned %d tracked file(s) outside docs/: %s", len(files), strings.Join(files, ", "))
	var violations []string
	for _, rel := range files {
		path := filepath.Join(root, rel)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		text := string(data)
		for _, re := range defaultWinLiteralPatterns {
			for _, loc := range re.FindAllStringIndex(text, -1) {
				line := strings.Count(text[:loc[0]], "\n") + 1
				excerpt := strings.Join(strings.Fields(text[loc[0]:loc[1]]), " ")
				violations = append(violations, fmt.Sprintf("%s:%d: %s", rel, line, excerpt))
			}
		}
	}
	if len(violations) > 0 {
		t.Fatalf("found %d \"default win\" / \"default winner\" / \"wins ... by default\" match(es) across %d tracked file(s) (%s) (operator ruling 2026-10-04: kendo has no such term; name the decision -- kiken, fusenpai, or fusensho -- instead):\n%s",
			len(violations), len(files), strings.Join(files, ", "), strings.Join(violations, "\n"))
	}
}
