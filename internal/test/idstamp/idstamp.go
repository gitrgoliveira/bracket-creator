// Package idstamp provides id-stamping helpers for hand-built test fixtures
// across internal/engine, internal/export, internal/mobileapp and
// internal/state, written before the bc-pnum operator ruling ("a record
// that carries an id field is resolved by id only; an empty id resolves to
// NOTHING"). It is a SEPARATE package from internal/test (rather than living
// in internal/test/helpers.go alongside the domain-only fixtures there)
// specifically because it needs internal/state and internal/helper types
// (state.MatchResult, helper.Pool, state.PlayerStanding): internal/state's
// and internal/helper's OWN in-package test files (package state / package
// helper, e.g. internal/state/models_test.go, internal/helper/bronze_band_test.go)
// already import internal/test for its domain-only helpers (HanteiExplicit,
// CreateTestPlayers, ...), and internal/test importing state/helper back
// would close an import cycle for exactly those two packages' test builds.
// idstamp has no such caller, so it is free to depend on state and helper.
package idstamp

import (
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// slugIDPart lowercases s and replaces every run of characters outside
// [a-z0-9] with a single "-", so StampPlayerID's id is deterministic and
// readable in test failure output regardless of what punctuation/whitespace
// the fixture's name or dojo contains.
func slugIDPart(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// StampPlayerID deterministically derives a participant id from (name, dojo)
// alone, so a fixture can predict the id a call to StampIDs/StampPoolIDs
// will assign without re-reading the roster afterward. Two players with the
// same (name, dojo) pair collide on this id by construction: that pairing is
// already a dedup violation (helper.CheckDuplicateEntriesByNameDojo), so a
// fixture legitimately exercising two DISTINCT same-name competitors from
// different dojos never collides, and a fixture that wants two distinct
// same-name-same-dojo rows (there is no such legal case) must assign ids by
// hand rather than relying on this derivation.
func StampPlayerID(name, dojo string) string {
	return "id-" + slugIDPart(name) + "-" + slugIDPart(dojo)
}

// StampIDs assigns a deterministic id (StampPlayerID) to every id-less
// entry in players, then fills SideAID/SideBID/WinnerID on every entry in
// matches whose field is still empty, resolving each side by looking up its
// SideA/SideB NAME against the now-fully-id-stamped players. Both slices
// are mutated in place.
//
// This exists so hand-built fixtures written before the bc-pnum operator
// ruling ("a record that carries an id field is resolved by id only") can
// still exercise id-only resolution paths without every fixture hand-rolling
// its own id scheme. A player or match field that already carries a
// non-empty value is left untouched, so a fixture can call StampIDs and then
// override specific ids by hand for a scenario that needs a particular id
// shape (e.g. a deliberately foreign/stale id).
//
// Winner is matched by NAME among the two sides (SideA/SideB): a fixture
// with a genuine same-name pair on one row (two different competitors
// sharing a display name, e.g. across dojos) must set
// SideAID/SideBID/WinnerID explicitly by hand, since matching by bare name
// cannot tell the two sides apart -- that ambiguity is exactly what the
// id-only resolution this helper exists to test is meant to resolve, so
// StampIDs deliberately does not attempt to guess it.
func StampIDs(players []domain.Player, matches []state.MatchResult) {
	byName := make(map[string]string, len(players))
	for i := range players {
		if players[i].ID == "" {
			players[i].ID = StampPlayerID(players[i].Name, players[i].Dojo)
		}
		byName[players[i].Name] = players[i].ID
	}
	for i := range matches {
		if matches[i].SideAID == "" {
			matches[i].SideAID = byName[matches[i].SideA]
		}
		if matches[i].SideBID == "" {
			matches[i].SideBID = byName[matches[i].SideB]
		}
		// SideA != SideB guards the exact ambiguity this helper's own doc
		// comment warns about: when both sides share a display name, Winner
		// matches BOTH of them identically, and a Go switch would silently
		// pick whichever case it evaluates first (SideA) -- a guess, not a
		// resolution. Requiring the two side names to differ before ever
		// comparing Winner against them is what actually keeps the promise
		// stated above, rather than merely asserting it in prose.
		if matches[i].WinnerID == "" && matches[i].Winner != "" && matches[i].SideA != matches[i].SideB {
			switch matches[i].Winner {
			case matches[i].SideA:
				matches[i].WinnerID = matches[i].SideAID
			case matches[i].SideB:
				matches[i].WinnerID = matches[i].SideBID
			}
		}
	}
}

// StampPoolIDs is the []helper.Pool variant of StampIDs: stamps every pool's
// own Players slice in place (helper.Player is a domain.Player alias,
// NFR-007), for fixtures that build pools without a separate matches slice
// to reconcile ids against.
func StampPoolIDs(pools []helper.Pool) {
	for i := range pools {
		players := pools[i].Players
		for j, p := range players {
			if p.ID == "" {
				players[j].ID = StampPlayerID(p.Name, p.Dojo)
			}
		}
	}
}

// StampStandingIDs is the []state.PlayerStanding variant of StampIDs: stamps
// each standing's own Player.ID in place, for fixtures that build standings
// directly rather than deriving them from a stamped roster.
func StampStandingIDs(standings []state.PlayerStanding) {
	for i := range standings {
		p := &standings[i].Player
		if p.ID == "" {
			p.ID = StampPlayerID(p.Name, p.Dojo)
		}
	}
}
