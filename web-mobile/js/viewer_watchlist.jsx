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
// numbered_name.jsx, side_cell.jsx, competitor_search.jsx and
// watchlist_link.jsx are the ES imports here, and all are safe where a window
// read would be pointless: none is script-tagged, so these imports and every
// other module's resolve to the same /dist/<name>.jsx URL and the browser
// evaluates each once. None is on `window` at all, so there is nothing to
// read.
//
// watchlist_link.jsx is the one that is not a bare leaf: it imports
// competitor_search.jsx. That is still safe, because the leaf wording above is
// a sufficient condition and not the real one -- what matters is that the
// chain is ACYCLIC and no module in it is script-tagged, so none of it can be
// evaluated twice or mid-cycle.
import { NumberedName } from './numbered_name.jsx';
import { sideWord, sideFillClass } from './side_cell.jsx';
import { competitorMatchesQuery } from './competitor_search.jsx';
import { buildWatchlistLink, watchlistLinkFitsQR } from './watchlist_link.jsx';

// The one line the picker and the hero-empty state show while a competition's
// participants failed to load (rosterAvailable:false on the aggregate, see
// rosterFullyLoaded). Everything else those two say is a claim ABOUT the
// roster, and with part of it missing the honest answer is this rather than
// silence: silence looked like a control that had stopped working (bc-wlhc),
// and the chips' own silence already covers the per-entry claim.
export const ROSTER_NOT_LOADED =
  "Some competitor lists could not be loaded, so not everyone can be found here yet.";

