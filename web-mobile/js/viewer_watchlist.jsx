// viewer_watchlist.jsx: the watchlist personalisation UI, extracted from
// viewer.jsx to keep that file's component count manageable (mp-42rg follow-up).
//
// Sharing model: this module keeps its OWN <script type="module"> tag in
// index.html because viewer.js does NOT import it (unlike the other viewer_*
// modules, which viewer.js is the sole entry for). It shares code with viewer.jsx
// via `window.*` reads at RENDER time to break the runtime cycle with
// viewer_home.jsx: NOT because ES imports would fail: the server rewrites
// `/dist/X.jsx` → the compiled `X.js`, so `import "./X.jsx"` resolves fine.
//
// This file and viewer_home.jsx form a runtime CYCLE: ViewerHome (in
// viewer_home.jsx) renders WatchlistPanel (here), while these components
// consume helpers exposed on `window` by viewer.jsx (TermV, VSchedItem,
// entryKey, …). The cycle is safe
// because every cross-boundary helper is read from `window` at RENDER time
// (inside the component body), by which point both scripts have evaluated and
// populated `window`. Only React (a vendor global) and pluralize (from ui.js,
// loaded before this file) are read at module-eval time.
const { useState, useMemo } = React;
const useRefV = React.useRef;
const pluralize = window.pluralize;

// Bell icon for the watchlist alert toggle (muted = diagonal slash).
// Exported on window so AnnBellBtn in viewer.jsx can reuse it without duplication.
function BellIcon({ muted, size = 17 }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9"/>
      <path d="M13.73 21a2 2 0 0 1-3.46 0"/>
      {muted && <line x1="1" y1="1" x2="23" y2="23"/>}
    </svg>
  );
}
window.BellIcon = BellIcon;

