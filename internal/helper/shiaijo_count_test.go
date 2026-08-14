package helper

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wantShiaijoSuggestion is the "use ..." fragment the message must carry for a
// rejected count: the powers of two either side of n, with the upper one
// dropped above 16 because 32 exceeds the A-Z label cap. Spelled out as a
// table rather than derived from the production helper so the test cannot
// agree with a broken implementation by construction.
var wantShiaijoSuggestion = map[int]string{
	3:  "use 2 or 4, or 1",
	5:  "use 4 or 8, or 1",
	6:  "use 4 or 8, or 1",
	7:  "use 4 or 8, or 1",
	9:  "use 8 or 16, or 1",
	10: "use 8 or 16, or 1",
	11: "use 8 or 16, or 1",
	12: "use 8 or 16, or 1",
	13: "use 8 or 16, or 1",
	14: "use 8 or 16, or 1",
	15: "use 8 or 16, or 1",
	17: "use 16, or 1",
}

// TestValidateShiaijoCount sweeps the shiaijo-count rule across the whole
// operator-plausible range, 1..17. Only a POWER OF TWO is legal: 1 (which
// draws as two half-blocks merging with each other), 2, 4, 8 and 16. Every
// other count is rejected, including the EVEN ones -- 6 and 10 pass the old
// "1 or even" rule and are wrong under R9, because the draw's blocks merge in
// pairs and 3 or 5 blocks in a half cannot.
func TestValidateShiaijoCount(t *testing.T) {
	t.Parallel()

	valid := map[int]bool{1: true, 2: true, 4: true, 8: true, 16: true}

	for n := 1; n <= 17; n++ {
		t.Run(fmt.Sprintf("courts=%d", n), func(t *testing.T) {
			t.Parallel()
			err := ValidateShiaijoCount(n)
			if valid[n] {
				assert.NoErrorf(t, err, "%d shiaijo must be accepted", n)
				return
			}
			require.Errorf(t, err, "%d shiaijo must be rejected", n)
			msg := err.Error()
			assert.Contains(t, msg, fmt.Sprintf("got %d", n),
				"the message must name the rejected count")
			want, ok := wantShiaijoSuggestion[n]
			require.Truef(t, ok, "no expected suggestion recorded for %d", n)
			assert.Contains(t, msg, want,
				"the message must name the nearest valid counts AND 1")
			assert.Contains(t, msg, "merge in pairs",
				"the message must carry the canonical reason")
			assert.Contains(t, msg, "halve cleanly",
				"the message must carry the canonical reason")
		})
	}
}

// TestValidateShiaijoCountRejectsEvenNonPowersOfTwo is the regression pin for
// the rule change itself. 6 and 10 were ACCEPTED by the previous "1 or an even
// number" rule; under R9 they are rejected and the message must point at the
// powers of two either side rather than at the neighbouring even numbers.
func TestValidateShiaijoCountRejectsEvenNonPowersOfTwo(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		n    int
		want string
	}{
		{6, "use 4 or 8, or 1"},
		{10, "use 8 or 16, or 1"},
		{12, "use 8 or 16, or 1"},
	} {
		t.Run(fmt.Sprintf("courts=%d", tc.n), func(t *testing.T) {
			t.Parallel()
			err := ValidateShiaijoCount(tc.n)
			require.Errorf(t, err, "%d is even but not a power of two, so it must be rejected", tc.n)
			assert.Contains(t, err.Error(), tc.want)
			// The old rule's suggestion was n-1 / n+1. Those are odd here, so
			// naming them would be actively wrong advice.
			assert.NotContains(t, err.Error(), fmt.Sprintf("use %d or %d", tc.n-1, tc.n+1))
		})
	}
}

// TestValidateShiaijoCountMessageNeverReadsAsMinimumTwo pins the wording
// requirement that outlives any rephrasing: a 1-shiaijo competition is legal,
// so the error must never tell an operator they need at least two courts. It
// also pins that the message no longer states the retired "even" rule.
func TestValidateShiaijoCountMessageNeverReadsAsMinimumTwo(t *testing.T) {
	t.Parallel()

	err := ValidateShiaijoCount(3)
	require.Error(t, err)
	msg := strings.ToLower(err.Error())
	assert.Contains(t, msg, "power of two",
		"the rule must be stated as a power of two")
	assert.Contains(t, msg, "1, 2, 4, 8 or 16",
		"the message must enumerate every legal count")
	assert.Contains(t, msg, ", or 1",
		"1 must be offered as a valid answer")
	assert.NotContains(t, msg, "even",
		"the retired '1 or an even number' rule must not survive in the wording")
	assert.NotContains(t, msg, "at least 2",
		"the rule is not a minimum: a single-shiaijo competition is valid")
	assert.NotContains(t, msg, "at least two",
		"the rule is not a minimum: a single-shiaijo competition is valid")
}

// TestValidateShiaijoCountNonPositive documents the division of labour with
// ValidateCourts: 0 and negative counts are that validator's business (and the
// competition-level validators read an empty court list as "inherit the
// tournament's courts"), so the shiaijo-count rule stays silent about them
// rather than emitting a confusing "use -1 or 1" suggestion.
func TestValidateShiaijoCountNonPositive(t *testing.T) {
	t.Parallel()

	assert.NoError(t, ValidateShiaijoCount(0))
	assert.NoError(t, ValidateShiaijoCount(-3))
	assert.Error(t, ValidateCourts(0), "0 courts stays ValidateCourts' rejection")
}

