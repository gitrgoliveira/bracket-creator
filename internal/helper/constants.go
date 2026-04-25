package helper

// MaxPlayersPerTree is the maximum leaf count on a single tree sheet.
// 16 players form a balanced bracket that fits on one A4 landscape page.
const MaxPlayersPerTree = 16

// Pool match sheet layout constants.
const (
	// PoolMatchesRowsPerPage is the soft row budget before inserting a page
	// break on the Pool Matches sheet.
	PoolMatchesRowsPerPage = 45

	// PoolSpaceLines is the number of blank rows added after the pool header
	// before the first match block.
	PoolSpaceLines = 3

	// PoolDrawRowsPerPage is the soft row budget before inserting a page
	// break on the Pool Draw sheet.
	PoolDrawRowsPerPage = 42
)

// Elimination match layout constants.
const (
	// EliminationRowsPerPage is the soft row budget before inserting a page
	// break in the elimination-match section.
	EliminationRowsPerPage = 44

	// EliminationSpaceLines is the number of blank rows printed between
	// elimination match rounds.
	EliminationSpaceLines = 5

	// EliminationMatchHeight is the row-height of a single individual
	// elimination match block.
	EliminationMatchHeight = 8

	// EliminationTeamMatchHeightBase is added to the team-match count to get
	// the row-height of a team elimination match block.
	EliminationTeamMatchHeightBase = 11
)

// Default flag values used by CLI commands and the web handler.
const (
	DefaultPort     = 8080
	DefaultWinners  = 2
	DefaultPoolSize = 3
	DefaultCourts   = 2
)

// MaxCourts is the upper bound for the number of Shiaijo (courts).
// It comes from the single-letter A–Z labelling used on Shiaijo headers
// throughout the workbook; values above this are rejected up front by
// ValidateCourts so we never silently truncate a user-requested layout.
const MaxCourts = 26
