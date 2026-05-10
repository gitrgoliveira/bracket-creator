package helper

import (
	"regexp"

	"github.com/google/uuid"
)

var uuidRE = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// IsUUIDv4 reports whether s is a lowercase UUID v4 string.
func IsUUIDv4(s string) bool {
	return uuidRE.MatchString(s)
}

// NewUUID4 generates a random UUID v4 string.
func NewUUID4() string {
	return uuid.New().String()
}
