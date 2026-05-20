package mobileapp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHub_Subscribe_EnforcesCap(t *testing.T) {
	hub := NewHubWithLimits(DefaultHistorySize, 3)

	a := hub.Subscribe()
	b := hub.Subscribe()
	c := hub.Subscribe()
	require.NotNil(t, a)
	require.NotNil(t, b)
	require.NotNil(t, c)

	// At cap — next subscribe rejected.
	d := hub.Subscribe()
	assert.Nil(t, d, "expected Subscribe to return nil when MaxClients reached")

	// Existing clients still served (basic sanity — they're real channels).
	assert.NotNil(t, a)
	assert.NotNil(t, b)
	assert.NotNil(t, c)

	// Unsubscribing frees a slot.
	hub.Unsubscribe(a)
	e := hub.Subscribe()
	assert.NotNil(t, e, "expected Subscribe to succeed after a slot was freed")
}

func TestHub_Subscribe_ZeroCapMeansUnbounded(t *testing.T) {
	hub := NewHubWithLimits(DefaultHistorySize, 0) // 0 → unbounded (tests / overrides)
	for i := 0; i < 5000; i++ {
		ch := hub.Subscribe()
		require.NotNilf(t, ch, "subscribe %d returned nil under zero cap", i)
	}
}

func TestHub_Close_RejectsNewSubscribersAndDrainsExisting(t *testing.T) {
	hub := NewHubWithLimits(DefaultHistorySize, 10)

	ch1 := hub.Subscribe()
	ch2 := hub.Subscribe()
	require.NotNil(t, ch1)
	require.NotNil(t, ch2)

	hub.Close()

	// Closed channels read zero-value with ok=false.
	_, ok := <-ch1
	assert.False(t, ok, "ch1 should be closed after Hub.Close()")
	_, ok = <-ch2
	assert.False(t, ok, "ch2 should be closed after Hub.Close()")

	// New subscribers rejected after close.
	assert.Nil(t, hub.Subscribe(), "Subscribe must return nil after Close")
}

func TestHub_Close_Idempotent(t *testing.T) {
	hub := NewHubWithLimits(DefaultHistorySize, 10)
	ch := hub.Subscribe()
	require.NotNil(t, ch)

	hub.Close()
	hub.Close() // must not panic on double-close

	_, ok := <-ch
	assert.False(t, ok)
}
