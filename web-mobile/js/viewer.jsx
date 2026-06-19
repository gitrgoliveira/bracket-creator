// Viewer side — mobile-first. Single tournament. Shows competitions as the home;
// each competition opens to its own Overview/Bracket/Pools/Schedule/Results.

import { withNumber } from './match_scoreboard.jsx';
// Re-export the shared scoreboard primitives so existing tests that import them
// from '../viewer.jsx' keep working (the canonical defs now live in match_scoreboard.jsx).
export { BoutSubRow, boutHansokuMark } from './match_scoreboard.jsx';
import { TermV, competitionKindLabel, poolLabel, compMatches, tournamentMatches, currentMatchOf, linkBase, isNonPublicOrigin, TournamentInfo } from './viewer_utils.jsx';
import { matchParticipantIds, matchParticipantNames, isFollowedPlayer, isPlayerWatched, WATCHLIST_MAX, entryKey, normalizeWatchlistEntry, normalizeWatchlist, migrateWatchlistOnLoad, addPlayerToWatchlist, resolveEntryPlayerIds, resolveWatchedPlayers, effectivePrimaryKey, findPrimaryEntry, buildPrimaryNextMatch, buildRoster, useWatchlist } from './viewer_watchlist_core.jsx';
import { NOTIF_SYNC_EVENT, runOnce, notifEnable, notifDisable, useChimeMuted, isFollowedMatchOnDeck, useFollowedMatchAlert, computeSecondaryAlert, useSecondaryWatchAlert, MyMatchAlertBanner } from './viewer_alerts.jsx';
import { notificationSupported, AnnBellBtn, AnnouncementCard, AnnouncementBanner } from './viewer_notifications.jsx';
import { mymatchQueueLabel, MatchDetailCard, VSchedItem, MatchViewerModal } from './viewer_match.jsx';
import { isSwissFinalStandings, swissStandingsHeading, WinnerBadge, SwissStandingsViewer, LeagueMatrix, PoolNumberedMatchRow, PoolsViewer } from './viewer_standings.jsx';
import { deriveAwards, bracketHasDecidedFinal, resolveCompetitionAwards, AwardsView, FightingSpiritSection, buildAllWinnersPublic, AllWinnersView } from './viewer_awards.jsx';
import { PlayerMultiFilter, applyFilters, matchHighlightedBy, buildPlayerMatchHighlight, buildWatchlistUpcoming, ScheduleViewer, ViewerSchedule, TWMatch, usePrimaryWatch, LS_WATCH_PRIMARY, WATCHED_UPCOMING_MAX, WATCHED_UPCOMING_LIST_MAX } from './viewer_schedule.jsx';
import { ViewerCompetition, ViewerOverview } from './viewer_competition.jsx';
import { ViewerHome, shouldShowRegister, resolveDeepLink } from './viewer_home.jsx';

// TermV — moved to viewer_utils.jsx (mp-pxxc step 1). Imported above.
// competitionKindLabel, poolLabel, compMatches, tournamentMatches, currentMatchOf
// — moved to viewer_utils.jsx (mp-pxxc step 1). Imported above.

// --- Slice 4 helpers: "Find my matches" + Watchlist (FR-020 / FR-022 / FR-024) ---
// matchParticipantIds, matchParticipantNames, isFollowedPlayer, isPlayerWatched,
// WATCHLIST_MAX, entryKey, normalizeWatchlistEntry, normalizeWatchlist,
// migrateWatchlistOnLoad, addPlayerToWatchlist, resolveEntryPlayerIds,
// resolveWatchedPlayers, effectivePrimaryKey, findPrimaryEntry,
// buildPrimaryNextMatch, buildRoster, useWatchlist
// — moved to viewer_watchlist_core.jsx (mp-pxxc step 2). Imported above.

// buildPlayerMatchHighlight — moved to viewer_schedule.jsx (mp-pxxc step 7). Imported above.

// LS_WATCH_PRIMARY, WATCHED_UPCOMING_MAX, WATCHED_UPCOMING_LIST_MAX,
// usePrimaryWatch, buildWatchlistUpcoming
// — moved to viewer_schedule.jsx (mp-pxxc step 7). Imported above.