const { useState, useMemo, useCallback } = React;
const useRefV = React.useRef;
const useEffectV = React.useEffect;
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
function WatchPicker({ roster, rosterLoaded = true, dojos, watchedPlayerIds, watchedDojos, onPickPlayer, onPickDojo, placeholder }) {
  const [query, setQuery] = useState("");
  const [open, setOpen] = useState(false);
  const ref = useRefV(null);
  const excludedPlayers = useMemo(() => new Set(watchedPlayerIds || []), [watchedPlayerIds]);
  const excludedDojos = useMemo(() => new Set(watchedDojos || []), [watchedDojos]);
  const q = query.trim().toLowerCase();

  // What "matches the query" means, stated ONCE per entity type. The dropdown's
  // lists and the empty-state's count below both ask these: they used to carry
  // independent copies of the same two expressions, so extending the rule (a
  // zekken, a competitor number) in one and not the other would have made the
  // picker say "No one here matches X" about people the roster does hold --
  // which is precisely the distinction the count exists to draw.
  // useCallback, not plain functions: the three memos below depend on them, and
  // the dependency they really have is on `q` THROUGH them, which the
  // exhaustive-deps rule cannot see past a fresh closure.
  // The rule itself lives in competitor_search.jsx, which the public schedule
  // picker and that page's free-text chip ask too -- this surface states it
  // zero times (bc-nsrc). Worth keeping in mind here: there is nothing to
  // match on before the draw, because competitor numbers belong to draw
  // POSITIONS and none exists until the draw is generated (bc-pnum), so the
  // number arm is simply false then. That is why the row renders the number
  // too -- matching on something the reader cannot see is worse than not
  // matching at all.
  const playerMatchesQuery = useCallback((p) => competitorMatchesQuery(p, q), [q]);
  const dojoMatchesQuery = useCallback((d) => !q || (d.name || "").toLowerCase().includes(q), [q]);

  // Dojo matches: a dojo is offered until it is watched as a dojo entry. The
  // count shows how many roster members it currently covers.
  const dojoMatches = useMemo(() => {
    return (dojos || [])
      .filter((d) => !excludedDojos.has(d.name))
      .filter(dojoMatchesQuery)
      .slice(0, 6);
  }, [dojos, dojoMatchesQuery, excludedDojos]);

  const playerMatches = useMemo(() => {
    return roster.filter((p) => !excludedPlayers.has(p.id) && playerMatchesQuery(p)).slice(0, 20);
  }, [roster, playerMatchesQuery, excludedPlayers]);

  const total = dojoMatches.length + playerMatches.length;

  // Everything the query matches INCLUDING what is already watched. The two
  // ways this picker can have nothing to offer are different facts with
  // different next actions -- "that name is not in this tournament" (check the
  // spelling) versus "you already watch all of them" (nothing to do) -- and
  // this is the only thing that tells them apart. See the empty row below.
  const matchedAnyIncludingWatched = useMemo(
    () => (dojos || []).some(dojoMatchesQuery) || roster.some(playerMatchesQuery),
    [roster, dojos, playerMatchesQuery, dojoMatchesQuery]
  );

  window.useClickOutside(ref, () => setOpen(false), open);

  const pickPlayer = (p) => { onPickPlayer(p); setQuery(""); setOpen(false); };
  const pickDojo = (d) => { onPickDojo(d); setQuery(""); setOpen(false); };

  return (
    <div className="pmf" ref={ref}>
      <div className="pmf__bar" onClick={() => setOpen(true)}>
        <input
          className="pmf__input"
          placeholder={placeholder || "Search name, dojo or number…"}
          aria-label={placeholder || "Search name, dojo or number"}
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
            {/* Every line below is a claim ABOUT the roster, so none may be
                made while a competition's participants failed to load: the
                missing people are exactly the ones this box cannot offer, and
                "nobody has been added" or "no one matches" would send the
                reader to re-check a spelling that was never wrong. Gated as a
                whole rather than per branch, so a fifth branch cannot inherit
                the gap. */}
            {!rosterLoaded
              ? ROSTER_NOT_LOADED
              : roster.length === 0
                ? "No competitors have been added to this tournament yet."
                : !matchedAnyIncludingWatched
                  ? `No one here matches “${query.trim()}”. Try a surname, a dojo, or a competitor number.`
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
                  {/* Through NumberedName, the one owner of the chip rule. No
                      `side` here on purpose: this is a one-name-per-row list,
                      not a left/right pairing, so the number leads and the
                      chips align in a column down the dropdown. */}
                  <NumberedName name={p.name} number={p.number || ""} />
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
// (mp-xhaa, rebuilt for a phone by bc-wlhc). Top to bottom: a solid navy band
// while the match runs, an eyebrow naming WHO the card is about, the court as
// the 34px headline (where to walk), the queue position or time, the round,
// then one tinted row per competitor with its visible Aka/Shiro word, the
// number chip, the dojo and a "you" marker on the watched person, all inside
// the "Match details" button. The subject is whichever side belongs to the
// primary: for a player primary that's the player; for a dojo primary it's the
// member currently competing (primaryIds covers all members).
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
  //
  // NOT a SideCell, and the reason is narrow. SideCell exists to make the
  // sr-only label arrive WITH the tint, and this card is the one surface that
  // names the side in VISIBLE text instead (CLAUDE.md's single exception,
  // granted on SIZE: a large personal card read at arm's length, not a dense
  // row), so the sr-only span would only double the word. It still takes BOTH
  // halves from the owner -- sideWord for the word, sideFillClass for the
  // class -- rather than spelling the class by hand, which the sweep in
  // side_is_carried_by_tint.test.jsx forbids everywhere but the owner; the
  // hero is that sweep's one sanctioned sideFillClass caller, by name. Do not
  // copy the visible word onto a dense row.
  const sideRow = (key, side, name, number, dojo, you) => (
    <div key={key} className={`wl-hero__side ${sideFillClass(side)}`}>
      <span className="wl-hero__side-head">
        <span className="wl-hero__side-lbl">{sideWord(side)}</span>
        {you ? <span className="wl-hero__you">you</span> : null}
      </span>
      <span className="wl-hero__side-name"><NumberedName name={name} number={number || ""} /></span>
      {dojo ? <span className="wl-hero__side-dojo">{dojo}</span> : null}
    </div>
  );

  // "you" is gated on the eyebrow, not hardcoded. For a DOJO entry the subject
  // is whichever member happens to be up next, and the reader is the coach or
  // parent watching them, so marking that row "you" told them they were about
  // to fight. showDojoEyebrow is already exactly the "the subject is not the
  // entity you watched" signal, so it is the gate.
  const sides = [
    sideRow("subject", mySide, subjectName, subject && subject.number, subject && subject.dojo, !showDojoEyebrow),
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
          {/* The band does NOT name the court. The body's 34px letter below is
              the card's hero and the one statement of where to go; the band
              naming it too put "SHIAIJO A" and "Shiaijo A" 20px apart. Same
              rule that removed the TV header chip and the team summary row:
              one fact, one home, and the bigger one wins. */}
          On court now
        </div>
      ) : null}
      <div className="wl-hero__body">
        {/* The eyebrow names WHO the card is about; the line below names WHEN.
            The scheduled dojo arm used to append "· next up", which restated
            the queue label two lines down -- and worse, said it even when the
            queue read "3 before yours", so the card asserted a position it did
            not hold. Naming the dojo alone matches what the running arm always
            did. */}
        <div className="wl-hero__lbl">
          {showDojoEyebrow ? entityLabel : (running ? "Your match" : "Your next match")}
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
        {/* NO aria-label on this button. An explicit name REPLACES the
            subtree for assistive tech (role=button has presentational
            children), so the label that used to sit here -- "Match details:
            X versus Y" -- was the only thing announced, and the side word,
            the "you" marker, both competitor numbers and both dojos inside
            it were not. The comment above calls the word a redundant channel
            beside the fill; that was true only for a sighted reader.
            Name-from-contents says all of it and cannot drift from what is
            on screen, which a parallel label would. */}
        <button type="button" className="wl-hero__sides wl-hero__sides--btn"
          onClick={() => onMatchClick(nextMatch)}>
          {sides}
          {/* The words name the ACTION and stay in the accessible name; only
              the arrow is hidden, since "right arrow" is noise. */}
          <span className="wl-hero__more">Match details<span aria-hidden="true"> →</span></span>
        </button>
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
// WatchlistShareModal: the watchlist as a link someone else can open
// (bc-wlpl). The list otherwise lives only in localStorage, so it does not
// survive a second phone, another browser profile or a cleared cache.
//
// Modal, renderQR and QR_MAX_BYTES are read from `window` at RENDER time,
// which is this file's convention for anything outside its leaf imports (see
// the header). qr.js is script-tagged and publishes both, exactly as the
// /display overlay consumes it.
//
// The QR is OFFERED, not assumed. A QR tops out at QR_MAX_BYTES bytes
// INCLUDING the origin, so a number-encoded list of around 45 fits and a
// list still carrying ids (nobody has a number until the draw runs) stops
// fitting at about five. Asking watchlistLinkFitsQR first is deliberate:
// the alternative is calling renderQR and catching its throw, which is
// control flow by exception for a fact we can simply measure.
function WatchlistShareModal({ url, onClose }) {
  const Modal = window.Modal;
  const canvasRef = useRefV(null);
  const [copied, setCopied] = useState(false);
  const fits = watchlistLinkFitsQR(url, window.QR_MAX_BYTES);

  useEffectV(() => {
    if (!fits || !canvasRef.current || !window.renderQR) return;
    try {
      window.renderQR(canvasRef.current, url, { moduleSize: 5, quietZone: 4 });
    } catch (e) {
      // NOT the capacity case: `fits` already ruled that out against the
      // encoder's own published ceiling. This is for a canvas that will not
      // give a 2d context, which is a browser condition rather than anything
      // about the payload. The link is the deliverable and still renders
      // below, so a failed QR must not take the modal down with it.
      console.error("watchlist QR render failed", e);
    }
  }, [url, fits]);

  return (
    <Modal title="Share your watchlist" onClose={onClose} footer={<>
      {/* window.copyToClipboard and window.Modal are read unguarded, like
          window.pluralize at the top of this file: index.html script-tags
          ui.jsx and qr.js ahead of viewer_watchlist.js, and module scripts
          execute in order, so all three are published before anything here
          renders. A guard would be describing a state index.html prevents. */}
      <button type="button" className="btn btn--primary" onClick={() => {
        window.copyToClipboard(url).then(() => setCopied(true)).catch(() => setCopied(false));
      }}>Copy link</button>
      <button type="button" className="btn" onClick={onClose}>Close</button>
    </>}>
      <div className="wl-share">
        {fits && <canvas ref={canvasRef} className="wl-share__qr" />}
        <div className="wl-share__url" data-testid="watchlist-share-url">{url}</div>
        {/* The copy confirmation is a PERSISTENT line, not a toast: a toast
            dwells for under three seconds, which is long enough to miss and
            too short to photograph, and this one answers "did that work?"
            about an action with no other visible effect. */}
        {copied && <p className="wl-share__note wl-share__note--ok" role="status">Copied.</p>}
        <p className="wl-share__note">
          Opening this link ADDS these competitors to someone's watchlist. It does not replace what they already watch.
        </p>
        {/* Stated because it is the cost of the short, scannable form the
            operator chose (bc-wlpl): a competitor is identified by their
            number where they have one, and a number belongs to a DRAW
            POSITION, so regenerating a draw re-points it. */}
        <p className="wl-share__note">
          Share it on the day. Competitor numbers come from the draw, so a link made before a draw is regenerated can point at someone else afterwards.
        </p>
      </div>
    </Modal>
  );
}

function WatchlistPanel({ roster, rosterLoaded = true, watchlist, setWatchlist, primaryKey, setPrimaryKey, primaryEntry, heroEntry, heroNextMatch, upcoming, onMatchClick, chimeMuted, onBellToggle, onFirstAdd }) {
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

  // Does this entry resolve to NOBODY in this tournament? One predicate for
  // both entry types, because the falsehood it prevents is the same one.
  //
  // It was originally gated on `entry.id`, which only players carry, so a dojo
  // went unchecked and the panel still printed "No upcoming matches for Hagane
  // Dojo" when the roster held no Hagane member at all -- the exact claim this
  // code exists to stop making. A dojo resolves through the roster rather than
  // by id, so it is asked of the same `dojos` list the picker offers from:
  // buildRoster (viewer_watchlist_core.jsx) drops every id-less row, so a dojo
  // with a chip count has a resolvable member by construction, and the two
  // cannot disagree.
  //
  // roster.length is load-bearing, not defensive: absence is a claim ABOUT a
  // roster, so with no roster there is nothing to be absent from and the honest
  // answer is silence. Reachable and browser-checked: a tournament with no
  // competitions yet (the roster is built from t.competitions) plus a watchlist
  // carried over from a previous event, which is the normal shape of
  // bc_watchlist -- one localStorage key per BROWSER, not per tournament.
  const entryUnresolved = (entry) => {
    // rosterLoaded is the other half of roster.length. A non-empty roster only
    // means SOME competition's participants loaded; if another one's failed,
    // this entry's absence says nothing about the tournament, so the chip
    // stays quiet and the picker and the hero-empty line say ROSTER_NOT_LOADED
    // instead of a claim about who is missing.
    if (!entry || !rosterLoaded || roster.length === 0) return false;
    // Against the memoised dojo list, not a roster scan: this used to filter
    // the whole roster and allocate an id array per dojo chip per render, only
    // to ask if it was empty -- ~50 chips x a few thousand participants at the
    // documented ceiling, on every aggregate refetch.
    if (entry.type === "dojo") return !dojos.some((d) => d.name === entry.dojo);
    return !!entry.id && !rosterById.get(entry.id);
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
      const unresolved = entryUnresolved(entry);
      return (
        <span key={k} className={`pmf__chip pmf__chip--dojo ${isPrimary ? "is-primary" : ""} ${unresolved ? "pmf__chip--unresolved" : ""}`}
          title={unresolved ? "No one from this dojo is in this tournament's roster" : undefined}>
          {multi && (
            <button type="button" className="pmf__chip-pin" onClick={() => togglePin(entry)} aria-label={isPrimary ? `Unpin ${entry.dojo}` : `Pin ${entry.dojo} as primary`} aria-pressed={isPrimary}>
              {isPrimary ? "★" : "☆"}
            </button>
          )}
          {unresolved && <span aria-hidden="true">⚠</span>}
          <span className="pmf__chip-icon" aria-hidden="true">⌂</span>
          {/* The count is a claim about the roster too: "(0)" beside a dojo
              whose competition failed to load says nobody entered. */}
          {entry.dojo}{rosterLoaded ? ` (${total})` : ""}
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
    // What changes is that the failure is now VISIBLE. See entryUnresolved.
    const unresolved = entryUnresolved(entry);
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
  const heroUnresolved = entryUnresolved(heroEntry);

  const heroLabel = heroEntry
    ? (heroEntry.type === "dojo" ? heroEntry.dojo : (rosterById.get(heroEntry.id)?.name || heroEntry.name || ""))
    : "";

  // The compact list must not repeat the match the hero card is already
  // showing, one element above it and far larger. Same rule mp-42rg applied
  // between this list and the global NOW section.
  //
  // Filtered HERE rather than in viewer_home's buildWatchlistUpcoming memo,
  // because globalRunning subtracts that memo's contents from the global NOW
  // section: dropping the hero match there would not remove it from the page,
  // it would move it back into NOW.
  const listed = heroNextMatch
    ? upcoming.filter((m) => !(m.compId === heroNextMatch.compId && m.id === heroNextMatch.id))
    : upcoming;

  const [shareOpen, setShareOpen] = useState(false);
  // BUILT, never read off the address bar. app.jsx syncs its state to the
  // PATH only (pathFromState emits no query), so the first navigation away
  // rewrites the URL and drops any ?w= that brought the reader here. The
  // address bar is therefore not the permalink, and copying location.href
  // would hand over an empty list.
  const shareUrl = useMemo(() => {
    if (typeof window === "undefined" || !window.location) return "";
    return buildWatchlistLink(`${window.location.origin}/`, watchlist, roster);
  }, [watchlist, roster]);

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
        {/* AFTER the bell, which carries the head's `margin-left: auto`: the
            bell absorbs the free space and this follows it, so the two group
            at the right. Placed BEFORE the bell it rendered hard against the
            count with a 127px void between the two controls (measured at
            390px), which read as a layout accident rather than a choice. */}
        {count > 0 && shareUrl && (
          <button type="button"
            className="watchlist-share-btn"
            onClick={() => setShareOpen(true)}
            aria-label="Share your watchlist"
            title="Share your watchlist"
          >Share</button>
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
        /* The placeholder names the NUMBER: a search nobody can discover is
           one nobody uses, and this overrides WatchPicker's own default, so
           changing the default alone did not reach this surface (operator
           request 2026-09-21). A plain block comment, not {}: this is a
           ternary BRANCH, an expression position, where a JSX comment is a
           syntax error -- and the first attempt put one among the ATTRIBUTES,
           where esbuild dropped the placeholder and I read the failure as a
           success because the error scrolled past a tail -1. */
        <WatchPicker
          roster={roster}
          rosterLoaded={rosterLoaded}
          dojos={dojos}
          watchedPlayerIds={watchedPlayerIds}
          watchedDojos={watchedDojos}
          onPickPlayer={addPlayer}
          onPickDojo={addDojo}
          placeholder={count === 0 ? "Add a name, dojo or number to watch…" : "Add another name, dojo or number…"}
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
      {/* "Showing X." only when there IS a card. Unconditionally, it sat
          directly above "X is not in this tournament's roster" or "No upcoming
          matches for X" -- two adjacent lines, one claiming to show them, the
          other saying they cannot be shown. The ☆ sentence stays in BOTH cases:
          it is the only place the chime is discoverable, and dropping the whole
          hint would leave the reader without a card AND without the way to fix
          which one they get. */}
      {multi && !primaryEntry && (
        <div className="hint watchlist-pin-hint">
          {heroNextMatch && heroLabel ? `Showing ${heroLabel}. ` : ""}Tap ☆ on a chip to pin who gets the on-deck chime.
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
          {/* A dojo gets its own wording. "Remove the entry and add them again"
              is unfollowable for a dojo that the picker cannot offer back,
              because the same missing members are why it is not listed. */}
          {/* A dojo resolves its members THROUGH the roster, so with part of
              it missing "no upcoming matches" would be a claim about people
              this panel cannot see. A player entry resolves by id and keeps
              the honest line. */}
          {heroUnresolved
            ? (heroEntry.type === "dojo"
              ? `No one from ${heroLabel} is in this tournament's roster, so there are no matches to show.`
              : `${heroLabel || "This competitor"} is not in this tournament's roster, so their matches can't be found. Remove the entry and add them again.`)
            : (!rosterLoaded && heroEntry.type === "dojo")
              ? ROSTER_NOT_LOADED
              : (heroLabel ? `No upcoming matches for ${heroLabel}.` : "No upcoming matches.")}
        </div>
      )}

      {/* Bounded compact list of running and upcoming watched matches: shown when
          ≥2 entities are watched so a coach sees the whole squad at a glance.
          Includes running matches (buildWatchlistUpcoming returns both). */}
      {multi && listed.length > 0 && (
        <div className="vsched vsched--incard">
          {listed.map((m) => (
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

      {shareOpen && shareUrl && (
        <WatchlistShareModal url={shareUrl} onClose={() => setShareOpen(false)} />
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
