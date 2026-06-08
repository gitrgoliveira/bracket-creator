// Viewer side — mobile-first. Single tournament. Shows competitions as the home;
// each competition opens to its own Overview/Bracket/Pools/Schedule/Results.

import { useTeamLineups, TeamScoreboard, IndividualScore } from './match_scoreboard.jsx';
// Re-export the shared scoreboard primitives so existing tests that import them
// from '../viewer.jsx' keep working (the canonical defs now live in match_scoreboard.jsx).
export { BoutSubRow, boutHansokuMark } from './match_scoreboard.jsx';

const { useState, useMemo, useRef: useRefV, useEffect } = React;
const StatusBadge = window.StatusBadge;
const formatDate = window.formatDate;
const formatLabel = window.formatLabel;
const formatViewerHeaderEyebrow = window.formatViewerHeaderEyebrow;

// shouldShowRegister returns true when a "Register for this competition" button
// should be shown on a competition card. Extracted for unit testability (mp-e5j).
function shouldShowRegister(tournament, competition, hasHandler) {
  return !!(hasHandler &&
    tournament && tournament.mode === "self-run" &&
    competition && competition.kind !== "team" &&
    (!competition.status || competition.status === "setup"));
}

// TermV — kendo-glossary tooltip wrapper. Lazy lookup so the script
// load order between glossary.jsx and viewer.jsx doesn't matter.
// U1 / glossary.md.
function TermV(props) {
  if (typeof window !== 'undefined' && window.Term) {
    return React.createElement(window.Term, props, props.children);
  }
  return React.createElement('span', null, props.children);
}
// Canonical "match has both sides for real participants" predicate.
// Replaces the local `m.sideA && m.sideB` shorthand that treated the
// `{id:"",name:""}` placeholder from normalizeMatch as a real side —
// see admin_helpers.jsx for the full bug shape and rationale.
//
// Wrapped as a lazy callable rather than `const x = window.hasBothSides`
// because index.html loads viewer.js BEFORE admin_helpers.js (viewer
// is reachable pre-auth; admin helpers load later). At module-eval
// time, window.hasBothSides is undefined; by the time any React render
// runs, admin_helpers.js has executed and set the global, so deferring
// the lookup to call time is safe.
const hasBothSides = (m) => window.hasBothSides(m);
// Lazy callable for the same load-order reason as hasBothSides above.
// Canonical date format is DD-MM-YYYY, which doesn't lex-sort
// chronologically — use compareDmy as the sort comparator everywhere
// dates are ordered.
const compareDmy = (a, b) => window.compareDmy(a, b);

// Local mirror of display.jsx::queueLabelCompact ("Next up" / "#N").
// display.js loads before viewer.js (see index.html), so
// window.queueLabelCompact is normally available on first render; this
// serves as defense-in-depth if that ever changes. (The "N before yours"
// wording lives in mymatchQueueLabel — followed-player context only.)
function _localQueueLabelCompact(m) {
  if (!m || m.status !== "scheduled") return null;
  const qp = Number(m.queuePosition);
  if (!Number.isFinite(qp) || qp <= 0) return null;
  return qp === 1 ? "Next up" : `#${qp}`;
}

function competitionKindLabel(c) {
  const base = c.kind === "team" ? "Teams" : "Individual";
  if (c.gender === "M") return `${base} · Men`;
  if (c.gender === "F") return `${base} · Women`;
  return base;
}

const pluralize = window.pluralize;
const isPoolDaihyosenID = id => id.includes('-DH-');
const poolLabel = (m) => m.compFormat === "league" ? m.compName : m.poolName;
window.poolLabel = poolLabel;

function compMatches(c) {
  const out = [];

  // Setup status: no public data yet — draw is admin-only preview.
  // draw-ready is allowed through: pool/bracket structure is already
  // persisted and returned by handlers_viewer.go unconditionally, so
  // spectators can see the pool draw before the first match is called.
  if (!c.status || c.status === "setup") return out;

  const POOL_ID_RE = /^(.+?)(?:-DH-\d+|-TB-\d+|-\d+)$/;
  const rawPoolMatches = c.poolMatches || (c.pools ? c.pools.flatMap(p => p.matches.map(m => ({ ...m, phase: "pool", poolName: p.name, phaseName: p.name }))) : []);
  // Pool-daihyosen matches ("Pool X-DH-N") are representative bouts scored as
  // individual matches even in team competitions — override compKind and teamSize
  // so all isTeam checks (compKind === "team" || teamSize > 0) evaluate false,
  // routing to the individual ScoreEditorModal and rendering individual match UI.
  // Flat poolMatches from the viewer API don't carry phase/poolName; derive them
  // from the match ID (e.g. "Pool A-0" → poolName "Pool A") when absent.
  rawPoolMatches.forEach(m => {
    const isDH = isPoolDaihyosenID(m.id || "");
    const derivedPool = m.poolName || (POOL_ID_RE.exec(m.id || "") || [])[1] || "";
    out.push({ phase: "pool", poolName: derivedPool, phaseName: derivedPool, ...m, compId: c.id, compName: c.name, compFormat: c.format, compKind: isDH ? "" : c.kind, teamSize: isDH ? 0 : c.teamSize });
  });

  // mp-9dz: a preview bracket on a mixed source carries pool-origin
  // placeholders ("Pool A-1st") with assigned scheduled times. It must
  // NOT contribute to the global match list that feeds Find-My-Matches /
  // Watchlist / schedule / TV displays — those treat every bracket match
  // as a real, scheduled bout. The viewer aggregate endpoint already
  // strips it server-side; this is defense-in-depth for older caches and
  // any code path that bypasses the aggregator.
  const isPreviewBracket = !!(c.bracket && c.bracket.preview);
  const rounds = (!isPreviewBracket && c.bracket && c.bracket.rounds) ? c.bracket.rounds : (!isPreviewBracket ? (c.bracket || []) : []);
  rounds.forEach((round, ri) => round.forEach((m) => out.push({
    ...m,
    phase: "bracket",
    round: window.roundLabel(ri, rounds.length),
    phaseName: window.roundLabel(ri, rounds.length),
    // Raw 0-based round index alongside the display label so consumers
    // (useTeamLineups) need not parse the label — now a bracket-size string
    // ("Round 16") that a "Round N"→N-1 parse would misread as round 15.
    roundIndex: ri,
    compId: c.id,
    compName: c.name,
    compFormat: c.format,
    compKind: c.kind,
    teamSize: c.teamSize
  })));
  return out;
}

function tournamentMatches(t) {
  // draw-ready comps contribute their (unscored) draw matches so the
  // "Find my matches" / watchlist / full-schedule views can surface a
  // spectator's upcoming bout once the draw is published. These never
  // appear in the home LIVE/Up-next strips: those gate on liveCompIds,
  // which excludes draw-ready (a draw-ready comp is not live).
  return (t.competitions || [])
    .filter(c => c.status && c.status !== "setup")
    .flatMap(compMatches);
}

// Next match to be played in this competition (live first, else first scheduled in time order)
function currentMatchOf(c) {
  const ms = compMatches(c);
  const live = ms.find((m) => m.status === "running" && hasBothSides(m));
  if (live) return live;
  const sched = ms.filter((m) => m.status === "scheduled" && hasBothSides(m));
  sched.sort((a, b) => (a.scheduledAt || "99:99").localeCompare(b.scheduledAt || "99:99"));
  return sched[0] || null;
}

// --- Slice 4 helpers: "Find my matches" + Watchlist (FR-020 / FR-022 / FR-024) ---

// Pull a participant id off a match in either the canonical shape
// (`m.sideA.id` / `m.sideB.id` as produced by api_serializers.jsx) or
// the flat shape (`m.sideAId` / `m.sideBId`) some tests/fixtures use.
// Returns the two ids as a [aId, bId] tuple, either of which may be "".
function matchParticipantIds(m) {
  if (!m) return ["", ""];
  const aId = (m.sideA && typeof m.sideA === "object" ? m.sideA.id : null) || m.sideAId || "";
  const bId = (m.sideB && typeof m.sideB === "object" ? m.sideB.id : null) || m.sideBId || "";
  return [aId, bId];
}

// Pull the two display names off a match, again tolerant of both shapes.
function matchParticipantNames(m) {
  if (!m) return ["", ""];
  const aName = (m.sideA && typeof m.sideA === "object" ? m.sideA.name : m.sideA) || "";
  const bName = (m.sideB && typeof m.sideB === "object" ? m.sideB.name : m.sideB) || "";
  return [aName, bName];
}

// Check whether a participant object `p` refers to the followed player,
// matching by ID first (UUID) then by name as a fallback for cases where
// team-match sub-players or legacy fixtures key by display name only.
function isFollowedPlayer(p, followed) {
  if (!p || !followed) return false;
  const pId = (typeof p === "object" ? p.id : null) || "";
  const pName = (typeof p === "object" ? p.name : p) || "";
  if (pId && followed.id && pId === followed.id) return true;
  if (pName && followed.name && pName.trim().toLowerCase() === followed.name.trim().toLowerCase()) return true;
  return false;
}

// Return the subset of `matches` where the followed player participates.
// Matching is by UUID first; if `fallbackName` is provided and no UUID hits
// (e.g., legacy data that still keys by display name), fall back to a
// case-insensitive exact match on either side's name.
function buildPlayerMatchHighlight(playerId, matches, fallbackName) {
  const id = (playerId || "").toString();
  const list = Array.isArray(matches) ? matches : [];
  const byId = id ? list.filter((m) => {
    const [a, b] = matchParticipantIds(m);
    return (a && a === id) || (b && b === id);
  }) : [];
  if (byId.length > 0 || !fallbackName) return byId;
  const needle = String(fallbackName).trim().toLowerCase();
  if (!needle) return byId;
  return list.filter((m) => {
    const [an, bn] = matchParticipantNames(m);
    return (an && an.trim().toLowerCase() === needle)
        || (bn && bn.trim().toLowerCase() === needle);
  });
}

// LocalStorage keys for FR-020 / FR-024. Centralised so the deep-link
// handler (T114) writes the same keys the panels read.
const LS_MY_PLAYER_ID = "bc_my_player_id";
const LS_MY_PLAYER_NAME = "bc_my_player_name";
const LS_WATCHLIST = "bc_watchlist";
const WATCHLIST_MAX = 50;
const WATCHED_UPCOMING_MAX = 6;

// Hook: followed-player ({id, name} or null) backed by localStorage.
// Read on mount, surfaces a setter that persists. A `null` clears both keys
// (used by the "[X]" header indicator in T113). We deliberately avoid
// useCallback here — the setter identity churning on each render is fine
// because the consumers wrap it in event handlers, and this keeps the
// hook portable across the vitest setup (which only mocks a small set of
// React primitives).
function useFollowedPlayer() {
  const [player, setPlayer] = useState(() => {
    if (typeof window === "undefined") return null;
    try {
      const id = window.localStorage.getItem(LS_MY_PLAYER_ID) || "";
      const name = window.localStorage.getItem(LS_MY_PLAYER_NAME) || "";
      if (!id && !name) return null;
      return { id, name };
    } catch (_e) {
      return null;
    }
  });
  const update = (next) => {
    setPlayer(next);
    if (typeof window === "undefined") return;
    try {
      if (next && next.id) {
        window.localStorage.setItem(LS_MY_PLAYER_ID, next.id);
        window.localStorage.setItem(LS_MY_PLAYER_NAME, next.name || "");
      } else {
        window.localStorage.removeItem(LS_MY_PLAYER_ID);
        window.localStorage.removeItem(LS_MY_PLAYER_NAME);
      }
    } catch (_e) {
      // Silent: localStorage can throw in private-mode Safari; the
      // in-memory state still works for the session.
    }
  };
  return [player, update];
}

// Hook: watchlist (array of `{id, name, dojo}`) backed by localStorage.
// Defends against malformed JSON in storage (rare, but a corrupt key
// shouldn't crash the viewer for everyone using that browser profile).
function useWatchlist() {
  const [list, setList] = useState(() => {
    if (typeof window === "undefined") return [];
    try {
      const raw = window.localStorage.getItem(LS_WATCHLIST);
      if (!raw) return [];
      const parsed = JSON.parse(raw);
      if (!Array.isArray(parsed)) return [];
      return parsed.filter((x) => x && typeof x === "object" && x.id);
    } catch (_e) {
      return [];
    }
  });
  const persist = (next) => {
    const capped = next.slice(0, WATCHLIST_MAX);
    setList(capped);
    if (typeof window === "undefined") return;
    try {
      window.localStorage.setItem(LS_WATCHLIST, JSON.stringify(capped));
    } catch (_e) { /* see useFollowedPlayer */ }
  };
  return [list, persist];
}

// Return up to 6 upcoming matches across any watched player, sorted by
// scheduledAt ascending (empty/missing times sort last via "99:99" sentinel).
// "Upcoming" = status !== "completed" — we keep `running` matches in the
// list so a coach can spot a watched player who just started.
function buildWatchlistUpcoming(watched, allMatches, max = WATCHED_UPCOMING_MAX) {
  const watchedIds = new Set();
  (Array.isArray(watched) ? watched : []).forEach((w) => {
    if (w && w.id) watchedIds.add(String(w.id));
  });
  if (watchedIds.size === 0) return [];
  const list = Array.isArray(allMatches) ? allMatches : [];
  const upcoming = list.filter((m) => {
    if (!m || m.status === "completed") return false;
    const [a, b] = matchParticipantIds(m);
    return (a && watchedIds.has(a)) || (b && watchedIds.has(b));
  });
  upcoming.sort((x, y) => {
    const xt = x.scheduledAt || "99:99";
    const yt = y.scheduledAt || "99:99";
    return xt.localeCompare(yt);
  });
  return upcoming.slice(0, max);
}

function buildFollowedNextMatch(followedPlayer, allMatches) {
  if (!followedPlayer || (!followedPlayer.id && !followedPlayer.name)) return null;
  const mine = buildPlayerMatchHighlight(followedPlayer.id, allMatches, followedPlayer.name)
    .filter(hasBothSides)
    .filter((m) => m.status !== "completed");
  mine.sort((a, b) => {
    const ao = a.status === "running" ? 0 : 1;
    const bo = b.status === "running" ? 0 : 1;
    if (ao !== bo) return ao - bo;
    return (a.scheduledAt || "99:99").localeCompare(b.scheduledAt || "99:99");
  });
  return mine[0] || null;
}

// checkedIn=true wins if any check-in-enabled competition has the player checked in.
function buildRoster(competitions) {
  const map = new Map();
  (competitions || []).forEach((c) => {
    (c.players || []).forEach((p) => {
      if (!p || !p.id) return;
      const checkedIn = !!c.checkInEnabled && !!p.checkedIn;
      const existing = map.get(p.id);
      if (!existing) {
        map.set(p.id, { ...p, checkedIn });
      } else if (checkedIn && !existing.checkedIn) {
        map.set(p.id, { ...existing, checkedIn: true });
      }
    });
  });
  return Array.from(map.values());
}

// ---------------------------------------------------------------------------
// mp-4fd: On-deck match alert — predicate, hook, banner, chime
// ---------------------------------------------------------------------------

// LocalStorage key for the chime mute preference (viewer-level).
const LS_CHIME_MUTED = "viewer.matchAlert.chimeMuted";

// Hook: chime-muted preference backed by localStorage.
// Multiple instances (ViewerHome + NotificationSettings) stay in sync via
// a custom DOM event dispatched on toggle — the native `storage` event only
// fires across tabs, not within the same page.
const CHIME_SYNC_EVENT = "chimeMutedSync";

function useChimeMuted() {
  const [muted, setMuted] = useState(() => {
    if (typeof window === "undefined") return false;
    try {
      return window.localStorage.getItem(LS_CHIME_MUTED) === "true";
    } catch (_e) { return false; }
  });
  // Sync across same-page instances via custom event.
  useEffect(() => {
    if (typeof window === "undefined") return;
    const onSync = (e) => setMuted(!!e.detail);
    window.addEventListener(CHIME_SYNC_EVENT, onSync);
    return () => window.removeEventListener(CHIME_SYNC_EVENT, onSync);
  }, []);
  const toggle = () => {
    const next = !muted;
    setMuted(next);
    if (typeof window === "undefined") return;
    try {
      window.localStorage.setItem(LS_CHIME_MUTED, next ? "true" : "false");
    } catch (_e) { /* storage unavailable — in-memory is fine */ }
    // Notify other hook instances in the same page.
    try {
      window.dispatchEvent(new CustomEvent(CHIME_SYNC_EVENT, { detail: next }));
    } catch (_e) { /* CustomEvent unavailable */ }
  };
  return [muted, toggle];
}

// Extract a side's display name from the normalised match shape.
// Handles both {sideA: {name}} (normalised) and flat sideAName (legacy).
function matchSideName(side, fallbackName) {
  return (side && side.name) || fallbackName || "";
}

// Predicate: is the followed player's match on-deck (up-next or running)?
// Exported for unit testing and mp-5px (service worker path must reuse this).
export function isFollowedMatchOnDeck(m) {
  if (!m) return false;
  if (m.status === "running") return true;
  if (m.status === "scheduled" && Number(m.queuePosition) === 1) return true;
  return false;
}

// Hook: detect transitions into on-deck state for the followed match.
// Fires the alert surfaces (document.title, chime, backgrounded Notification)
// exactly once per genuine transition; ignores SSE re-renders that leave the
// state unchanged. Accepts a callback `onAlert` for testability.
//
// Dedup is by "signature" (matchId + "running" or "upnext"), NOT a timer.
// First render primes the ref without alerting (avoids false alert on mount
// when loading a late-open tab).
function useFollowedMatchAlert(myNextMatch, { chimeMuted, onAlert } = {}) {
  const lastSigRef = useRefV(null); // null = not yet primed
  const audioCtxRef = useRefV(null);
  const originalTitleRef = useRefV(null);

  // Unlock AudioContext on first user interaction (gesture gate).
  // Also cleans up AudioContext and restores document.title on unmount.
  useEffect(() => {
    const unlock = () => {
      if (audioCtxRef.current && audioCtxRef.current.state === "suspended") {
        audioCtxRef.current.resume().catch(() => {});
      }
    };
    if (typeof window !== "undefined") {
      window.addEventListener("click", unlock, { passive: true });
      window.addEventListener("touchstart", unlock, { passive: true });
    }
    return () => {
      if (typeof window !== "undefined") {
        window.removeEventListener("click", unlock);
        window.removeEventListener("touchstart", unlock);
      }
      // Restore document.title if it was flashed when unmounting.
      if (originalTitleRef.current !== null && typeof document !== "undefined") {
        document.title = originalTitleRef.current;
        originalTitleRef.current = null;
      }
      // Close AudioContext to free the browser resource.
      if (audioCtxRef.current) {
        try { audioCtxRef.current.close(); } catch (_e) { /* already closed */ }
        audioCtxRef.current = null;
      }
    };
  }, []);

  useEffect(() => {
    const m = myNextMatch;
    const onDeck = isFollowedMatchOnDeck(m);

    // Build the signature for this state.  Always a string so the
    // strict-equality fast-path (sig === lastSigRef.current) works when
    // off-deck too — null !== "" would bypass the dedup on every render.
    const sig = onDeck && m
      ? m.id + ":" + (m.status === "running" ? "running" : "upnext")
      : "";

    // First call: prime without alerting (avoids false alert on reconnect).
    if (lastSigRef.current === null) {
      lastSigRef.current = sig || "";
      return;
    }

    // No transition: same sig as before.
    if (sig === lastSigRef.current) return;
    lastSigRef.current = sig || "";

    if (!onDeck) {
      // Left on-deck: restore title.
      if (originalTitleRef.current !== null && typeof document !== "undefined") {
        document.title = originalTitleRef.current;
        originalTitleRef.current = null;
      }
      return;
    }

    // Genuine transition INTO on-deck: fire all alert surfaces.

    // 1. document.title flash.
    if (typeof document !== "undefined") {
      if (originalTitleRef.current === null) {
        originalTitleRef.current = document.title;
      }
      const titlePrefix = m.status === "running" ? "🔴 NOW — " : "(1) Your match is next — ";
      document.title = titlePrefix + (originalTitleRef.current || "Tournament");
    }

    // 2. Chime via WebAudio (two-tone, no asset needed).
    if (!chimeMuted) {
      try {
        const AudioCtx = typeof AudioContext !== "undefined" ? AudioContext
          : (typeof window !== "undefined" ? window.AudioContext || window.webkitAudioContext : null);
        if (AudioCtx) {
          if (!audioCtxRef.current) audioCtxRef.current = new AudioCtx();
          const ctx = audioCtxRef.current;
          const playTone = (freq, startTime, duration) => {
            const osc = ctx.createOscillator();
            const gain = ctx.createGain();
            osc.connect(gain);
            gain.connect(ctx.destination);
            osc.type = "sine";
            osc.frequency.value = freq;
            gain.gain.setValueAtTime(0.3, startTime);
            gain.gain.exponentialRampToValueAtTime(0.001, startTime + duration);
            osc.start(startTime);
            osc.stop(startTime + duration);
          };
          const t0 = ctx.currentTime;
          playTone(880, t0, 0.25);
          playTone(1100, t0 + 0.3, 0.35);
        }
      } catch (_e) { /* autoplay restrictions or unavailable AudioContext — silent fail */ }
    }

    // 3. Backgrounded browser Notification (reuses existing opt-in).
    if (typeof window !== "undefined" && typeof window.fireNotification === "function" && m) {
      const sideA = matchSideName(m.sideA, m.sideAName);
      const sideB = matchSideName(m.sideB, m.sideBName);
      const courtStr = m.court ? ` — Shiaijo ${m.court}` : "";
      const body = (sideA && sideB) ? `${sideA} vs ${sideB}${courtStr}` : courtStr.slice(3) || "";
      const notifTitle = m.status === "running" ? "Your match is on now" : "Your match is next";
      window.fireNotification(notifTitle, body, { tag: "match-" + m.id });
    }

    // 4. Notify consumer (e.g. to show/update the banner).
    if (typeof onAlert === "function") onAlert(m);
  });
}