// NOTIF_SYNC_EVENT, dispatchNotif, runOnce, notifEnable, notifDisable,
// useChimeMuted, isFollowedMatchOnDeck, useFollowedMatchAlert,
// computeSecondaryAlert, useSecondaryWatchAlert, MyMatchAlertBanner
// — moved to viewer_alerts.jsx (mp-pxxc step 3). Imported above.
// The export keyword is preserved via re-export in the export{} block below.

// _permSubscribed, _permGaveUp, subscribePermissionChanges
// — moved to viewer_notifications.jsx (mp-pxxc step 8). Imported above via AnnBellBtn.

// isHttpURL, linkBase, isNonPublicOrigin, TournamentInfo
// — moved to viewer_utils.jsx (mp-pxxc step 1). Imported above.
// The `export` keyword is preserved via re-export in the export{} block below.

// shouldShowRegister, resolveDeepLink, ViewerHome, DisplayModes
// — moved to viewer_home.jsx (mp-pxxc step 10). Imported above.

// mymatchQueueLabel, subBoutLabel — moved to viewer_match.jsx (mp-pxxc step 4).
// Imported above; re-exported in the big export {} block below.

// isSwissFinalStandings, swissStandingsHeading, WinnerBadge, SwissStandingsViewer
// — moved to viewer_standings.jsx (mp-pxxc step 5). Imported above.

// ViewerCompetition, ViewerOverview — moved to viewer_competition.jsx (mp-pxxc step 9). Imported above.

// VSchedItem — moved to viewer_match.jsx (mp-pxxc step 4). Imported above.

// PoolMatchRow, LeagueMatrix, rankOrdinal, PoolNumberedMatchRow, PoolsViewer
// — moved to viewer_standings.jsx (mp-pxxc step 5). Imported above.

// MatchDetailCard — moved to viewer_match.jsx (mp-pxxc step 4). Imported above.

// PlayerMultiFilter, applyFilters, matchHighlightedBy
// — moved to viewer_schedule.jsx (mp-pxxc step 7). Imported above.

export { PlayerMultiFilter, applyFilters, matchHighlightedBy, competitionKindLabel, compMatches, tournamentMatches, currentMatchOf, buildPlayerMatchHighlight, buildWatchlistUpcoming, useWatchlist, entryKey, normalizeWatchlistEntry, normalizeWatchlist, migrateWatchlistOnLoad, resolveEntryPlayerIds, resolveWatchedPlayers, effectivePrimaryKey, findPrimaryEntry, buildPrimaryNextMatch, computeSecondaryAlert, isSwissFinalStandings, swissStandingsHeading, isFollowedPlayer, isPlayerWatched, deriveAwards, bracketHasDecidedFinal, resolveCompetitionAwards, buildRoster, MatchDetailCard, MatchViewerModal, AnnouncementCard, AnnouncementBanner, ViewerCompetition, ViewerOverview, ViewerHome, MyMatchAlertBanner, LeagueMatrix, PoolsViewer, PoolNumberedMatchRow, AwardsView, FightingSpiritSection, matchParticipantIds, TermV, VSchedItem, addPlayerToWatchlist, poolLabel, WATCHLIST_MAX };
// Re-export symbols moved to viewer_utils.jsx so the public export surface is unchanged (mp-pxxc step 1).
export { isHttpURL, linkBase, isNonPublicOrigin, TournamentInfo } from './viewer_utils.jsx';
// Re-export symbols moved to viewer_alerts.jsx so the public export surface is unchanged (mp-pxxc step 3).
export { NOTIF_SYNC_EVENT, notifEnable, notifDisable, isFollowedMatchOnDeck } from './viewer_alerts.jsx';
// Re-export symbols moved to viewer_notifications.jsx so the public export surface is unchanged (mp-pxxc step 8).
export { notificationSupported, AnnBellBtn } from './viewer_notifications.jsx';
// Re-export symbols moved to viewer_match.jsx so the public export surface is unchanged (mp-pxxc step 4).
// mymatchQueueLabel is imported above (used for window.mymatchQueueLabel); subBoutLabel has no local use.
export { subBoutLabel } from './viewer_match.jsx';
export { mymatchQueueLabel };
// isSwissFinalStandings, swissStandingsHeading, WinnerBadge (private), SwissStandingsViewer,
// PoolMatchRow (private), LeagueMatrix, PoolNumberedMatchRow, PoolsViewer
// — moved to viewer_standings.jsx (mp-pxxc step 5). Imported above.
// PlayerMultiFilter, applyFilters, matchHighlightedBy, buildPlayerMatchHighlight,
// buildWatchlistUpcoming, ScheduleViewer, ViewerSchedule, TWMatch
// — moved to viewer_schedule.jsx (mp-pxxc step 7). Imported above; window.* set there.

