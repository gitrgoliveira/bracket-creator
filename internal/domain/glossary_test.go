package domain_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGlossaryAllSpecTermsRepresented parses glossary.md (the locked
// source of truth) and asserts every `### TermName` heading has a
// corresponding entry in domain.Glossary keyed by the lowercase
// hyphenated romaji. Catches "added a term to the spec but forgot the
// Go side" drift, which would otherwise break the JS <Term name=...>
// component at runtime with no compile-time signal.
func TestGlossaryAllSpecTermsRepresented(t *testing.T) {
	specPath := findGlossarySpec(t)
	body, err := os.ReadFile(specPath) //nolint:gosec // test reads a checked-in spec file
	require.NoError(t, err, "read glossary.md")

	// Match `### TermName` headings (skip the `## Tier` h2s and other
	// non-term sections like "## Audience" / "## Implementation
	// checklist"). The first token on the heading is the romaji form
	// we use as the map ID; the kanji follows in parens.
	//
	// Examples we want to capture:
	//   ### Hikiwake (引き分け)
	//   ### Ippon-shobu (一本勝負)
	//   ### Kachinuki-exhaustion
	headingRe := regexp.MustCompile(`(?m)^### ([A-Za-z][A-Za-z0-9-]*)`)
	matches := headingRe.FindAllStringSubmatch(string(body), -1)
	require.NotEmpty(t, matches, "glossary.md must contain at least one term heading")

	for _, m := range matches {
		raw := m[1]
		id := strings.ToLower(raw)
		t.Run(id, func(t *testing.T) {
			entry, ok := domain.Glossary[id]
			assert.True(t, ok, "missing entry in domain.Glossary for %q (heading: %q)", id, raw)
			if ok {
				assert.Equal(t, id, entry.ID, "Term.ID must equal map key for %q", id)
				assert.NotEmpty(t, entry.Short, "Term.Short must not be empty for %q", id)
				assert.NotEmpty(t, entry.Tooltip, "Term.Tooltip must not be empty for %q", id)
			}
		})
	}
}

// TestGlossarySeeAlsoResolves pins the cross-reference invariant: every
// ID in a Term.SeeAlso list must itself be a key in the map. Catches
// typos (e.g. "ippon-shobu" mistyped "ipponshobu") that would render as
// a broken nested <Term> in the JS popover.
func TestGlossarySeeAlsoResolves(t *testing.T) {
	for id, term := range domain.Glossary {
		for _, ref := range term.SeeAlso {
			refLower := strings.ToLower(ref)
			_, ok := domain.Glossary[refLower]
			assert.True(t, ok, "Term %q has unresolved SeeAlso reference %q", id, ref)
		}
	}
}

// TestGlossaryIDsAreLowercase pins the convention that map keys are
// lowercase romaji (the convention the JS <Term name=...> prop relies
// on). A capitalised entry would silently miss every lookup site.
func TestGlossaryIDsAreLowercase(t *testing.T) {
	for id := range domain.Glossary {
		assert.Equal(t, strings.ToLower(id), id, "map key %q must be lowercase", id)
		assert.Equal(t, id, domain.Glossary[id].ID, "Term.ID must equal map key for %q", id)
	}
}

// TestGlossaryTooltipNoUntranslatedTerms is the "no jargon-translating-
// to-jargon" check from glossary.md §Tone register: a tooltip whose
// definition uses another untranslated kendo term is broken. We allow
// references via SeeAlso (the JSX renders them as nested <Term>
// popovers); we surface any OTHER glossary term that appears in the
// tooltip text without being declared in SeeAlso. The check is
// best-effort — it catches "tooltip mentions ippon-shobu but SeeAlso
// is empty" and similar drift.
//
// Compound terms: when a tooltip mentions "ippon-shobu", we don't
// also flag a missing SeeAlso entry for the bare "ippon" hit inside
// the compound — the compound covers the volunteer's lookup path.
// We strip every SeeAlso'd compound from the tooltip before scanning
// for bare prefixes.
func TestGlossaryTooltipNoUntranslatedTerms(t *testing.T) {
	for id, term := range domain.Glossary {
		seen := make(map[string]bool)
		for _, ref := range term.SeeAlso {
			seen[strings.ToLower(ref)] = true
		}
		tooltipLower := strings.ToLower(term.Tooltip)
		// Mask every SeeAlso'd ID with spaces so a prefix-of-a-compound
		// (e.g. "ippon" inside "ippon-shobu") doesn't spuriously
		// trigger when the compound is already declared.
		masked := tooltipLower
		for ref := range seen {
			masked = strings.ReplaceAll(masked, ref, strings.Repeat(" ", len(ref)))
		}
		for otherID := range domain.Glossary {
			if otherID == id {
				continue
			}
			if seen[otherID] {
				continue
			}
			pattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(otherID) + `\b`)
			if pattern.MatchString(masked) {
				t.Errorf(
					"Term %q tooltip mentions glossary term %q but does not list it in SeeAlso (would render as plain text)",
					id, otherID,
				)
			}
		}
	}
}

