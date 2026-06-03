// Viewer side — mobile-first. Single tournament. Shows competitions as the home;
// each competition opens to its own Overview/Bracket/Pools/Schedule/Results.

const { useState, useMemo, useRef: useRefV, useEffect } = React;
const StatusBadge = window.StatusBadge;
const formatDate = window.formatDate;
const formatLabel = window.formatLabel;

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

// Local mirrors of display.jsx::queueLabel / queueLabelCompact.
// display.js loads before viewer.js (see index.html), so window.queueLabel /
// window.queueLabelCompact are normally available on first render.  These
// serve as defense-in-depth if that ever changes.
function _localQueueLabel(m) {
  if (!m || m.status !== "scheduled") return "";
  const qp = Number(m.queuePosition);
  if (Number.isFinite(qp) && qp > 0) return qp === 1 ? "Next up" : `${qp - 1} before yours`;
  if (m.scheduledAt) return `Scheduled ${m.scheduledAt}`;
  return "";
}
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
    out.push({ phase: "pool", poolName: derivedPool, phaseName: derivedPool, ...m, compId: c.id, compName: c.name, compKind: isDH ? "" : c.kind, teamSize: isDH ? 0 : c.teamSize });
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
    compId: c.id,
    compName: c.name,
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
      const titlePrefix = m.status === "running" ? "🔴 LIVE NOW — " : "(1) Your match is next — ";
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
      const notifTitle = m.status === "running" ? "Your match is LIVE" : "Your match is next";
      window.fireNotification(notifTitle, body, { tag: "match-" + m.id });
    }

    // 4. Notify consumer (e.g. to show/update the banner).
    if (typeof onAlert === "function") onAlert(m);
  });
}

