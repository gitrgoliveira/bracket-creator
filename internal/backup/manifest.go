package backup

import "time"

type Scope string

const (
	ScopeTournament  Scope = "tournament"
	ScopeCompetition Scope = "competition"
)

type Kind string

const (
	KindManual        Kind = "manual"
	KindScheduled     Kind = "scheduled"
	KindBeforeRestore Kind = "before-restore"
)

type TournamentRef struct {
	Name string `json:"name"`
}

type CompetitionRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type FileEntry struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"` // lowercase hex
}

type Manifest struct {
	Format         int              `json:"format"`
	AppVersion     string           `json:"appVersion"`
	CreatedAt      time.Time        `json:"createdAt"` // UTC
	Scope          Scope            `json:"scope"`
	Kind           Kind             `json:"kind"`
	Tournament     TournamentRef    `json:"tournament"`
	Competitions   []CompetitionRef `json:"competitions"` // always an array, [] when none
	Files          []FileEntry      `json:"files"`        // always an array
	Fingerprint    string           `json:"fingerprint"`
	RestoreCreated *[]string        `json:"restoreCreated,omitempty"` // non-nil only when Kind is before-restore
}

// Meta is what a caller supplies; Write computes Format and Files.
type Meta struct {
	// AppVersion is the version the manifest names; empty means the running
	// app's (RunningVersion, the same seam the restore check reads, so the
	// copy a caller writes and the check it makes name the same version).
	AppVersion     string
	CreatedAt      time.Time
	Scope          Scope
	Kind           Kind
	Tournament     TournamentRef
	Competitions   []CompetitionRef
	Fingerprint    string
	RestoreCreated []string // only for KindBeforeRestore; Write emits [] when nil for that kind
}
