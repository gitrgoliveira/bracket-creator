// Viewer side — mobile-first. Single tournament. Shows competitions as the home;
// each competition opens to its own Overview/Bracket/Pools/Schedule/Results.
//
// Thin entry point (mp-pxxc): the implementation lives in the viewer_*.jsx
// modules imported below. This file's only job is to re-export the original
// public surface two ways so nothing downstream breaks:
//   1. the ES `export {…}` blocks  — consumed by ../viewer.jsx test imports;
//   2. the `window.*` assignments  — consumed by non-module HTML/admin scripts
//      that pick globals off window rather than ES-importing this file.
// Keep both in sync with the modules; do not add logic here.

export { BoutSubRow, boutHansokuMark } from './match_scoreboard.jsx';
import { TermV, competitionKindLabel, poolLabel, compMatches, tournamentMatches, currentMatchOf, linkBase, isNonPublicOrigin } from './viewer_utils.jsx';
import { matchParticipantIds, isFollowedPlayer, isPlayerWatched, WATCHLIST_MAX, entryKey, normalizeWatchlistEntry, normalizeWatchlist, migrateWatchlistOnLoad, addPlayerToWatchlist, resolveEntryPlayerIds, resolveWatchedPlayers, effectivePrimaryKey, findPrimaryEntry, buildPrimaryNextMatch, buildRoster, useWatchlist } from './viewer_watchlist_core.jsx';
import { computeSecondaryAlert, MyMatchAlertBanner } from './viewer_alerts.jsx';
import { AnnouncementCard, AnnouncementBanner } from './viewer_notifications.jsx';
import { mymatchQueueLabel, MatchDetailCard, VSchedItem, MatchViewerModal } from './viewer_match.jsx';
import { isSwissFinalStandings, swissStandingsHeading, SwissStandingsViewer, LeagueMatrix, PoolNumberedMatchRow, PoolsViewer } from './viewer_standings.jsx';
import { deriveAwards, bracketHasDecidedFinal, resolveCompetitionAwards, AwardsView, FightingSpiritSection } from './viewer_awards.jsx';
import { PlayerMultiFilter, applyFilters, matchHighlightedBy, buildPlayerMatchHighlight, buildWatchlistUpcoming, ScheduleViewer, ViewerSchedule } from './viewer_schedule.jsx';
import { ViewerCompetition, ViewerOverview } from './viewer_competition.jsx';
import { ViewerHome, shouldShowRegister } from './viewer_home.jsx';

// --- ES re-export surface (kept identical to the pre-split viewer.jsx) ---
export { PlayerMultiFilter, applyFilters, matchHighlightedBy, competitionKindLabel, compMatches, tournamentMatches, currentMatchOf, buildPlayerMatchHighlight, buildWatchlistUpcoming, useWatchlist, entryKey, normalizeWatchlistEntry, normalizeWatchlist, migrateWatchlistOnLoad, resolveEntryPlayerIds, resolveWatchedPlayers, effectivePrimaryKey, findPrimaryEntry, buildPrimaryNextMatch, computeSecondaryAlert, isSwissFinalStandings, swissStandingsHeading, isFollowedPlayer, isPlayerWatched, deriveAwards, bracketHasDecidedFinal, resolveCompetitionAwards, buildRoster, MatchDetailCard, MatchViewerModal, AnnouncementCard, AnnouncementBanner, ViewerCompetition, ViewerOverview, ViewerHome, MyMatchAlertBanner, LeagueMatrix, PoolsViewer, PoolNumberedMatchRow, AwardsView, FightingSpiritSection, matchParticipantIds, TermV, VSchedItem, addPlayerToWatchlist, poolLabel, WATCHLIST_MAX };
export { isHttpURL, linkBase, isNonPublicOrigin, TournamentInfo } from './viewer_utils.jsx';
export { NOTIF_SYNC_EVENT, notifEnable, notifDisable, isFollowedMatchOnDeck } from './viewer_alerts.jsx';
export { notificationSupported, AnnBellBtn } from './viewer_notifications.jsx';
export { subBoutLabel } from './viewer_match.jsx';
export { mymatchQueueLabel };
export { shouldShowRegister };
export { resolveDeepLink } from './viewer_home.jsx';

// --- window.* surface for non-module consumers ---
// (viewer_awards.jsx sets window.{buildAllWinnersPublic,AllWinnersView}; the
// viewer_schedule.jsx/_utils.jsx/_notifications.jsx modules set their own
// window globals at module-eval time — those are NOT re-set here.)
if (typeof window !== 'undefined') {
    window.deriveAwards = deriveAwards;
    window.bracketHasDecidedFinal = bracketHasDecidedFinal;
    window.resolveCompetitionAwards = resolveCompetitionAwards;
}

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
// Reused read-only on the shiaijo operator console (pool standings + results).
window.PoolsViewer = PoolsViewer;

// Helpers consumed by viewer_watchlist.jsx (WatchPicker/WatchHeroCard/
// WatchlistPanel) at render time. poolLabel is already exposed by viewer_utils.jsx.
window.matchParticipantIds = matchParticipantIds;
window.addPlayerToWatchlist = addPlayerToWatchlist;
window.effectivePrimaryKey = effectivePrimaryKey;
window.entryKey = entryKey;
window.resolveEntryPlayerIds = resolveEntryPlayerIds;
window.mymatchQueueLabel = mymatchQueueLabel;
window.TermV = TermV;
window.VSchedItem = VSchedItem;
window.WATCHLIST_MAX = WATCHLIST_MAX;
