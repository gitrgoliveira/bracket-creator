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
//
// Mirrored client-side as `MAX_COURTS` in web-mobile/js/admin_helpers.jsx,
// keep the two in lockstep. The JS side is anchored by a comment back here
// so changes are visible at both edit points.
const MaxCourts = 26

// MaxRankOverride is the absolute upper bound for a manual rank override
// submitted via PUT /api/competitions/:id/pools/:poolId/override-rank.
// The override-rank handler ALSO validates against the actual pool size
// (the real semantic constraint, rank within a pool must be in [1..N]
// where N is the number of players in that pool). This cap is a
// defense-in-depth overflow guard for the rare case where pools have
// not been generated yet or LoadPools returns stale/unexpected data.
//
// Mirrored client-side as `MAX_RANK` in web-mobile/js/admin_helpers.jsx,
// keep the two in lockstep. 1000 is arbitrary; no real pool has 1000+
// participants.
const MaxRankOverride = 1000

// MinDateYear / MaxDateYear are the inclusive bounds on the year
// component of tournament + competition dates. The mobile-app HTTP
// handlers (validateDateDMY in handlers_tournament.go) enforce these
// on every write path so a value the API accepts is also one the
// admin UI can edit. Without matching bounds, a direct API/import
// write landing an out-of-range date would block every subsequent
// admin Settings save, the JS validator re-validates the stored
// date on every PUT and surfaces an inline error before reaching the
// wire.
//
// Mirrored client-side as `MIN_YEAR` / `MAX_YEAR` in
// web-mobile/js/admin_helpers.jsx, keep all four in lockstep. Pin
// tests on both sides assert the literal values so cross-language
// drift fails CI rather than waiting for a date-related UX bug.
const (
	MinDateYear = 1900
	MaxDateYear = 2100
)

// CourtsColumnsPerCourt is the number of Excel columns allocated to each
// court (Shiaijo) on the Pool Matches and Elimination Matches sheets.
// Layout: Name | V | P | vs | P | V | Name | Spacer = 8 columns.
const CourtsColumnsPerCourt = 8

// Column-width constants for match layout sheets.
const (
	matchNameColWidth   = 30
	matchScoreColWidth  = 5
	matchSpacerColWidth = 5
)

// ColHeaderFlags is the single-source column header label for the engi
// referee flag count in the Pool Matches standings table. Used by both the
// writer (printIndividualResultsTableSection) and the overlay reader
// (overlayPoolStandings / buildCourtColumnMap) so the header and the overlay
// can never drift independently.
const ColHeaderFlags = "Flags"

// Sheet names for every tab in the workbook. Use these constants wherever a
// sheet name is needed so that a rename only requires one edit here.
//
// SheetKachinukiDetail is opt-in: only emitted by the engine export path when
// a competition has teamMatchType=kachinuki AND at least one kachinuki match
// has bout data to display. See excel_kachinuki.go (T199–T203).
const (
	SheetData               = "data"
	SheetTimeEstimator      = "Time Estimator"
	SheetPoolDraw           = "Pool Draw"
	SheetPoolMatches        = "Pool Matches"
	SheetEliminationMatches = "Elimination Matches"
	SheetNamesToPrint       = "Names to Print"
	SheetTags               = "Tags"
	SheetTree               = "Tree"
	SheetKachinukiDetail    = "Kachinuki Detail"
)
