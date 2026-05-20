package state

import (
	"regexp"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
)

var pIDRE = regexp.MustCompile(`^[a-zA-Z0-9_-]+-p[0-9]+$`)

var uuidRE = func(s string) bool {
	return helper.IsUUIDv4(s) || pIDRE.MatchString(s)
}

func newParticipantID() string { return helper.NewUUID4() }
