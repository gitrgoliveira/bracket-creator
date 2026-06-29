// display.jsx: thin entry point and barrel re-export.
//
// The actual implementations live in the split modules:
//   display_helpers.jsx    : shared data/label layer (pure functions + TermD)
//   display_scoreboard.jsx: per-court TV scoreboard (TvDisplay + boards)
//   display_lobby.jsx      : lobby / schedule TV (LobbyDisplay)
//   streaming_overlay.jsx  : OBS/vMix streaming overlay (StreamingOverlay)
//
// This file keeps DisplayRoute (the router-level entry) and sets the
// window.* globals consumed by the legacy script-tag surface, then
// re-exports the full public surface so existing
//   import { X } from './display.jsx'
// call sites (tests, viewer.jsx, app.jsx) continue to work unchanged.

import { TvDisplay, TvWhiteBoard, TvIndividualBoard, gatherIndividualGroup, findNextPoolOnCourt } from './display_scoreboard.jsx';
import { LobbyDisplay, LobbyMatchCell, LOBBY_COLORS, buildCourtSlots, LOBBY_PAGE_SIZE, LOBBY_CYCLE_MS, LOBBY_ROWS } from './display_lobby.jsx';
import { StreamingOverlay, overlayPositionLabel, findCurrentBoutIndex } from './streaming_overlay.jsx';
import {
    sideLabel,
    findRunningOnCourt,
    findUpcomingOnCourt,
    findActiveCourts,
    countCourtMatches,
    queueLabel,
    queueLabelCompact,
    poolNameOf,
    phaseProgressOnCourt,
} from './display_helpers.jsx';

// Wrapper that picks the right display component based on URL query.
// Used by the router when the path is /display. Reads `?court=A`,
// `?court=all`, `?overlay=true|1`, and `?position=top|bottom` off the
// current URL via useQuery(): see web-mobile/js/router.jsx.
//
// Defaults: missing court → "A", missing overlay → false, missing
// position → "bottom". Both "true" and "1" toggle overlay mode so the
// OBS muscle-memory ?overlay=1 form works alongside ?overlay=true.
//
// `connected` is forwarded so the per-court / lobby surfaces can show
// the SSE reconnect indicator. app.jsx owns the EventSource lifecycle
// and feeds the boolean down through the AppRouter props.
function DisplayRoute({ tournament, competitions, connected = true }) {
    const useQuery = window.AppRouter?.useQuery;
    const query = useQuery ? useQuery() : (() => {
        if (typeof window === 'undefined') return {};
        const s = window.location.search || '';
        const out = {};
        if (s.length < 2) return out;
        const trimmed = s.startsWith('?') ? s.slice(1) : s;
        for (const pair of trimmed.split('&')) {
            if (!pair) continue;
            const eq = pair.indexOf('=');
            if (eq === -1) out[decodeURIComponent(pair)] = '';
            else out[decodeURIComponent(pair.slice(0, eq))] = decodeURIComponent(pair.slice(eq + 1));
        }
        return out;
    })();
    const courtRaw = (query.court || 'A');
    const overlay = query.overlay === 'true' || query.overlay === '1';
    const position = query.position === 'top' ? 'top' : 'bottom';
    // Normalise "all" case-insensitively; keep single-court labels in
    // their declared case because the venue uses "A", "B", … not "a".
    const court = courtRaw.toLowerCase() === 'all' ? 'ALL' : courtRaw.toUpperCase();

    if (overlay) {
        return <StreamingOverlay court={court} position={position} competitions={competitions} />;
    }
    if (court === 'ALL') {
        return <LobbyDisplay tournament={tournament} competitions={competitions} connected={connected} />;
    }
    return <TvDisplay court={court} tournament={tournament} competitions={competitions} connected={connected} />;
}

export {
    TvDisplay,
    LobbyDisplay,
    LobbyMatchCell,
    LOBBY_COLORS,
    StreamingOverlay,
    DisplayRoute,
    sideLabel,
    findRunningOnCourt,
    findUpcomingOnCourt,
    findActiveCourts,
    countCourtMatches,
    queueLabel,
    queueLabelCompact,
    LOBBY_PAGE_SIZE,
    LOBBY_CYCLE_MS,
    LOBBY_ROWS,
    buildCourtSlots,
    // mp-13y: helpers exported for vitest.
    findCurrentBoutIndex,
    overlayPositionLabel,
    TvWhiteBoard,
    TvIndividualBoard,
    gatherIndividualGroup,
    findNextPoolOnCourt,
    phaseProgressOnCourt,
    poolNameOf,
};

if (typeof window !== 'undefined') {
    window.TvDisplay = TvDisplay;
    window.LobbyDisplay = LobbyDisplay;
    window.StreamingOverlay = StreamingOverlay;
    window.DisplayRoute = DisplayRoute;
    window.queueLabel = queueLabel;
    window.queueLabelCompact = queueLabelCompact;
}
