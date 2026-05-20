package state

import "github.com/gitrgoliveira/bracket-creator/internal/helper"

var uuidRE = helper.IsUUIDv4

func newParticipantID() string { return helper.NewUUID4() }
