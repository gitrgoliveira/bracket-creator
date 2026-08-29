package helper

// CompetitorKey returns the competitor IDENTITY string used everywhere a
// competitor must be resolved unambiguously from (id, name, dojo): the
// participant ID when present -- globally unique, and minted for essentially
// every stored participant, see state.saveParticipantsNoLock's ID-fill
// branch -- falling back to a normalized (name, dojo) composite otherwise.
//
// This is the operator identity rule (CLAUDE.md): competitor identity is
// (name, dojo), not name; two competitors sharing a name from different
// dojos are different people and are legal (CheckDuplicateEntriesByNameDojo
// only refuses a true (name, dojo) collision), so a name-only fallback would
// silently collide two distinct, ID-less competitors.
//
// Originally engine-local (internal/engine/engi.go, Swiss pairing state:
// wins, byes, rematch-avoidance, rank). Moved here (bc-cse) so
// internal/state -- which cannot import internal/engine without a cycle,
// engine already imports state -- can key manual pool-rank overrides
// (state.Overrides.PoolRanks) with the same scheme the Swiss pipeline and
// the pool/league/Swiss standings readers use. Both callers reuse this exact
// function rather than each hand-rolling their own composite, so a
// same-name-different-dojo pair resolves identically everywhere in the
// codebase.
//
// A bare name with no id and no dojo (dojo == "") still produces a
// deterministic key ("nd:<name>|"), but callers that need to fall back to a
// PRE-EXISTING plain-name key (e.g. an overrides.json written before this
// function existed) must do that fallback explicitly and separately -- this
// function does not special-case the empty-dojo input into the legacy bare
// name, so the two forms never collide.
func CompetitorKey(id, name, dojo string) string {
	if id != "" {
		return "id:" + id
	}
	return "nd:" + NormalizeParticipantName(name) + "|" + NormalizeParticipantName(dojo)
}