// Banner component: rendered when the followed match is on-deck.
function MyMatchAlertBanner({ match, onView, onDismiss }) {
  if (!match) return null;
  const kind = match.status === "running" ? "NOW" : "Next up";
  const sideA = matchSideName(match.sideA, match.sideAName);
  const sideB = matchSideName(match.sideB, match.sideBName);
  const vs = (sideA && sideB) ? `${sideA} vs ${sideB}` : "";
  const courtStr = match.court ? `Shiaijo ${match.court}` : "";
  return (
    <div className="match-alert-banner" data-testid="match-alert-banner" role="alert">
      <div className="match-alert-banner__content">
        <span className="match-alert-banner__badge">{kind}</span>
        <span className="match-alert-banner__text">
          {vs && <strong>{vs}</strong>}
          {courtStr && <span> · {courtStr}</span>}
        </span>
      </div>
      <div className="match-alert-banner__actions">
        {onView && (
          <button className="btn btn--sm btn--primary" onClick={() => onView(match)}>
            View
          </button>
        )}
        {onDismiss && <button
          className="match-alert-banner__dismiss"
          onClick={onDismiss}
          aria-label="Dismiss match alert"
        >✕</button>}
      </div>
    </div>
  );
}

// isHttpURL returns true when u starts with http:// or https://. Exported for
// testing (mp-ef3 Copilot round 2).
export const isHttpURL = (u) => /^https?:\/\//i.test(u);

// linkBase returns the configured publicURL (mp-s1gl) for the given tournament,
// falling back to the current frame's origin when publicURL is not set. This is
// the single source of truth for building externally-shareable links (QR codes,
// viewer share links) so operators can set one canonical URL regardless of what
// address the admin browser is using.
// Guard against opaque-origin contexts (sandboxed iframes) where
// window.location.origin is the literal string "null". Falling back to
// window.location.origin in that case would produce "null/register/..." links,
// so return an empty string instead so callers get a relative path.
export const linkBase = (t) => {
  if (t && t.publicURL) return t.publicURL;
  const origin = window.location.origin;
  return (!origin || origin === "null") ? "" : origin;
};

// isNonPublicOrigin returns true when the given origin looks like a device-local
// or LAN address that won't be reachable by remote attendees. Used to warn the
// operator when publicURL is unset and the fallback origin would produce
// unworkable QR codes / share links (mp-s1gl).
//
// Treats falsy / "null" (opaque-origin sandboxed iframe) as non-public so the
// warning degrades gracefully and never renders "null/register/..." links.
export const isNonPublicOrigin = (origin) => {
  if (!origin || origin === "null") return true;
  let host, hasPort = false;
  try {
    const u = new URL(origin);
    host = u.hostname;
    hasPort = u.port !== "";
  } catch { return true; }
  if (host === "localhost" || host === "0.0.0.0") return true;
  if (host === "::1" || host === "[::1]") return true;  // IPv6 loopback
  if (host.endsWith(".local")) return true;
  if (/^127\./.test(host)) return true;
  if (/^10\./.test(host)) return true;
  if (/^192\.168\./.test(host)) return true;
  if (/^172\.(1[6-9]|2\d|3[01])\./.test(host)) return true;
  if (hasPort) return true;
  return false;
};

