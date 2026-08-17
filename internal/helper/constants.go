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

// courtLabelAlphabet is the complete set of Shiaijo labels, in order. Shiaijo
// headers throughout the workbook are a single letter, so this string IS the
// supported court set: a court that cannot be named cannot be printed, listed
// in an operator view, or filtered on.
const courtLabelAlphabet = "ABCDEFGHIJKLMNOP"

// MaxCourts is the upper bound for the number of Shiaijo (courts), derived from
// the labels rather than written twice -- the two cannot drift.
//
// SIXTEEN, not the full A–Z. 16 is the largest allocation any single
// competition can legally hold (validShiaijoCounts, R9: a knockout draw gives
// each shiaijo its own block and the blocks merge in pairs), so shiaijo beyond
// the 16th could never all be given to one competition. Supporting counts no
// competition can use bought nothing but wider allocations, bigger sheets and
// more numbers to validate.
//
// A count above it is REFUSED at every write path (ValidateCourts on the CLI,
// validateCourtLabels in the app), so an operator is never silently given a
// smaller layout than they asked for. The deeper helpers that have no error
// channel -- the draw builder and the sheet writers -- clamp to it instead
// (clampCourts), so an unvalidated caller cannot make them allocate or index
// past the labelling.
//
// Mirrored client-side as `MAX_COURTS` in web-mobile/js/admin_helpers.jsx and
// web/js/validation.js; keep all three in lockstep (pinned by
// TestPinMaxCourts and TestShiaijoRuleJSMirrorsMatchTheGoMessage).
const MaxCourts = len(courtLabelAlphabet)

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

// ThirdPlaceLabel is the header text written by PrintThirdPlaceBlock and
// detected by the results overlays to locate the bronze match block, so
// writer and reader cannot drift independently.
const ThirdPlaceLabel = "3rd Place"

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
