package helper

import "github.com/gitrgoliveira/bracket-creator/internal/domain"

// PlayerToDomain converts a helper.Player (the Excel-coupled rendering
// type) into a domain.Player (the clean business model). All scalar
// fields copy 1:1; the Metadata slice is shallow-copied so the caller
// can mutate the result without aliasing helper-side data (NFR-007).
//
// The converter lives in the helper package (not domain) to avoid a
// helper → domain → helper import cycle: helper already imports domain
// (see seed.go for domain.SeedAssignment).
//
// Used at the boundary where engine / mobileapp / state code receives a
// helper.Player from the on-disk path and wants to operate on it as a
// pure domain object.
func PlayerToDomain(hp Player) domain.Player {
	var meta []string
	if len(hp.Metadata) > 0 {
		meta = make([]string, len(hp.Metadata))
		copy(meta, hp.Metadata)
	}
	return domain.Player{
		ID:           hp.ID,
		Name:         hp.Name,
		DisplayName:  hp.DisplayName,
		Dojo:         hp.Dojo,
		Metadata:     meta,
		Tag:          hp.Tag,
		PoolPosition: hp.PoolPosition,
		Seed:         hp.Seed,
		Number:       hp.Number,
	}
}

// PlayerFromDomain converts a domain.Player back into a helper.Player
// for any path that still requires the Excel-coupled type (e.g. calls
// into helper rendering helpers). Mirrors PlayerToDomain — shallow copy
// of Metadata to avoid aliasing.
func PlayerFromDomain(p domain.Player) Player {
	var meta []string
	if len(p.Metadata) > 0 {
		meta = make([]string, len(p.Metadata))
		copy(meta, p.Metadata)
	}
	return Player{
		ID:           p.ID,
		Name:         p.Name,
		DisplayName:  p.DisplayName,
		Dojo:         p.Dojo,
		Metadata:     meta,
		Tag:          p.Tag,
		PoolPosition: p.PoolPosition,
		Seed:         p.Seed,
		Number:       p.Number,
	}
}

// PlayersToDomain converts a slice of helper.Player to domain.Player.
// Returns nil for nil input (preserves the distinction between "no
// participants" and "empty list" that some callers rely on).
func PlayersToDomain(hps []Player) []domain.Player {
	if hps == nil {
		return nil
	}
	out := make([]domain.Player, len(hps))
	for i, hp := range hps {
		out[i] = PlayerToDomain(hp)
	}
	return out
}

// PlayersFromDomain converts a slice of domain.Player back to
// helper.Player. Symmetric with PlayersToDomain.
func PlayersFromDomain(ps []domain.Player) []Player {
	if ps == nil {
		return nil
	}
	out := make([]Player, len(ps))
	for i, p := range ps {
		out[i] = PlayerFromDomain(p)
	}
	return out
}