// TestValidateShiaijoCountWithinLabelCap checks the two validators compose at
// the top of the range. 25 is a legal label count but an illegal allocation,
// and because 32 is beyond the A-Z cap the suggestion must name only 16 (plus
// 1) -- suggesting a count the operator could not label would be unactionable.
func TestValidateShiaijoCountWithinLabelCap(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateCourts(25), "25 is within the A-Z label cap")
	err := ValidateShiaijoCount(25)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "use 16, or 1")
	assert.NotContains(t, err.Error(), "32", "32 shiaijo cannot be labelled A-Z")
	assert.NoError(t, ValidateCourts(16), "the suggested 16 must itself be legal")

	// 16 is the ceiling of the rule, not just of the labels: 32 is a power of
	// two and is still refused.
	assert.NoError(t, ValidateShiaijoCount(16))
	assert.Error(t, ValidateShiaijoCount(32), "32 exceeds the A-Z label cap")
}

// TestLargestShiaijoCountAtMost pins the shared step-down every clamp uses. It
// must land on a power of two, never merely on an even number: 7 steps to 4,
// not to 6.
func TestLargestShiaijoCountAtMost(t *testing.T) {
	t.Parallel()

	cases := map[int]int{
		-1: 1, 0: 1, 1: 1, 2: 2, 3: 2, 4: 4, 5: 4, 6: 4, 7: 4,
		8: 8, 9: 8, 15: 8, 16: 16, 17: 16, 26: 16,
	}
	for in, want := range cases {
		t.Run(fmt.Sprintf("at_most_%d", in), func(t *testing.T) {
			t.Parallel()
			got := LargestShiaijoCountAtMost(in)
			assert.Equal(t, want, got)
			assert.NoErrorf(t, ValidateShiaijoCount(got),
				"the step-down must itself be a legal allocation")
		})
	}
}

// shiaijoRuleJSMirrors are the JS restatements of this rule, one per browser
// surface. Both are hand-written copies of ValidateShiaijoCount's wording, so
// both can drift from it silently.
//
// Both are now covered by suites the gate executes: web-mobile's vitest suite
// and web/'s, the latter only since web/package.json stopped stubbing "test"
// out with `echo 'no JS unit tests' && exit 0` while four real spec files sat
// unrun beside it. That stub is exactly how a stale assertion of the RETIRED
// "1 or an even number" rule survived in web/tests.
//
// This test still earns its place, because those suites check each mirror
// against ITSELF and cannot see the Go message at all. What it pins is the
// cross-language agreement: the shared REASON clause, the promise that 1 is
// always offered, and the absence of the retired rule. What it CANNOT pin is
// the derived value -- dropping 16 from the legal set leaves every literal it
// greps for intact -- which is precisely what the JS suites do catch.
var shiaijoRuleJSMirrors = []struct {
	surface string
	path    string
}{
	{"operator console (mobile app)", "../../web-mobile/js/admin_helpers.jsx"},
	{"CLI web form (classic UI)", "../../web/js/validation.js"},
}

// TestShiaijoRuleJSMirrorsMatchTheGoMessage pins every JS restatement of the
// rule against this package's message, which is its single source of truth.
//
// It asserts the parts an operator actually reads and that a rewording would
// silently break: the canonical REASON clause (identical prose on all three
// surfaces, so an operator who meets the rule in the CLI form and again in the
// console is told the same thing), the promise that 1 is always offered, the
// absence of the retired "even" rule, and the A-Z cap the legal set is derived
// from. The reason is written out here as a literal rather than taken from
// ValidateShiaijoCount so the test cannot agree with a reworded implementation
// by construction; the first assertion is what ties the literal back to Go.
func TestShiaijoRuleJSMirrorsMatchTheGoMessage(t *testing.T) {
	t.Parallel()

	const canonicalReason = "the knockout draw gives each shiaijo its own block of the bracket " +
		"and the blocks merge in pairs, so the count has to halve cleanly"

	err := ValidateShiaijoCount(3)
	require.Error(t, err)
	require.Contains(t, err.Error(), canonicalReason,
		"the Go message is the source of truth; update this literal AND every JS mirror together")

	for _, m := range shiaijoRuleJSMirrors {
		t.Run(m.surface, func(t *testing.T) {
			t.Parallel()

			raw, readErr := os.ReadFile(m.path)
			require.NoErrorf(t, readErr, "cannot read the %s mirror", m.surface)
			src := string(raw)
			// The NEGATIVE assertions run against code only. Both mirrors
			// document the two banned phrasings in prose ("must never read as
			// 'at least 2 shiaijo'"), which is the comment doing its job, so
			// asserting over the raw file would fail on the prohibition itself.
			code := jsCodeWithoutLineComments(src)

			assert.Containsf(t, src, canonicalReason,
				"the %s states the rule to operators and must give the same reason as ValidateShiaijoCount", m.surface)
			assert.Containsf(t, code, ", or 1",
				"the %s must offer 1: a single-shiaijo competition is legal", m.surface)
			assert.NotContainsf(t, code, "at least 2 shiaijo",
				"the %s must not state the rule as a minimum", m.surface)
			assert.NotContainsf(t, code, "1 or an even number",
				"the retired parity rule must not survive in the %s", m.surface)
			assert.Containsf(t, code, fmt.Sprintf("MAX_COURTS = %d", MaxCourts),
				"the %s derives the legal counts from the A-Z cap, which must equal helper.MaxCourts", m.surface)
		})
	}
}

// jsCodeWithoutLineComments drops whole-line `//` comments from JS source so a
// phrase can be asserted ABSENT from the code an operator sees without the
// comment that forbids it counting as a hit.
//
// Deliberately crude: it does not understand block comments, trailing
// comments or `//` inside a string. That is enough here because both mirrors
// document the rule in leading line comments, and a cruder filter can only
// make the negative assertions STRICTER (it keeps more text), never weaker.
func jsCodeWithoutLineComments(src string) string {
	var b strings.Builder
	for _, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}