// TestGlossaryHasAtLeast22Terms is the count guard — the spec ships at
// least the agreed 22 terms (current count is 23 after the kachinuki-
// exhaustion addition). A future deletion that drops below 22 should
// fail loudly so it gets reviewed against the spec.
func TestGlossaryHasAtLeast22Terms(t *testing.T) {
	assert.GreaterOrEqual(t, len(domain.Glossary), 22, "glossary should contain at least 22 entries (got %d)", len(domain.Glossary))
}

// TestLookupCaseInsensitive pins the convenience-accessor contract:
// lookups by ID are case- and whitespace-insensitive so callers can
// pass whatever they have (e.g. an upper-case label from a UI button).
func TestLookupCaseInsensitive(t *testing.T) {
	t.Run("exact lowercase", func(t *testing.T) {
		term, ok := domain.Lookup("kiken")
		assert.True(t, ok)
		assert.Equal(t, "kiken", term.ID)
	})
	t.Run("uppercase", func(t *testing.T) {
		term, ok := domain.Lookup("KIKEN")
		assert.True(t, ok)
		assert.Equal(t, "kiken", term.ID)
	})
	t.Run("mixed case with whitespace", func(t *testing.T) {
		term, ok := domain.Lookup("  Ippon-Shobu  ")
		assert.True(t, ok)
		assert.Equal(t, "ippon-shobu", term.ID)
	})
	t.Run("unknown", func(t *testing.T) {
		_, ok := domain.Lookup("bogus")
		assert.False(t, ok)
	})
}

// TestResolveReasonHuman is the pattern-resolution table for the
// engine-emitted reason fragments wired into 409 error responses.
// Pins the wire shape so a refactor to the patterns breaks the test
// rather than silently shipping "kiken at m_12" to the operator UI.
func TestResolveReasonHuman(t *testing.T) {
	cases := []struct {
		name   string
		reason string
		want   string
	}{
		{"kiken at match", "kiken at m_12", "withdrew from match m_12"},
		{"fusenpai at match", "fusenpai at m_007", "no-show forfeit at match m_007"},
		{"fusensho at match", "fusensho at pool-A-1", "bye-win at match pool-A-1"},
		{"daihyosen at match", "daihyosen at r3-m1", "representative bout at match r3-m1"},
		{"kachinuki-exhaustion at match", "kachinuki-exhaustion at k_5", "team exhausted at match k_5"},

		// Bare-term fallback path — when the engine emits just the
		// canonical decision name without a match context.
		{"bare kiken", "kiken", "withdrawal"},
		{"bare fusenpai", "fusenpai", "no-show forfeit"},

		// Empty input / unknown patterns return "" so the caller can
		// omit the field via JSON omitempty.
		{"empty input", "", ""},
		{"whitespace only", "   ", ""},
		{"unknown pattern", "something else entirely", ""},
		{"unknown verb at match", "thinking at m_1", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := domain.ResolveReasonHuman(tc.reason)
			assert.Equal(t, tc.want, got)
		})
	}
}

// findGlossarySpec returns the absolute path to
// specs/003-tournament-gap-closure/glossary.md, walking up from the
// test file's package directory so the test runs from any CWD.
func findGlossarySpec(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")
	dir := filepath.Dir(file)
	for i := 0; i < 6; i++ {
		candidate := filepath.Join(dir, "specs", "003-tournament-gap-closure", "glossary.md")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("could not locate specs/003-tournament-gap-closure/glossary.md from %s", filepath.Dir(file))
	return ""
}
