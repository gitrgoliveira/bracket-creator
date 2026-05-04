package engine

import (
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

type Engine struct {
	store *state.Store
}

func New(store *state.Store) *Engine {
	return &Engine{
		store: store,
	}
}
