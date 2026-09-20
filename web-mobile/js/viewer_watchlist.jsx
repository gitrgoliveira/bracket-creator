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
//
// NumberedName is the one ES import here, and it is safe where a window read
// would be pointless: it is a LEAF (no imports of its own, so no cycle to
// break) and it is never script-tagged, so this import and the ten existing
// ones all resolve to the same /dist/numbered_name.jsx URL and the browser
// evaluates it once. It is not on `window` at all, so there is nothing to read.
import { NumberedName } from './numbered_name.jsx';

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

  // Everything the query matches INCLUDING what is already watched. The two
  // ways this picker can have nothing to offer are different facts with
  // different next actions -- "that name is not in this tournament" (check the
  // spelling) versus "you already watch all of them" (nothing to do) -- and
  // this is the only thing that tells them apart. See the empty row below.
  const matchedIncludingWatched = useMemo(() => {
    if (!q) return roster.length + (dojos || []).length;
    const d = (dojos || []).filter((x) => x.name.toLowerCase().includes(q)).length;
    const p = roster.filter((x) =>
      (x.name || "").toLowerCase().includes(q) || (x.dojo || "").toLowerCase().includes(q)
    ).length;
    return d + p;
  }, [roster, dojos, q]);

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
      {/* Both dead ends speak (bc-wlhc). The dropdown used to render nothing at
          all when it had nothing to offer, so typing a name that is not in the
          tournament looked identical to a control that had stopped working --
          on a phone, with no hover state and no console, there was no way to
          tell. Reuses .pmf__empty, the row PlayerMultiFilter already uses. */}
      {open && total === 0 && (
        <div className="pmf__dropdown">
          <div className="pmf__empty" data-testid="watchpicker-empty">
            {roster.length === 0
              ? "No competitors have been added to this tournament yet."
              : matchedIncludingWatched === 0
                ? `No one here matches “${query.trim()}”. Try a surname, or a dojo name.`
                : q
                  ? `Everyone matching “${query.trim()}” is already on your watchlist.`
                  : "Everyone in this tournament is already on your watchlist."}
          </div>
        </div>
      )}
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
  // sideA = Aka, sideB = Shiro (the app-wide convention). The side is carried by
  // a TINTED ROW per competitor (.side-fill--aka/--shiro, the same pair the
  // scores list and court console use) rather than by an 8px badge: on the old
  // navy card the Aka badge's FILL measured 1.99:1 against the card while Shiro
  // measured 12.36:1, so one side was found instantly and the other sank
  // (operator: "it's not clear which colour the player watched or the opponent
  // are"). The word stays as a redundant channel beside the fill, and the fill
  // is attached to the PERSON's row -- the old Aka badge sat inside the label
  // div, rendering "AKA VS OPPONENT" as one phrase, labelling the wrong thing.
  const mySide = isOnSideA ? "aka" : "shiro";
  const oppSide = isOnSideA ? "shiro" : "aka";
  const running = nextMatch.status === "running";
  const phaseLabel = nextMatch.phase === "pool" ? poolLabel(nextMatch) : (nextMatch.round || "Bracket");
  // FR-025: 1-indexed queue position. Wording mirrors VSchedItem + display.jsx.
  // Null while running, by contract (viewer_match.jsx), which is why the line
  // below reads "Now" then rather than losing its only status text.
  const queueLabel = mymatchQueueLabel(nextMatch);
  // ONE line, never three side-by-side chips: at 390px three flex:1 chips got
  // 86px each, which wrapped "Shiaijo A" onto two lines with the court LETTER
  // orphaned and "11 before yours" onto three. The surface is phone-first and
  // must never truncate or wrap into rubble (operator ruling 2026-09-20).
  const whenLine = [running ? "Now" : (nextMatch.scheduledAt || "Time TBA"), queueLabel]
    .filter(Boolean).join(" · ");
  // For a dojo primary, name the dojo above the competing member so the
  // relationship is clear ("Hagane Dojo" → "Aoi" is up).
  const showDojoEyebrow = entityLabel && entityLabel !== subjectName;

  // One competitor's row: the tint carries the side, the word repeats it, and
  // "you" marks which of the two is the watched entity. Three redundant
  // channels, none of them 8px (DESIGN.md Principle 2: treatment, not hue).
  //
  // A plain function returning vnodes, NOT a component: a component declared
  // inside the render body is a new type on every render, so React remounts the
  // subtree on every SSE tick, and it also hides these rows from the panel
  // suite's shim, which does not execute child component vnodes.
  const sideRow = (key, side, name, number, dojo, you) => (
    <div key={key} className={`wl-hero__side side-fill--${side}`}>
      <span className="wl-hero__side-head">
        <span className="wl-hero__side-lbl">{side === "aka" ? "Aka" : "Shiro"}</span>
        {you ? <span className="wl-hero__you">you</span> : null}
      </span>
      <span className="wl-hero__side-name"><NumberedName name={name} number={number || ""} /></span>
      {dojo ? <span className="wl-hero__side-dojo">{dojo}</span> : null}
    </div>
  );

  const sides = [
    sideRow("subject", mySide, subjectName, subject && subject.number, subject && subject.dojo, true),
    opponent && (typeof opponent === "object")
      ? sideRow("opp", oppSide, opponent.name, opponent.number, opponent.dojo, false)
      : null,
  ];

  return (
    <div className={`wl-hero ${running ? "wl-hero--running" : ""}`} data-testid="watch-hero">
      {/* Running is a SOLID NAVY BAND, not a ring. The old signal was a
          0 0 0 4px --accent-soft ring measuring 1.20:1 against its surround
          (WCAG 1.4.11 wants >=3:1 for a state indicator), and the card was
          already navy in every state so the fill had nothing left to say.
          The band is white-on---accent at 12.36:1. Static: DESIGN.md reserves
          motion for warnings and expected actions, not for "ongoing". */}
      {running ? (
        <div className="wl-hero__now">
          On court now{nextMatch.court ? <> · <TermV name="shiaijo">Shiaijo</TermV> {nextMatch.court}</> : null}
        </div>
      ) : null}
      <div className="wl-hero__body">
        <div className="wl-hero__lbl">
          {running
            ? (showDojoEyebrow ? entityLabel : "Your match")
            : (showDojoEyebrow ? `${entityLabel} · next up` : "Your next match")}
        </div>
        {/* The INSTRUCTION is the headline: where to walk. The watched person's
            own name is the one fact they already know, so it moves down to its
            tinted row. nowrap on the letter: it is the single token that tells
            a competitor where to go and it used to wrap onto its own line. */}
        <div className="wl-hero__where">
          {nextMatch.court ? (
            <>
              <span className="wl-hero__where-l"><TermV name="shiaijo">Shiaijo</TermV></span>
              <span className="wl-hero__where-v">{nextMatch.court}</span>
            </>
          ) : (
            <span className="wl-hero__where-l">Court to be announced</span>
          )}
        </div>
        {/* Always rendered, so the live region survives the moment the match
            starts. The old role=status sat on the Queue chip, which is removed
            when running, so "your match has started" could never be announced. */}
        <div className="wl-hero__when" data-testid="my-match-queue" role="status" aria-live="polite" aria-atomic="true">
          {whenLine}
        </div>
        <div className="wl-hero__round">
          {nextMatch.compName ? `${nextMatch.compName} · ` : ""}{phaseLabel}
        </div>
        {onMatchClick ? (
          <button type="button" className="wl-hero__sides wl-hero__sides--btn"
            aria-label={`Match details: ${subjectName}${opponent && opponent.name ? ` versus ${opponent.name}` : ""}`}
            onClick={() => onMatchClick(nextMatch)}>
            {sides}
            <span className="wl-hero__more" aria-hidden="true">Match details →</span>
          </button>
        ) : (
          <div className="wl-hero__sides">{sides}</div>
        )}
      </div>
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
// Two entry props, on purpose (bc-wlhc). `primaryEntry` is who the CHIME
// follows: null until the reader pins someone, so the loud tier is always a
// choice. `heroEntry` is who the CARD shows, which falls back to the
// first-added entry, because a card is not a chime and hiding it cost the
// reader the one thing they opened the page for. Everything visual below reads
// heroEntry; only the pin hint reads primaryEntry.
function WatchlistPanel({ roster, watchlist, setWatchlist, primaryKey, setPrimaryKey, primaryEntry, heroEntry, heroNextMatch, upcoming, onMatchClick, chimeMuted, onBellToggle, onFirstAdd }) {
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
  }, [watchlist.length, firstAddFiredRef]);

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
    // UNRESOLVED: the stored id is not in this tournament's roster (a re-import,
    // a delete/recreate, a participant replacement). The entry still renders
    // from its stored name, so the chip used to look perfectly healthy while
    // buildPrimaryNextMatch returned nothing -- and the panel then printed "No
    // upcoming matches for X" while X was fighting. The id is NOT re-resolved by
    // name: bc-pnum rules an id that resolves to nothing resolves to nothing.
    // What changes is that the failure is now VISIBLE.
    // `roster.length` is load-bearing, not defensive: absence is a claim ABOUT
    // a roster, so with no roster there is nothing to be absent from and the
    // honest answer is silence. The state is reachable and was checked in the
    // browser -- a tournament with no competitions yet (roster is built from
    // t.competitions) plus a watchlist carried over from a previous event,
    // which is the normal shape of bc_watchlist: one localStorage key per
    // BROWSER, not per tournament. Without this every chip there would go
    // amber and tell the reader to delete people who are perfectly fine.
    const unresolved = roster.length > 0 && !!entry.id && !pRecord;
    return (
      <span key={k} className={`pmf__chip ${checkedIn ? "is-checked-in" : ""} ${isPrimary ? "is-primary" : ""} ${unresolved ? "pmf__chip--unresolved" : ""}`}
        title={unresolved ? "Not in this tournament's roster" : (checkedIn ? "Checked in" : undefined)}>
        {multi && (
          <button type="button" className="pmf__chip-pin" onClick={() => togglePin(entry)} aria-label={isPrimary ? `Unpin ${name}` : `Pin ${name} as primary`} aria-pressed={isPrimary}>
            {isPrimary ? "★" : "☆"}
          </button>
        )}
        {unresolved && <span aria-hidden="true">⚠</span>}
        <NumberedName name={name} number={(pRecord && pRecord.number) || ""} />
        {checkedIn && <span className="pmf__chip-tick" aria-hidden="true">✓</span>}
        <button type="button" onClick={() => removeEntry(entry)} aria-label={`Remove ${name}`}>×</button>
      </span>
    );
  };

  // Is the shown entry's stored id absent from this tournament's roster? Drives
  // the wording below: "not in the roster" is a different fact from "has no
  // more matches", and conflating them is what made the panel state a falsehood.
  const heroUnresolved = !!(roster.length > 0 && heroEntry && heroEntry.id && !rosterById.get(heroEntry.id));

  const heroLabel = heroEntry
    ? (heroEntry.type === "dojo" ? heroEntry.dojo : (rosterById.get(heroEntry.id)?.name || heroEntry.name || ""))
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

      {/* Hint when ≥2 entities are watched but none is pinned. The card below
          is already showing the first-added entry, so this no longer says
          "pin to get a card": it says what pinning still DOES, which is turn
          the chime on (and move the card).

          It must NOT say "follow someone ELSE". The shown person is unpinned
          too, so a reader watching themselves plus a training partner would
          read that as "nothing here for me" and never get the chime -- which
          is precisely the state the display/chime split creates and this hint
          exists to resolve. */}
      {multi && !primaryEntry && (
        <div className="hint watchlist-pin-hint">
          Showing {heroLabel || "the first on your list"}. Tap ☆ on a chip to pin who gets the on-deck chime.
        </div>
      )}

      {/* The hero: the pinned entry, or the first-added one when nothing is
          pinned. Never gated on the pin (bc-wlhc). */}
      {heroEntry && heroNextMatch && (
        <WatchHeroCard
          nextMatch={heroNextMatch}
          primaryIds={new Set(resolveEntryPlayerIds(heroEntry, roster))}
          entityLabel={heroLabel}
          onMatchClick={onMatchClick}
        />
      )}
      {heroEntry && !heroNextMatch && (
        <div className={`hint--md ${heroUnresolved ? "watchlist-primary-unresolved" : "watchlist-primary-done"}`}
          data-testid={heroUnresolved ? "watchlist-unresolved" : "watchlist-primary-done"}>
          {heroUnresolved
            ? `${heroLabel || "This competitor"} is not in this tournament's roster, so their matches can't be found. Remove the entry and add them again.`
            : (heroLabel ? `No upcoming matches for ${heroLabel}.` : "No upcoming matches.")}
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