// Banner component: rendered when the followed match is on-deck.
function MyMatchAlertBanner({ match, onView, onDismiss }) {
  if (!match) return null;
  const kind = match.status === "running" ? "LIVE NOW" : "Next up";
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

function ViewerHome({ tournament, onSelectCompetition, onAdminClick, onOpenSchedule, onRegister }) {
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
  // Live-comp set: gates the home LIVE NOW / Up-next strips and the live dot.
  // BOTH setup and draw-ready are excluded — a draw-ready comp has a published
  // draw but no match has been called, so it is NOT live. (The competition
  // detail view still shows its Pools/Bracket tabs; that is governed separately
  // by isPreStart in ViewerCompetition.)
  const liveCompIds = useMemo(() => new Set((t.competitions || []).filter(c => c.status && c.status !== "setup" && c.status !== "draw-ready").map(c => c.id)), [t.competitions]);
  // Apply hasBothSides here too — pre-fix, a bracket match marked
  // `running` while one side was still an unresolved "Winner of rX-mY"
  // placeholder would appear in the public LIVE NOW strip, even though
  // the upcoming list / cards / detail view all reject placeholder
  // sides. Mirrors the upNext filter below.
  const live = allMatches.filter((m) => m.status === "running" && hasBothSides(m) && liveCompIds.has(m.compId) && (courtFilter === "all" || m.court === courtFilter));
  let upNext = allMatches.filter((m) => m.status === "scheduled" && hasBothSides(m) && liveCompIds.has(m.compId) && (courtFilter === "all" || m.court === courtFilter));
  if (courtFilter === "all") upNext = upNext.slice(0, 3);

  // FR-020 / FR-022: derive my-player's next match across all competitions
  // for the activated "Your next match" card (T112).
  const myNextMatch = useMemo(() => {
    if (!followedPlayer || !followedPlayer.id) return null;
    const mine = buildPlayerMatchHighlight(followedPlayer.id, allMatches, followedPlayer.name)
      .filter(hasBothSides)
      .filter((m) => m.status !== "completed");
    mine.sort((a, b) => {
      // Live ahead of scheduled — a followed player mid-match should be
      // the top thing the viewer sees, not their *next* scheduled fight.
      const ao = a.status === "running" ? 0 : 1;
      const bo = b.status === "running" ? 0 : 1;
      if (ao !== bo) return ao - bo;
      return (a.scheduledAt || "99:99").localeCompare(b.scheduledAt || "99:99");
    });
    return mine[0] || null;
  }, [followedPlayer, allMatches]);

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
          <img src="/logo.jpeg" alt="Kendo Tournament Logo" className="topbar__logo viewer__logo" decoding="async" />
          <div className="viewer__title-block">
            <div className="viewer__eyebrow">{formatDate(t.date)} · {t.venue}</div>
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
              <div className="hero-live__lbl"><span className="dot dot--live"></span> LIVE NOW · {pluralize(live.length, "match", "matches")}</div>
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

          {dates.length === 0 ? (
            <>
              <div className="section-title">Competitions</div>
              <div className="vlist">
                <div className="empty">
                  <div className="icon">⏳</div>
                  <h3>No competitions yet</h3>
                  <div style={{ fontSize: 13 }}>Check back soon for the tournament schedule and live updates.</div>
                </div>
              </div>
            </>
          ) : dates.map((d) => (
            <div key={d}>
              <div className="section-title">{formatDate(d)}</div>
              <div className="vlist">
                {compsByDate[d].map((c) => {
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
                            <div className="vlist-item__eyebrow">{competitionKindLabel(c)}{c.teamSize ? ` · ${c.teamSize}-person` : ""}</div>
                            <div className="vlist-item__name">{c.name}</div>
                            <div className="vlist-item__meta">
                              {c.players.length} {c.kind === "team" ? "teams" : "players"} · {formatLabel(c.format)} · Starts {c.startTime}
                            </div>
                          </div>
                          <StatusBadge status={c.status} showLiveDot />
                        </div>
                        {c.status && c.status !== "setup" && c.status !== "draw-ready" && total > 0 && (
                          <div className="vlist-item__progress">
                            <div className="vlist-item__bar"><div style={{ width: pct + "%" }}></div></div>
                            <div className="vlist-item__pct">
                              {liveCount > 0 ? <span style={{ color: "var(--red)", fontWeight: 600 }}>● {liveCount} live</span> : pluralize(done, "match", "matches") + " / " + total}
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

          {/* T069 / FR-016: "Display modes" — link cards into the public
              /display routes for TV screens, lobby boards, and OBS browser
              sources. Each link opens in a new tab so the host page
              (viewer) keeps its tab and stays interactive. Active courts
              come from tournament.courts (the full configured list); the
              "all-courts overview" card is always present. Placed below
              Up Next so a viewer who has already glanced at their next
              match can drop into the display mode they need. */}
          <DisplayModes tournament={t} />

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
//   - status==="running"                       → null (round label already shows " · LIVE NOW")
//   - anything else (completed/forfeit/cancelled, or no qp)  → null (hide chip)
//
// Wording mirrors the VSchedItem helper below and display.jsx::queueLabel
// so all three viewer surfaces agree. Running matches return null because
// the my-match__round label already appends " · LIVE NOW" — rendering it
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
  const phaseLabel = nextMatch.phase === "pool" ? nextMatch.poolName : (nextMatch.round || "Bracket");
  // FR-025: queue position is 1-indexed per court for scheduled matches; 0 for
  // running/completed. Treat null/undefined/0 as "don't render" so we stay
  // gracefully empty for non-queued matches and pre-T046 responses. Wording
  // ("Next up" / "N before yours") mirrors VSchedItem below and display.jsx
  // so all three viewer surfaces agree. Running matches show null here
  // because the round label already appends " · LIVE NOW".
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
        {nextMatch.status === "running" ? " · LIVE NOW" : ""}
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
// Renders one card per configured court plus an "all-courts overview" card.
// Each card opens in a new tab so the operator's viewer session stays open.
// Lives in viewer.jsx (not display.jsx) because it's a viewer-side surface
// that consumes the display routes rather than rendering them.
function DisplayModes({ tournament }) {
  const courts = (tournament && tournament.courts) || [];
  // No court list — render nothing rather than a confusing single "all"
  // card. The tournament setup flow guarantees ≥1 court in practice; this
  // guard exists for the very-early-bootstrap window before tournament
  // data has loaded fully.
  if (courts.length === 0) return null;
  return (
    <>
      <div className="section-title" style={{ marginTop: 20 }}>Display modes</div>
      <div className="vlist" data-testid="viewer-home-display-modes">
        {courts.map((cc) => (
          <a
            key={cc}
            className="vlist-item vlist-item--row"
            href={`/display?court=${encodeURIComponent(cc)}`}
            target="_blank"
            rel="noopener noreferrer"
            style={{ textDecoration: "none" }}
          >
            <span className="vlist-item__icon">📺</span>
            <div className="vlist-item__rowbody">
              <div className="vlist-item__rowtitle"><TermV name="shiaijo">Shiaijo</TermV> {cc} display</div>
              <div className="vlist-item__rowsub">Fullscreen board for a single court · opens in a new tab</div>
            </div>
            <span className="vlist-item__rowchev">→</span>
          </a>
        ))}
        {/* Per-court OBS/vMix overlay entry (previously URL-only). Links to the
            transparent lower-third streaming overlay so operators can grab the
            browser-source URL per shiaijo straight from the UI. */}
        {courts.map((cc) => (
          <a
            key={`overlay-${cc}`}
            className="vlist-item vlist-item--row"
            href={`/display?court=${encodeURIComponent(cc)}&overlay=1`}
            target="_blank"
            rel="noopener noreferrer"
            style={{ textDecoration: "none" }}
          >
            <span className="vlist-item__icon">🎥</span>
            <div className="vlist-item__rowbody">
              <div className="vlist-item__rowtitle"><TermV name="shiaijo">Shiaijo</TermV> {cc} streaming overlay</div>
              <div className="vlist-item__rowsub">Transparent lower-third for OBS / vMix · opens in a new tab</div>
            </div>
            <span className="vlist-item__rowchev">→</span>
          </a>
        ))}
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
      </div>
    </>
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

function ViewerCompetition({ tournament, competition, pools, poolMatches, standings, bracket, onBack, onSelectCompetition, tweaks }) {
  const [tab, setTab] = useState("overview");
  const c = competition;

  // Phase 2 (mp-rrd) — link a pools/mixed comp to its separate playoffs comp
  // and vice-versa. A mixed tournament is TWO competitions: the pools/mixed
  // comp (holds the pools) and a playoffs comp created via POST /playoffs that
  // carries a `sourceCompID` back-reference. Surface a one-tap affordance so a
  // spectator landing on one can jump to the other under the same tournament.
  const linkedComp = useMemo(() => {
    const all = (tournament && tournament.competitions) || [];
    // This comp IS a playoffs comp → link to its source pools/mixed comp.
    if (c.sourceCompID) {
      const src = all.find(x => x.id === c.sourceCompID);
      if (src) return { comp: src, role: "pools" };
    }
    // This comp is a pools/mixed comp → link to the playoffs comp that
    // references it (if one has been created yet).
    const playoffs = all.find(x => x.sourceCompID === c.id);
    if (playoffs) return { comp: playoffs, role: "playoffs" };
    return null;
  }, [tournament, c.id, c.sourceCompID]);

  const allMatches = useMemo(() => {
    const out = [];
    if (pools) {
        pools.forEach((p) => {
            const matches = poolMatches ? poolMatches.filter(m => m.id.startsWith(p.poolName + "-")) : [];
            matches.forEach((m) => {
                const isDH = isPoolDaihyosenID(m.id || "");
                out.push({ ...m, phase: "pool", phaseName: p.poolName, poolName: p.poolName, compKind: isDH ? "" : c.kind, teamSize: isDH ? 0 : c.teamSize });
            });
        });
    }
    if (bracket && bracket.rounds) {
        bracket.rounds.forEach((round, ri) => {
            round.forEach((m) => out.push({ ...m, phase: "bracket", round: window.roundLabel(ri, bracket.rounds.length), phaseName: window.roundLabel(ri, bracket.rounds.length), compKind: c.kind, teamSize: c.teamSize }));
        });
    }
    return out;
  }, [pools, poolMatches, bracket, c.kind, c.teamSize]);

  const liveMatches = allMatches.filter((m) => m.status === "running" && hasBothSides(m));
  const upcomingMatches = allMatches.filter((m) => m.status === "scheduled" && hasBothSides(m)).slice(0, 3);
  const recentMatches = allMatches.filter((m) => m.status === "completed" && m.winner).slice(-5).reverse();

  // pick a "my match" — placeholder for now
  const myPlayer = null;
  const myUpcoming = null;

  const derivedBracket = useMemo(() => {
    if (bracket && bracket.rounds && bracket.rounds.length > 0) return bracket;
    if (c.format === "mixed" && pools && pools.length > 0) {
      const placeholders = [];
      const winners = c.poolWinners || 2;
      pools.forEach(p => {
        for(let i=0; i<winners; i++) {
           let rank = i===0?'1st':i===1?'2nd':i===2?'3rd':(i+1)+'th';
           placeholders.push({ id: `tbd-${p.poolName}-${i}`, name: `${p.poolName} ${rank}`, dojo: "", seed: null });
        }
      });
      return { rounds: window.buildBracket(placeholders, c.courts) };
    }
    return null;
  }, [bracket, c, pools]);

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
  const tabs = [
    { id: "overview", label: "Overview" },
    isSwiss ? { id: "swiss", label: "Standings" } : null,
    hasPools && !isSwiss ? { id: "pools", label: "Pools" } : null,
    hasBracket && !isSwiss ? { id: "bracket", label: "Bracket" } : null,
    c.status === "completed" ? { id: "results", label: "Awards" } : null,
  ].filter(Boolean);

  const currentMatch = useMemo(() => {
      const live = allMatches.find((m) => m.status === "running" && hasBothSides(m));
      if (live) return live;
      const sched = allMatches.filter((m) => m.status === "scheduled" && hasBothSides(m));
      sched.sort((a, b) => (a.scheduledAt || "99:99").localeCompare(b.scheduledAt || "99:99"));
      return sched[0] || null;
  }, [allMatches]);

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
          <StatusBadge status={c.status} showLiveDot />
        </div>
        <div className="viewer__tabs">
          {tabs.map((tb) => (
            <button key={tb.id} className={`viewer__tab ${tab === tb.id ? "is-active" : ""}`} onClick={() => setTab(tb.id)}>
              {tb.label}
            </button>
          ))}
        </div>
        <div className="viewer__body">
          {linkedComp && onSelectCompetition && (
            // Phase 2 (mp-rrd): pools <-> playoffs cross-link. A mixed
            // tournament splits across two competitions; this lets a
            // spectator hop between the pool draw and the knockout bracket.
            <button
              className="vlist-item vlist-item--row"
              style={{ marginBottom: 12, width: "100%" }}
              onClick={() => onSelectCompetition(linkedComp.comp.id)}
            >
              <span className="vlist-item__icon">{linkedComp.role === "playoffs" ? "🏆" : "👥"}</span>
              <div className="vlist-item__rowbody">
                <div className="vlist-item__rowtitle">
                  {linkedComp.role === "playoffs" ? "View the playoffs bracket" : "View the pools"}
                </div>
                <div className="vlist-item__rowsub">{linkedComp.comp.name}</div>
              </div>
              <span className="vlist-item__rowchev">→</span>
            </button>
          )}
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
            <PoolsViewer pools={pools} standings={standings} poolMatches={poolMatches} tweaks={tweaks} competition={c} onMatchClick={setSelectedMatch} />
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
  const aName = match.sideA?.name || "TBD";
  const bName = match.sideB?.name || "TBD";
  const aWin = match.winner?.id === match.sideA?.id && match.winner?.id;
  const bWin = match.winner?.id === match.sideB?.id && match.winner?.id;
  const isLive = match.status === "running";
  const isDone = match.status === "completed";
  // Bracket matches use scoreA/scoreB strings; derive ippons arrays with the
  // same fallback used in VSchedItem so the score display is consistent.
  const mdcIpponsA = match.ipponsA || window.ipponsFromScore(match.scoreA);
  const mdcIpponsB = match.ipponsB || window.ipponsFromScore(match.scoreB);

  return (
    <div className="match-detail-card">
      <div className="match-detail-card__head">
        <div className="match-detail-card__meta">
          <span><TermV name="shiaijo">Shiaijo</TermV> {match.court}</span>
          <span>·</span>
          <span>{match.phase === "pool" ? match.poolName : (match.round || "")}</span>
          {match.scheduledAt && <><span>·</span><span>{match.scheduledAt}</span></>}
        </div>
        <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
          {isLive && <span className="bc-live">● LIVE</span>}
          {isDone && <span style={{ fontSize: 11, color: "var(--ink-3)", fontWeight: 600 }}>FINAL</span>}
          {onClose && <button className="match-detail-card__close" onClick={onClose} aria-label="Close">×</button>}
        </div>
      </div>

      <div className="match-detail-card__players">
        <div className={`match-detail-card__side ${bWin ? "match-detail-card__side--win" : ""}`}>
          <span className="match-detail-card__color-badge match-detail-card__color-badge--shiro">SHIRO</span>
          <span className="match-detail-card__name">{bName}</span>
        </div>
        <div className="match-detail-card__score">
          {isDone
            ? <span>{window.formatIpponsScore(mdcIpponsB, mdcIpponsA, match.score, match.decision, match.encho, match.decidedByHantei) || "—"}</span>
            : <span className="match-detail-card__vs">vs</span>}
        </div>
        <div className={`match-detail-card__side match-detail-card__side--right ${aWin ? "match-detail-card__side--win" : ""}`}>
          <span className="match-detail-card__name">{aName}</span>
          <span className="match-detail-card__color-badge match-detail-card__color-badge--aka">AKA</span>
        </div>
      </div>

      {isDone && !isTeam && (
        <div className="match-detail-card__ippons">
          <div className="match-detail-card__ippons-side">
            <span className="match-detail-card__ippons-val">{mdcIpponsB.filter(x => x && x !== "•").join("") || "—"}</span>
            {match.hansokuB > 0 && <span className="match-detail-card__fouls">Fouls: {match.hansokuB}</span>}
          </div>
          <div className="match-detail-card__ippons-center">
            {match.decidedByHantei && <span className="match-detail-card__decision" data-testid="match-detail-hantei">Hantei</span>}
            {(window.isHikiwake(match.score?.type) || window.isHikiwake(match.decision)) && <span className="match-detail-card__decision">Draw</span>}
          </div>
          <div className="match-detail-card__ippons-side match-detail-card__ippons-side--right">
            <span className="match-detail-card__ippons-val">{mdcIpponsA.filter(x => x && x !== "•").join("") || "—"}</span>
            {match.hansokuA > 0 && <span className="match-detail-card__fouls">Fouls: {match.hansokuA}</span>}
          </div>
        </div>
      )}

      {isDone && isTeam && match.subResults && match.subResults.length > 0 && (
        <div className="match-detail-card__team-subs">
          {match.subResults.map((sub, i) => {
            const sA = (sub.ipponsA || []).filter(x => x && x !== "•").join("") || "—";
            const sB = (sub.ipponsB || []).filter(x => x && x !== "•").join("") || "—";
            return (
              <div key={i} className="match-detail-card__sub-row">
                <span className="match-detail-card__sub-score">{sB}</span>
                <span className="match-detail-card__sub-pos">
                  {subBoutLabel(sub, i)}
                  {sub.decidedByHantei && <span className="match-detail-card__decision" data-testid="sub-row-hantei" style={{ marginLeft: 6 }}>Hantei</span>}
                </span>
                <span className="match-detail-card__sub-score match-detail-card__sub-score--right">{sA}</span>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

function ViewerOverview({ c, myPlayer, myUpcoming, currentMatch, liveMatches, upcomingMatches, recentMatches, tweaks }) {
  const [expandedMatchId, setExpandedMatchId] = useState(null);

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
            : "Browse the Pools and Bracket tabs to see the draw."}
        </div>
      </div>
    );
  }

  const handleMatchClick = (m) => {
    setExpandedMatchId(prev => prev === m.id ? null : m.id);
  };

  return (
    <div>
      {myUpcoming && myPlayer ? (
        <div className="my-match">
          <div className="my-match__lbl">Your next match</div>
          <div className="my-match__name">{myPlayer.name}</div>
          <div className="my-match__round">
            {myUpcoming.phase === "pool" ? myUpcoming.poolName : myUpcoming.round}
            {myUpcoming.status === "running" ? " · LIVE NOW" : ""}
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

      {/* Current match — shown inline, before Up Next */}
      {currentMatch && currentMatch.status === "running" && (
        <div style={{ marginBottom: 12 }}>
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
            <span className="dot dot--live"></span> LIVE NOW · {liveMatches.length}
          </div>
          <div className="vsched">
            {liveMatches.filter(m => !currentMatch || m.id !== currentMatch.id).map((m) => (
              <React.Fragment key={m.id}>
                <VSchedItem m={m} tweaks={tweaks} onClick={() => handleMatchClick(m)} />
                {expandedMatchId === m.id && <MatchDetailCard match={m} onClose={() => setExpandedMatchId(null)} />}
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
                <VSchedItem m={m} tweaks={tweaks} onClick={() => handleMatchClick(m)} />
                {expandedMatchId === m.id && <MatchDetailCard match={m} onClose={() => setExpandedMatchId(null)} />}
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
                <VSchedItem m={m} tweaks={tweaks} onClick={() => setExpandedMatchId(prev => prev === m.id ? null : m.id)} />
                {expandedMatchId === m.id && <MatchDetailCard match={m} onClose={() => setExpandedMatchId(null)} />}
              </React.Fragment>
            ))}
          </div>
        </>
      )}
    </div>
  );
}

const VSchedItem = React.memo(({ m, tweaks, showCompetition, onClick }) => {
  const aWin = m.winner && m.sideA && m.winner.id === m.sideA.id;
  const bWin = m.winner && m.sideB && m.winner.id === m.sideB.id;
  // Bracket matches carry scoreA/scoreB strings rather than ipponsA/B arrays.
  // Fall back so the score string reflects per-side letters instead of the
  // orientation-agnostic winnerPts–loserPts that formatIpponsScore uses when
  // both ippon arrays are absent (which would invert left/right when AKA wins).
  const vIpponsA = m.ipponsA || window.ipponsFromScore(m.scoreA);
  const vIpponsB = m.ipponsB || window.ipponsFromScore(m.scoreB);
  const scoreStr = m.status === "completed" ? window.formatIpponsScore(vIpponsB, vIpponsA, m.score, m.decision, m.encho, m.decidedByHantei) : null;
  // FR-025: queue position is 1-indexed per court for scheduled matches;
  // running/completed are 0 (set server-side, omitempty in JSON → undefined
  // on older payloads). Treat null/undefined/0 as "don't render" so the UI
  // stays gracefully empty for non-queued matches and pre-T046 responses.
  // Wording is owned by display.jsx::queueLabel (bead mp-e3k) so every
  // viewer surface stays in sync; we still gate on scheduled+qp>0 here
  // because this row already renders ●LIVE / Final on the right for
  // running/completed and we don't want the fallback "Scheduled hh:mm".
  const qp = Number(m.queuePosition);
  const queueLabel = (m.status === "scheduled" && Number.isFinite(qp) && qp > 0)
    ? (window.queueLabel ? window.queueLabel(m) : _localQueueLabel(m))
    : null;
  return (
    <button className={`vsched-item ${m.status === "running" ? "vsched-item--live" : ""}`} onClick={onClick} style={{ textAlign: "left", width: "100%", border: "none", background: "none", cursor: onClick ? "pointer" : "default" }}>
      <div className="vsched-item__head">
        <span className="vsched-item__time">{m.scheduledAt || "—"}</span>
        <span className="vsched-item__court">SHIAIJO {m.court}</span>
        {showCompetition && m.compName ? <span>· {m.compName}</span> : null}
        {m.phase === "pool" ? <span>· {m.poolName}</span> : <span>· {m.round || ""}</span>}
        {queueLabel && (
          <span className="vsched-item__queue" style={{ marginLeft: "auto", fontSize: 11, fontWeight: 700, color: qp === 1 ? "var(--accent)" : "var(--ink-3)" }}>
            {queueLabel}
          </span>
        )}
        {m.status === "running" && <span className="bc-live" style={{ marginLeft: "auto" }}>● LIVE</span>}
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
    ? window.formatIpponsScore(m.ipponsB, m.ipponsA, m.score, m.decision, m.encho, m.decidedByHantei)
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

// Round-robin matrix for a single pool. Rows = players (AKA), cols = players (SHIRO).
// Diagonal and upper triangle are empty; lower triangle shows result from match AKA vs SHIRO.
function PoolMatrix({ pool, matches, tweaks, onMatchClick }) {
  const players = pool.players || [];
  if (players.length < 2) return null;

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

  const enrichMatch = (m) => ({ ...m, phase: "pool", poolName: pool.poolName, phaseName: pool.poolName, compKind: "", teamSize: 0 });

  const handleCellClick = (m) => { if (onMatchClick) onMatchClick(enrichMatch(m)); };

  const handleCellKeyDown = (e, m) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); handleCellClick(m); } };

  const cellLabel = (rowPlayer, colPlayer, result) => `Match: ${rowPlayer.name} vs ${colPlayer.name} — ${result}`;

  return (
    <div className="pool-matrix-wrap">
      <table className="pool-matrix">
        <thead>
          <tr>
            <th className="pool-matrix__corner"></th>
            {players.map((p, i) => (
              <th key={p.name} className="pool-matrix__col-head" title={p.name}>{i + 1}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {players.map((rowPlayer, ri) => (
            <tr key={rowPlayer.name}>
              <td className="pool-matrix__row-head" title={rowPlayer.name}>
                <span className="pool-matrix__num">{ri + 1}</span>
                <span className="pool-matrix__pname">{tweaks.showDojo ? rowPlayer.name : shortName(rowPlayer)}</span>
              </td>
              {players.map((colPlayer, ci) => {
                if (ri === ci) return <td key={colPlayer.name} className="pool-matrix__cell pool-matrix__cell--self">&mdash;</td>;
                const m = matchMap[`${rowPlayer.name}||${colPlayer.name}`];
                if (!m) return <td key={colPlayer.name} className="pool-matrix__cell pool-matrix__cell--empty"></td>;

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
                  return <td key={colPlayer.name} className={`pool-matrix__cell pool-matrix__cell--pending ${m.status === "running" ? "pool-matrix__cell--live" : ""}`} aria-label={cellLabel(rowPlayer, colPlayer, m.status === "running" ? "Live" : "Pending")} {...interactiveProps}>
                    {m.status === "running" ? <span className="bc-live" style={{ fontSize: 9 }}>&bull;</span> : "–"}
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
                  <td key={colPlayer.name} className={`pool-matrix__cell ${rowWon ? "pool-matrix__cell--win" : isDraw ? "pool-matrix__cell--draw" : "pool-matrix__cell--loss"}`} aria-label={cellLabel(rowPlayer, colPlayer, resultLabel)} {...interactiveProps}>
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
        <span style={{ color: "var(--ink-3)", fontSize: 11 }}>{onMatchClick ? "Tap a cell to view match details" : "Row plays AKA vs col SHIRO"}</span>
      </div>
    </div>
  );
}

function PoolsViewer({ pools, standings, poolMatches, tweaks, competition, onMatchClick }) {
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

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      {isLeague && leagueWinner && <WinnerBadge name={leagueWinner.player?.name || ""} />}
      {pools.map((pool) => {
        const poolStandings = standings ? standings[pool.poolName] : null;
        const matches = poolMatches ? poolMatches.filter(m => {
          const id = m.id || "";
          return id.startsWith(pool.poolName + "-");
        }) : [];
        return (
          <div key={pool.poolName} className="pool" style={{ padding: 14 }}>
            <div className="pool__head">
              <div className="pool__name">{isLeague ? "Final standings" : pool.poolName}</div>
              <div style={{ fontSize: 12, color: "var(--ink-3)" }}>
                {matches.filter(m => m.status === "completed").length}/{matches.length} matches
              </div>
            </div>

            {/* Standings table */}
            <table className="pool__table">
              <thead>
                {isTeam ? (
                  <tr><th>#</th><th>Team</th><th className="num">W</th><th className="num">L</th><th className="num">T</th><th className="num">IV</th><th className="num">IL</th><th className="num">PW</th><th className="num">PL</th></tr>
                ) : (
                  <tr><th>#</th><th>Player</th><th className="num">W</th><th className="num">L</th><th className="num">D</th><th className="num">PW</th><th className="num">PL</th></tr>
                )}
              </thead>
              <tbody>
                {poolStandings && poolStandings.length > 0 ? poolStandings.map((s, i) => (
                  <tr key={s.player.name}>
                    <td style={{ color: s.isOverridden ? "var(--accent)" : "var(--ink-3)", fontFamily: "var(--font-mono)", fontWeight: s.isOverridden ? 700 : 400 }}>{i + 1}{s.isOverridden ? "*" : ""}</td>
                    <td>
                      <div style={{ fontWeight: 500 }}>
                        {s.player.number ? <span className="num-prefix">{s.player.number}</span> : null}
                        {s.player.name}
                      </div>
                      {tweaks.showDojo ? <div style={{ fontSize: 11, color: "var(--ink-3)" }}>{s.player.dojo}</div> : null}
                    </td>
                    <td className="num">{s.wins}</td>
                    <td className="num">{s.losses}</td>
                    <td className="num">{s.draws}</td>
                    {isTeam && <td className="num">{s.individualWins || 0}</td>}
                    {isTeam && <td className="num">{s.individualLosses || 0}</td>}
                    <td className="num">{isTeam ? (s.pointsWon || 0) : s.ipponsGiven}</td>
                    <td className="num">{isTeam ? (s.pointsLost || 0) : s.ipponsTaken}</td>
                  </tr>
                )) : pool.players.map((p, i) => {
                  const cols = isTeam ? 7 : 5;
                  return (
                    <tr key={p.name}>
                      <td style={{ color: "var(--ink-3)", fontFamily: "var(--font-mono)" }}>{i + 1}</td>
                      <td>
                        <div style={{ fontWeight: 500 }}>
                          {p.number ? <span className="num-prefix">{p.number}</span> : null}
                          {p.name}
                        </div>
                        {tweaks.showDojo ? <div style={{ fontSize: 11, color: "var(--ink-3)" }}>{p.dojo}</div> : null}
                      </td>
                      {Array.from({ length: cols }, (_, j) => <td key={j} className="num">—</td>)}
                    </tr>
                  );
                })}
              </tbody>
            </table>

            {/* Round-robin matrix — always visible */}
            {matches.length > 0 && !isTeam && (
              <div style={{ marginTop: 12 }}>
                <PoolMatrix pool={pool} matches={matches} tweaks={tweaks} onMatchClick={onMatchClick} />
              </div>
            )}

            {/* Team matches: show as list (no matrix for team) */}
            {matches.length > 0 && isTeam && (
              <div style={{ marginTop: 12 }}>
                <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
                  {matches.map(m => {
                    // Thread the same metadata compMatches/allMatches supply so the
                    // MatchViewerModal renders team sub-bouts (compKind/teamSize) and a
                    // correct header (phase/poolName), not an empty round label. Pool
                    // daihyosen bouts ("…-DH-…") are scored individually — null the team
                    // flags so they route to the individual UI (mirrors compMatches).
                    const isDH = isPoolDaihyosenID(m.id || "");
                    const enriched = { ...m, phase: "pool", poolName: pool.poolName, phaseName: pool.poolName, compKind: isDH ? "" : competition.kind, teamSize: isDH ? 0 : competition.teamSize };
                    return <PoolMatchRow key={m.id} m={m} onClick={() => onMatchClick && onMatchClick(enriched)} />;
                  })}
                </div>
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
  const dt = (dojoText || "").trim().toLowerCase();
  return matches.filter((m) => {
    if (compFilter !== "all" && m.compId !== compFilter) return false;
    if (ids.size > 0) {
      const hit = (m.sideA && ids.has(m.sideA.id)) || (m.sideB && ids.has(m.sideB.id));
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
  if (ids.size > 0 && ((m.sideA && ids.has(m.sideA.id)) || (m.sideB && ids.has(m.sideB.id)))) return true;
  const dt = (dojoText || "").trim().toLowerCase();
  if (dt && [m.sideA?.name, m.sideB?.name, m.sideA?.dojo, m.sideB?.dojo].some((s) => (s || "").toLowerCase().includes(dt))) return true;
  return false;
}

export { PlayerMultiFilter, applyFilters, matchHighlightedBy, competitionKindLabel, compMatches, tournamentMatches, currentMatchOf, buildPlayerMatchHighlight, buildWatchlistUpcoming, isSwissFinalStandings, swissStandingsHeading, isFollowedPlayer, deriveAwards, addDojoToWatchlist, buildRoster, MatchDetailCard, MatchViewerModal, AnnouncementCard, AnnouncementBanner, ViewerCompetition, ViewerOverview, MyMatchAlertBanner, PoolMatrix };

if (typeof window !== 'undefined') {
    window.PlayerMultiFilter = PlayerMultiFilter;
    window.applyFilters = applyFilters;
    window.matchHighlightedBy = matchHighlightedBy;
    window.buildPlayerMatchHighlight = buildPlayerMatchHighlight;
    window.buildWatchlistUpcoming = buildWatchlistUpcoming;
    window.deriveAwards = deriveAwards;
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
                {liveOn && <span className="bc-live">● LIVE</span>}
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
  const scoreStr = m.status === "completed" ? window.formatIpponsScore(twIpponsB, twIpponsA, m.score, m.decision, m.encho, m.decidedByHantei) : null;
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
        <div className="tw-match__phase">{m.phase === "pool" ? m.poolName : m.round}</div>
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

const PLACE_STYLE = {
  1: { icon: "🏆", label: "1st Place", accent: "var(--gold, #d4af37)" },
  2: { icon: "🥈", label: "2nd Place", accent: "var(--silver, #c0c0c0)" },
  3: { icon: "🥉", label: "3rd Place", accent: "var(--bronze, #cd7f32)" },
};

function AwardsView({ c, bracket, standings, pools, players }) {
  const containerRef = useRefV(null);
  const [isFs, setIsFs] = useState(false);
  const isLeague = c?.format === "league";
  // Swiss standings aren't part of the competition-detail payload — they live
  // behind /swiss/standings. Fetch them here when the format is swiss so the
  // Awards tab works for Swiss competitions too.
  const [swissStandings, setSwissStandings] = useState(null);

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

  const nameToPlayer = useMemo(() => {
    const m = new Map();
    (players || []).forEach((p) => {
      if (p && p.name) m.set(p.name, p);
    });
    return m;
  }, [players]);

  const effectiveStandings = c?.format === "swiss" ? swissStandings : standings;
  const isSwissLoading = c?.format === "swiss" && swissStandings === null;
  const awards = useMemo(
    () => deriveAwards(bracket, effectiveStandings, pools, nameToPlayer),
    [bracket, effectiveStandings, pools, nameToPlayer]
  );
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

  if (isSwissLoading) {
    return (
      <div className="empty" data-testid="awards-loading">
        <div className="icon">🏆</div>
        <h3>Loading final standings…</h3>
      </div>
    );
  }

  if (awards.length === 0) {
    return (
      <div className="empty" data-testid="awards-empty">
        <div className="icon">🏆</div>
        <h3>Final standings not yet available</h3>
      </div>
    );
  }

  // Visual podium ordering is driven by CSS order rules: 2 left, 1 center, then 3rd-place cards.
  // For the fullscreen ceremony layout we keep the same order but enlarge.
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
  const isTeam = match.compKind === "team" || match.teamSize > 0;
  const aName = match.sideA?.name || "TBD";
  const bName = match.sideB?.name || "TBD";
  const aWin = match.winner?.id === match.sideA?.id;
  const bWin = match.winner?.id === match.sideB?.id;
  // Bracket matches use scoreA/scoreB strings; derive ippons arrays with the
  // same fallback used in MatchDetailCard and VSchedItem.
  const mvmIpponsA = match.ipponsA || window.ipponsFromScore(match.scoreA);
  const mvmIpponsB = match.ipponsB || window.ipponsFromScore(match.scoreB);

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
      <div className="card" onClick={e => e.stopPropagation()} style={{ width: "100%", maxWidth: 500, margin: 16 }}>
        <div style={{ display: "flex", justifyContent: "space-between", marginBottom: 16 }}>
          <h2 style={{ margin: 0, fontSize: 18 }}>Match Details</h2>
          <button className="btn btn--ghost btn--sm" onClick={onClose}>Close</button>
        </div>
        <div style={{ marginBottom: 16, fontSize: 13, color: "var(--ink-3)", display: "flex", justifyContent: "space-between", alignItems: "center" }}>
          <span><TermV name="shiaijo">Shiaijo</TermV> {match.court} · {match.phase === "pool" ? match.poolName : match.round}</span>
          <window.StatusBadge status={match.status} showLiveDot={match.status === "running"} />
        </div>
        
        <div className="se-head" style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 24 }}>
          <div className={`se-head__side ${bWin ? "se-head__side--w" : ""}`} style={{ flex: 1 }}>
             <div className="se-head__name" style={{ fontWeight: 600 }}>{bName}</div>
             <div className="se-head__badge se-head__badge--shiro" style={{ display: "inline-block", background: "#fff", color: "#000", border: "1px solid #ddd", padding: "2px 6px", fontSize: 11, borderRadius: 4, marginTop: 4 }}>SHIRO</div>
          </div>
          <div className="se-head__vs" style={{ padding: "0 16px", color: "var(--ink-3)" }}>vs</div>
          <div className={`se-head__side ${aWin ? "se-head__side--w" : ""}`} style={{ flex: 1, textAlign: "right" }}>
             <div className="se-head__name" style={{ fontWeight: 600 }}>{aName}</div>
             <div className="se-head__badge se-head__badge--aka" style={{ display: "inline-block", background: "var(--red)", color: "#fff", padding: "2px 6px", fontSize: 11, borderRadius: 4, marginTop: 4 }}>AKA</div>
          </div>
        </div>

        {isTeam ? (
          <div>
            <div style={{ display: "flex", justifyContent: "space-between", borderBottom: "1px solid var(--line)", paddingBottom: 8, marginBottom: 8, fontSize: 12, fontWeight: 600, color: "var(--ink-3)" }}>
              <div style={{ width: 80 }}>SHIRO</div>
              <div style={{ flex: 1, textAlign: "center" }}>Position</div>
              <div style={{ width: 80, textAlign: "right" }}>AKA</div>
            </div>
            {match.subResults && match.subResults.length > 0 ? match.subResults.map((sub, i) => {
               const sIpponsB = (sub.ipponsB || []).filter(x => x && x !== "•").join("");
               const sIpponsA = (sub.ipponsA || []).filter(x => x && x !== "•").join("");
               const hansokuB = sub.hansokuB || 0;
               const hansokuA = sub.hansokuA || 0;
               return (
                 <div key={i} style={{ display: "flex", justifyContent: "space-between", alignItems: "center", padding: "8px 0", borderBottom: "1px solid var(--line-light)" }}>
                   <div style={{ width: 80, display: "flex", flexDirection: "column" }}>
                     <span style={{ fontWeight: 600, fontSize: 14 }}>{sIpponsB || "—"}</span>
                     {hansokuB > 0 && <span style={{ fontSize: 11, color: "var(--ink-3)" }}>Fouls: {hansokuB}</span>}
                   </div>
                   <div style={{ flex: 1, textAlign: "center", fontSize: 13, color: "var(--ink-3)" }}>
                     {subBoutLabel(sub, i)}
                     {sub.decidedByHantei && <span data-testid="sub-pool-hantei" style={{ marginLeft: 6, fontWeight: 600 }}>Hantei</span>}
                   </div>
                   <div style={{ width: 80, textAlign: "right", display: "flex", flexDirection: "column" }}>
                     <span style={{ fontWeight: 600, fontSize: 14 }}>{sIpponsA || "—"}</span>
                     {hansokuA > 0 && <span style={{ fontSize: 11, color: "var(--ink-3)" }}>Fouls: {hansokuA}</span>}
                   </div>
                 </div>
               );
            }) : (
              <div style={{ textAlign: "center", padding: 20, color: "var(--ink-3)" }}>No individual match details available.</div>
            )}
          </div>
        ) : (
          <div style={{ textAlign: "center", background: "var(--bg-2)", borderRadius: 8, padding: 16 }}>
            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
               <div style={{ display: "flex", flexDirection: "column", alignItems: "flex-start", flex: 1 }}>
                  <div style={{ fontSize: 12, color: "var(--ink-3)", marginBottom: 4 }}>Ippons</div>
                  <div style={{ fontSize: 24, fontWeight: 700 }}>{mvmIpponsB.filter(x => x && x !== "•").join("") || "—"}</div>
                  {match.hansokuB > 0 && <div style={{ fontSize: 12, color: "var(--ink-3)", marginTop: 4 }}>Fouls: {match.hansokuB}</div>}
               </div>
               <div style={{ fontSize: 24, color: "var(--ink-3)", padding: "0 16px" }}>-</div>
               <div style={{ display: "flex", flexDirection: "column", alignItems: "flex-end", flex: 1 }}>
                  <div style={{ fontSize: 12, color: "var(--ink-3)", marginBottom: 4 }}>Ippons</div>
                  <div style={{ fontSize: 24, fontWeight: 700 }}>{mvmIpponsA.filter(x => x && x !== "•").join("") || "—"}</div>
                  {match.hansokuA > 0 && <div style={{ fontSize: 12, color: "var(--ink-3)", marginTop: 4 }}>Fouls: {match.hansokuA}</div>}
               </div>
            </div>
            {match.decidedByHantei && <div style={{ marginTop: 16, fontSize: 14, fontWeight: 600 }}>Decision (Hantei)</div>}
            {match.score?.type === "hikiwake" && <div style={{ marginTop: 16, fontSize: 14, fontWeight: 600 }}>Draw (<TermV name="hikiwake">Hikiwake</TermV>)</div>}
            {match.score?.type === "bye" && <div style={{ marginTop: 16, fontSize: 14, fontWeight: 600 }}>BYE</div>}
          </div>
        )}

        {isSelfRun && bothSidesReady && (
          <div style={{ marginTop: 16, paddingTop: 12, borderTop: "1px solid var(--line)" }}>
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

window.AnnouncementCard = AnnouncementCard;
window.AnnouncementBanner = AnnouncementBanner;
window.ViewerHome = ViewerHome;
window.ViewerCompetition = ViewerCompetition;
window.ViewerSchedule = ViewerSchedule;
window.ScheduleViewer = ScheduleViewer;
window.SwissStandingsViewer = SwissStandingsViewer;
window.competitionKindLabel = competitionKindLabel;
window.compMatches = compMatches;
window.tournamentMatches = tournamentMatches;
window.currentMatchOf = currentMatchOf;
window.NotificationSettings = NotificationSettings;
window.LS_NOTIFICATIONS_ENABLED = LS_NOTIFICATIONS_ENABLED;
export { shouldShowRegister };