// WatchPicker: unified typeahead over the tournament roster that yields
// EITHER a player pick or a whole-dojo pick. A single search box surfaces both
// (matching dojos first, then matching players), so "Hagane" offers
// "Watch all of Hagane Dojo" as one entry instead of forcing the user to add
// members one by one. Replaces the old SinglePlayerPicker (mp-xhaa).
function WatchPicker({ roster, dojos, watchedPlayerIds, watchedDojos, onPickPlayer, onPickDojo, placeholder }) {
  const [query, setQuery] = useState("");
  const [open, setOpen] = useState(false);
  const ref = useRefV(null);
  const excludedPlayers = useMemo(() => new Set(watchedPlayerIds || []), [watchedPlayerIds]);
  const excludedDojos = useMemo(() => new Set(watchedDojos || []), [watchedDojos]);
  const q = query.trim().toLowerCase();

  // Dojo matches: a dojo is offered until it is watched as a dojo entry. The
  // count shows how many roster members it currently covers.
  const dojoMatches = useMemo(() => {
    return (dojos || [])
      .filter((d) => !excludedDojos.has(d.name))
      .filter((d) => !q || d.name.toLowerCase().includes(q))
      .slice(0, 6);
  }, [dojos, q, excludedDojos]);

  const playerMatches = useMemo(() => {
    const base = roster.filter((p) => !excludedPlayers.has(p.id));
    if (!q) return base.slice(0, 20);
    return base.filter((p) =>
      (p.name || "").toLowerCase().includes(q) || (p.dojo || "").toLowerCase().includes(q)
    ).slice(0, 20);
  }, [roster, q, excludedPlayers]);

  const total = dojoMatches.length + playerMatches.length;

  window.useClickOutside(ref, () => setOpen(false), open);

  const pickPlayer = (p) => { onPickPlayer(p); setQuery(""); setOpen(false); };
  const pickDojo = (d) => { onPickDojo(d); setQuery(""); setOpen(false); };

  return (
    <div className="pmf" ref={ref}>
      <div className="pmf__bar" onClick={() => setOpen(true)}>
        <input
          className="pmf__input"
          placeholder={placeholder || "Search players or dojos…"}
          aria-label={placeholder || "Search players or dojos"}
          value={query}
          onChange={(e) => { setQuery(e.target.value); setOpen(true); }}
          onFocus={() => setOpen(true)}
        />
      </div>
      {open && total > 0 && (
        <div className="pmf__dropdown">
          <div className="pmf__dropdown-head">
            {q ? pluralize(total, "match", "matches") : `${pluralize(roster.length, "participant")} · ${pluralize((dojos || []).length, "dojo")}: type to search`}
          </div>
          {/* Dojo options first: a dojo entry is dynamic (auto-includes late
              registrations), so it's the higher-leverage choice for a coach. */}
          {dojoMatches.map((d) => (
            <button type="button"
              key={"dojo:" + d.name}
              className="pmf__option pmf__option--dojo"
              onClick={() => pickDojo(d)}
            >
              <span className="pmf__check" aria-hidden="true">⌂</span>
              <span className="pmf__opt-body">
                <span className="pmf__opt-name">{d.name}</span>
                <span className="pmf__opt-dojo">Watch all · {pluralize(d.total, "member")}</span>
              </span>
            </button>
          ))}
          {playerMatches.map((p) => (
            <button type="button"
              key={p.id}
              className="pmf__option"
              onClick={() => pickPlayer(p)}
            >
              <span className="pmf__check">{p.checkedIn ? "✓" : ""}</span>
              <span className="pmf__opt-body">
                <span className="pmf__opt-name">
                  {p.name}
                  {p.checkedIn && <span className="tag-badge pmf__checkin-tag">Checked in</span>}
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

// WatchHeroCard: the rich "next match" hero for the PRIMARY watched entity
// (mp-xhaa). Lifted from the former MyMatchPanel so the lone-competitor case
// keeps its full treatment: AKA/SHIRO badge, queue position, opponent, court,
// time. The subject is whichever side belongs to the primary: for a player
// primary that's the player; for a dojo primary it's the member currently
// competing (primaryIds covers all members).
function WatchHeroCard({ nextMatch, primaryIds, entityLabel, onMatchClick }) {
  // Cross-boundary helpers from viewer.jsx, read at render time (see header).
  const { matchParticipantIds, poolLabel, mymatchQueueLabel, TermV } = window;
  if (!nextMatch) return null;
  const ids = primaryIds || new Set();
  const [aId, bId] = matchParticipantIds(nextMatch);
  // Pick the side belonging to the primary; default to side A if neither id
  // resolves (name-only legacy data).
  const isOnSideA = aId && ids.has(aId) ? true : (bId && ids.has(bId) ? false : true);
  const subject = isOnSideA ? nextMatch.sideA : nextMatch.sideB;
  const opponent = isOnSideA ? nextMatch.sideB : nextMatch.sideA;
  const subjectName = (subject && subject.name) || entityLabel || "";
  // Full-text Aka/Shiro badge (bc-color-badge), consistent with bracket.jsx;
  // the compact 14×14 variant would clip "AKA"/"SHIRO".
  const myBadgeClass = isOnSideA ? "bc-color-badge--aka" : "bc-color-badge--shiro";
  const myBadgeLabel = isOnSideA ? "AKA" : "SHIRO";
  const oppBadgeClass = isOnSideA ? "bc-color-badge--shiro" : "bc-color-badge--aka";
  const oppBadgeLabel = isOnSideA ? "SHIRO" : "AKA";
  const phaseLabel = nextMatch.phase === "pool" ? poolLabel(nextMatch) : (nextMatch.round || "Bracket");
  // FR-025: 1-indexed queue position. Wording mirrors VSchedItem + display.jsx.
  const queueLabel = mymatchQueueLabel(nextMatch);
  const queueHighlight = queueLabel === "Next up";
  // For a dojo primary, name the dojo above the competing member so the
  // relationship is clear ("Hagane Dojo" → "Aoi" is up).
  const showDojoEyebrow = entityLabel && entityLabel !== subjectName;

  return (
    <div className={`my-match ${nextMatch.status === "running" ? "my-match--running" : ""}`} data-testid="watch-hero">
      <div className="my-match__lbl">
        {nextMatch.status === "running"
          ? (showDojoEyebrow ? entityLabel : "Your match")
          : (showDojoEyebrow ? `${entityLabel} · next up` : "Your next match")}
      </div>
      <div className="my-match__name">
        <span className={`bc-color-badge ${myBadgeClass}`}>{myBadgeLabel}</span>
        {subjectName}
      </div>
      <div className="my-match__round">
        {nextMatch.compName ? `${nextMatch.compName} · ` : ""}{phaseLabel}
      </div>
      <div className="my-match__row">
        <div className="my-match__chip">
          <span className="l">Court</span>
          <span className="v"><TermV name="shiaijo">Shiaijo</TermV> {nextMatch.court || "-"}</span>
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
                inherits white from --accent-fg; emphasise the in-progress/up-next
                state with full opacity + a Unicode bullet instead.
                Wrap the decorative bullet in aria-hidden to keep screen reader
                announcements clean and focused on the queue label text. */}
            <span className="v" style={{ opacity: queueHighlight ? 1 : 0.92 }}>
              {queueHighlight ? <span aria-hidden="true">{"• "}</span> : null}
              {queueLabel}
            </span>
          </div>
        )}
      </div>
      {opponent && (typeof opponent === "object") ? (
        <button type="button"
          className="my-match__opp"
          onClick={() => onMatchClick && onMatchClick(nextMatch)}
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

// WatchlistPanel: the unified personalisation panel (mp-xhaa). One card that
// absorbs the former "Find my matches" hero and the multi-player watchlist:
//   - chip list of watched entities (players + whole dojos), each removable,
//     each pin-able when ≥2 entities exist;
//   - a single unified picker (WatchPicker) that adds a player OR a dojo;
//   - the hero card for the primary entity (implicit when 1, pinned when ≥2);
//   - a bounded "watched upcoming" compact list when ≥2 entities are watched.
function WatchlistPanel({ roster, watchlist, setWatchlist, primaryKey, setPrimaryKey, primaryEntry, primaryNextMatch, upcoming, onMatchClick, chimeMuted, onBellToggle, onFirstAdd }) {
  // Cross-boundary helpers from viewer.jsx, read at render time (see header).
  const { effectivePrimaryKey, addPlayerToWatchlist, entryKey, resolveEntryPlayerIds, VSchedItem, WATCHLIST_MAX } = window;
  const rosterById = useMemo(() => new Map(roster.map((p) => [p.id, p])), [roster]);

  // Dojos present in the roster, with current member counts.
  const dojos = useMemo(() => {
    const counts = new Map();
    roster.forEach((p) => { if (p.dojo) counts.set(p.dojo, (counts.get(p.dojo) || 0) + 1); });
    return Array.from(counts.entries()).map(([name, total]) => ({ name, total })).sort((a, b) => a.name.localeCompare(b.name));
  }, [roster]);

  const watchedPlayerIds = useMemo(() => watchlist.filter((e) => e.type === "player").map((e) => e.id), [watchlist]);
  const watchedDojos = useMemo(() => watchlist.filter((e) => e.type === "dojo").map((e) => e.dojo), [watchlist]);

  const count = watchlist.length;
  const multi = count >= 2;
  const effectiveKey = effectivePrimaryKey(watchlist, primaryKey);

  const firstAddFiredRef = useRefV(false);
  React.useLayoutEffect(() => {
    if (watchlist.length === 0) firstAddFiredRef.current = false;
  }, [watchlist.length]);

  const maybeFirstAdd = () => {
    if (!firstAddFiredRef.current && watchlist.length === 0 && onFirstAdd) {
      firstAddFiredRef.current = true;
      onFirstAdd();
    }
  };

  const addPlayer = (p) => {
    maybeFirstAdd();
    setWatchlist(prev => addPlayerToWatchlist(prev, p));
  };
  const addDojo = (d) => {
    if (!d || !d.name) return;
    maybeFirstAdd();
    setWatchlist(prev => {
      if (prev.some((e) => e.type === "dojo" && e.dojo === d.name)) return prev;
      return [...prev, { type: "dojo", dojo: d.name }];
    });
  };
  const removeEntry = (entry) => {
    const k = entryKey(entry);
    setWatchlist(prev => prev.filter((e) => entryKey(e) !== k));
    if (primaryKey === k) setPrimaryKey("");
  };
  const togglePin = (entry) => {
    const k = entryKey(entry);
    setPrimaryKey(primaryKey === k ? "" : k);
  };

  // Chip for one entry: player or dojo, with optional pin star (≥2 entries)
  // and a remove button.
  const renderChip = (entry) => {
    const k = entryKey(entry);
    // Only flag the primary chip visually when there's a choice to make (≥2
    // entries). With a lone entry the navy fill is decorative and clashes with
    // the navy hero directly below it.
    const isPrimary = multi && effectiveKey === k;
    if (entry.type === "dojo") {
      const total = dojos.find((d) => d.name === entry.dojo)?.total ?? 0;
      return (
        <span key={k} className={`pmf__chip pmf__chip--dojo ${isPrimary ? "is-primary" : ""}`}>
          {multi && (
            <button type="button" className="pmf__chip-pin" onClick={() => togglePin(entry)} aria-label={isPrimary ? `Unpin ${entry.dojo}` : `Pin ${entry.dojo} as primary`} aria-pressed={isPrimary}>
              {isPrimary ? "★" : "☆"}
            </button>
          )}
          <span className="pmf__chip-icon" aria-hidden="true">⌂</span>
          {entry.dojo} ({total})
          <button type="button" onClick={() => removeEntry(entry)} aria-label={`Remove ${entry.dojo}`}>×</button>
        </span>
      );
    }
    const pRecord = rosterById.get(entry.id);
    const checkedIn = pRecord && pRecord.checkedIn;
    const name = (pRecord && pRecord.name) || entry.name || "(unknown)";
    return (
      <span key={k} className={`pmf__chip ${checkedIn ? "is-checked-in" : ""} ${isPrimary ? "is-primary" : ""}`} title={checkedIn ? "Checked in" : undefined}>
        {multi && (
          <button type="button" className="pmf__chip-pin" onClick={() => togglePin(entry)} aria-label={isPrimary ? `Unpin ${name}` : `Pin ${name} as primary`} aria-pressed={isPrimary}>
            {isPrimary ? "★" : "☆"}
          </button>
        )}
        {name}
        {checkedIn && <span className="pmf__chip-tick" aria-hidden="true">✓</span>}
        <button type="button" onClick={() => removeEntry(entry)} aria-label={`Remove ${name}`}>×</button>
      </span>
    );
  };

  const primaryLabel = primaryEntry
    ? (primaryEntry.type === "dojo" ? primaryEntry.dojo : (rosterById.get(primaryEntry.id)?.name || primaryEntry.name || ""))
    : "";

  return (
    <div className="card card--sm mymatch-card" data-testid="viewer-home-watchlist">
      <div className="watchlist-card-head">
        <span className="watchlist-card-title">Watchlist</span>
        {count > 0 && <span className="watchlist-count" aria-label={`${count} watched`}>{count}</span>}
        {onBellToggle != null && (
          <button type="button"
            className={`watchlist-bell-btn${chimeMuted ? " watchlist-bell-btn--muted" : ""}`}
            onClick={onBellToggle}
            aria-pressed={!chimeMuted}
            aria-label={chimeMuted ? "Alerts muted: tap to enable" : "Alerts on: tap to mute"}
            title={chimeMuted ? "Alerts muted: tap to enable" : "Alerts on: tap to mute"}
          >
            <BellIcon muted={chimeMuted} />
          </button>
        )}
      </div>

      {count === 0 ? (
        <div className="hint">
          Track yourself, a few competitors, or a whole dojo: we'll surface their next matches and alert you when they're on deck. Add up to {WATCHLIST_MAX}.
        </div>
      ) : (
        <div className="pmf__bar pmf__bar--standalone watchlist-chips">
          {watchlist.map(renderChip)}
        </div>
      )}

      {count >= WATCHLIST_MAX ? (
        <div className="hint--sm">Watchlist full ({WATCHLIST_MAX}). Remove an entry to add more.</div>
      ) : (
        <WatchPicker
          roster={roster}
          dojos={dojos}
          watchedPlayerIds={watchedPlayerIds}
          watchedDojos={watchedDojos}
          onPickPlayer={addPlayer}
          onPickDojo={addDojo}
          placeholder={count === 0 ? "Add a player or dojo to watch…" : "Add another player or dojo…"}
        />
      )}

      {/* Hint when ≥2 entities are watched but none is pinned: no hero/chime
          until the user picks a primary. */}
      {multi && !primaryEntry && (
        <div className="hint watchlist-pin-hint">
          Tap ☆ on a chip to pin your primary: they get the big card and an on-deck chime.
        </div>
      )}

      {/* Primary hero: implicit when 1 entity, pinned when ≥2. */}
      {primaryEntry && primaryNextMatch && (
        <WatchHeroCard
          nextMatch={primaryNextMatch}
          primaryIds={new Set(resolveEntryPlayerIds(primaryEntry, roster))}
          entityLabel={primaryLabel}
          onMatchClick={onMatchClick}
        />
      )}
      {primaryEntry && !primaryNextMatch && (
        <div className="hint--md watchlist-primary-done">
          {primaryLabel ? `No upcoming matches for ${primaryLabel}.` : "No upcoming matches."}
        </div>
      )}

      {/* Bounded compact list of running and upcoming watched matches: shown when
          ≥2 entities are watched so a coach sees the whole squad at a glance.
          Includes running matches (buildWatchlistUpcoming returns both). */}
      {multi && upcoming.length > 0 && (
        <div className="vsched vsched--incard">
          {upcoming.map((m) => (
            <VSchedItem
              key={`${m.compId}:${m.id}`}
              m={m}
              tweaks={{ showDojo: true }}
              showCompetition
              onClick={() => onMatchClick && onMatchClick(m)}
            />
          ))}
        </div>
      )}
    </div>
  );
}

// Runtime sharing: expose WatchlistPanel on window so viewer_home.jsx (ViewerHome)
// can render it at render time regardless of script load order. WatchPicker and
// WatchHeroCard are used only internally by WatchlistPanel (this file), so they
// are not exposed on window.
window.WatchlistPanel = WatchlistPanel;

// ES exports for the vitest suite, which imports these directly.
export { WatchPicker, WatchHeroCard, WatchlistPanel };
