package engine

import (
	"sync"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

type standingsCacheEntry struct {
	poolMatchesMtime int64
	overridesMtime   int64
	result           map[string][]state.PlayerStanding
}

type Engine struct {
	store           *state.Store
	standingsCache  sync.Map // map[compID string]*standingsCacheEntry
	standingsFlight sync.Map // map[compID string]*sync.Once, collapses concurrent cold-cache calls
}

func New(store *state.Store) *Engine {
	return &Engine{
		store: store,
	}
}
