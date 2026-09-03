package helper

import (
	"fmt"
	"strings"
	"unicode"
)

// DefaultNumberPrefixFallback is the prefix DefaultNumberPrefix returns when a
// name yields no usable letters at all (an empty name, or one written entirely
// in a script this ASCII-only derivation cannot reduce). "K" for kendo.
const DefaultNumberPrefixFallback = "K"

// MaxNumberPrefixLen bounds a derived prefix. It matches the admin UI's
// maxLength="3" and mobileapp's MaxLenCompetitionNumberPrefix; stated here too
// because DefaultNumberPrefix must never propose a value its own validator
// would then reject, and helper cannot import mobileapp.
const MaxNumberPrefixLen = 3

// AssignPlayerNumbers sets Number on each player to prefix+counter, where counter
// starts at start and increments by one. Returns the next counter value so callers
// can chain across multiple slices (e.g. pools).
//
// This is the ONE place the competitor-number string is composed. Three callers
// used to spell `prefix + strconv(i+1)` themselves (the pool loop in
// engine/pools.go, its verbatim twin in cmd/create-pools.go, and the playoffs
// re-derivation in mobileapp/handlers_viewer.go); they now all route here, so a
// change to the number's shape cannot reach one surface and miss another.
func AssignPlayerNumbers(players []Player, prefix string, start int) int {
	for i := range players {
		players[i].Number = fmt.Sprintf("%s%d", prefix, start+i)
	}
	return start + len(players)
}

// NumberPools numbers every competitor across pools with a single counter that
// runs THROUGH the pools in their final order: with prefix "K" and pools of
// four, K1-K4 are the first pool, K5-K8 the second. The numbering is therefore
// only meaningful once the pools are in their published order, which they are
// at every call site: ReorderPoolsForCourts runs last inside pool formation and
// reassigns PoolName, so the slice handed here already IS the published sheet.
//
// Callers must pass a non-empty prefix. A competition's prefix is never empty
// by the time it reaches this call: the app assigns one before any draw (at
// create, at settings save, and at start/generate-draw for a legacy record
// that predates this rule), and the CLI resolves one when --number-prefix is
// omitted. There is no unnumbered mode to fall back to, and this function does
// not guard against an empty prefix reaching it: that guarantee belongs at the
// boundary that persists or draws, not here (see bc-pnum).
func NumberPools(pools []Pool, prefix string) {
	counter := 1
	for i := range pools {
		counter = AssignPlayerNumbers(pools[i].Players, prefix, counter)
	}
}

// DefaultNumberPrefix derives a competition's default number prefix from its
// name, avoiding every prefix in taken (compared case-insensitively, as
// checkUniqueCompFields compares them).
//
// The derivation walks the name's words and takes their initials, so "Kendo
// Open" gives "K" and, if "K" is taken, "KO". When initials are exhausted or
// still collide, a numeric suffix is appended ("K2", "K3", ...). Every
// candidate is capped at MaxNumberPrefixLen so the value can never be one the
// length validator would reject.
//
// The result is deterministic: the same name and the same taken set always
// produce the same prefix, which is what lets the create form show the operator
// the value the server would pick.
func DefaultNumberPrefix(name string, taken []string) string {
	used := make(map[string]bool, len(taken))
	for _, t := range taken {
		if t = strings.TrimSpace(t); t != "" {
			used[strings.ToUpper(t)] = true
		}
	}

	initials := nameInitials(name)
	if initials == "" {
		initials = DefaultNumberPrefixFallback
	}

	// Prefer a bare initial, then progressively more of them: "K", then "KO".
	for n := 1; n <= len(initials); n++ {
		if candidate := initials[:n]; !used[candidate] {
			return candidate
		}
	}

	// Every initial-based candidate collides, so fall to a numeric suffix on
	// the shortest stem that still leaves room for the digits. Bounded at 999
	// (three digits): MaxNumberPrefixLen is 3, so a four-digit suffix ("1000")
	// would demand a negative trim (MaxNumberPrefixLen-len(digits) < 0) and
	// panic on the slice below. Reaching this bound needs ~1000 competitions
	// sharing the same derived initials on one tournament day -- this repo's
	// tests cannot construct that scenario honestly, but bounding the loop
	// rather than looping forever (or panicking) is cheap insurance.
	stem := initials
	lastCandidate := stem
	for suffix := 2; suffix <= 999; suffix++ {
		digits := fmt.Sprintf("%d", suffix)
		trimmed := stem
		if len(trimmed)+len(digits) > MaxNumberPrefixLen {
			trimmed = trimmed[:MaxNumberPrefixLen-len(digits)]
		}
		candidate := trimmed + digits
		lastCandidate = candidate
		if !used[candidate] {
			return candidate
		}
	}
	// Exhausted every candidate up to the length cap: every one of them is
	// already taken. Return the last one tried rather than inventing a value
	// beyond MaxNumberPrefixLen -- every request-driven caller
	// (checkUniqueCompFields, on the create, settings-save, import and
	// start/generate-draw paths) re-validates uniqueness against
	// the SAME taken set and rejects the collision with its own "prefix
	// already used by competition ..." error, which names the actual
	// conflict. That is the right place for this to surface: this function's
	// contract is a best-effort SUGGESTION, not a uniqueness guarantee. The
	// one caller that does not re-validate, the load-time migration
	// (engine.MigrateNumberPrefixes), derives over the COMPLETE taken set
	// including its own in-pass assignments, so it can only collide once a
	// tournament holds a thousand same-initial competitions.
	return lastCandidate
}

// nameInitials reduces a name to the uppercased ASCII initials of its words,
// capped at MaxNumberPrefixLen. Non-letters are word separators, so "Kendo -
// Open (Senior)" reads as three words. Returns "" when the name holds no ASCII
// letters, which is what makes the fallback constant necessary.
func nameInitials(name string) string {
	var b strings.Builder
	inWord := false
	for _, r := range name {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z':
			if !inWord {
				b.WriteRune(unicode.ToUpper(r))
				if b.Len() == MaxNumberPrefixLen {
					return b.String()
				}
			}
			inWord = true
		default:
			inWord = false
		}
	}
	return b.String()
}