// TournamentInfo renders optional public tournament info (mp-ef3) as a
// read-only card on the viewer home page. Returns null when no info fields
// are set so the card is invisible for tournaments that haven't filled them in.
export function TournamentInfo({ tournament }) {
  const t = tournament;
  if (!t.venueAddress && !t.venueMapURL && !t.openingTime && !t.closingTime && !t.awardsNote && !t.rulesURL && !t.infoNotes && !(t.contacts && t.contacts.length > 0)) return null;

  const contactLink = (value) => {
    if (!value) return value;
    if (isHttpURL(value)) return <a href={value} className="tournament-info__link" target="_blank" rel="noopener noreferrer">{value.replace(/^https?:\/\//i, "")}</a>;
    if (value.includes("@")) return <a href={"mailto:" + value} className="tournament-info__link">{value}</a>;
    if (/^\+?[\d\s()-]+$/.test(value)) return <a href={"tel:" + value.replace(/[\s()-]/g, "")} className="tournament-info__link">{value}</a>;
    return value;
  };

  return (
    <div className="tournament-info">
      <div className="tournament-info__title">Tournament Info</div>
      <dl className="tournament-info__grid">
        {(t.venueAddress || t.venueMapURL) && <>
          <dt className="tournament-info__label">Venue</dt>
          <dd className="tournament-info__value">
            {t.venueAddress}
            {t.venueMapURL && isHttpURL(t.venueMapURL) && <>{t.venueAddress ? " " : ""}<a href={t.venueMapURL} className="tournament-info__link" target="_blank" rel="noopener noreferrer">View map ↗</a></>}
          </dd>
        </>}
        {(t.openingTime || t.closingTime) && <>
          <dt className="tournament-info__label">Times</dt>
          <dd className="tournament-info__value">
            {t.openingTime && t.closingTime ? `${t.openingTime} – ${t.closingTime}` : t.openingTime ? `Opens ${t.openingTime}` : `Closes ${t.closingTime}`}
          </dd>
        </>}
        {t.awardsNote && <>
          <dt className="tournament-info__label">Awards</dt>
          <dd className="tournament-info__value">{t.awardsNote}</dd>
        </>}
        {t.rulesURL && <>
          <dt className="tournament-info__label">Rules</dt>
          <dd className="tournament-info__value">{isHttpURL(t.rulesURL) ? <a href={t.rulesURL} className="tournament-info__link" target="_blank" rel="noopener noreferrer">{t.rulesURL.replace(/^https?:\/\//i, "")}</a> : t.rulesURL}</dd>
        </>}
        {t.infoNotes && <>
          <dt className="tournament-info__label">Notes</dt>
          <dd className="tournament-info__value">{t.infoNotes}</dd>
        </>}
        {t.contacts && t.contacts.length > 0 && <>
          <dt className="tournament-info__label">Contact</dt>
          <dd className="tournament-info__value">
            {t.contacts.map((ct, i) => (
              <div key={i} className="tournament-info__contact">
                {ct.label && <span className="tournament-info__contact-label">{ct.label}:</span>}
                {" "}{contactLink(ct.value)}
              </div>
            ))}
          </dd>
        </>}
      </dl>
    </div>
  );
}

function ViewerHome({ tournament, onSelectCompetition, onAdminClick, onOpenSchedule, onRegister, onOpenResults }) {
  const t = tournament;
  const comps = t.competitions || [];
  const compsByDate = useMemo(() => {
    const map = {};
    comps.forEach((c) => {
      const d = c.date || t.date || "";
      if (!map[d]) map[d] = [];
      map[d].push(c);
    });
    return map;
  }, [comps, t.date]);
  const dates = Object.keys(compsByDate).sort(compareDmy);

  const displayEntriesByDate = useMemo(() => {
    const result = {};
    Object.keys(compsByDate).forEach(d => {
      result[d] = compsByDate[d].map(c => ({ kind: "single", comp: c }));
    });
    return result;
  }, [compsByDate]);

  const [courtFilter, setCourtFilter] = useState("all");
  const [selectedMatch, setSelectedMatch] = useState(null);

  // Slice 4 / FR-020 / FR-022 / FR-024: per-viewer personalisation —
  // the followed player ("Find my matches") and a watchlist of up to 50
  // participants. Both panels read/write localStorage so the selection
  // survives reload and SSE-driven re-renders.
  const [followedPlayer, setFollowedPlayer] = useFollowedPlayer();
  const [watchlist, setWatchlist] = useWatchlist();

  // T114: parse `?player=<uuid>` (and optionally `?name=<name>`) deep
  // links from QR codes. We don't import useQuery here because the parsing
  // is tiny and we want the auto-apply to run exactly once, even if
  // history is later changed in-app via route(). When the UUID hits a
  // participant we set the followed-player; if the UUID misses we fall
  // back to a name match (FR-020 / acceptance scenario 5).
  const roster = useMemo(() => buildRoster(t.competitions), [t.competitions]);

  const deepLinkApplied = useRefV(false);
  React.useEffect(() => {
    if (deepLinkApplied.current) return;
    if (typeof window === "undefined" || !window.location) return;
    if (roster.length === 0) return; // wait until participants are loaded
    const params = new URLSearchParams(window.location.search || "");
    const qpPlayer = (params.get("player") || "").trim();
    const qpName = (params.get("name") || "").trim();
    if (!qpPlayer && !qpName) { deepLinkApplied.current = true; return; }
    // 1) UUID lookup
    let hit = qpPlayer ? roster.find((p) => p.id === qpPlayer) : null;
    // 2) Fall back to name (use ?name= if present, else treat ?player= as a name)
    if (!hit) {
      const needle = (qpName || qpPlayer).toLowerCase();
      if (needle) hit = roster.find((p) => (p.name || "").toLowerCase().includes(needle));
    }
    if (hit) setFollowedPlayer({ id: hit.id, name: hit.name });
    deepLinkApplied.current = true;
  }, [roster, setFollowedPlayer]);

  // global "across-all-competitions" lists for the home page
  const allMatches = useMemo(() => tournamentMatches(t), [t]);
  // Live-comp set: gates the home NOW / Up-next strips and the live dot.
  // BOTH setup and draw-ready are excluded — a draw-ready comp has a published
  // draw but no match has been called, so it is NOT live. (The competition
  // detail view still shows its Pools/Bracket tabs; that is governed separately
  // by isPreStart in ViewerCompetition.)
  const liveCompIds = useMemo(() => new Set((t.competitions || []).filter(c => c.status && c.status !== "setup" && c.status !== "draw-ready").map(c => c.id)), [t.competitions]);
  // Apply hasBothSides here too — pre-fix, a bracket match marked
  // `running` while one side was still an unresolved "Winner of rX-mY"
  // placeholder would appear in the public NOW strip, even though
  // the upcoming list / cards / detail view all reject placeholder
  // sides. Mirrors the upNext filter below.
  const live = allMatches.filter((m) => m.status === "running" && hasBothSides(m) && liveCompIds.has(m.compId) && (courtFilter === "all" || m.court === courtFilter));
  let upNext = allMatches.filter((m) => m.status === "scheduled" && hasBothSides(m) && liveCompIds.has(m.compId) && (courtFilter === "all" || m.court === courtFilter));
  if (courtFilter === "all") upNext = upNext.slice(0, 3);

  const myNextMatch = useMemo(() => buildFollowedNextMatch(followedPlayer, allMatches), [followedPlayer, allMatches]);

  // FR-024: up to 6 upcoming matches across the watchlist for the home
  // "Watched matches" section (T116).
  const watchedUpcoming = useMemo(
    () => buildWatchlistUpcoming(watchlist, allMatches.filter(hasBothSides)),
    [watchlist, allMatches]
  );

  // mp-4fd: on-deck alert for the followed player's match.
  const [chimeMuted] = useChimeMuted();
  const [alertMatch, setAlertMatch] = useState(null);
  const [alertDismissed, setAlertDismissed] = useState(false);
  // Reset dismissal when the followed player or match changes.
  useEffect(() => {
    setAlertDismissed(false);
  }, [followedPlayer, myNextMatch && myNextMatch.id]);
  useFollowedMatchAlert(myNextMatch, {
    chimeMuted,
    onAlert: (m) => { setAlertMatch(m); setAlertDismissed(false); },
  });
  const showAlertBanner = alertMatch && !alertDismissed && isFollowedMatchOnDeck(myNextMatch);

  return (
    <div className="viewer">
      <div className="viewer__shell">
        <div className="viewer__head viewer__head--hero">
          <img src="/api/branding/logo" onError={(e) => { e.target.onerror = null; e.target.src = "/logo.jpeg"; }} alt="Tournament logo" className="topbar__logo viewer__logo" decoding="async" />
          <div className="viewer__title-block">
            <div className="viewer__eyebrow">
              {formatViewerHeaderEyebrow(formatDate(t.date), t.venue)}
            </div>
            <div className="viewer__title viewer__title--lg">{t.name}</div>
          </div>
          <button className="viewer__admin-pill" onClick={onAdminClick}>
            <span>🔒</span> Admin
          </button>
        </div>

        <div className="viewer__body">
          <TournamentInfo tournament={t} />
          <div style={{ marginBottom: 16, display: "flex", justifyContent: "flex-end" }}>
             <select className="input" style={{ width: "auto" }} value={courtFilter} onChange={(e) => setCourtFilter(e.target.value)}>
               <option value="all">All Shiaijo</option>
               {(t.courts || ["A"]).map(c => <option key={c} value={c}>Shiaijo {c}</option>)}
             </select>
          </div>

          {/* mp-4fd: on-deck alert banner — shown when the followed player's
              match is up-next or running; dismissed by the viewer or auto-
              cleared when the match is no longer on-deck. */}
          {showAlertBanner && (
            <MyMatchAlertBanner
              match={alertMatch}
              onView={(m) => { setSelectedMatch(m); setAlertDismissed(true); }}
              onDismiss={() => setAlertDismissed(true)}
            />
          )}

          {/* T111 / T112 / FR-020 / FR-022: "Find my matches" + "Your next
              match" card. Rendered up top so a competitor or coach who
              opens the viewer mid-tournament lands on their next fight
              without scrolling past the competition list. */}
          <MyMatchPanel
            roster={roster}
            followedPlayer={followedPlayer}
            setFollowedPlayer={setFollowedPlayer}
            nextMatch={myNextMatch}
            onMatchClick={setSelectedMatch}
          />

          {/* T115 / T116 / FR-024: Watchlist + Watched matches. Coaches
              follow multiple students; up to six upcoming watched matches
              are surfaced as a single list. */}
          <WatchlistPanel
            tournament={t}
            watchlist={watchlist}
            setWatchlist={setWatchlist}
            upcoming={watchedUpcoming}
            onMatchClick={setSelectedMatch}
          />

          {/* mp-cw1: Browser push notification opt-in toggle. */}
          <NotificationSettings />

          {live.length > 0 && (
            <div className="hero-live">
              <div className="hero-live__lbl"><span className="dot dot--live"></span> NOW · {pluralize(live.length, "match", "matches")}</div>
              <div className="vsched" style={{ marginTop: 8 }}>
                {live.slice(0, 3).map((m) => <VSchedItem key={m.compId + m.id} m={m} tweaks={{ showDojo: true }} showCompetition onClick={() => setSelectedMatch(m)} />)}
              </div>
            </div>
          )}

          <button
            className="vlist-item vlist-item--row"
            onClick={onOpenSchedule}
          >
            <span className="vlist-item__icon">🗓</span>
            <div className="vlist-item__rowbody">
              <div className="vlist-item__rowtitle">Full schedule</div>
              <div className="vlist-item__rowsub">{pluralize(allMatches.filter(hasBothSides).length, "match", "matches")} across {pluralize((tournament.courts || []).length, "shiaijo (court)", "shiaijo (courts)")} · search by player or team</div>
            </div>
            <span className="vlist-item__rowchev">→</span>
          </button>

          {/* mp-koqh: Results summary — only shown when at least one comp has completed. */}
          {onOpenResults && comps.some((c) => c.status === "completed") && (
            <button
              className="vlist-item vlist-item--row"
              onClick={onOpenResults}
              data-testid="open-results-btn"
            >
              <span className="vlist-item__icon">🏅</span>
              <div className="vlist-item__rowbody">
                <div className="vlist-item__rowtitle">Results</div>
                <div className="vlist-item__rowsub">All competition placings</div>
              </div>
              <span className="vlist-item__rowchev">→</span>
            </button>
          )}

          {dates.length === 0 ? (
            <>
              <div className="section-title">Competitions</div>
              <div className="vlist">
                <div className="empty">
                  <div className="icon">⏳</div>
                  <h3>No competitions yet</h3>
                  <div style={{ fontSize: 13 }}>Check back soon for the tournament schedule and updates.</div>
                </div>
              </div>
            </>
          ) : dates.map((d) => (
            <div key={d}>
              <div className="section-title">{formatDate(d)}</div>
              <div className="vlist">
                {(displayEntriesByDate[d] || []).map((entry) => {
                  const c = entry.comp;
                  const matches = compMatches(c).filter(hasBothSides);
                  const total = matches.length;
                  const done = matches.filter((m) => m.status === "completed").length;
                  const liveCount = matches.filter((m) => m.status === "running").length;
                  const pct = total ? Math.round((done / total) * 100) : 0;
                  const showRegister = shouldShowRegister(t, c, !!onRegister);
                  return (
                    <div key={c.id} style={{ position: "relative" }}>
                      <button className="vlist-item vlist-item--comp" style={{ width: "100%" }} onClick={() => onSelectCompetition(c.id)}>
                        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start", gap: 8 }}>
                          <div style={{ minWidth: 0 }}>
                            <div className="vlist-item__eyebrow">{competitionKindLabel(c)}{c.teamSize > 1 ? ` · ${c.teamSize}-person` : ""}</div>
                            <div className="vlist-item__name">{c.name}</div>
                            <div className="vlist-item__meta">
                              {c.players.length} {c.kind === "team" ? "teams" : "players"} · {formatLabel(c.format)} · Starts {c.startTime}
                            </div>
                          </div>
                          <StatusBadge status={c.status} showLiveDot format={c.format} />
                        </div>
                        {c.status && c.status !== "setup" && c.status !== "draw-ready" && total > 0 && (
                          <div className="vlist-item__progress">
                            <div className="vlist-item__bar"><div style={{ width: pct + "%" }}></div></div>
                            <div className="vlist-item__pct">
                              {liveCount > 0 ? <span style={{ color: "var(--red)", fontWeight: 600 }}>● {liveCount} now</span> : pluralize(done, "match", "matches") + " / " + total}
                            </div>
                          </div>
                        )}
                      </button>
                      {showRegister && (
                        <div style={{ padding: "0 12px 12px" }}>
                          <button
                            className="btn btn--primary btn--sm btn--full"
                            onClick={(e) => {
                              e.stopPropagation();
                              onRegister(c.id);
                            }}
                          >
                            Register for this competition
                          </button>
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            </div>
          ))}

          {upNext.length > 0 && (
            <>
              <div className="section-title" style={{ marginTop: 20 }}>Up next · {upNext.length}</div>
              <div className="vsched">
                {upNext.map((m) => <VSchedItem key={m.compId + m.id} m={m} tweaks={{ showDojo: true }} showCompetition onClick={() => setSelectedMatch(m)} />)}
              </div>
            </>
          )}

          {/* mp-c38: sponsor logos. Hidden when none configured. */}
          {window.SponsorStrip && <window.SponsorStrip sponsors={t && t.sponsors} variant="viewer" />}

          {/* U1: link to the kendo glossary so volunteers (and
              spectators new to kendo) can browse the term register
              that the inline tooltips draw from. */}
          <div className="vlist" style={{ marginTop: 12 }}>
            <a
              className="vlist-item vlist-item--row"
              href="/glossary"
              onClick={(e) => {
                e.preventDefault();
                if (window.AppRouter && window.AppRouter.route) window.AppRouter.route("/glossary");
                else window.location.href = "/glossary";
              }}
              style={{ textDecoration: "none" }}
            >
              <span className="vlist-item__icon">📖</span>
              <div className="vlist-item__rowbody">
                <div className="vlist-item__rowtitle">Kendo glossary</div>
                <div className="vlist-item__rowsub">Quick reference for scoring-table terms (Kiken, Hikiwake, Encho, etc.)</div>
              </div>
              <span className="vlist-item__rowchev">→</span>
            </a>
          </div>

          {/* T069 / FR-016: "Display modes" — links into the public
              /display routes for TV screens, lobby boards, and OBS browser
              sources. Each link opens in a new tab so the host page
              (viewer) keeps its tab and stays interactive. Collapsed by
              default (mp-mjaq) since most viewers are spectators; only
              tournament operators need these links. */}
          <DisplayModes tournament={t} />
          {window.VersionFooter && <window.VersionFooter />}
        </div>
      </div>
      {selectedMatch && <MatchViewerModal match={selectedMatch} onClose={() => setSelectedMatch(null)} tournament={t} />}
    </div>
  );
}

// SinglePlayerPicker — typeahead search over the full tournament roster.
// Used by MyMatchPanel + WatchlistPanel. Distinct from PlayerMultiFilter
// because we want a one-shot "pick a player" interaction (followed
// player is single-valued; watchlist entries are added one at a time)
// rather than a chip-list of active filters.
function SinglePlayerPicker({ roster, onPick, placeholder, excludeIds }) {
  const [query, setQuery] = useState("");
  const [open, setOpen] = useState(false);
  const ref = useRefV(null);
  const excluded = useMemo(() => new Set(excludeIds || []), [excludeIds]);
  const q = query.trim().toLowerCase();
  const candidates = useMemo(() => {
    const base = roster.filter((p) => !excluded.has(p.id));
    if (!q) return base.slice(0, 20);
    return base.filter((p) =>
      (p.name || "").toLowerCase().includes(q) || (p.dojo || "").toLowerCase().includes(q)
    ).slice(0, 20);
  }, [roster, q, excluded]);

  React.useEffect(() => {
    const onDoc = (e) => { if (ref.current && !ref.current.contains(e.target)) setOpen(false); };
    document.addEventListener("mousedown", onDoc);
    return () => document.removeEventListener("mousedown", onDoc);
  }, []);

  return (
    <div className="pmf" ref={ref} style={{ marginBottom: 8 }}>
      <div className="pmf__bar" onClick={() => setOpen(true)}>
        <input
          className="pmf__input"
          placeholder={placeholder || "Search by name or dojo…"}
          value={query}
          onChange={(e) => { setQuery(e.target.value); setOpen(true); }}
          onFocus={() => setOpen(true)}
        />
      </div>
      {open && candidates.length > 0 && (
        <div className="pmf__dropdown">
          <div className="pmf__dropdown-head">
            {q ? pluralize(candidates.length, "match", "matches") : `${pluralize(roster.length, "participant")} — type to search`}
          </div>
          {candidates.map((p) => (
            <button
              key={p.id}
              className="pmf__option"
              onClick={() => { onPick(p); setQuery(""); setOpen(false); }}
            >
              <span className="pmf__check">{p.checkedIn ? "✓" : ""}</span>
              <span className="pmf__opt-body">
                <span className="pmf__opt-name">
                  {p.name}
                  {p.checkedIn && <span className="tag-badge" style={{ marginLeft: 8, fontSize: 9 }}>Checked in</span>}
                </span>
                <span className="pmf__opt-dojo">{p.dojo || ""}</span>
              </span>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

// mymatchQueueLabel — FR-025 label for the "Your next match" Queue chip.
//
// Contract:
//   - status==="scheduled" + queuePosition===1 → "Next up"
//   - status==="scheduled" + queuePosition>1   → "<qp-1> before yours"
//   - status==="running"                       → null (round label already shows " · NOW")
//   - anything else (completed/forfeit/cancelled, or no qp)  → null (hide chip)
//
// Wording mirrors the VSchedItem helper below and display.jsx::queueLabel
// so all three viewer surfaces agree. Running matches return null because
// the my-match__round label already appends " · NOW" — rendering it
// again in the Queue chip would be a duplicate. We intentionally do NOT
// fall back to "Scheduled HH:MM" the way display.jsx does — the
// MyMatchPanel already has a dedicated Time chip.
// Exported for unit-testing.
export function mymatchQueueLabel(m) {
  if (!m) return null;
  if (m.status === "running") return null;
  if (m.status !== "scheduled") return null;
  const qp = Number(m.queuePosition);
  if (!Number.isFinite(qp) || qp <= 0) return null;
  if (qp === 1) return "Next up";
  return `${qp - 1} before yours`;
}

// subBoutLabel — center label for a team sub-bout row. The daihyosen
// (representative bout) is stored with the sentinel position -1 (see
// admin_scoring_modal.jsx buildPatch); render it as "Daihyosen" (matching
// admin_pools.jsx wording) rather than the literal "Match -1" the
// `position || index+1` fallback would otherwise produce. Shared by both
// viewer sub-row sites (MatchDetailCard, MatchViewerModal). Exported for
// unit-testing.
export function subBoutLabel(sub, index) {
  if (sub && sub.position === -1) return "Daihyosen";
  return `Match ${(sub && sub.position) || index + 1}`;
}

// MyMatchPanel — "Find my matches" entry point + active "Your next match"
// card. Two states:
//   1) No followed player yet → render a picker; selecting persists to
//      localStorage via setFollowedPlayer (FR-020).
//   2) Followed player set → render the next-match card (or a finished
//      empty-state if all matches are complete) + a "Following: name [X]"
//      header so the viewer can clear the selection (FR-022).
function MyMatchPanel({ roster, followedPlayer, setFollowedPlayer, nextMatch, onMatchClick }) {
  // Hoisted above the early return so it is always computed before the guard;
  // used in the non-empty branch to show the check-in badge.
  const pRecord = followedPlayer?.id
    ? (roster.find(p => p.id === followedPlayer.id) ?? null)
    : null;

  if (!followedPlayer || !followedPlayer.id) {
    return (
      <div className="card" data-testid="viewer-home-mymatch" style={{ marginBottom: 16, padding: 14 }}>
        <div className="section-title" style={{ marginTop: 0 }}>Find my matches</div>
        <div style={{ fontSize: 12, color: "var(--ink-3)", marginBottom: 8 }}>
          Pick a participant — we'll surface their next match and highlight them across the schedule.
        </div>
        <SinglePlayerPicker
          roster={roster}
          onPick={(p) => setFollowedPlayer({ id: p.id, name: p.name })}
          placeholder="Type your name or dojo…"
        />
      </div>
    );
  }

  // Followed-player state: header indicator + next-match details.
  const header = (
    <div style={{ display: "flex", alignItems: "center", gap: 6, marginBottom: 8 }}>
      <span style={{ fontSize: 12, color: "var(--ink-3)" }}>Following:</span>
      <span style={{ fontWeight: 600 }}>{followedPlayer.name || "(unknown)"}</span>
      {pRecord && pRecord.checkedIn && <span className="tag-badge" style={{ fontSize: 9 }}>✓ Checked in</span>}
      <button
        className="btn btn--ghost btn--sm btn--clear-follow"
        onClick={() => setFollowedPlayer(null)}
        aria-label="Stop following"
        style={{ marginLeft: 4 }}
      >
        ✕ Clear
      </button>
    </div>
  );

  if (!nextMatch) {
    return (
      <div className="card" data-testid="viewer-home-mymatch" style={{ marginBottom: 16, padding: 14 }}>
        {header}
        <div style={{ fontSize: 13, color: "var(--ink-3)" }}>All your matches are completed.</div>
      </div>
    );
  }

  const isOnSideA = isFollowedPlayer(nextMatch.sideA, followedPlayer);
  const opponent = isOnSideA ? nextMatch.sideB : nextMatch.sideA;
  // Use the full-text Aka/Shiro badge class (bc-color-badge) consistent with
  // bracket.jsx — the compact `tw-match__badge` variant is sized 14×14 for
  // single-letter labels and would clip "AKA"/"SHIRO".
  const myBadgeClass = isOnSideA ? "bc-color-badge--aka" : "bc-color-badge--shiro";
  const myBadgeLabel = isOnSideA ? "AKA" : "SHIRO";
  const oppBadgeClass = isOnSideA ? "bc-color-badge--shiro" : "bc-color-badge--aka";
  const oppBadgeLabel = isOnSideA ? "SHIRO" : "AKA";
  const phaseLabel = nextMatch.phase === "pool" ? poolLabel(nextMatch) : (nextMatch.round || "Bracket");
  // FR-025: queue position is 1-indexed per court for scheduled matches; 0 for
  // running/completed. Treat null/undefined/0 as "don't render" so we stay
  // gracefully empty for non-queued matches and pre-T046 responses. Wording
  // ("Next up" / "N before yours") mirrors VSchedItem below and display.jsx
  // so all three viewer surfaces agree. Running matches show null here
  // because the round label already appends " · NOW".
  const queueLabel = mymatchQueueLabel(nextMatch);
  const queueHighlight = queueLabel === "Next up";

  return (
    <div className="my-match" data-testid="viewer-home-mymatch" style={{ marginBottom: 16 }}>
      {header}
      <div className="my-match__lbl">Your next match</div>
      <div className="my-match__name">
        <span className={`bc-color-badge ${myBadgeClass}`}>{myBadgeLabel}</span>
        {followedPlayer.name}
      </div>
      <div className="my-match__round">
        {nextMatch.compName ? `${nextMatch.compName} · ` : ""}{phaseLabel}
        {nextMatch.status === "running" ? " · NOW" : ""}
      </div>
      <div className="my-match__row">
        <div className="my-match__chip">
          <span className="l">Court</span>
          <span className="v"><TermV name="shiaijo">Shiaijo</TermV> {nextMatch.court || "—"}</span>
        </div>
        <div className="my-match__chip">
          <span className="l">Time</span>
          <span className="v">{nextMatch.scheduledAt || "TBA"}</span>
        </div>
        {queueLabel && (
          <div
            className="my-match__chip"
            data-testid="my-match-queue"
            role="status"
            aria-live="polite"
            aria-atomic="true"
          >
            <span className="l">Queue</span>
            {/* The .my-match card background is var(--accent) (dark blue), so
                colouring text with var(--accent) renders unreadable. The chip
                inherits white from --accent-fg; emphasise the live/up-next
                state with full opacity + a Unicode bullet instead.
                Wrap the decorative bullet in aria-hidden to keep screen reader
                announcements clean and focused on the queue label text. */}
            <span className="v" style={{ opacity: queueHighlight ? 1 : 0.92 }}>
              {/* Decorative bullet glyph — hidden from screen readers so the
                  announcement is just the queue label text ("Next up" /
                  "1 before yours") without a spurious "bullet" prefix. */}
              {queueHighlight ? <span aria-hidden="true">{"• "}</span> : null}
              {queueLabel}
            </span>
          </div>
        )}
      </div>
      {opponent && (typeof opponent === "object") ? (
        <button
          className="my-match__opp"
          onClick={() => onMatchClick && onMatchClick(nextMatch)}
          style={{ color: "inherit" }}
        >
          <div className="l">
            <span className={`bc-color-badge ${oppBadgeClass}`}>{oppBadgeLabel}</span>
            vs Opponent
          </div>
          <div className="n">{opponent.name}</div>
          {opponent.dojo ? <div className="d">{opponent.dojo}</div> : null}
        </button>
      ) : null}
    </div>
  );
}

// WatchlistPanel — Watchlist management + "Watched matches" home section.
// Empty state hides the list; once at least one watched player exists,
// renders the chip list, an "Add another" picker, and (when applicable)
// the upcoming-matches preview.
// addDojoToWatchlist — pure helper extracted for testability.
// Given the current watchlist and a roster, return a new watchlist with every
// roster player from `dojo` added (dedup by id, cap at `max`). Players not
// matching the dojo (and any already in the list) are unchanged.
function addDojoToWatchlist(watchlist, roster, dojo, max) {
  if (!dojo) return { next: watchlist, added: 0, skipped: 0 };
  const have = new Set(watchlist.map((w) => w.id));
  const candidates = (roster || []).filter((p) => p && p.id && p.dojo === dojo && !have.has(p.id));
  const room = Math.max(0, max - watchlist.length);
  const added = candidates.slice(0, room);
  const skipped = candidates.length - added.length;
  return {
    next: [...watchlist, ...added.map((p) => ({ id: p.id, name: p.name, dojo: p.dojo || "" }))],
    added: added.length,
    skipped,
  };
}

function WatchlistPanel({ tournament, watchlist, setWatchlist, upcoming, onMatchClick }) {
  const [dojoSel, setDojoSel] = useState("");
  const [bulkMsg, setBulkMsg] = useState(null);
  const bulkMsgTimer = useRefV(null);
  React.useEffect(() => () => clearTimeout(bulkMsgTimer.current), []);
  const removeOne = (id) => setWatchlist(watchlist.filter((w) => w.id !== id));
  const addOne = (p) => {
    if (watchlist.find((w) => w.id === p.id)) return;
    setWatchlist([...watchlist, { id: p.id, name: p.name, dojo: p.dojo || "" }]);
  };
  const roster = useMemo(() => buildRoster(tournament.competitions), [tournament.competitions]);
  const rosterById = useMemo(() => new Map(roster.map(p => [p.id, p])), [roster]);

  // Unique sorted dojos from the roster, excluding empty values.
  const dojos = useMemo(() => {
    const set = new Set();
    roster.forEach((p) => { if (p.dojo) set.add(p.dojo); });
    return Array.from(set).sort();
  }, [roster]);

  // Per-dojo summary: total members + currently watched. Used to label the
  // dropdown options and to disable the "Add dojo" button when nothing new
  // would be added.
  const dojoStats = useMemo(() => {
    const have = new Set(watchlist.map((w) => w.id));
    const stats = new Map();
    roster.forEach((p) => {
      if (!p.dojo) return;
      const s = stats.get(p.dojo) || { total: 0, watched: 0 };
      s.total += 1;
      if (have.has(p.id)) s.watched += 1;
      stats.set(p.dojo, s);
    });
    return stats;
  }, [roster, watchlist]);

  const addDojo = () => {
    if (!dojoSel) return;
    const { next, added, skipped } = addDojoToWatchlist(watchlist, roster, dojoSel, WATCHLIST_MAX);
    setWatchlist(next);
    setBulkMsg(
      skipped > 0
        ? added === 0
          ? `Watchlist full · ${skipped} from ${dojoSel} skipped`
          : `Added ${added} from ${dojoSel} · ${skipped} skipped (watchlist full)`
        : added === 0
        ? `Everyone from ${dojoSel} is already in your watchlist`
        : `Added ${added} from ${dojoSel}`
    );
    setDojoSel("");
    // Auto-clear the toast after a few seconds so it doesn't linger.
    clearTimeout(bulkMsgTimer.current);
    bulkMsgTimer.current = setTimeout(() => setBulkMsg(null), 4000);
  };

  const selStats = dojoStats.get(dojoSel);
  const addDojoDisabled = watchlist.length >= WATCHLIST_MAX || !dojoSel || !selStats || selStats.watched >= selStats.total;

  return (
    <div className="card" data-testid="viewer-home-watchlist" style={{ marginBottom: 16, padding: 14 }}>
      <div className="section-title" style={{ marginTop: 0, display: "flex", alignItems: "center", gap: 8 }}>
        <span>Watchlist</span>
        {watchlist.length > 0 && (
          <span style={{ fontSize: 11, color: "var(--ink-3)", fontWeight: 500 }}>
            {pluralize(watchlist.length, "player")}
          </span>
        )}
      </div>
      {watchlist.length === 0 ? (
        <div style={{ fontSize: 12, color: "var(--ink-3)", marginBottom: 8 }}>
          Watching a coach's students or a few key competitors? Add up to {WATCHLIST_MAX} participants and we'll surface their upcoming matches.
        </div>
      ) : (
        <div className="pmf__bar" style={{ marginBottom: 8 }}>
          {watchlist.map((w) => {
            const pRecord = rosterById.get(w.id);
            return (
              <span key={w.id} className={`pmf__chip ${pRecord && pRecord.checkedIn ? "is-checked-in" : ""}`} title={pRecord && pRecord.checkedIn ? "Checked in" : undefined}>
                {w.name}
                {pRecord && pRecord.checkedIn && <span style={{ marginLeft: 4, fontSize: 10 }}>✓</span>}
                <button onClick={() => removeOne(w.id)} aria-label={`Remove ${w.name}`}>×</button>
              </span>
            );
          })}
        </div>
      )}
      <SinglePlayerPicker
        roster={roster}
        onPick={addOne}
        placeholder={watchlist.length === 0 ? "Add a participant to watch…" : "Add another participant…"}
        excludeIds={watchlist.map((w) => w.id)}
      />
      {dojos.length > 0 && (
        <div style={{ marginTop: 8, display: "flex", gap: 8, alignItems: "center", flexWrap: "wrap" }} data-testid="watchlist-dojo-picker">
          <label style={{ fontSize: 12, color: "var(--ink-3)" }} htmlFor="watchlist-dojo-select">Watch all from dojo</label>
          <select
            id="watchlist-dojo-select"
            value={dojoSel}
            onChange={(e) => setDojoSel(e.target.value)}
            style={{ fontSize: 13, padding: "4px 8px" }}
            data-testid="watchlist-dojo-select"
          >
            <option value="">— pick a dojo —</option>
            {dojos.map((d) => {
              const s = dojoStats.get(d) || { total: 0, watched: 0 };
              const remaining = s.total - s.watched;
              const label = remaining === 0
                ? `${d} (all ${s.total} watched)`
                : `${d} (+${remaining} of ${s.total})`;
              return <option key={d} value={d}>{label}</option>;
            })}
          </select>
          <button
            className="btn btn--sm"
            disabled={addDojoDisabled}
            onClick={addDojo}
            data-testid="watchlist-dojo-add"
          >
            Add dojo
          </button>
          {bulkMsg && <span style={{ fontSize: 11, color: "var(--ink-3)" }} role="status">{bulkMsg}</span>}
        </div>
      )}

      {upcoming.length > 0 && (
        <>
          <div className="section-title" style={{ marginTop: 14, fontSize: 13 }}>
            Watched matches · upcoming {upcoming.length}
          </div>
          <div className="vsched">
            {upcoming.map((m) => (
              <VSchedItem
                key={m.compId + m.id}
                m={m}
                tweaks={{ showDojo: true }}
                showCompetition
                onClick={() => onMatchClick && onMatchClick(m)}
              />
            ))}
          </div>
        </>
      )}
    </div>
  );
}

// DisplayModes — viewer-home section linking to the public /display routes.
// Collapsed by default inside a <details> element. Contains one "all-courts
// overview" link plus two compact inline rows (court displays, streaming
// overlays) each listing per-court links inline rather than one card per
// court. Each link opens in a new tab so the operator's viewer session stays
// open. Lives in viewer.jsx (not display.jsx) because it is a viewer-side
// surface that consumes the display routes rather than rendering them.
function DisplayModes({ tournament }) {
  const courts = (tournament && tournament.courts) || [];
  // No court list — render nothing rather than a confusing single "all"
  // card. The tournament setup flow guarantees ≥1 court in practice; this
  // guard exists for the very-early-bootstrap window before tournament
  // data has loaded fully.
  if (courts.length === 0) return null;
  return (
    <details style={{ marginTop: 20 }}>
      <summary className="section-title" style={{ cursor: "pointer" }}>Display modes</summary>
      <div className="vlist" data-testid="viewer-home-display-modes">
        <a
          className="vlist-item vlist-item--row"
          href="/display?court=all"
          target="_blank"
          rel="noopener noreferrer"
          style={{ textDecoration: "none" }}
        >
          <span className="vlist-item__icon">🪟</span>
          <div className="vlist-item__rowbody">
            <div className="vlist-item__rowtitle">All courts overview</div>
            <div className="vlist-item__rowsub">Lobby grid showing every court at a glance · opens in a new tab</div>
          </div>
          <span className="vlist-item__rowchev">→</span>
        </a>
        {/* Per-court links consolidated into compact inline rows. */}
        {[
          { icon: "📺", title: "Court displays", suffix: "" },
          { icon: "🎥", title: "Streaming overlays", suffix: "&overlay=1" },
        ].map((row) => (
          <div key={row.title} className="vlist-item vlist-item--row" style={{ cursor: "default" }}>
            <span className="vlist-item__icon">{row.icon}</span>
            <div className="vlist-item__rowbody">
              <div className="vlist-item__rowtitle">{row.title}</div>
              <div className="vlist-item__rowsub" style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
                {courts.map((cc, i) => (
                  <span key={cc}>
                    <a href={`/display?court=${encodeURIComponent(cc)}${row.suffix}`} target="_blank" rel="noopener noreferrer"
                      style={{ color: "var(--text-link, #2563eb)" }}>Shiaijo {cc}</a>
                    {i < courts.length - 1 && <span aria-hidden="true" style={{ color: "var(--text-3, #999)", marginLeft: 8 }}>·</span>}
                  </span>
                ))}
              </div>
            </div>
          </div>
        ))}
      </div>
    </details>
  );
}

// T192 (US13 — FR-050e): pure helpers for the Swiss standings viewer.
// Extracted so the conditional header / winner-detection logic is unit
// testable without mounting the component. Mirrors the
// admin_competition.jsx swiss helpers pattern.

// `comp.swissCurrentRound >= comp.swissRounds` is the precondition for
// "final standings"; we also require every match in the final round
// to be completed so a half-finished final round doesn't prematurely
// claim a winner.
function isSwissFinalStandings(comp, poolMatches) {
  if (!comp || comp.format !== "swiss") return false;
  const total = comp.swissRounds || 0;
  const current = comp.swissCurrentRound || 0;
  if (total < 1 || current < total) return false;
  const finalRoundPrefix = `Swiss-R${total}-`;
  const finalMatches = (poolMatches || []).filter(m => (m.id || "").startsWith(finalRoundPrefix));
  if (finalMatches.length === 0) return false;
  return finalMatches.every(m => m.status === "completed");
}

// Heading string for the standings table. "After round N" while
// the competition is in progress; "Final standings" once every
// configured round is in the books.
function swissStandingsHeading(comp, poolMatches) {
  if (isSwissFinalStandings(comp, poolMatches)) return "Final standings";
  const current = (comp && comp.swissCurrentRound) || 0;
  if (current === 0) return "Standings — pending";
  return `Standings after round ${current}`;
}

function WinnerBadge({ name, isFs = false, testId, marginBottom }) {
  return (
    <div
      className="winner-badge"
      data-testid={testId}
      style={{
        padding: isFs ? "14px 18px" : "10px 14px",
        background: "linear-gradient(135deg, var(--accent) 0%, var(--accent-2, var(--accent)) 100%)",
        color: "white",
        borderRadius: 8,
        fontWeight: 700,
        fontSize: isFs ? 18 : 14,
        display: "flex",
        alignItems: "center",
        gap: 8,
        marginBottom: marginBottom,
      }}
    >
      <span style={{ fontSize: isFs ? 28 : 18 }}>🏆</span>
      <span>Winner: {name}</span>
    </div>
  );
}

function SwissStandingsViewer({ competition, poolMatches, tweaks }) {
  const c = competition;
  const [standings, setStandings] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  // Re-fetch whenever the round counter advances (SSE-driven) so the
  // standings table reflects the latest cumulative state. Also depend
  // on poolMatches length so a fresh round's matches landing triggers
  // a refresh — the round may have completed even when swissCurrentRound
  // didn't move (final round).
  React.useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    window.API.swissStandings(c.id)
      .then(data => {
        if (cancelled) return;
        setStandings(Array.isArray(data) ? data : []);
        setLoading(false);
      })
      .catch(err => {
        if (cancelled) return;
        console.error("Failed to load Swiss standings", err);
        setError(err.message || "Failed to load standings");
        setLoading(false);
      });
    return () => { cancelled = true; };
  }, [c.id, c.swissCurrentRound, (poolMatches || []).length]);

  const isFinal = isSwissFinalStandings(c, poolMatches);
  const heading = swissStandingsHeading(c, poolMatches);
  const winner = isFinal && standings.length > 0 ? standings[0] : null;

  if (loading) return <div className="loading">Loading standings…</div>;
  if (error) return <div className="alert alert--error">{error}</div>;

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      {isFinal && winner && <WinnerBadge name={winner.player?.name || ""} />}
      <div className="pool" style={{ padding: 14 }}>
        <div className="pool__head">
          <div className="pool__name">{heading}</div>
          <div style={{ fontSize: 12, color: "var(--ink-3)" }}>
            Round {c.swissCurrentRound || 0} of {c.swissRounds || 0}
          </div>
        </div>
        <table className="pool__table">
          <thead>
            {/* Head-to-head is a tiebreaker between equal-wins-and-points */}
            {/* pairs; surfaced as the column label so the order is */}
            {/* explicit to viewers. The backend resolves head-to-head */}
            {/* into the stable rank value used for row order. */}
            <tr><th>#</th><th>Player</th><th className="num">W</th><th className="num">L</th><th className="num">D</th><th className="num">PW</th><th className="num">PL</th></tr>
          </thead>
          <tbody>
            {standings.length > 0 ? standings.map((s, i) => (
              <tr key={s.player?.id || s.player?.name || i}>
                <td style={{ color: s.isOverridden ? "var(--accent)" : "var(--ink-3)", fontFamily: "var(--font-mono)", fontWeight: s.isOverridden ? 700 : 400 }}>{i + 1}{s.isOverridden ? "*" : ""}</td>
                <td>
                  <div style={{ fontWeight: 500 }}>{s.player?.name || ""}</div>
                  {tweaks?.showDojo ? <div style={{ fontSize: 11, color: "var(--ink-3)" }}>{s.player?.dojo || ""}</div> : null}
                </td>
                <td className="num">{s.wins || 0}</td>
                <td className="num">{s.losses || 0}</td>
                <td className="num">{s.draws || 0}</td>
                <td className="num">{s.ipponsGiven || 0}</td>
                <td className="num">{s.ipponsTaken || 0}</td>
              </tr>
            )) : (
              <tr><td colSpan={7} style={{ textAlign: "center", color: "var(--ink-3)", fontSize: 13, padding: 16 }}>No matches scored yet.</td></tr>
            )}
          </tbody>
        </table>
        {standings.length > 0 && (
          <div className="pool-matrix__legend" style={{ marginTop: 8, fontSize: 11, color: "var(--ink-3)" }}>
            Ranked by: wins → points scored (PW) → head-to-head.
          </div>
        )}
      </div>
    </div>
  );
}

function ViewerCompetition({ tournament, competition, pools, poolMatches, standings, bracket, onBack, authed, onEditCompetition, tweaks }) {
  const [tab, setTab] = useState("overview");
  const c = competition;

  const allMatches = useMemo(() => {
    const out = [];
    if (pools) {
        pools.forEach((p) => {
            const matches = poolMatches ? poolMatches.filter(m => m.id.startsWith(p.poolName + "-")) : [];
            matches.forEach((m) => {
                const isDH = isPoolDaihyosenID(m.id || "");
                out.push({ ...m, phase: "pool", phaseName: p.poolName, poolName: p.poolName, compFormat: c.format, compName: c.name, compKind: isDH ? "" : c.kind, teamSize: isDH ? 0 : c.teamSize });
            });
        });
    }
    // mp-9dz/mp-8jbo: a preview bracket (bracket.preview === true) on a mixed
    // (Pools + Knockout) competition carries pool-origin TBD placeholders
    // ("Pool A-1st", "Pool B-2nd", …). These must NOT flow into allMatches
    // because allMatches feeds Up next / upcoming / recent / watchlist, and
    // spectators would see meaningless placeholder entries. The Bracket tab
    // renders `bracket`/`derivedBracket` directly and is NOT affected.
    if (bracket && bracket.rounds && !bracket.preview) {
        bracket.rounds.forEach((round, ri) => {
            round.forEach((m) => out.push({ ...m, phase: "bracket", round: window.roundLabel(ri, bracket.rounds.length), phaseName: window.roundLabel(ri, bracket.rounds.length), roundIndex: ri, compKind: c.kind, teamSize: c.teamSize }));
        });
    }
    return out;
  }, [pools, poolMatches, bracket, c.kind, c.teamSize]);

  const [followedPlayer] = useFollowedPlayer();
  const [watchlist] = useWatchlist();

  const watchedIds = useMemo(() => {
    const ids = new Set();
    if (followedPlayer && followedPlayer.id) ids.add(String(followedPlayer.id));
    (watchlist || []).forEach(w => { if (w.id) ids.add(String(w.id)); });
    return ids;
  }, [followedPlayer, watchlist]);

  const followedName = followedPlayer && followedPlayer.name ? followedPlayer.name.trim().toLowerCase() : "";
  const hasActiveFilter = watchedIds.size > 0 || !!followedName;

  const myPlayer = followedPlayer;
  const myUpcoming = useMemo(() => buildFollowedNextMatch(followedPlayer, allMatches), [followedPlayer, allMatches]);

  const { liveMatches, upcomingMatches, recentMatches } = useMemo(() => {
    const matchInvolvesWatched = (m) => {
      if (!hasActiveFilter) return true;
      const [aId, bId] = matchParticipantIds(m);
      if ((aId && watchedIds.has(aId)) || (bId && watchedIds.has(bId))) return true;
      if (followedName) {
        const [aName, bName] = matchParticipantNames(m);
        const aN = aName ? aName.trim().toLowerCase() : "";
        const bN = bName ? bName.trim().toLowerCase() : "";
        if (aN === followedName || bN === followedName) return true;
      }
      return false;
    };
    const live = allMatches.filter((m) => m.status === "running" && hasBothSides(m) && matchInvolvesWatched(m));
    const upcoming = allMatches.filter((m) => m.status === "scheduled" && hasBothSides(m) && matchInvolvesWatched(m))
      .sort((a, b) => (a.scheduledAt || "99:99").localeCompare(b.scheduledAt || "99:99"))
      .slice(0, hasActiveFilter ? 20 : 3);
    // Reverse-chronological by scheduled time. allMatches arrives in
    // pool-then-bracket order (not time order), so a bare slice(-N).reverse()
    // produced a jumbled "Recent results" list — sort by scheduledAt desc,
    // then take the most recent N. Missing times sort last (oldest).
    const recent = allMatches
      .filter((m) => m.status === "completed" && m.winner && matchInvolvesWatched(m))
      .sort((a, b) => (b.scheduledAt || "00:00").localeCompare(a.scheduledAt || "00:00"))
      .slice(0, hasActiveFilter ? 20 : 5);
    return { liveMatches: live, upcomingMatches: upcoming, recentMatches: recent };
  }, [allMatches, watchedIds, hasActiveFilter, followedName]);

  const filterLabel = useMemo(() => {
    if (!hasActiveFilter) return null;
    const name = (followedPlayer && followedPlayer.name && followedPlayer.name.trim()) || null;
    const followedId = followedPlayer && followedPlayer.id;
    const wl = followedId ? watchedIds.size - 1 : watchedIds.size;
    if (name && wl <= 0) return name;
    if (!name && wl > 0) return `${wl} watched`;
    if (name && wl > 0) return `${name} + ${wl} watched`;
    return "filtered";
  }, [followedPlayer, watchedIds, hasActiveFilter]);

  // A mixed competition always carries a real bracket payload from the server
  // (pool-origin placeholder leaves like "Pool A-1st" while pools are running,
  // each replaced by the real finisher as that pool completes). No need for a
  // client-side placeholder fallback — just use what the server sends.
  const derivedBracket = useMemo(() => {
    if (bracket && bracket.rounds && bracket.rounds.length > 0) return bracket;
    return null;
  }, [bracket]);

  // draw-ready is NOT pre-start for the purposes of showing pool/bracket
  // structure — the draw has been generated and the payload already includes
  // pools + bracket data returned unconditionally by handlers_viewer.go.
  // Only setup (no draw yet) is treated as pre-start.
  const isPreStart = !c.status || c.status === "setup";
  const hasPools = !isPreStart && !!pools && pools.length > 0;
  const hasBracket = !isPreStart && !!derivedBracket && derivedBracket.rounds && derivedBracket.rounds.length > 0;
  // T192 (FR-050e): Swiss competitions surface a dedicated standings
  // tab in place of pools/bracket. The standings tab fetches its own
  // data via /swiss/standings (it's not part of the competition-detail
  // payload — see api_client.jsx).
  const isSwiss = c.format === "swiss";
  const isLeague = c.format === "league";
  const tabs = [
    { id: "overview", label: "Overview" },
    isSwiss ? { id: "swiss", label: "Standings" } : null,
    hasPools && !isSwiss ? { id: "pools", label: isLeague ? "League" : "Pools" } : null,
    hasBracket && !isSwiss ? { id: "bracket", label: "Bracket" } : null,
    c.status === "completed" ? { id: "results", label: "Awards" } : null,
  ].filter(Boolean);

  const currentMatch = useMemo(() => {
    if (liveMatches.length > 0) return liveMatches[0];
    return upcomingMatches[0] || null;
  }, [liveMatches, upcomingMatches]);

  const [bracketScrollTarget, setBracketScrollTarget] = useState(null);
  const bracketScrollRef = useRefV(null);
  const [selectedMatch, setSelectedMatch] = useState(null);

  React.useEffect(() => {
    if (tab === "bracket" && currentMatch) {
      setBracketScrollTarget(currentMatch.id + "::" + Date.now());
    }
  }, [tab, currentMatch?.id]);

  return (
    <div className="viewer">
      <div className="viewer__shell">
        <div className="viewer__head">
          <button className="viewer__back" onClick={onBack} aria-label="Back">←</button>
          <div className="viewer__title-block">
            <div className="viewer__eyebrow">
              {c.date && <span style={{ fontWeight: 600 }}>{formatDate(c.date)}</span>}
              {c.date && c.startTime && " at "}
              {c.startTime} · {c.courts.join(", ")}
            </div>
            <div className="viewer__title">{c.name}</div>
            <div className="viewer__sub">{competitionKindLabel(c)}</div>
          </div>
          <StatusBadge status={c.status} showLiveDot format={c.format} />
          {authed && onEditCompetition && (
            <button className="viewer__admin-pill" onClick={() => onEditCompetition(c.id)}>✎ Edit</button>
          )}
        </div>
        <div className="viewer__tabs">
          {tabs.map((tb) => (
            <button key={tb.id} className={`viewer__tab ${tab === tb.id ? "is-active" : ""}`} onClick={() => setTab(tb.id)}>
              {tb.label}
            </button>
          ))}
        </div>
        <div className="viewer__body">
          {tab === "overview" && (
            <ViewerOverview
              c={c}
              myPlayer={myPlayer}
              myUpcoming={myUpcoming}
              currentMatch={currentMatch}
              liveMatches={liveMatches}
              upcomingMatches={upcomingMatches}
              recentMatches={recentMatches}
              tweaks={tweaks}
              tournament={tournament}
              compId={c.id}
              standings={standings}
              pools={pools}
              poolMatches={poolMatches}
              onSwitchTab={setTab}
              hasActiveFilter={hasActiveFilter}
              filterLabel={filterLabel}
              highlightPlayer={followedPlayer}
            />
          )}
          {tab === "bracket" && derivedBracket && (
            <div style={{ marginLeft: -16, marginRight: -16 }}>
              <div ref={bracketScrollRef} className="bracket-canvas" style={{ borderRadius: 0, borderLeft: 0, borderRight: 0 }}>
                <div className="bracket-canvas__inner" style={{ padding: 18 }}>
                  <window.BracketTree
                    rounds={derivedBracket.rounds}
                    variant={tweaks.cardVariant}
                    showDojo={tweaks.showDojo}
                    highlightedMatchId={currentMatch?.id}
                    autoScrollMatchId={bracketScrollTarget}
                    scrollContainerRef={bracketScrollRef}
                    highlightPlayer={followedPlayer}
                    onMatchClick={(m, ri) => {
                      const label = window.roundLabel(ri, derivedBracket.rounds.length);
                      setSelectedMatch({ ...m, phase: "bracket", round: label, phaseName: label, compKind: c.kind, teamSize: c.teamSize });
                    }}
                  />
                </div>
              </div>
            </div>
          )}
          {tab === "pools" && hasPools && (
            <PoolsViewer pools={pools} standings={standings} poolMatches={poolMatches} tweaks={tweaks} competition={c} onMatchClick={setSelectedMatch} highlightPlayer={followedPlayer} />
          )}
          {tab === "swiss" && isSwiss && (
            <SwissStandingsViewer competition={c} poolMatches={poolMatches} tweaks={tweaks} />
          )}
          {tab === "results" && c.status === "completed" && (
            // Pass the *real* server bracket (not derivedBracket) — the latter
            // is a TBD placeholder for visualization only and carries no
            // winner data. Using real server data ensures deriveAwards sees
            // actual winners; when the final has no winner yet, deriveAwards
            // explicitly falls through to the standings-based path rather
            // than short-circuiting.
            <AwardsView c={c} bracket={bracket} standings={standings} pools={pools} players={c.players} />
          )}
          {window.VersionFooter && <window.VersionFooter />}
        </div>
      </div>
      {selectedMatch && <MatchViewerModal match={selectedMatch} onClose={() => setSelectedMatch(null)} tournament={tournament} compId={c.id} />}
    </div>
  );
}

// Inline match detail card — shown directly on the page (no modal needed).
function MatchDetailCard({ match, onClose }) {
  if (!match) return null;
  const isTeam = match.compKind === "team" || match.teamSize > 0;
  const teamSize = match.teamSize || 0;
  const aName = match.sideA?.name || "TBD";
  const bName = match.sideB?.name || "TBD";
  const aWin = match.winner?.id === match.sideA?.id && match.winner?.id;
  const bWin = match.winner?.id === match.sideB?.id && match.winner?.id;
  const isLive = match.status === "running";
  const isDone = match.status === "completed";

  // mp-13y: fetch per-match lineups for team matches so bout rows show
  // competitor names instead of bout numbers.
  const { lineupA, lineupB } = useTeamLineups(isTeam ? match : null, undefined, isTeam ? match.roundIndex : undefined);
  // Show the Daihyosen row when a rep-bout subResult exists (position -1);
  // TeamScoreboard additionally gates it on the match actually being tied.
  const showDH = isTeam && (match.subResults || []).some(s => s.position === -1);

  return (
    <div className="match-detail-card">
      <div className="match-detail-card__head">
        <div className="match-detail-card__meta">
          <span><TermV name="shiaijo">Shiaijo</TermV> {match.court}</span>
          <span>·</span>
          <span>{match.phase === "pool" ? poolLabel(match) : (match.round || "")}</span>
          {match.scheduledAt && <><span>·</span><span>{match.scheduledAt}</span></>}
        </div>
        <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
          {isLive && <span className="bc-live">● NOW</span>}
          {isDone && <span style={{ fontSize: 11, color: "var(--ink-3)", fontWeight: 600 }}>FINAL</span>}
          {onClose && <button className="match-detail-card__close" onClick={onClose} aria-label="Close">×</button>}
        </div>
      </div>

      {/* Individual matches keep the SHIRO/AKA player header (there is no
          summary row to carry the names); team matches put the team names in
          the scoreboard's summary row instead (mp-13y #2). */}
      {!isTeam && (
        <div className="match-detail-card__players">
          <div className={`match-detail-card__side ${bWin ? "match-detail-card__side--win" : ""}`}>
            <span className="match-detail-card__color-badge match-detail-card__color-badge--shiro">SHIRO</span>
            <span className="match-detail-card__name">{bName}</span>
          </div>
          <div className="match-detail-card__score"><span className="match-detail-card__vs">vs</span></div>
          <div className={`match-detail-card__side match-detail-card__side--right ${aWin ? "match-detail-card__side--win" : ""}`}>
            <span className="match-detail-card__name">{aName}</span>
            <span className="match-detail-card__color-badge match-detail-card__color-badge--aka">AKA</span>
          </div>
        </div>
      )}

      {/* mp-13y: the ONE shared FIK scoreboard (match_scoreboard.jsx) — same
          component the TV display uses. Team → team-name + IV/PW summary row +
          per-bout rows (numbered when not yet started) + Daihyosen (tie only);
          individual → ippon-letter slots. */}
      {isTeam
        ? <TeamScoreboard subResults={match.subResults || []} lineupA={lineupA} lineupB={lineupB}
            teamSize={teamSize} showDH={showDH} variant="card" shiroName={bName} akaName={aName} />
        : (isDone || isLive) && <IndividualScore match={match} variant="card" />}
    </div>
  );
}

function ViewerOverview({ c, myPlayer, myUpcoming, currentMatch, liveMatches, upcomingMatches, recentMatches, tweaks, tournament, compId, standings, pools, poolMatches, onSwitchTab, hasActiveFilter, filterLabel, highlightPlayer }) {
  const [expandedMatchId, setExpandedMatchId] = useState(null);
  const [selectedMatch, setSelectedMatch] = useState(null);
  const isSelfRun = tournament && tournament.mode === "self-run";

  const isLeague = c.format === "league";
  const isTeam = c.kind === "team" || c.teamSize > 0;
  const leaguePoolName = isLeague && pools && pools[0] ? pools[0].poolName : null;
  const leagueStandings = leaguePoolName && standings ? (standings[leaguePoolName] || []) : [];
  const allMatchesComplete = isLeague && (() => {
    const all = poolMatches || [];
    return all.length > 0 && all.every(m => m.status === "completed");
  })();
  const leagueWinner = allMatchesComplete && leagueStandings.length > 0 ? leagueStandings[0] : null;

  // setup: no draw yet — plain "not started" message.
  if (!c.status || c.status === "setup") {
    return (
      <div className="empty" style={{ padding: 32 }}>
        <div className="icon">⏳</div>
        <h3>Not started yet</h3>
        <div style={{ fontSize: 13 }}>Starts at {c.startTime}. Check back when the competition begins.</div>
      </div>
    );
  }

  // draw-ready: the draw is published but no match has been called yet.
  // The comp is not live, so the Overview has no live/recent matches to
  // show — point spectators to the now-available tabs. Swiss comps render a
  // Standings tab instead of Pools/Bracket (same isSwiss signal as the tab
  // logic in ViewerCompetition), so the pointer text must match.
  if (c.status === "draw-ready") {
    const isSwiss = c.format === "swiss";
    return (
      <div className="empty" style={{ padding: 32 }}>
        <div className="icon">📋</div>
        <h3>Draw is ready</h3>
        <div style={{ fontSize: 13 }}>
          Starts at {c.startTime}. {isSwiss
            ? "Check the Standings tab to follow the rounds."
            : isLeague
            ? "Browse the League tab to see the draw."
            : "Browse the Pools and Bracket tabs to see the draw."}
        </div>
      </div>
    );
  }

  const handleMatchClick = (m) => {
    if (isSelfRun) {
      setSelectedMatch(m);
    } else {
      setExpandedMatchId(prev => prev === m.id ? null : m.id);
    }
  };

  return (
    <div>
      {hasActiveFilter && (
        <div style={{
          display: "flex", alignItems: "center", gap: 8,
          padding: "8px 12px", marginBottom: 12,
          background: "var(--accent-soft, #eef)",
          borderRadius: 8, fontSize: 13
        }}>
          <span aria-hidden="true" style={{ color: "var(--accent, #36c)" }}>👤</span>
          <span>Showing matches for <strong>{filterLabel}</strong></span>
        </div>
      )}
      {myUpcoming && myPlayer ? (
        <div className="my-match">
          <div className="my-match__lbl">Your next match</div>
          <div className="my-match__name">{myPlayer.name}</div>
          <div className="my-match__round">
            {myUpcoming.phase === "pool" ? poolLabel(myUpcoming) : myUpcoming.round}
            {myUpcoming.status === "running" ? " · NOW" : ""}
          </div>
          <div className="my-match__row">
            <div className="my-match__chip">
              <span className="l">Court</span>
              <span className="v"><TermV name="shiaijo">Shiaijo</TermV> {myUpcoming.court}</span>
            </div>
            <div className="my-match__chip">
              <span className="l">Time</span>
              <span className="v">{myUpcoming.scheduledAt || "TBA"}</span>
            </div>
          </div>
          {(() => {
            const opp = isFollowedPlayer(myUpcoming.sideA, myPlayer) ? myUpcoming.sideB : myUpcoming.sideA;
            return opp ? (
              <div className="my-match__opp">
                <div className="l">vs Opponent</div>
                <div className="n">{opp.name}</div>
                {tweaks.showDojo ? <div className="d">{opp.dojo}</div> : null}
              </div>
            ) : null;
          })()}
        </div>
      ) : null}

      {/* League standings summary (mp-ldnr) */}
      {isLeague && leagueStandings.length > 0 && (c.status === "pools" || c.status === "playoffs" || c.status === "completed") && (
        <div className="pool" style={{ padding: 14, marginBottom: 12 }} data-testid="league-overview-standings">
          {leagueWinner && <WinnerBadge name={leagueWinner.player?.name || ""} testId="league-overview-winner" marginBottom={12} />}
          <div className="pool__head">
            <div className="pool__name">{allMatchesComplete ? "Final standings" : "Standings"}</div>
            {poolMatches && (
              <div style={{ fontSize: 12, color: "var(--ink-3)" }}>
                {poolMatches.filter(m => m.status === "completed").length}/{poolMatches.length} matches
              </div>
            )}
          </div>
          <table className="pool__table">
            <thead>
              {isTeam ? (
                <tr><th>#</th><th>Team</th><th className="num">W</th><th className="num">L</th><th className="num">T</th><th className="num">IV</th><th className="num">IL</th><th className="num">PW</th><th className="num">PL</th></tr>
              ) : (
                <tr><th>#</th><th>Player</th><th className="num">W</th><th className="num">L</th><th className="num">D</th><th className="num">PW</th><th className="num">PL</th></tr>
              )}
            </thead>
            <tbody>
              {leagueStandings.slice(0, 5).map((s, i) => (
                <tr key={s.player?.id || s.player?.name || i}>
                  <td style={{ color: s.isOverridden ? "var(--accent)" : "var(--ink-3)", fontFamily: "var(--font-mono)", fontWeight: s.isOverridden ? 700 : 400 }}>{i + 1}{s.isOverridden ? "*" : ""}</td>
                  <td>
                    <div style={{ fontWeight: 500 }}>
                      {s.player?.number ? <span className="num-prefix">{s.player.number}</span> : null}
                      {s.player?.name || ""}
                    </div>
                    {tweaks?.showDojo ? <div style={{ fontSize: 11, color: "var(--ink-3)" }}>{s.player?.dojo || ""}</div> : null}
                  </td>
                  <td className="num">{s.wins || 0}</td>
                  <td className="num">{s.losses || 0}</td>
                  <td className="num">{s.draws || 0}</td>
                  {isTeam && <td className="num">{s.individualWins || 0}</td>}
                  {isTeam && <td className="num">{s.individualLosses || 0}</td>}
                  <td className="num">{isTeam ? (s.pointsWon || 0) : (s.ipponsGiven || 0)}</td>
                  <td className="num">{isTeam ? (s.pointsLost || 0) : (s.ipponsTaken || 0)}</td>
                </tr>
              ))}
            </tbody>
          </table>
          {leagueStandings.length > 5 && (
            <div style={{ marginTop: 8, fontSize: 11, color: "var(--ink-3)" }}>
              Showing top 5 of {leagueStandings.length}
            </div>
          )}
          {onSwitchTab && (
            <button
              className="btn btn--link"
              style={{ marginTop: 8, fontSize: 13, padding: 0 }}
              onClick={() => onSwitchTab("pools")}
              data-testid="league-overview-view-all"
            >
              View full standings →
            </button>
          )}
        </div>
      )}

      {/* Current match — shown inline, before Up Next */}
      {currentMatch && currentMatch.status === "running" && (
        <div
          style={{ marginBottom: 12, cursor: isSelfRun ? "pointer" : undefined }}
          role={isSelfRun ? "button" : undefined}
          aria-label={isSelfRun ? "View current match details" : undefined}
          tabIndex={isSelfRun ? 0 : undefined}
          onClick={isSelfRun ? () => handleMatchClick(currentMatch) : undefined}
          onKeyDown={isSelfRun ? (e) => {
            if (e.key === "Enter" || e.key === " ") {
              e.preventDefault();
              handleMatchClick(currentMatch);
            }
          } : undefined}
        >
          <div className="section-title" style={{ display: "flex", alignItems: "center", gap: 6 }}>
            <span className="dot dot--live"></span> ON NOW
          </div>
          <MatchDetailCard match={currentMatch} />
        </div>
      )}

      {/* Live matches beyond the single current match */}
      {liveMatches.length > 1 && (
        <>
          <div className="section-title" style={{ display: "flex", alignItems: "center", gap: 6 }}>
            <span className="dot dot--live"></span> NOW · {liveMatches.length}
          </div>
          <div className="vsched">
            {liveMatches.filter(m => !currentMatch || m.id !== currentMatch.id).map((m) => (
              <React.Fragment key={m.id}>
                <VSchedItem m={m} tweaks={tweaks} onClick={() => handleMatchClick(m)} highlight={isFollowedPlayer(m.sideA, highlightPlayer) || isFollowedPlayer(m.sideB, highlightPlayer)} />
                {!isSelfRun && expandedMatchId === m.id && <MatchDetailCard match={m} onClose={() => setExpandedMatchId(null)} />}
              </React.Fragment>
            ))}
          </div>
        </>
      )}

      {/* Up next */}
      {upcomingMatches.length > 0 && (
        <>
          <div className="section-title">Up next · {upcomingMatches.length}</div>
          <div className="vsched">
            {upcomingMatches.map((m) => (
              <React.Fragment key={m.id}>
                <VSchedItem m={m} tweaks={tweaks} onClick={() => handleMatchClick(m)} highlight={isFollowedPlayer(m.sideA, highlightPlayer) || isFollowedPlayer(m.sideB, highlightPlayer)} />
                {!isSelfRun && expandedMatchId === m.id && <MatchDetailCard match={m} onClose={() => setExpandedMatchId(null)} />}
              </React.Fragment>
            ))}
          </div>
        </>
      )}

      {/* No live or upcoming */}
      {!currentMatch && upcomingMatches.length === 0 && liveMatches.length === 0 && (
        <div className="empty" style={{ padding: 20 }}><h3>Nothing scheduled</h3></div>
      )}

      {recentMatches.length > 0 && (
        <>
          <div className="section-title">Recent results</div>
          <div className="vsched">
            {recentMatches.map((m) => (
              <React.Fragment key={m.id}>
                <VSchedItem m={m} tweaks={tweaks} onClick={() => handleMatchClick(m)} highlight={isFollowedPlayer(m.sideA, highlightPlayer) || isFollowedPlayer(m.sideB, highlightPlayer)} />
                {!isSelfRun && expandedMatchId === m.id && <MatchDetailCard match={m} onClose={() => setExpandedMatchId(null)} />}
              </React.Fragment>
            ))}
          </div>
        </>
      )}
      {isSelfRun && selectedMatch && <MatchViewerModal match={selectedMatch} onClose={() => setSelectedMatch(null)} tournament={tournament} compId={compId} />}
    </div>
  );
}

const VSchedItem = React.memo(({ m, tweaks, showCompetition, onClick, highlight }) => {
  const aWin = m.winner && m.sideA && m.winner.id === m.sideA.id;
  const bWin = m.winner && m.sideB && m.winner.id === m.sideB.id;
  // Bracket matches carry scoreA/scoreB strings rather than ipponsA/B arrays.
  // Fall back so the score string reflects per-side letters instead of the
  // orientation-agnostic winnerPts–loserPts that formatIpponsScore uses when
  // both ippon arrays are absent (which would invert left/right when AKA wins).
  const vIpponsA = m.ipponsA || window.ipponsFromScore(m.scoreA);
  const vIpponsB = m.ipponsB || window.ipponsFromScore(m.scoreB);
  const scoreStr = m.status === "completed" ? window.matchScoreStr(m, vIpponsB, vIpponsA) : null;
  // FR-025: queue position is 1-indexed per court for scheduled matches;
  // running/completed are 0 (set server-side, omitempty in JSON → undefined
  // on older payloads). Treat null/undefined/0 as "don't render" so the UI
  // stays gracefully empty for non-queued matches and pre-T046 responses.
  // Wording is owned by display.jsx::queueLabel (bead mp-e3k) so every
  // viewer surface stays in sync; we still gate on scheduled+qp>0 here
  // because this row already renders ●LIVE / Final on the right for
  // running/completed and we don't want the fallback "Scheduled hh:mm".
  const qp = Number(m.queuePosition);
  // Use the NEUTRAL court-queue label ("Next up" / "#N") here, not the
  // "N before yours" wording — VSchedItem renders on the general schedule and
  // home lists where no player is selected, so "yours" is meaningless. The
  // "…before yours" phrasing is reserved for the followed-player next-match
  // banner (mymatchQueueLabel), which has a real "you" context.
  const queueLabel = (m.status === "scheduled" && Number.isFinite(qp) && qp > 0)
    ? (window.queueLabelCompact ? window.queueLabelCompact(m) : _localQueueLabelCompact(m))
    : null;
  return (
    <button className={`vsched-item ${m.status === "running" ? "vsched-item--live" : ""} ${highlight ? "vsched-item--me" : ""}`} onClick={onClick} style={{ textAlign: "left", width: "100%", border: "none", background: "none", cursor: onClick ? "pointer" : "default" }}>
      <div className="vsched-item__head">
        <span className="vsched-item__time">{m.scheduledAt || "—"}</span>
        <span className="vsched-item__court">SHIAIJO {m.court}</span>
        {showCompetition && m.compName ? <span>· {m.compName}</span> : null}
        {m.phase === "pool" ? <span>· {poolLabel(m)}</span> : <span>· {m.round || ""}</span>}
        {m.round != null && typeof m.round === "number" && m.round >= 0 && (
          <span className="tw-match__round">R{m.round + 1}</span>
        )}
        {queueLabel && (
          <span className="vsched-item__queue" style={{ marginLeft: "auto", fontSize: 11, fontWeight: 700, color: qp === 1 ? "var(--accent)" : "var(--ink-3)" }}>
            {queueLabel}
          </span>
        )}
        {m.status === "running" && <span className="bc-live" style={{ marginLeft: "auto" }}>● NOW</span>}
        {m.status === "completed" && <span style={{ marginLeft: "auto", color: "var(--ink-3)" }}>Final</span>}
        {m.status === "completed" && m.decidedByHantei && (
          <span className="vsched-item__hantei" data-testid="vsched-hantei" style={{ marginLeft: 6, fontSize: 10, fontWeight: 700, padding: "1px 5px", borderRadius: 3, background: "var(--accent-soft, #eef)", color: "var(--accent, #36c)" }}>
            HANTEI
          </span>
        )}
      </div>
      <div className="vsched-item__players">
        <div className={`vsched-item__side ${bWin ? "vsched-item__side--w" : ""}`} style={{ textAlign: "right" }}>
          <span className="n">{m.sideB?.name || "TBD"}</span>
          {tweaks.showDojo && m.sideB?.dojo ? <span className="d">{m.sideB.dojo}</span> : null}
          <span className="vsched-item__color-badge vsched-item__color-badge--shiro">SHIRO</span>
        </div>
        {m.status === "completed" && scoreStr ? (
          <span className="vsched-item__score">{scoreStr}</span>
        ) : m.status === "completed" ? (
          <span className="vsched-item__vs">—</span>
        ) : (
          <span className="vsched-item__vs">vs</span>
        )}
        <div className={`vsched-item__side ${aWin ? "vsched-item__side--w" : ""}`}>
          <span className="vsched-item__color-badge vsched-item__color-badge--aka">AKA</span>
          <span className="n">{m.sideA?.name || "TBD"}</span>
          {tweaks.showDojo && m.sideA?.dojo ? <span className="d">{m.sideA.dojo}</span> : null}
        </div>
      </div>
    </button>
  );
});
VSchedItem.displayName = "VSchedItem";

const PoolMatchRow = React.memo(({ m, onClick }) => {
  const aName = typeof m.sideA === "object" ? m.sideA?.name : m.sideA;
  const bName = typeof m.sideB === "object" ? m.sideB?.name : m.sideB;
  const winnerName = typeof m.winner === "object" ? m.winner?.name : m.winner;
  const aWin = winnerName && winnerName === aName;
  const bWin = winnerName && winnerName === bName;

  const scoreStr = m.status === "completed"
    ? window.matchScoreStr(m, m.ipponsB, m.ipponsA)
    : null;

  return (
    <button className="pool-match-row" onClick={onClick} style={{ textAlign: "left", width: "100%", border: "none", background: "none", cursor: onClick ? "pointer" : "default" }}>
      <div className={`pool-match-row__side pool-match-row__side--right ${bWin ? "pool-match-row__side--win" : ""}`}>
        <span className="pool-match-row__name">{bName}</span>
        <span className="pool-match-row__badge pool-match-row__badge--shiro">SHIRO</span>
      </div>
      <span className="pool-match-row__score">
        {m.status === "completed" ? (
          scoreStr || "—"
        ) : m.status === "running" ? (
          <span className="bc-live" style={{ fontSize: 10 }}>●</span>
        ) : "–"}
      </span>
      <div className={`pool-match-row__side ${aWin ? "pool-match-row__side--win" : ""}`}>
        <span className="pool-match-row__badge pool-match-row__badge--aka">AKA</span>
        <span className="pool-match-row__name">{aName}</span>
      </div>
    </button>
  );
});
PoolMatchRow.displayName = "PoolMatchRow";

// Round-robin matrix for a single pool. Each off-diagonal cell shows the row
// player's result (W/L/X) against the column player; diagonal cells are self.
function PoolMatrix({ pool, matches, tweaks, onMatchClick, highlightPlayer }) {
  const players = pool.players || [];
  if (players.length < 2) return null;

  const isHighlighted = (p) => isFollowedPlayer(p, highlightPlayer);
  const matchMap = {};
  matches.forEach(m => {
    const aName = typeof m.sideA === "object" ? m.sideA?.name : m.sideA;
    const bName = typeof m.sideB === "object" ? m.sideB?.name : m.sideB;
    if (aName && bName) {
      matchMap[`${aName}||${bName}`] = m;
      matchMap[`${bName}||${aName}`] = m;
    }
  });

  const shortName = (p) => {
    const n = p.name || "";
    const parts = n.split(" ");
    return parts.length > 1 ? parts[0][0] + ". " + parts.slice(1).join(" ") : n;
  };

  const enrichMatch = (m) => ({ ...m, phase: "pool", poolName: pool.poolName, phaseName: pool.poolName, compFormat: m.compFormat, compName: m.compName, compKind: "", teamSize: 0 });

  const handleCellClick = (m) => { if (onMatchClick) onMatchClick(enrichMatch(m)); };

  const handleCellKeyDown = (e, m) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); handleCellClick(m); } };

  const cellLabel = (rowPlayer, colPlayer, result) => `Match: ${rowPlayer.name} vs ${colPlayer.name} — ${result}`;

  return (
    <div className="pool-matrix-wrap">
      <table className="pool-matrix">
        <thead>
          <tr>
            <th className="pool-matrix__corner"></th>
            {players.map((p) => (
              <th key={p.name} scope="col" aria-label={p.name} className={`pool-matrix__col-head${isHighlighted(p) ? " pool-matrix__col--me" : ""}`} title={p.name}>{p.number || ""}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {players.map((rowPlayer, ri) => (
            <tr key={rowPlayer.name} className={isHighlighted(rowPlayer) ? "pool-matrix__row--me" : ""}>
              <td className="pool-matrix__row-head" title={rowPlayer.name}>
                {rowPlayer.number ? <span className="pool-matrix__num">{rowPlayer.number}</span> : null}
                <span className="pool-matrix__pname">{tweaks.showDojo ? rowPlayer.name : shortName(rowPlayer)}</span>
              </td>
              {players.map((colPlayer, ci) => {
                const colMe = isHighlighted(colPlayer) ? " pool-matrix__col--me" : "";
                if (ri === ci) return <td key={colPlayer.name} className={`pool-matrix__cell pool-matrix__cell--self${colMe}`}>&mdash;</td>;
                const m = matchMap[`${rowPlayer.name}||${colPlayer.name}`];
                if (!m) return <td key={colPlayer.name} className={`pool-matrix__cell pool-matrix__cell--empty${colMe}`}></td>;

                const aName = typeof m.sideA === "object" ? m.sideA?.name : m.sideA;
                const winnerName = typeof m.winner === "object" ? m.winner?.name : m.winner;
                const rowIsAka = aName === rowPlayer.name;

                const interactiveProps = onMatchClick ? {
                  role: "button",
                  tabIndex: 0,
                  onClick: () => handleCellClick(m),
                  onKeyDown: (e) => handleCellKeyDown(e, m),
                } : {};

                if (m.status !== "completed") {
                  return <td key={colPlayer.name} className={`pool-matrix__cell pool-matrix__cell--pending ${m.status === "running" ? "pool-matrix__cell--live" : ""}${colMe}`} aria-label={cellLabel(rowPlayer, colPlayer, m.status === "running" ? "Now" : "Pending")} {...interactiveProps}>
                    {m.status === "running" ? <span className="bc-live" style={{ fontSize: 9 }}>●</span> : "–"}
                  </td>;
                }

                const ipponsA = (m.ipponsA || []).filter(x => x && x !== "•");
                const ipponsB = (m.ipponsB || []).filter(x => x && x !== "•");
                const rowIppons = rowIsAka ? ipponsA : ipponsB;
                const rowWon = winnerName && winnerName === rowPlayer.name;
                const isDraw = window.isHikiwake(m.decision) || window.isHikiwake(m.score?.type);

                let cellContent;
                let resultLabel;
                if (isDraw) {
                  cellContent = <span className="pool-matrix__draw">X</span>;
                  resultLabel = "Draw";
                } else if (rowWon) {
                  cellContent = <span className="pool-matrix__win">{rowIppons.join("") || "W"}</span>;
                  resultLabel = "Win";
                } else {
                  cellContent = <span className="pool-matrix__loss">{rowIppons.join("") || "L"}</span>;
                  resultLabel = "Loss";
                }

                return (
                  <td key={colPlayer.name} className={`pool-matrix__cell ${rowWon ? "pool-matrix__cell--win" : isDraw ? "pool-matrix__cell--draw" : "pool-matrix__cell--loss"}${colMe}`} aria-label={cellLabel(rowPlayer, colPlayer, resultLabel)} {...interactiveProps}>
                    {cellContent}
                  </td>
                );
              })}
            </tr>
          ))}
        </tbody>
      </table>
      <div className="pool-matrix__legend">
        <span className="pool-matrix__legend-item pool-matrix__legend-item--win">W = win</span>
        <span className="pool-matrix__legend-item pool-matrix__legend-item--loss">L = loss</span>
        <span className="pool-matrix__legend-item pool-matrix__legend-item--draw">X = draw</span>
        <span style={{ color: "var(--ink-3)", fontSize: 11 }}>{onMatchClick ? "Tap a cell to view match details" : "Row player's result vs column player"}</span>
      </div>
    </div>
  );
}

// rankOrdinal converts a 1-based rank integer to a short ordinal string.
// Handles the 11th/12th/13th exception and the general st/nd/rd/th rules.
function rankOrdinal(rank) {
  const mod100 = rank % 100;
  const mod10 = rank % 10;
  if (mod100 >= 11 && mod100 <= 13) return rank + "th";
  if (mod10 === 1) return rank + "st";
  if (mod10 === 2) return rank + "nd";
  if (mod10 === 3) return rank + "rd";
  return rank + "th";
}

// PoolNumberedMatchRow renders a single numbered pool match with Shiro/Aka
// sides and the formatted score string (via formatIpponsScore when completed).
// sideB = Shiro (left), sideA = Aka (right) — matches PoolMatchRow convention.
const PoolNumberedMatchRow = React.memo(({ m, num, onMatchClick }) => {
  const aName = typeof m.sideA === "object" ? m.sideA?.name : m.sideA;
  const bName = typeof m.sideB === "object" ? m.sideB?.name : m.sideB;

  // scoreStr: non-null only for completed matches; the render span below
  // handles the empty-string/null → "—" fallback in one place.
  // For team matches, teamIVScore derives the IV aggregate from subResults (mp-o4xl);
  // for individual matches it returns null and formatIpponsScore is used instead.
  // ipponsB = Shiro (left), ipponsA = Aka (right) — same arg order as all other callers.
  const scoreStr = m.status === "completed"
    ? window.matchScoreStr(m, m.ipponsB, m.ipponsA)
    : null;

  const handleClick = onMatchClick ? () => onMatchClick(m) : undefined;

  return (
    <button type="button" className="pool-match-numbered-row" style={{ cursor: handleClick ? "pointer" : "default" }} onClick={handleClick}>
      <span className="pool-match-numbered-row__num">{num}</span>
      <div className="pool-match-numbered-row__side pool-match-numbered-row__side--shiro">
        <span className="cbadge cbadge--shiro">Shiro</span>
        <span className="pool-match-numbered-row__name">{bName || "—"}</span>
      </div>
      <span className="pool-match-numbered-row__score">
        {m.status === "completed" ? (scoreStr || "—") : m.status === "running" ? <span className="bc-live" style={{ fontSize: 10 }}>●</span> : "–"}
      </span>
      <div className="pool-match-numbered-row__side pool-match-numbered-row__side--aka">
        <span className="cbadge cbadge--aka">Aka</span>
        <span className="pool-match-numbered-row__name">{aName || "—"}</span>
      </div>
    </button>
  );
});
PoolNumberedMatchRow.displayName = "PoolNumberedMatchRow";

function PoolsViewer({ pools, standings, poolMatches, tweaks, competition, onMatchClick, highlightPlayer }) {
  if (!pools || pools.length === 0) {
    return <div className="empty"><div className="icon">⏳</div><h3>Pools not drawn yet</h3></div>;
  }
  const isTeam = competition && (competition.kind === "team" || competition.teamSize > 0);
  // FR-050 / FR-051: league competitions render Final standings instead of
  // pool standings, and surface a Winner badge once every match is complete.
  // Format check is value-based so older competitions without a Format
  // field render as before (no header relabel, no winner badge).
  const isLeague = competition && competition.format === "league";
  const allMatchesComplete = isLeague && (() => {
    const all = poolMatches || [];
    return all.length > 0 && all.every(m => m.status === "completed");
  })();
  // Winner is the top-ranked player from the (single) league standings.
  // pools[0] is the only pool for league formats — see internal/engine.
  const leagueWinner = (isLeague && allMatchesComplete && pools[0] && standings)
    ? (standings[pools[0].poolName] || [])[0]
    : null;
  const poolWinners = competition ? (competition.poolWinners || 2) : 2;

  return (
    <div className="pools-grid">
      {isLeague && leagueWinner && <div style={{ gridColumn: "1 / -1" }}><WinnerBadge name={leagueWinner.player?.name || ""} /></div>}
      {pools.map((pool) => {
        const poolStandings = standings ? standings[pool.poolName] : null;
        const matches = poolMatches ? poolMatches.filter(m => {
          const id = m.id || "";
          return id.startsWith(pool.poolName + "-");
        }) : [];

        // Build playerKey→{rank, standingEntry} map from the rank-sorted standings array.
        // Rank is 1-based (index + 1). Used to look up each draw-position row's rank
        // without resorting the table (mp-938b: table is draw-order, not rank-order).
        // Key = player.id when available; falls back to "name||dojo" composite so that
        // duplicate names across different dojos don't collide — mirrors the lookup below.
        const rankByPlayerKey = new Map();
        if (poolStandings) {
          poolStandings.forEach((s, i) => {
            const key = s.player.id || `${s.player.name}||${s.player.dojo || ""}`;
            rankByPlayerKey.set(key, { rank: i + 1, standing: s });
          });
        }

        // Determine which players to iterate.
        // - League: standings already arrive in rank order; use them so the table is
        //   rank-first (pool.players is not meaningful for leagues and may be empty).
        // - Non-league pools: use pool.players draw order so operators can read the
        //   fight-order chart alongside the standings.
        const drawOrderPlayers = isLeague && poolStandings
          ? poolStandings.map(s => s.player)
          : (pool.players || []);

        return (
          <div key={pool.poolName} className="pool" style={{ padding: 14 }}>
            <div className="pool__head">
              <div className="pool__name">{isLeague ? (allMatchesComplete ? "Final standings" : "Standings") : pool.poolName}</div>
              <div style={{ fontSize: 12, color: "var(--ink-3)" }}>
                {matches.filter(m => m.status === "completed").length}/{matches.length} matches
              </div>
            </div>

            {/* Standings table — rows in draw position order, rank looked up by player key (id or name||dojo) */}
            <table className="pool__table">
              <thead>
                {isTeam ? (
                  <tr><th>#</th><th>Team</th><th className="num" title="Team matches won">W</th><th className="num" title="Team matches lost">L</th><th className="num" title="Team matches tied">T</th><th className="num" title="Individual victories">IV</th><th className="num" title="Individual losses">IL</th><th className="num" title="Individual ties (draws)">IT</th><th className="num" title="Points won">PW</th><th className="num" title="Points lost">PL</th></tr>
                ) : (
                  <tr><th>#</th><th>Player</th><th className="num" title="Fights won">W</th><th className="num" title="Fights lost">L</th><th className="num" title="Draws (hikiwake)">D</th><th className="num" title="Points won (ippon)">PW</th><th className="num" title="Points lost">PL</th></tr>
                )}
              </thead>
              <tbody>
                {drawOrderPlayers.map((p, i) => {
                  const drawPos = i + 1;
                  // Look up by id first (stable), fall back to name||dojo composite for
                  // legacy fixtures without UUIDs. Mirrors the key used when building rankByPlayerKey.
                  const lookup = rankByPlayerKey.get(p.id || `${p.name}||${p.dojo || ""}`);
                  const s = lookup ? lookup.standing : null;
                  const rank = lookup ? lookup.rank : null;
                  const isAdvancing = !isLeague && rank !== null && rank <= poolWinners;

                  const rowClasses = [
                    isFollowedPlayer(p, highlightPlayer) ? "pool__row--me" : "",
                    isAdvancing ? "advancing" : "",
                  ].filter(Boolean).join(" ");

                  return (
                    <tr key={p.id || `${p.name}||${p.dojo || ""}` || drawPos} className={rowClasses || undefined}>
                      <td className="pool-standings__draw-pos" style={{ color: "var(--ink-3)", fontFamily: "var(--font-mono)" }}>{drawPos}</td>
                      <td>
                        <div style={{ fontWeight: 500 }}>
                          {p.number ? <span className="num-prefix">{p.number}</span> : null}
                          {p.name}
                          {rank !== null ? (
                            <span className={`rank-badge${isAdvancing ? " rank-badge--adv" : ""}`}>
                              {rankOrdinal(rank)}{s && s.isOverridden ? "*" : ""}
                            </span>
                          ) : null}
                        </div>
                        {tweaks.showDojo ? <div style={{ fontSize: 11, color: "var(--ink-3)" }}>{p.dojo}</div> : null}
                      </td>
                      {s ? (
                        <>
                          <td className="num">{s.wins}</td>
                          <td className="num">{s.losses}</td>
                          <td className="num">{s.draws}</td>
                          {isTeam && <td className="num">{s.individualWins || 0}</td>}
                          {isTeam && <td className="num">{s.individualLosses || 0}</td>}
                          {isTeam && <td className="num">{s.individualDraws || 0}</td>}
                          <td className="num">{isTeam ? (s.pointsWon || 0) : s.ipponsGiven}</td>
                          <td className="num">{isTeam ? (s.pointsLost || 0) : s.ipponsTaken}</td>
                        </>
                      ) : (
                        Array.from({ length: isTeam ? 8 : 5 }, (_, j) => <td key={j} className="num">—</td>)
                      )}
                    </tr>
                  );
                })}
              </tbody>
            </table>

            {/* Numbered match list — shown for both individual and team pools */}
            {matches.length > 0 && (
              <>
                <div className="pool-match-section-label">Matches</div>
                <div className="pool-match-numbered-list">
                  {matches.map((m, idx) => {
                    if (isTeam) {
                      // Team: enrich match the same way as the legacy PoolMatchRow path
                      const isDH = isPoolDaihyosenID(m.id || "");
                      const enriched = { ...m, phase: "pool", poolName: pool.poolName, phaseName: pool.poolName, compFormat: competition.format, compName: competition.name, compKind: isDH ? "" : competition.kind, teamSize: isDH ? 0 : competition.teamSize };
                      return (
                        <PoolNumberedMatchRow
                          key={m.id}
                          m={m}
                          num={idx + 1}
                          onMatchClick={onMatchClick ? () => onMatchClick(enriched) : null}
                        />
                      );
                    } else {
                      // Individual: ippon notation score
                      const enriched = { ...m, phase: "pool", poolName: pool.poolName, phaseName: pool.poolName, compFormat: competition.format, compName: competition.name, compKind: "", teamSize: 0 };
                      return (
                        <PoolNumberedMatchRow
                          key={m.id}
                          m={m}
                          num={idx + 1}
                          onMatchClick={onMatchClick ? () => onMatchClick(enriched) : null}
                        />
                      );
                    }
                  })}
                </div>
              </>
            )}

            {/* Round-robin matrix — optional grid below the match list (individual only) */}
            {matches.length > 0 && !isTeam && (
              <div style={{ marginTop: 4 }}>
                <PoolMatrix pool={pool} matches={matches} tweaks={tweaks} onMatchClick={onMatchClick} highlightPlayer={highlightPlayer} />
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}

// Reusable multi-player filter — used by both viewer & admin schedule pages.
// Picks any number of participants/teams across all competitions; matches are
// kept if they involve ANY of the picked sides. Free-text dojo search works in parallel.
function PlayerMultiFilter({ tournament, picked, setPicked, dojoText, setDojoText }) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const ref = useRefV(null);

  // build a deduped roster across all competitions
  const roster = useMemo(() => {
    const map = new Map();
    (tournament.competitions || []).forEach((c) => {
      c.players.forEach((p) => {
        const key = p.id;
        if (!map.has(key)) map.set(key, { ...p, comps: [c.name] });
        else map.get(key).comps.push(c.name);
      });
    });
    return Array.from(map.values());
  }, [tournament]);

  const q = query.trim().toLowerCase();
  const matches = q ? roster.filter((p) =>
    p.name.toLowerCase().includes(q) || (p.dojo || "").toLowerCase().includes(q)
  ).slice(0, 30) : roster.slice(0, 30);

  React.useEffect(() => {
    const onDoc = (e) => { if (ref.current && !ref.current.contains(e.target)) setOpen(false); };
    document.addEventListener("mousedown", onDoc);
    return () => document.removeEventListener("mousedown", onDoc);
  }, []);

  const toggle = (p) => {
    if (picked.find((x) => x.id === p.id)) setPicked(picked.filter((x) => x.id !== p.id));
    else setPicked([...picked, p]);
  };

  return (
    <div className="pmf" ref={ref}>
      <div className="pmf__bar" onClick={() => setOpen(true)}>
        {picked.length === 0 && !dojoText ? (
          <span className="pmf__placeholder">Filter by player, team, or dojo…</span>
        ) : null}
        {picked.map((p) => (
          <span key={p.id} className="pmf__chip">
            {p.name}
            <button onClick={(e) => { e.stopPropagation(); toggle(p); }} aria-label="Remove">×</button>
          </span>
        ))}
        {dojoText ? (
          <span className="pmf__chip pmf__chip--text">
            “{dojoText}”
            <button onClick={(e) => { e.stopPropagation(); setDojoText(""); }} aria-label="Remove">×</button>
          </span>
        ) : null}
        <input
          className="pmf__input"
          placeholder={picked.length || dojoText ? "Add more…" : ""}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onFocus={() => setOpen(true)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && query.trim()) {
              setDojoText(query.trim());
              setQuery("");
            } else if (e.key === "Backspace" && !query && picked.length) {
              setPicked(picked.slice(0, -1));
            }
          }}
        />
      </div>
      {open && (
        <div className="pmf__dropdown">
          <div className="pmf__dropdown-head">
            {q ? pluralize(matches.length, "match", "matches") : `${pluralize(roster.length, "participant")} — type to search`}
            {(picked.length > 0 || dojoText) && (
              <button className="btn btn--ghost btn--sm" onClick={() => { setPicked([]); setDojoText(""); setQuery(""); }}>Clear all</button>
            )}
          </div>
          {q && (
            <button className="pmf__option pmf__option--text" onClick={() => { setDojoText(query.trim()); setQuery(""); }}>
              <span>Match text “<b>{query}</b>” in any name/dojo</span>
            </button>
          )}
          {matches.map((p) => {
            const isPicked = !!picked.find((x) => x.id === p.id);
            return (
              <button key={p.id} className={`pmf__option ${isPicked ? "is-picked" : ""}`} onClick={() => toggle(p)}>
                <span className="pmf__check">{isPicked ? "✓" : ""}</span>
                <span className="pmf__opt-body">
                  <span className="pmf__opt-name">{p.name}</span>
                  <span className="pmf__opt-dojo">{p.dojo}{p.comps?.length ? ` · ${p.comps.join(", ")}` : ""}</span>
                </span>
              </button>
            );
          })}
          {matches.length === 0 && !q && <div className="pmf__empty">No participants yet.</div>}
        </div>
      )}
    </div>
  );
}

function applyFilters(matches, picked, dojoText, compFilter) {
  const ids = new Set(picked.map((p) => p.id));
  const names = new Set(picked.map((p) => p.name).filter(Boolean));
  const dt = (dojoText || "").trim().toLowerCase();
  return matches.filter((m) => {
    if (compFilter !== "all" && m.compId !== compFilter) return false;
    if (ids.size > 0) {
      const hit = (m.sideA && (ids.has(m.sideA.id) || names.has(m.sideA.name))) || (m.sideB && (ids.has(m.sideB.id) || names.has(m.sideB.name)));
      if (!hit) return false;
    }
    if (dt) {
      const hit = [m.sideA?.name, m.sideB?.name, m.sideA?.dojo, m.sideB?.dojo].some((s) => (s || "").toLowerCase().includes(dt));
      if (!hit) return false;
    }
    return true;
  });
}

function matchHighlightedBy(m, picked, dojoText) {
  const ids = new Set(picked.map((p) => p.id));
  const names = new Set(picked.map((p) => p.name).filter(Boolean));
  if (ids.size > 0 && ((m.sideA && (ids.has(m.sideA.id) || names.has(m.sideA.name))) || (m.sideB && (ids.has(m.sideB.id) || names.has(m.sideB.name))))) return true;
  const dt = (dojoText || "").trim().toLowerCase();
  if (dt && [m.sideA?.name, m.sideB?.name, m.sideA?.dojo, m.sideB?.dojo].some((s) => (s || "").toLowerCase().includes(dt))) return true;
  return false;
}

export { PlayerMultiFilter, applyFilters, matchHighlightedBy, competitionKindLabel, compMatches, tournamentMatches, currentMatchOf, buildPlayerMatchHighlight, buildWatchlistUpcoming, buildFollowedNextMatch, isSwissFinalStandings, swissStandingsHeading, isFollowedPlayer, deriveAwards, bracketHasDecidedFinal, resolveCompetitionAwards, addDojoToWatchlist, buildRoster, MatchDetailCard, MatchViewerModal, AnnouncementCard, AnnouncementBanner, ViewerCompetition, ViewerOverview, MyMatchAlertBanner, PoolMatrix, PoolsViewer, PoolNumberedMatchRow, AwardsView, FightingSpiritSection };

if (typeof window !== 'undefined') {
    window.PlayerMultiFilter = PlayerMultiFilter;
    window.applyFilters = applyFilters;
    window.matchHighlightedBy = matchHighlightedBy;
    window.buildPlayerMatchHighlight = buildPlayerMatchHighlight;
    window.buildWatchlistUpcoming = buildWatchlistUpcoming;
    window.deriveAwards = deriveAwards;
    window.bracketHasDecidedFinal = bracketHasDecidedFinal;
    window.resolveCompetitionAwards = resolveCompetitionAwards;
    window.addDojoToWatchlist = addDojoToWatchlist;
}

// Tournament-wide schedule (across competitions) — grouped by day, then court swimlanes + filter
function ScheduleViewer({ tournament, tweaks }) {
  const allMatches = useMemo(() => tournamentMatches(tournament).filter(hasBothSides), [tournament]);
  const courts = tournament.courts || [];

  // T113 / T117 / FR-022 / FR-024: auto-populate the schedule's `picked`
  // filter with the followed-player + watchlist so the existing
  // matchHighlightedBy / .tw-match--highlight path lights up the rows the
  // viewer cares about. Both lists are seeded once from localStorage; the
  // user can still add or remove chips like before — we only set the
  // initial value, then `picked` is owned by the schedule (no live
  // re-sync to localStorage, which would fight the user's edits).
  const [followedPlayer, setFollowedPlayer] = useFollowedPlayer();
  const [watchlist] = useWatchlist();
  const initialPicked = useMemo(() => {
    const seen = new Set();
    const out = [];
    const push = (p) => {
      if (!p || !p.id || seen.has(p.id)) return;
      seen.add(p.id);
      out.push({ id: p.id, name: p.name || "", dojo: p.dojo || "" });
    };
    if (followedPlayer && followedPlayer.id) push(followedPlayer);
    (watchlist || []).forEach(push);
    return out;
    // Compute once at mount only — re-derivation would clobber user edits
    // as soon as they removed a seeded chip. Re-sync explicitly via the
    // Clear/Reseed buttons instead.
  }, []);
  const [picked, setPicked] = useState(initialPicked);
  const [dojoText, setDojoText] = useState("");
  const [courtFilter, setCourtFilter] = useState("all");

  // Derive unique days from competitions and matches
  const allDates = useMemo(() => {
    const days = new Set();
    (tournament.competitions || []).forEach((c) => { if (c.date) days.add(c.date); });
    allMatches.forEach((m) => { if (m.date) days.add(m.date); });
    const sorted = Array.from(days).sort(compareDmy);
    return sorted.length > 0 ? sorted : [""];
  }, [tournament, allMatches]);

  const [activeDay, setActiveDay] = useState(() => allDates[0] || "");
  const [compFilter, setCompFilter] = useState("all");

  // When dates change (data loads), reset to first
  React.useEffect(() => {
    setActiveDay(prev => allDates.includes(prev) ? prev : (allDates[0] || ""));
  }, [allDates]);

  const filtered = applyFilters(allMatches, picked, dojoText, compFilter);

  // For day-based filtering: if no dates at all, show everything; otherwise filter by active day
  const dayFiltered = allDates.length <= 1 && allDates[0] === ""
    ? filtered
    : filtered.filter((m) => {
        const mDate = m.date || (tournament.competitions || []).find(c => c.id === m.compId)?.date || "";
        return mDate === activeDay || (!mDate && activeDay === "");
      });

  const byCourt = {};
  courts.forEach((cc) => byCourt[cc] = []);
  dayFiltered.forEach((m) => { (byCourt[m.court] = byCourt[m.court] || []).push(m); });
  Object.values(byCourt).forEach((list) => list.sort((a, b) => {
    const order = { running: 0, scheduled: 1, completed: 2 };
    const ao = order[a.status] ?? 2;
    const bo = order[b.status] ?? 2;
    if (ao !== bo) return ao - bo;
    return (a.scheduledAt || "99:99").localeCompare(b.scheduledAt || "99:99");
  }));

  const matchHasFilter = (m) => matchHighlightedBy(m, picked, dojoText);
  const hasAnyFilter = picked.length > 0 || dojoText || compFilter !== "all" || courtFilter !== "all";
  const multiDay = allDates.length > 1 || (allDates.length === 1 && allDates[0] !== "");

  return (
    <div className="tw-sched">
      {followedPlayer && followedPlayer.id ? (
        <div
          className="tw-sched__following"
          style={{
            display: "flex", alignItems: "center", gap: 8, padding: "6px 10px",
            marginBottom: 8, fontSize: 13, background: "var(--bg-2)",
            border: "1px solid var(--line)", borderRadius: 6
          }}
        >
          <span style={{ color: "var(--ink-3)" }}>Following:</span>
          <span style={{ fontWeight: 600 }}>{followedPlayer.name || "(unknown)"}</span>
          <button
            className="btn btn--ghost btn--sm btn--clear-follow"
            style={{ marginLeft: "auto" }}
            onClick={() => {
              // Clear both the persisted selection and the local schedule
              // chip so the highlight disappears without a reload.
              setFollowedPlayer(null);
              setPicked(picked.filter((p) => p.id !== followedPlayer.id));
            }}
            aria-label="Stop following"
          >
            ✕ Clear
          </button>
        </div>
      ) : null}
      <div className="tw-sched__filters">
        <PlayerMultiFilter tournament={tournament} picked={picked} setPicked={setPicked} dojoText={dojoText} setDojoText={setDojoText} />
        <select className="input" style={{ width: "auto", minWidth: 160 }} value={compFilter} onChange={(e) => setCompFilter(e.target.value)}>
          <option value="all">All competitions</option>
          {(tournament.competitions || []).map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
        </select>
        <select className="input" style={{ width: "auto" }} value={courtFilter} onChange={(e) => setCourtFilter(e.target.value)}>
          <option value="all">All Shiaijo</option>
          {courts.map(c => <option key={c} value={c}>Shiaijo {c}</option>)}
        </select>
        {hasAnyFilter && (
          <button className="btn btn--ghost btn--sm" onClick={() => { setPicked([]); setDojoText(""); setCompFilter("all"); setCourtFilter("all"); }}>Clear</button>
        )}
        <span style={{ marginLeft: "auto", fontSize: 12, color: "var(--ink-3)" }}>{pluralize(dayFiltered.length, "match", "matches")} of {allMatches.length}</span>
      </div>

      {multiDay && (
        <div className="day-tabs">
          {allDates.map((d) => (
            <button key={d} className={`day-tab ${activeDay === d ? "is-active" : ""}`} onClick={() => setActiveDay(d)}>
              {d ? formatDate(d) : "All days"}
            </button>
          ))}
        </div>
      )}

      <div className="tw-courts">
        {allMatches.length === 0 ? (
          <div className="empty" style={{ gridColumn: "1 / -1" }}>
            <div className="icon">🗓</div>
            <h3>No matches scheduled yet</h3>
            <div style={{ fontSize: 13 }}>The schedule will appear here once the tournament begins.</div>
          </div>
        ) : courts.map((cc) => {
          const list = byCourt[cc] || [];
          const liveOn = list.find((m) => m.status === "running");
          if (courtFilter !== "all" && cc !== courtFilter) return null;
          return (
            <div key={cc} className="tw-court">
              <div className="tw-court__head">
                <div>
                  <div className="tw-court__title">SHIAIJO {cc}</div>
                  <div className="tw-court__sub">{list.length} match{list.length === 1 ? "" : "es"}{liveOn ? " · live now" : ""}</div>
                </div>
                {liveOn && <span className="bc-live">● NOW</span>}
              </div>
              <div className="tw-court__list">
                {list.length === 0 ? (
                  <div style={{ fontSize: 12, color: "var(--ink-3)", padding: "20px 8px", textAlign: "center" }}>No matches</div>
                ) : list.map((m) => (
                  <TWMatch key={m.compId + m.id} m={m} highlight={matchHasFilter(m)} tweaks={tweaks} onClick={() => tweaks.onMatchClick && tweaks.onMatchClick(m)} />
                ))}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}

function TWMatch({ m, highlight, _tweaks, onClick }) {
  const aWin = m.winner && m.sideA && m.winner.id === m.sideA.id;
  const bWin = m.winner && m.sideB && m.winner.id === m.sideB.id;
  // Bracket matches carry scoreA/scoreB strings rather than ipponsA/B arrays
  // (see normalizeMatch). Apply the same fallback used in VSchedItem so the
  // score cell renders the derived winnerPts–loserPts string instead of "—".
  const twIpponsA = m.ipponsA || window.ipponsFromScore(m.scoreA);
  const twIpponsB = m.ipponsB || window.ipponsFromScore(m.scoreB);
  const scoreStr = m.status === "completed" ? window.matchScoreStr(m, twIpponsB, twIpponsA) : null;
  // FR-025: per-court queue position — see VSchedItem for the contract.
  // Short pill form here because the tw-match row is denser than the
  // upcoming-list row in the per-competition viewer. Wording is owned
  // by display.jsx::queueLabelCompact (bead mp-e3k); we still grab `qp`
  // separately because the accent-color styling below keys off qp===1.
  const qp = Number(m.queuePosition);
  const queuePill = (window.queueLabelCompact || _localQueueLabelCompact)(m);
  return (
    <button className={`tw-match ${m.status === "running" ? "tw-match--live" : ""} ${m.status === "completed" ? "tw-match--done" : ""} ${highlight ? "tw-match--highlight" : ""}`} onClick={onClick} style={{ textAlign: "left", border: "none", background: "none", cursor: onClick ? "pointer" : "default" }}>
      <div className="tw-match__meta">
        <div className="tw-match__time">{m.scheduledAt || "—"}</div>
        <div className="tw-match__phase">{m.phase === "pool" ? poolLabel(m) : m.round}</div>
        {queuePill && (
          <div className="tw-match__queue" style={{ fontSize: 10, fontWeight: 700, color: qp === 1 ? "var(--accent)" : "var(--ink-3)", marginTop: 2 }}>
            {queuePill}
          </div>
        )}
      </div>
      <div className="tw-match__players">
        <div className={`tw-match__name ${bWin ? "tw-match__name--w" : ""}`}>
          <span className="tw-match__badge tw-match__badge--shiro">S</span>
          {m.sideB?.name || "TBD"}
        </div>
        <div className={`tw-match__name ${aWin ? "tw-match__name--w" : ""}`}>
          <span className="tw-match__badge tw-match__badge--aka">A</span>
          {m.sideA?.name || "TBD"}
        </div>
        <div className="tw-match__comp">{m.compName}</div>
      </div>
      <div style={{ textAlign: "right", fontFamily: "var(--font-mono)", fontWeight: 700, fontSize: 13 }}>
        {m.status === "completed" && scoreStr}
        {m.status === "completed" && m.score?.type === "bye" && <span style={{ fontSize: 10, color: "var(--ink-3)" }}>BYE</span>}
        {m.status === "running" && <span className="bc-live">●</span>}
      </div>
    </button>
  );
}

// deriveAwards returns up to four placements for the closing ceremony per
// FIK convention: 1st, 2nd, and two 3rds (semi-final losers — no bronze match).
// Returns [] when no podium data exists yet.
// `nameToPlayer` is an optional Map(name → {name, dojo}) to enrich bracket
// entries with dojo info; missing names fall back to {name, dojo: ""}.
// Bracket match fields (sideA/sideB/winner) may be either plain strings (raw
// backend payload) or normalized objects ({id, name, dojo}) as produced by
// normalizeMatch() in api_serializers.jsx — both shapes are handled.
// `standings` may be either a flat array (Swiss-shape) or an object keyed by
// pool name (pools/league shape).
function deriveAwards(bracket, standings, pools, nameToPlayer) {
  // Extract the name string from a match field that may be a string (raw
  // backend payload) or a normalized object ({id, name, dojo}) produced by
  // normalizeMatch() in api_serializers.jsx.
  const toName = (v) => (v && typeof v === "object" ? v.name || "" : v || "");

  // Enrich a player field with dojo info. If the field is already a normalized
  // object with a dojo, use it directly; otherwise fall back to nameToPlayer.
  const lookup = (v) => {
    const name = toName(v);
    if (!name) return null;
    const fromField = v && typeof v === "object" ? v.dojo : null;
    const p = nameToPlayer && nameToPlayer.get(name);
    return { name, dojo: fromField || (p && p.dojo) || "" };
  };

  // Bracket-based: final + semi-finals. Only used when the final has a
  // decided winner; if it doesn't, fall through to standings below so a
  // pools-only competition that has a TBD-placeholder bracket still shows
  // the podium from its pool standings.
  if (bracket && bracket.rounds && bracket.rounds.length > 0) {
    const finalRound = bracket.rounds[bracket.rounds.length - 1];
    const sfRound = bracket.rounds[bracket.rounds.length - 2] || [];
    const final = finalRound[0];
    if (final && final.winner) {
      const winnerName = toName(final.winner);
      const champion = final.winner;
      const runnerUp = winnerName === toName(final.sideA) ? final.sideB : final.sideA;
      const thirds = sfRound
        .map((m) => {
          if (!m.winner) return null;
          return toName(m.winner) === toName(m.sideA) ? m.sideB : m.sideA;
        })
        .filter(Boolean);
      const slot = (place, side) => {
        const r = lookup(side);
        return r ? { place, ...r } : null;
      };
      return [
        slot(1, champion),
        slot(2, runnerUp),
        slot(3, thirds[0]),
        slot(3, thirds[1]),
      ].filter(Boolean);
    }
  }

  // Standings-based fallback. Two payload shapes are supported:
  //   - Swiss/`/swiss/standings`: a flat array of standings rows.
  //   - Pools/league: an object keyed by poolName → array of rows.
  // We take the top four from the (single) leaderboard; for pools-only with
  // multiple pools we use the first pool (consistent with PoolsViewer's
  // leagueWinner pick).
  let list = null;
  if (Array.isArray(standings)) {
    list = standings;
  } else if (standings && pools && pools.length > 0) {
    list = standings[pools[0].poolName] || [];
  }
  if (list && list.length > 0) {
    const slice = list.slice(0, 4).map((s, i) => ({
      place: i < 3 ? i + 1 : 3,
      name: s.player?.name || "",
      dojo: s.player?.dojo || "",
    }));
    return slice.filter((e) => e.name);
  }

  return [];
}

// bracketHasDecidedFinal: true iff the bracket's last round has a decided final.
function bracketHasDecidedFinal(bracket) {
  if (!bracket || !bracket.rounds || bracket.rounds.length === 0) return false;
  const finalRound = bracket.rounds[bracket.rounds.length - 1];
  const final = finalRound && finalRound[0];
  return !!(final && final.winner);
}

// resolveCompetitionAwards: the single source of truth for a competition's
// podium. Mixed (Pools + Knockout) is a single competition whose knockout fills
// in place, so its podium is derived from its OWN bracket (the KNOCKOUT result,
// 1/2/3/3 — never pool standings) once the final is decided.
// Returns { state, podium } where state is one of:
//   'final'       — podium is the final result
//   'in-progress' — knockout not yet decided (podium [])
//   'skip'        — linked-playoffs shell (sourceCompID set); caller excludes from results
// fetchers = { fetchCompetitionDetails(id), swissStandings(id)|null }
async function resolveCompetitionAwards(comp, fetchers) {
  // A linked-playoffs shell (legacy split-comp layout carrying sourceCompID)
  // derives its podium from its source comp, never standalone — drop it so it
  // doesn't appear twice in the results summary. buildAllWinnersPublic filters
  // state==="skip". (The current data model no longer emits sourceCompID, so
  // this is defensive parity with the documented behaviour.)
  if (comp && comp.sourceCompID) {
    return { state: "skip", podium: [] };
  }
  const fmt = comp && comp.format;
  const ntpFrom = (players) => {
    const m = new Map();
    (players || []).forEach((p) => { if (p && p.name) m.set(p.name, p); });
    return m;
  };
  if (fmt === "mixed" || fmt === "playoffs") {
    // Mixed is now a SINGLE competition: its knockout bracket fills in place as
    // pools finish (no separate "- Playoffs" comp). Both mixed and standalone
    // playoffs derive their podium from their OWN bracket once the final is
    // decided; until then the knockout is still in progress.
    const d = await fetchers.fetchCompetitionDetails(comp.id);
    if (bracketHasDecidedFinal(d.bracket)) {
      return { state: "final", podium: deriveAwards(d.bracket, null, null, ntpFrom(d.players)) };
    }
    return { state: "in-progress", podium: [] };
  }
  // league / swiss (and any standings-based) → standings
  const d = await fetchers.fetchCompetitionDetails(comp.id);
  let standings = d.standings;
  if (fmt === "swiss" && fetchers.swissStandings) {
    try { standings = await fetchers.swissStandings(comp.id); } catch (_) { /* keep detail standings */ }
  }
  return { state: "final", podium: deriveAwards(null, standings, d.pools, ntpFrom(d.players)) };
}

const PLACE_STYLE = {
  1: { icon: "🏆", label: "1st Place", accent: "var(--gold, #d4af37)" },
  2: { icon: "🥈", label: "2nd Place", accent: "var(--silver, #c0c0c0)" },
  3: { icon: "🥉", label: "3rd Place", accent: "var(--bronze, #cd7f32)" },
};

function AwardsView({ c, bracket, standings, pools, players }) {
  const containerRef = useRefV(null);
  const [isFs, setIsFs] = useState(false);
  const isLeague = c?.format === "league";
  const isMixed = c?.format === "mixed";
  // Swiss standings aren't part of the competition-detail payload — they live
  // behind /swiss/standings. Fetch them here when the format is swiss so the
  // Awards tab works for Swiss competitions too.
  const [swissStandings, setSwissStandings] = useState(null);
  // koAwards: undefined = not yet loaded, object = { state, awards } for mixed comps.
  const [koAwards, setKoAwards] = useState(undefined);

  React.useEffect(() => {
    const onFsChange = () => setIsFs(!!document.fullscreenElement);
    document.addEventListener("fullscreenchange", onFsChange);
    return () => document.removeEventListener("fullscreenchange", onFsChange);
  }, []);

  React.useEffect(() => {
    if (c?.format !== "swiss" || !window.API?.swissStandings) return;
    let cancelled = false;
    window.API.swissStandings(c.id)
      .then((data) => { if (!cancelled) setSwissStandings(Array.isArray(data) ? data : []); })
      .catch(() => { if (!cancelled) setSwissStandings([]); });
    return () => { cancelled = true; };
  }, [c?.id, c?.format, c?.swissCurrentRound]);

  // For mixed (pools+knockout) comps, delegate to resolveCompetitionAwards so
  // this view and the All Winners modal share the same resolver (no drift).
  React.useEffect(() => {
    if (!isMixed) return;
    setKoAwards(undefined);
    if (!window.API?.fetchCompetitionDetails) {
      setKoAwards({ state: "in-progress", awards: [] });
      return;
    }
    let cancelled = false;
    const fetchers = { fetchCompetitionDetails: window.API.fetchCompetitionDetails, swissStandings: null };
    resolveCompetitionAwards(c, fetchers)
      .then(({ state, podium }) => {
        if (!cancelled) setKoAwards({ state, awards: podium });
      })
      .catch(() => {
        if (!cancelled) setKoAwards({ state: "in-progress", awards: [] });
      });
    return () => { cancelled = true; };
  }, [c?.id, c?.format]);

  const nameToPlayer = useMemo(() => {
    const m = new Map();
    (players || []).forEach((p) => {
      if (p && p.name) m.set(p.name, p);
    });
    return m;
  }, [players]);

  const effectiveStandings = c?.format === "swiss" ? swissStandings : standings;
  const isSwissLoading = c?.format === "swiss" && swissStandings === null;
  const baseAwards = useMemo(
    () => deriveAwards(bracket, effectiveStandings, pools, nameToPlayer),
    [bracket, effectiveStandings, pools, nameToPlayer]
  );

  // Determine the awards array and state to use for rendering.
  let awards, resolvedState;
  if (isMixed) {
    if (koAwards === undefined) {
      // Still loading — show loading spinner below.
      awards = [];
      resolvedState = "loading";
    } else {
      awards = koAwards.awards;
      resolvedState = koAwards.state;
    }
  } else {
    awards = baseAwards;
    resolvedState = "final";
  }

  const leagueWinner = isLeague ? (awards.find(a => a.place === 1) || null) : null;

  const toggleFs = () => {
    const el = containerRef.current;
    if (!el) return;
    if (document.fullscreenElement) {
      document.exitFullscreen().catch(() => {});
    } else if (el.requestFullscreen) {
      el.requestFullscreen().catch(() => {});
    }
  };

  if (isSwissLoading || resolvedState === "loading") {
    return (
      <div className="empty" data-testid="awards-loading">
        <div className="icon">🏆</div>
        <h3>Loading final standings…</h3>
      </div>
    );
  }

  if (awards.length === 0) {
    const fsAwards = c?.fightingSpiritAwards;
    const hasFsAwards = fsAwards && fsAwards.length > 0;
    if (isMixed && resolvedState === "in-progress") {
      return (
        <div>
          <div className="empty" data-testid="awards-in-progress">
            <div className="icon">🏆</div>
            <h3>Knockout in progress</h3>
          </div>
          {hasFsAwards && <FightingSpiritSection fsAwards={fsAwards} isFs={isFs} />}
        </div>
      );
    }
    return (
      <div>
        <div className="empty" data-testid="awards-empty">
          <div className="icon">🏆</div>
          <h3>Final standings not yet available</h3>
        </div>
        {hasFsAwards && <FightingSpiritSection fsAwards={fsAwards} isFs={isFs} />}
      </div>
    );
  }

  const champion = awards.find(a => a.place === 1) || null;
  const second = awards.find(a => a.place === 2) || null;
  const thirds = awards.filter(a => a.place === 3);

  // Shared header (title + place count + fullscreen toggle) — identical for the
  // league and champion-hero layouts.
  const header = (
    <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 16 }}>
      <div>
        <div className="section-title" style={{ margin: 0, fontSize: isFs ? 28 : 18 }}>
          {c?.name ? `${c.name} — Awards` : "Awards"}
        </div>
        <div style={{ fontSize: isFs ? 16 : 12, color: "var(--ink-3)" }}>
          Closing ceremony · {awards.length} place{awards.length === 1 ? "" : "s"}
        </div>
      </div>
      <button className="btn btn--sm" onClick={toggleFs} data-testid="awards-fullscreen">
        {isFs ? "Exit fullscreen" : "Fullscreen"}
      </button>
    </div>
  );

  // League format: keep WinnerBadge above and fall back to the classic podium
  // row layout — the hero card doesn't fit the league ceremony well.
  if (isLeague) {
    return (
      <div
        ref={containerRef}
        className="awards"
        data-testid="awards-view"
        style={{
          background: isFs ? "var(--bg)" : "transparent",
          padding: isFs ? 40 : 0,
          minHeight: isFs ? "100vh" : "auto",
        }}
      >
        {header}
        {leagueWinner && <WinnerBadge name={leagueWinner.name} isFs={isFs} testId="league-winner-badge" marginBottom={16} />}
        <div className="podium" style={isFs ? { gap: 24, fontSize: 18 } : null}>
          {awards.map((a, idx) => {
            const style = PLACE_STYLE[a.place] || PLACE_STYLE[3];
            return (
              <div
                key={`${a.place}-${a.name}-${idx}`}
                className={`podium-step podium-step--${a.place}`}
                data-testid={`awards-place-${a.place}-${idx}`}
                style={{ borderTop: `4px solid ${style.accent}` }}
              >
                <div style={{ fontSize: isFs ? 56 : 28 }}>{style.icon}</div>
                <div className="place" style={{ fontSize: isFs ? 18 : 12 }}>{style.label}</div>
                <div className="name" style={{ fontSize: isFs ? 28 : 16 }}>{a.name}</div>
                {a.dojo && (
                  <div className="dojo" style={{ fontSize: isFs ? 16 : 12, color: "var(--ink-3)" }}>{a.dojo}</div>
                )}
              </div>
            );
          })}
        </div>
        {/* Fighting Spirit awards — distinct section, independent of the podium */}
        <FightingSpiritSection fsAwards={c?.fightingSpiritAwards} isFs={isFs} />
      </div>
    );
  }

  // mp-8jbo: Champion-hero podium layout for non-league competitions.
  // 1st place → large gold hero card (top, full width).
  // 2nd place → single centered card in its own row.
  // 3rd places → side-by-side equal-width cards in their own row (kendo has two
  // joint 3rds — both beaten semi-finalists; no 4th, no bronze match).
  return (
    <div
      ref={containerRef}
      className="awards"
      data-testid="awards-view"
      style={{
        background: isFs ? "var(--bg)" : "transparent",
        padding: isFs ? 40 : 0,
        minHeight: isFs ? "100vh" : "auto",
      }}
    >
      {header}

      {/* 1st place — champion hero */}
      {champion && (
        <div
          className="awards-hero"
          data-testid={`awards-place-1-${awards.indexOf(champion)}`}
          style={isFs ? { padding: 32, marginBottom: 20 } : null}
        >
          <div className="awards-hero__crown" style={isFs ? { fontSize: 48 } : null}>{PLACE_STYLE[1].icon}</div>
          <div className="awards-hero__eyebrow" style={isFs ? { fontSize: 16 } : null}>Champion</div>
          <div className="awards-hero__name" style={isFs ? { fontSize: 34 } : null}>{champion.name}</div>
          {champion.dojo && (
            <div className="awards-hero__dojo" style={isFs ? { fontSize: 18 } : null}>{champion.dojo}</div>
          )}
        </div>
      )}

      {/* 2nd place — single centered card */}
      {second && (
        <div className="awards-row awards-row--center">
          <div
            className="podium-step podium-step--2 place--eq"
            data-testid={`awards-place-2-${awards.indexOf(second)}`}
            style={isFs ? { fontSize: 18, padding: "20px 24px" } : null}
          >
            <div style={{ fontSize: isFs ? 40 : 22 }}>{PLACE_STYLE[2].icon}</div>
            <div className="place" style={{ fontSize: isFs ? 16 : 12 }}>{PLACE_STYLE[2].label}</div>
            <div className="name" style={{ fontSize: isFs ? 24 : 16 }}>{second.name}</div>
            {second.dojo && (
              <div className="dojo" style={{ fontSize: isFs ? 14 : 12 }}>{second.dojo}</div>
            )}
          </div>
        </div>
      )}

      {/* 3rd places — side-by-side (kendo: two joint 3rds, no bronze match) */}
      {thirds.length > 0 && (
        <div className="awards-row">
          {thirds.map((a, idx) => (
            <div
              key={`3-${a.name}-${idx}`}
              className="podium-step podium-step--3 place--eq"
              data-testid={`awards-place-3-${awards.indexOf(a)}`}
              style={isFs ? { fontSize: 16, padding: "16px 20px" } : null}
            >
              <div style={{ fontSize: isFs ? 36 : 20 }}>{PLACE_STYLE[3].icon}</div>
              <div className="place" style={{ fontSize: isFs ? 14 : 11 }}>{PLACE_STYLE[3].label}</div>
              <div className="name" style={{ fontSize: isFs ? 20 : 14 }}>{a.name}</div>
              {a.dojo && (
                <div className="dojo" style={{ fontSize: isFs ? 13 : 11 }}>{a.dojo}</div>
              )}
            </div>
          ))}
        </div>
      )}

      {/* Fighting Spirit awards — distinct section, independent of the podium */}
      <FightingSpiritSection fsAwards={c?.fightingSpiritAwards} isFs={isFs} />
    </div>
  );
}

// FightingSpiritSection renders the 🔥 Fighting Spirit subsection inside
// AwardsView. Distinct from the placement podium — never merged into
// deriveAwards/podium logic. Renders nothing when the list is empty/absent.
function FightingSpiritSection({ fsAwards, isFs }) {
  if (!fsAwards || fsAwards.length === 0) return null;
  return (
    <div
      style={{ marginTop: 24, borderTop: "1px solid var(--line)", paddingTop: 16 }}
      data-testid="fighting-spirit-section"
    >
      <div
        className="section-title"
        style={{ margin: "0 0 12px", fontSize: isFs ? 22 : 15 }}
      >
        🔥 Fighting Spirit
      </div>
      <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
        {fsAwards.map((a, idx) => (
          <div
            key={idx}
            style={{
              padding: isFs ? "14px 20px" : "10px 14px",
              borderRadius: 8,
              background: "var(--accent-soft, #fff7ed)",
              border: "1px solid var(--accent-warm, #fb923c)",
              display: "flex",
              flexDirection: "column",
              gap: 2,
            }}
            data-testid={`fighting-spirit-award-${idx}`}
          >
            <div style={{ fontSize: isFs ? 13 : 11, fontWeight: 600, color: "var(--ink-3)", textTransform: "uppercase", letterSpacing: "0.04em" }}>
              {a.title}
            </div>
            <div style={{ fontSize: isFs ? 22 : 16, fontWeight: 700, color: "var(--ink-1)" }}>
              {a.recipientName}
            </div>
            {a.recipientDojo && (
              <div style={{ fontSize: isFs ? 14 : 12, color: "var(--ink-3)" }}>
                {a.recipientDojo}
              </div>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}

// Tournament-wide schedule wrapper for the viewer (its own screen)
function ViewerSchedule({ tournament, onBack, tweaks }) {
  const [selectedMatch, setSelectedMatch] = useState(null);
  const extendedTweaks = { ...tweaks, onMatchClick: setSelectedMatch };
  return (
    <div className="viewer">
      <div className="viewer__shell">
        <div className="viewer__head">
          <button className="viewer__back" onClick={onBack} aria-label="Back">←</button>
          <div className="viewer__title-block">
            <div className="viewer__eyebrow">{tournament.name}</div>
            <div className="viewer__title">Schedule</div>
            <div className="viewer__sub">All matches across courts and competitions</div>
          </div>
        </div>
        <div className="viewer__body">
          <ScheduleViewer tournament={tournament} tweaks={extendedTweaks} />
          {window.VersionFooter && <window.VersionFooter />}
        </div>
      </div>
      {selectedMatch && <MatchViewerModal match={selectedMatch} onClose={() => setSelectedMatch(null)} tournament={tournament} />}
    </div>
  );
}

function MatchViewerModal({ match, onClose, tournament, compId: defaultCompId }) {
  window.useEscapeToClose(onClose);
  const [scoringMatch, setScoringMatch] = useState(null);
  if (!match) return null;
  const isSelfRun = tournament && tournament.mode === "self-run";
  const bothSidesReady = window.hasBothSides ? window.hasBothSides(match) : false;
  const isFinalized = match.status === "completed";

  if (scoringMatch && window.ScoreEditorModal) {
    return React.createElement(window.ScoreEditorModal, {
      match: scoringMatch,
      onClose: () => setScoringMatch(null),
      onSubmit: async (patch) => {
        try {
          await window.API.recordScore(scoringMatch.compId || defaultCompId, scoringMatch.id, patch, "", scoringMatch);
          setScoringMatch(null);
          onClose();
        } catch (_err) {
          // leave modal open so the error is visible
        }
      },
      password: "",
      selfReport: true,
    });
  }

  return (
    <div className="modal-backdrop" onClick={onClose} style={{ zIndex: 1000, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
      <div onClick={e => e.stopPropagation()} style={{ width: "100%", maxWidth: 500, margin: 16 }}>
        {/* Reuse the canonical MatchDetailCard so the modal and the inline
            card render identically (DRY): same header, colour badges and
            BoutSubRow team grid. The modal adds only the self-run scoring. */}
        <MatchDetailCard match={match} onClose={onClose} />
        {isSelfRun && bothSidesReady && (
          <div className="card" style={{ marginTop: 12, padding: 16 }}>
            {isFinalized ? (
              <div style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 13, color: "var(--ink-3)" }}>
                <span style={{ background: "var(--bg-2)", padding: "4px 10px", borderRadius: 4, fontSize: 12 }}>Result reported</span>
                <span>Contact the organizer to correct this result.</span>
              </div>
            ) : (
              <button
                className="btn btn--primary btn--sm"
                onClick={() => setScoringMatch({ ...match, id: match.id })}
              >
                Report result
              </button>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// NotificationSettings — viewer settings panel for browser push notifications.
// Exported for unit testing.
// ---------------------------------------------------------------------------

// LocalStorage key for the notification opt-in toggle.
const LS_NOTIFICATIONS_ENABLED = "viewer.notifications.enabled";

// Pure helper: detect Notification API support.
// Exported for unit testing.
export function notificationSupported() {
  return typeof Notification !== "undefined";
}

// NotificationSettings — "Enable browser notifications" toggle for the
// viewer home settings area. Phases:
//   1) Secure-context warning (edge case, normally hidden in production).
//   2) Notification API unavailable — hide the toggle.
//   3) Permission "denied" — show blocked state.
//   4) Permission "default" or "granted" — show the opt-in toggle.
//
// Requests permission ONLY on a user click (the gesture gate). Never
// calls requestPermission() automatically. Exported for unit testing.
export function NotificationSettings() {
  // mp-4fd: chime preference for the on-deck match alert.
  const [chimeMuted, toggleChimeMuted] = useChimeMuted();

  // Compute the initial permission outside useState so tests using the
  // static React mock (which passes the value through unmodified rather
  // than calling function initialisers) see the correct starting value.
  const initialPermission = (typeof Notification === "undefined")
    ? "unavailable"
    : Notification.permission;

  // Read the current permission state reactively: re-query after the user
  // interacts with the native permission prompt. We keep a local state so
  // the UI stays responsive without relying on a global re-render.
  const [permission, setPermission] = useState(initialPermission);

  let storedOptIn = false;
  try {
    storedOptIn = window.localStorage.getItem(LS_NOTIFICATIONS_ENABLED) === "true";
  } catch (_e) { /* storage unavailable */ }
  // Only treat the opt-in as enabled when the browser permission is ALSO
  // still granted. If the user reset the site permission back to "default"
  // (or "denied") since they last opted in, starting `enabled` at true would
  // render the checkbox unchecked (checked={enabled && permission==="granted"})
  // yet send the first click down the "turning off" branch — the user would
  // have to click twice and never see the prompt. Gating on granted keeps the
  // handler branch aligned with the visible checkbox state.
  const [enabled, setEnabled] = useState(storedOptIn && initialPermission === "granted");

  // Phase 4: secure-context warning. In production the TLS proxy makes this
  // false; it only matters for bare http:// (no proxy) access.
  const insecure = typeof window !== "undefined" && window.isSecureContext === false;

  // Phase 3 / Phase 4 ordering: when the API is unavailable we normally hide
  // the panel entirely. BUT some browsers expose `Notification` only in a
  // secure context, so a bare http:// page can have BOTH no API AND
  // isSecureContext === false — which is the exact situation this panel is
  // meant to explain. In that case render the warning (no toggle) instead of
  // hiding. Only hide outright when the API is unavailable for some OTHER
  // reason (secure context but an old/unsupported browser).
  // mp-4fd: the chime toggle uses WebAudio, not the Notification API, so it
  // must remain visible even when browser notifications are unavailable.
  const chimeToggle = (
    <label style={{ display: "flex", alignItems: "center", gap: 10, cursor: "pointer", fontSize: 13, marginTop: 10 }}>
      <input
        type="checkbox"
        checked={!chimeMuted}
        onChange={toggleChimeMuted}
        data-testid="chime-toggle"
      />
      <span>Play a sound when your match is up next</span>
    </label>
  );

  if (permission === "unavailable") {
    if (!insecure) {
      // Notification API unavailable but context is secure — hide the
      // notification toggle but still show the chime toggle.
      return (
        <div className="card" data-testid="notification-settings" style={{ marginBottom: 16, padding: 14 }}>
          <div className="section-title" style={{ marginTop: 0 }}>Match alerts</div>
          {chimeToggle}
        </div>
      );
    }
    return (
      <div className="card" data-testid="notification-settings" style={{ marginBottom: 16, padding: 14 }}>
        <div className="section-title" style={{ marginTop: 0 }}>Notifications</div>
        <div style={{ fontSize: 12, color: "var(--amber, #b45309)" }} data-testid="notification-insecure-warning">
          Browser notifications require a secure connection (https or localhost).
        </div>
        {chimeToggle}
      </div>
    );
  }

  const handleToggle = async () => {
    if (enabled) {
      // Turning off: just persist the preference.
      try { window.localStorage.setItem(LS_NOTIFICATIONS_ENABLED, "false"); } catch (_e) { /* storage unavailable */ }
      setEnabled(false);
      return;
    }
    // Turning on: request permission first (this is the user-gesture gate).
    if (Notification.permission === "default") {
      const result = await Notification.requestPermission();
      setPermission(result);
      if (result !== "granted") return; // user denied — don't toggle on
    } else {
      setPermission(Notification.permission);
    }
    // Only mark the toggle ON if the preference actually persisted.
    // fireBrowserNotifications() reads this localStorage flag at fire time, so
    // an unpersisted "on" would render a checked box that never fires (storage
    // throwing → flag missing → firing path reads opt-out). Keep the UI honest
    // with the firing path: if persistence failed, leave the toggle off.
    let persisted = false;
    try {
      window.localStorage.setItem(LS_NOTIFICATIONS_ENABLED, "true");
      persisted = true;
    } catch (_e) { /* storage unavailable — keep toggle off */ }
    setEnabled(persisted);
  };

  const denied = permission === "denied";

  return (
    <div className="card" data-testid="notification-settings" style={{ marginBottom: 16, padding: 14 }}>
      <div className="section-title" style={{ marginTop: 0 }}>Notifications</div>
      {insecure && (
        <div style={{ fontSize: 12, color: "var(--amber, #b45309)", marginBottom: 8 }} data-testid="notification-insecure-warning">
          Browser notifications require a secure connection (https or localhost).
        </div>
      )}
      {denied ? (
        <div style={{ fontSize: 12, color: "var(--ink-3)" }} data-testid="notification-denied">
          Browser notifications are blocked. Allow them in your browser settings, then reload.
        </div>
      ) : (
        <label style={{ display: "flex", alignItems: "center", gap: 10, cursor: "pointer", fontSize: 13 }}>
          <input
            type="checkbox"
            checked={enabled && permission === "granted"}
            onChange={handleToggle}
            data-testid="notification-toggle"
            disabled={insecure}
          />
          <span>
            Enable browser notifications for announcements
            {permission === "granted" && enabled && (
              <span style={{ marginLeft: 6, fontSize: 11, color: "var(--ink-3)" }}>(granted)</span>
            )}
            {permission === "default" && (
              <span style={{ marginLeft: 6, fontSize: 11, color: "var(--ink-3)" }}>(permission not yet requested)</span>
            )}
          </span>
        </label>
      )}
      {/* mp-4fd: chime opt-out for the on-deck match alert (shared variable). */}
      {chimeToggle}
    </div>
  );
}

// Shared formatter so the synchronous initializer and the useEffect tick
// produce identical strings — keeps the first paint stable.
function formatAnnouncementTimeLeft(expiresAtIso) {
  const diff = new Date(expiresAtIso).getTime() - Date.now();
  if (diff <= 0) return "";
  const totalSeconds = Math.floor(diff / 1000);
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  const paddedSeconds = seconds.toString().padStart(2, "0");
  return minutes > 0 ? `${minutes}:${paddedSeconds} left` : `${seconds}s left`;
}

// AnnouncementCard — renders a single announcement card with its own
// independent per-card countdown and auto-dismiss timer.
// Exported for unit testing; consumed only by AnnouncementBanner below.
function AnnouncementCard({ ann, onDismiss }) {
  const [timeLeft, setTimeLeft] = useState(() => formatAnnouncementTimeLeft(ann.expiresAt));

  useEffect(() => {
    // intervalId and dismissed are captured in the closure so updateTimer can
    // self-clear the interval on expiry and guard against repeated onDismiss
    // calls if React state updates are delayed before cleanup runs.
    let intervalId;
    let dismissed = false;
    const updateTimer = () => {
      const diff = new Date(ann.expiresAt).getTime() - Date.now();
      if (diff <= 0) {
        clearInterval(intervalId);
        if (!dismissed) {
          dismissed = true;
          onDismiss(ann.id);
        }
        return;
      }
      setTimeLeft(formatAnnouncementTimeLeft(ann.expiresAt));
    };
    updateTimer();
    intervalId = setInterval(updateTimer, 1000);
    return () => clearInterval(intervalId);
  }, [ann.id, ann.expiresAt, onDismiss]);

  return (
    <div className="announcement-banner">
      <div className="announcement-banner__content">
        <div className="announcement-banner__icon" aria-hidden="true">📢</div>
        <p className="announcement-banner__message">{ann.message}</p>
      </div>
      <div className="announcement-banner__meta">
        <span className="announcement-banner__badge">{timeLeft}</span>
        <button
          className="announcement-banner__dismiss"
          onClick={() => onDismiss(ann.id)}
          aria-label="Dismiss announcement"
        >
          &times;
        </button>
      </div>
    </div>
  );
}

// AnnouncementBanner — fixed-position overlay that stacks ALL active
// announcements as independent cards. Does NOT rotate; each card owns
// its own countdown and auto-dismiss timer. Public props unchanged so
// app.jsx needs no edit.
function AnnouncementBanner({ announcements, onDismiss }) {
  const list = announcements || [];
  if (list.length === 0) return null;

  return (
    <div className="announcement-overlay" role="region" aria-label="Announcements">
      {list.map(ann => (
        <AnnouncementCard key={ann.id} ann={ann} onDismiss={onDismiss} />
      ))}
    </div>
  );
}

// ---------------------------------------------------------------------------
// buildAllWinnersPublic — public-viewer equivalent of admin_shell's
// buildAllWinners. Thin orchestrator: filter completed comps (excluding linked
// playoffs shells whose sourceCompID marks them as driven by a parent mixed
// competition), resolve each through resolveCompetitionAwards.
// Exported to window so AllWinnersView and tests can reach it.
// ---------------------------------------------------------------------------
async function buildAllWinnersPublic(comps, fetchers) {
  const completed = (comps || []).filter((c) => c.status === "completed" && !c.sourceCompID);
  const results = await Promise.all(
    completed.map(async (comp) => {
      try {
        const { state, podium } = await resolveCompetitionAwards(comp, fetchers);
        return { comp, state, podium };
      } catch (err) {
        return { comp, state: "error", podium: [], error: err?.message || String(err) };
      }
    })
  );
  return results;
}

// ---------------------------------------------------------------------------
// AllWinnersView — full-page public results summary.  Mirrors the admin
// AllWinnersModal rendering but as a full-page view (not a modal), matching
// the ViewerSchedule / GlossaryPage page pattern. Props: { tournament, onBack, tweaks }.
// ---------------------------------------------------------------------------
function AllWinnersView({ tournament, onBack, tweaks }) {
  const comps = (tournament && tournament.competitions) || [];
  const [viewState, setViewState] = useState({ loading: true, results: [], error: null });

  // Stable signature of every comp's id:status — triggers refetch when any
  // competition completes or its knockout resolves (same pattern as admin modal).
  const compsSig = comps.map((c) => `${c.id}:${c.status}`).join("|");

  useEffect(() => {
    let cancelled = false;
    setViewState((s) => ({ ...s, loading: true }));
    buildAllWinnersPublic(comps, {
      fetchCompetitionDetails: window.API.fetchCompetitionDetails.bind(window.API),
      swissStandings: window.API.swissStandings ? window.API.swissStandings.bind(window.API) : null,
    }).then((results) => {
      if (!cancelled) setViewState({ loading: false, results, error: null });
    }).catch((err) => {
      if (!cancelled) setViewState({ loading: false, results: [], error: err?.message || String(err) });
    });
    return () => { cancelled = true; };
  }, [compsSig]);

  return (
    <div className="viewer">
      <div className="viewer__shell">
        <div className="viewer__head">
          <button className="viewer__back" onClick={onBack} aria-label="Back">←</button>
          <div className="viewer__title-block">
            <div className="viewer__eyebrow">{tournament && tournament.name}</div>
            <div className="viewer__title">Results</div>
            <div className="viewer__sub">All competition placings</div>
          </div>
        </div>
        <div className="viewer__body">
          {viewState.loading && (
            <div style={{ textAlign: "center", color: "var(--ink-3)", padding: "24px 0" }} data-testid="all-winners-loading">
              Loading results…
            </div>
          )}
          {!viewState.loading && viewState.error && (
            <div style={{ color: "var(--red)", padding: "8px 0" }} data-testid="all-winners-error">
              Failed to load results: {viewState.error}
            </div>
          )}
          {!viewState.loading && !viewState.error && viewState.results.length === 0 && (
            <div className="empty" data-testid="all-winners-empty">
              <div className="icon">🏅</div>
              <h3>No completed competitions</h3>
              <div style={{ fontSize: 13 }}>Check back once competitions have finished.</div>
            </div>
          )}
          {!viewState.loading && viewState.results.map(({ comp, state: compState, podium, error: compErr }) => (
            <div key={comp.id} className="card" style={{ padding: "12px 16px", marginBottom: 12 }} data-testid={`all-winners-card-${comp.id}`}>
              <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 8 }}>
                <div style={{ fontWeight: 700, fontSize: 15 }}>{comp.name}</div>
                <div style={{ fontSize: 12, color: "var(--ink-3)", background: "var(--surface-2, #f0f0f0)", borderRadius: 4, padding: "1px 6px" }}>
                  {competitionKindLabel(comp)}
                </div>
              </div>
              {compErr && (
                <div style={{ fontSize: 13, color: "var(--red)" }} data-testid={`all-winners-error-${comp.id}`}>
                  Could not load results: {compErr}
                </div>
              )}
              {!compErr && compState === "in-progress" && (
                <div style={{ fontSize: 13, color: "var(--ink-3)" }} data-testid={`all-winners-inprogress-${comp.id}`}>
                  Knockout in progress
                </div>
              )}
              {!compErr && compState === "final" && podium.length === 0 && (
                <div style={{ fontSize: 13, color: "var(--ink-3)" }}>No results yet</div>
              )}
              {!compErr && compState === "final" && podium.map((a, idx) => {
                const style = PLACE_STYLE[a.place] || PLACE_STYLE[3];
                return (
                  <div
                    key={`${a.place}-${a.name}-${idx}`}
                    style={{ display: "flex", alignItems: "center", gap: 10, padding: "6px 0", borderBottom: idx < podium.length - 1 ? "1px solid var(--line)" : "none" }}
                    data-testid={`all-winners-place-${comp.id}-${a.place}-${idx}`}
                  >
                    <span style={{ fontSize: 22 }}>{style.icon}</span>
                    <span style={{ fontSize: 12, color: "var(--ink-3)", minWidth: 60 }}>{style.label}</span>
                    <div style={{ minWidth: 0 }}>
                      <div style={{ fontWeight: 600, fontSize: 14 }}>{a.name}</div>
                      {tweaks.showDojo && a.dojo && <div style={{ fontSize: 12, color: "var(--ink-3)" }}>{a.dojo}</div>}
                    </div>
                  </div>
                );
              })}
              {/* Fighting Spirit awards: an independent annotation (not the
                  placement podium), shown for any competition that has them
                  regardless of podium state. Self-guards when empty. */}
              <FightingSpiritSection fsAwards={comp.fightingSpiritAwards} isFs={false} />
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

window.AnnouncementCard = AnnouncementCard;
window.AnnouncementBanner = AnnouncementBanner;
window.ViewerHome = ViewerHome;
window.ViewerCompetition = ViewerCompetition;
window.isFollowedPlayer = isFollowedPlayer;
window.ViewerSchedule = ViewerSchedule;
window.ScheduleViewer = ScheduleViewer;
window.SwissStandingsViewer = SwissStandingsViewer;
window.competitionKindLabel = competitionKindLabel;
window.compMatches = compMatches;
window.tournamentMatches = tournamentMatches;
window.currentMatchOf = currentMatchOf;
window.NotificationSettings = NotificationSettings;
window.LS_NOTIFICATIONS_ENABLED = LS_NOTIFICATIONS_ENABLED;
// mp-s1gl: expose link-base helpers for admin_shell.jsx / admin_schedule.jsx
// (those files don't ES-import viewer.jsx; they pick globals off window).
window.linkBase = linkBase;
window.isNonPublicOrigin = isNonPublicOrigin;
// mp-koqh: public results page.
window.buildAllWinnersPublic = buildAllWinnersPublic;
window.AllWinnersView = AllWinnersView;
export { shouldShowRegister };