// window.PlayerMultiFilter, window.applyFilters, window.matchHighlightedBy,
// window.buildPlayerMatchHighlight, window.buildWatchlistUpcoming,
// window.ViewerSchedule, window.ScheduleViewer
// — set in viewer_schedule.jsx (mp-pxxc step 7).
if (typeof window !== 'undefined') {
    window.deriveAwards = deriveAwards;
    window.bracketHasDecidedFinal = bracketHasDecidedFinal;
    window.resolveCompetitionAwards = resolveCompetitionAwards;
}

// deriveAwards, bracketHasDecidedFinal, resolveCompetitionAwards, AwardsView,
// FightingSpiritSection — moved to viewer_awards.jsx (mp-pxxc step 6). Imported above.

// ViewerSchedule, ScheduleViewer, TWMatch
// — moved to viewer_schedule.jsx (mp-pxxc step 7). Imported above.

// MatchViewerModal — moved to viewer_match.jsx (mp-pxxc step 4). Imported above.

// notificationSupported, formatAnnouncementTimeLeft, AnnBellBtn,
// AnnouncementCard, AnnouncementBanner
// — moved to viewer_notifications.jsx (mp-pxxc step 8). Imported above.
// window.AnnouncementCard and window.AnnouncementBanner are set in viewer_notifications.jsx.

// buildAllWinnersPublic, AllWinnersView — moved to viewer_awards.jsx (mp-pxxc step 6). Imported above.

window.ViewerHome = ViewerHome;
window.ViewerCompetition = ViewerCompetition;
window.isFollowedPlayer = isFollowedPlayer;
window.isPlayerWatched = isPlayerWatched;
window.ViewerSchedule = ViewerSchedule;
window.ScheduleViewer = ScheduleViewer;
window.SwissStandingsViewer = SwissStandingsViewer;
window.competitionKindLabel = competitionKindLabel;
window.compMatches = compMatches;
window.tournamentMatches = tournamentMatches;
window.currentMatchOf = currentMatchOf;
// mp-s1gl: expose link-base helpers for admin_shell.jsx / admin_schedule.jsx
// (those files don't ES-import viewer.jsx; they pick globals off window).
window.linkBase = linkBase;
window.isNonPublicOrigin = isNonPublicOrigin;
// mp-koqh: public results page.
window.buildAllWinnersPublic = buildAllWinnersPublic;
window.AllWinnersView = AllWinnersView;
// Reused read-only on the shiaijo operator console (pool standings + results).
window.PoolsViewer = PoolsViewer;

// Helpers consumed by viewer_watchlist.jsx (WatchPicker/WatchHeroCard/
// WatchlistPanel) at render time. poolLabel is already exposed above.
window.matchParticipantIds = matchParticipantIds;
window.addPlayerToWatchlist = addPlayerToWatchlist;
window.effectivePrimaryKey = effectivePrimaryKey;
window.entryKey = entryKey;
window.resolveEntryPlayerIds = resolveEntryPlayerIds;
window.mymatchQueueLabel = mymatchQueueLabel;
window.TermV = TermV;
window.VSchedItem = VSchedItem;
window.WATCHLIST_MAX = WATCHLIST_MAX;

export { shouldShowRegister };
// Re-export symbols moved to viewer_home.jsx so the public export surface is unchanged (mp-pxxc step 10).
export { resolveDeepLink } from './viewer_home.jsx';

