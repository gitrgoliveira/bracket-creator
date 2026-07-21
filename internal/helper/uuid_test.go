package helper

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsUUIDv4(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"canonical v4", "9b2edd6b-3f1c-4be5-a8e3-1c7d55f2b9c1", true},
		// IsUUIDv4 is deliberately a SHAPE check: it accepts any canonical
		// 8-4-4-4-12 lowercase-hex string regardless of version/variant
		// nibbles. Legacy participants.csv fixtures rely on this.
		{"v1 timestamp UUID accepted (shape only)", "6fa1fe25-7c3a-11e7-a919-92ebcb67fe33", true},
		{"synthetic invalid-variant accepted (shape only)", "dddddddd-dddd-4ddd-dddd-dddddddddddd", true},
		{"all zeros accepted (shape only)", "00000000-0000-0000-0000-000000000000", true},
		{"uppercase rejected", "9B2EDD6B-3F1C-4BE5-A8E3-1C7D55F2B9C1", false},
		{"mixed case rejected", "9b2edd6b-3f1c-4Be5-a8e3-1c7d55f2b9c1", false},
		{"missing hyphens", "9b2edd6b3f1c4be5a8e31c7d55f2b9c1", false},
		{"too short", "9b2edd6b-3f1c-4be5-a8e3-1c7d55f2b9c", false},
		{"too long", "9b2edd6b-3f1c-4be5-a8e3-1c7d55f2b9c12", false},
		{"non-hex characters", "9b2edd6g-3f1c-4be5-a8e3-1c7d55f2b9c1", false},
		{"leading whitespace", " 9b2edd6b-3f1c-4be5-a8e3-1c7d55f2b9c1", false},
		{"trailing whitespace", "9b2edd6b-3f1c-4be5-a8e3-1c7d55f2b9c1 ", false},
		{"embedded in longer string", "x9b2edd6b-3f1c-4be5-a8e3-1c7d55f2b9c1", false},
		{"braced form rejected", "{9b2edd6b-3f1c-4be5-a8e3-1c7d55f2b9c1}", false},
		{"urn prefix rejected", "urn:uuid:9b2edd6b-3f1c-4be5-a8e3-1c7d55f2b9c1", false},
		{"empty string", "", false},
		{"plain name", "Kenshi Yamada", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsUUIDv4(tt.input))
		})
	}
}

func TestNewUUID4(t *testing.T) {
	t.Run("output passes IsUUIDv4", func(t *testing.T) {
		assert.True(t, IsUUIDv4(NewUUID4()))
	})

	t.Run("is a real v4 UUID", func(t *testing.T) {
		parsed, err := uuid.Parse(NewUUID4())
		require.NoError(t, err)
		assert.Equal(t, uuid.Version(4), parsed.Version())
		assert.Equal(t, uuid.RFC4122, parsed.Variant())
	})

	t.Run("successive calls are unique", func(t *testing.T) {
		seen := make(map[string]bool)
		for i := 0; i < 100; i++ {
			id := NewUUID4()
			assert.False(t, seen[id], "duplicate UUID generated: %s", id)
			seen[id] = true
		}
	})
}
