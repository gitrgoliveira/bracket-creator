package helper

import (
	"regexp"

	"github.com/google/uuid"
)

var uuidRE = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// IsUUIDv4 reports whether s has the lowercase canonical-UUID shape
// (8-4-4-4-12 lowercase hex). Despite the historical name, this is a
// SHAPE check, it does NOT enforce the v4 version nibble or the
// 8/9/a/b variant nibble. Many in-repo test fixtures use non-v4 or
// invalid-variant UUIDs (e.g. `…-7c3a-11e7-…` v1 timestamps,
// `…-4ddd-dddd-dddd-…` synthetic ids) and pass this check. Callers
// that need true v4 validation should parse with `uuid.Parse(s)` and
// inspect `Version()`/`Variant()` instead.
func IsUUIDv4(s string) bool {
	return uuidRE.MatchString(s)
}

// NewUUID4 generates a random UUID v4 string.
func NewUUID4() string {
	return uuid.New().String()
}
