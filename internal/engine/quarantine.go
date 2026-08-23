package engine

import (
	"fmt"
	"log/slog"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// QuarantineResult reports what a quarantine did, so the operator is told
// whether they are looking at a rebuilt tree or an empty stage.
type QuarantineResult struct {
	// QuarantinedAs is the name the broken file was renamed to. It is still in
	// the competition folder; nothing is deleted.
	QuarantinedAs string
	// Rebuilt is false when the competition has no knockout stage to rebuild.
	Rebuilt bool
	// ResolvedPools counts the finished pools whose qualifiers were seeded back
	// into the fresh bracket.
	ResolvedPools int
}

// QuarantineCorruptBracket is the operator's way out of a bracket.json that
// will not parse. It renames the broken file aside (never deletes it) and
// rebuilds the knockout stage from the records that are still readable.
//
// It is the LAST resort, not the first. A corrupt bracket blocks scoring but
// destroys nothing: every bracket write path aborts on the parse before
// saving, so the file stays exactly as it was left and repairing it by hand
// restores the competition completely, results included. This throws the
// recorded knockout results away. The operator chooses.
//
// WHAT A REBUILD CAN AND CANNOT PROMISE. The knockout topology is not stored
// anywhere else, so a rebuild recomputes it, and ResolveQualifiedPools' own
// comment records why that is not free: it deliberately reads the placeholder
// labels PERSISTED at draw time rather than recomputing the template, because
// an operator who upgrades between a competition's draw and the end of its
// pool phase (ordinary for a two-day event) would otherwise get qualifiers
// written into the wrong slots, undetectably. A rebuild IS that recompute, and
// the original template died with the file. So the tree is only guaranteed to
// match the one that was drawn when the placement algorithm has not changed
// since. The caller must say so; the surfaces do.
//
// THREE OUTCOMES, decided by the shared format table (see
// CompetitionRebuildableFromPools). Mixed is REBUILT, because its draw survives
// in pools.csv. League and Swiss are moved aside and nothing more, because they
// never drew a bracket and the file is vestigial; Rebuilt reports false so the
// caller can say which of the two happened. Everything that draws its bracket
// DIRECTLY is refused, and that refusal is the point: there, bracket.json is
// the ONLY record of the draw. Rebuilding it from today's roster and seeds
// would not restore the tournament, it would invent a different one, silently
// disagreeing with the bracket already printed and posted on the wall.
// CompetitionRebuildableFromPools reports whether a competition's knockout
// bracket can be built again after bracket.json is lost, which is true only
// when the draw that produced it survives in another file.
//
// Mixed is the one format where it does: its knockout is drawn from pools.csv,
// so the same builder can lay the tree out again and the finished pools can be
// re-seeded into it. Every other format either draws its bracket directly (so
// the file IS the draw) or never draws one at all.
//
// Pinned with CompetitionDrawsBracket against the shared Go/JS table in
// testdata/format_draws_bracket.json, whose `_comment` sets out how the two
// answers combine into rebuild / refuse / discard. Mirrored client-side as
// bracketRecoveryKind in web-mobile/js/data_integrity.jsx.
func CompetitionRebuildableFromPools(format string) bool {
	return format == state.CompFormatMixed
}

func (e *Engine) QuarantineCorruptBracket(id string) (*QuarantineResult, error) {
	comp, err := e.store.LoadCompetition(id)
	if err != nil {
		return nil, err
	}
	if comp == nil {
		return nil, notFoundErrorf("competition %s not found", id)
	}

	// The guard that keeps this from becoming a general bracket-wipe: it acts
	// only on a file that genuinely will not parse. A readable bracket is
	// discarded through DiscardDraw, which has its own status rules.
	_, loadErr := e.store.LoadBracket(id)
	if loadErr == nil {
		return nil, validationErrorf("competition %s has a readable bracket; nothing to quarantine", id)
	}
	if _, ok := state.AsCorruptFile(loadErr); !ok {
		// Anything else (a permissions problem, an I/O failure) is not repaired
		// by renaming the file, and pretending otherwise would hide it.
		return nil, loadErr
	}

	// Refuse whenever the draw builds a bracket that nothing else records.
	// Derived from the same pair of questions the draw pipeline answers, rather
	// than from a hand-written list: this used to read `Format == playoffs ||
	// Format == ""`, which let every UNRECOGNISED format through -- a typo in a
	// hand-edited config.md, exactly the class of edit this whole area exists
	// for. Such a competition takes the pipeline's DEFAULT branch and gets a
	// standalone playoffs bracket, so bracket.json is the only record of its
	// draw, and it was being quarantined with nothing rebuilt in its place.
	if CompetitionDrawsBracket(comp.Format) && !CompetitionRebuildableFromPools(comp.Format) {
		return nil, validationErrorf(
			"competition %s draws its bracket directly, so bracket.json is the only "+
				"record of its draw: rebuilding it would produce a different set of pairings "+
				"rather than restoring this one. Repair the file and reload", id)
	}

	quarantined, err := e.store.QuarantineCompetitionFile(id, "bracket.json")
	if err != nil {
		return nil, fmt.Errorf("quarantining bracket.json for %s: %w", id, err)
	}
	slog.Warn("engine: bracket.json quarantined after a parse failure",
		"competition", id, "quarantinedAs", quarantined, "reason", loadErr)

	result := &QuarantineResult{QuarantinedAs: quarantined}

	// League and Swiss have no knockout stage. Their bracket.json is vestigial,
	// so moving it aside is the whole repair, and Rebuilt stays false to say so.
	if !CompetitionRebuildableFromPools(comp.Format) {
		return result, nil
	}

	// The pool draw survives in pools.csv, so the knockout structure is
	// rebuildable from it by the same builder that drew it.
	if err := e.generatePoolPreviewBracket(comp); err != nil {
		return nil, fmt.Errorf("rebuilding the bracket for %s: %w", id, err)
	}
	result.Rebuilt = true

	// Seed the qualifiers every FINISHED pool has already produced, so the
	// operator gets back a bracket with real names in it rather than a sheet of
	// placeholders, and only has to re-enter the knockout bouts themselves.
	resolved, _, err := e.ResolveQualifiedPools(id)
	if err != nil {
		return nil, fmt.Errorf("re-seeding qualifiers for %s: %w", id, err)
	}
	result.ResolvedPools = resolved
	return result, nil
}
