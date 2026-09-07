// Package idstamp provides id-stamping helpers for hand-built test fixtures
// written before the bc-pnum operator ruling ("a record that carries an id
// field is resolved by id only; an empty id resolves to NOTHING"). It is a
// SEPARATE package from internal/test (rather than living in
// internal/test/helpers.go alongside the domain-only fixtures there)
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
	"crypto/md5" // #nosec G501 -- fixture id derivation only, not a security use
	"fmt"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// StampPlayerID deterministically derives a participant id from (name, dojo)
// alone, so a fixture can predict the id a call to StampIDs/StampPoolIDs
// will assign without re-reading the roster afterward. Two players with the
// same (name, dojo) pair collide on this id by construction: that pairing is
// already a dedup violation (helper.CheckDuplicateEntriesByNameDojo), so a
// fixture legitimately exercising two DISTINCT same-name competitors from
// different dojos never collides, and a fixture that wants two distinct
// same-name-same-dojo rows (there is no such legal case) must assign ids by
// hand rather than relying on this derivation.
//
// UUID-v4-SHAPED (8-4-4-4-12 lowercase hex, version nibble '4', variant
// nibble in 8-b): participants.csv's has-ids sniff (internal/state/
// participants.go, via helper.IsUUIDv4) only checks the 8-4-4-4-12 shape,
// but the version/variant nibbles are set anyway so a stamped id is
// indistinguishable from a genuine random one wherever something DOES parse
// further (uuid.Parse(...).Version()/Variant()).
func StampPlayerID(name, dojo string) string {
	sum := md5.Sum([]byte(name + "\x00" + dojo)) // #nosec G401 -- fixture id derivation only, not a security use
	sum[6] = (sum[6] & 0x0f) | 0x40              // version 4
	sum[8] = (sum[8] & 0x3f) | 0x80              // variant 10xx (RFC 4122)
	h := fmt.Sprintf("%x", sum)
	return fmt.Sprintf("%s-%s-%s-%s-%s", h[0:8], h[8:12], h[12:16], h[16:20], h[20:32])
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
//
// The SAME ambiguity applies to the SideAID/SideBID lookup itself, one
// level up: byName is keyed by bare NAME (not (name, dojo)), so a roster
// carrying two DIFFERENT players sharing a display name -- legal across
// dojos -- would otherwise resolve every match row that relies on this
// lookup for that name to whichever player was inserted LAST, silently. A
// plain map assignment is last-write-wins with no signal that it happened;
// the second player's own id would silently overwrite the first's map
// entry, and any id-less match row meaning the FIRST namesake would then
// stamp the SECOND's id instead. dupName below tracks which names collide;
// the match loop panics ONLY when a row actually NEEDS byName to resolve
// that ambiguous name (its own SideAID/SideBID/WinnerID is empty) -- a row
// that already carries explicit ids for a same-name pair (the legitimate
// pattern the Winner-side guard above exists for) never consults byName at
// all and must not panic just because the ROSTER happens to contain a
// duplicate name it never needed.
func StampIDs(players []domain.Player, matches []state.MatchResult) {
	byName := make(map[string]string, len(players))
	dupName := make(map[string]bool, len(players))
	for i := range players {
		if players[i].ID == "" {
			players[i].ID = StampPlayerID(players[i].Name, players[i].Dojo)
		}
		if _, seen := byName[players[i].Name]; seen {
			dupName[players[i].Name] = true
		}
		byName[players[i].Name] = players[i].ID
	}
	requireUnambiguous := func(name string) {
		if dupName[name] {
			panic("idstamp.StampIDs: roster has two players named " + name +
				" (legal across dojos), and a match row needs byName to resolve that " +
				"ambiguous name -- a bare-name lookup cannot tell them apart, so stamp " +
				"this fixture's match-side ids by hand instead of calling StampIDs")
		}
	}
	for i := range matches {
		if matches[i].SideAID == "" {
			requireUnambiguous(matches[i].SideA)
			matches[i].SideAID = byName[matches[i].SideA]
		}
		if matches[i].SideBID == "" {
			requireUnambiguous(matches[i].SideB)
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
