// viewer_standings.jsx: pool/standings components extracted from viewer.jsx
// (mp-pxxc step 5). Pure file split: no behaviour change.
//
// Sharing model: esbuild compiles each .jsx to dist/.js individually (no bundle);
// the server rewrites `/dist/X.jsx` → the compiled `X.js`, so ES `import "./X.jsx"`
// specifiers resolve in the browser. viewer.js is the SOLE viewer entry script in
// index.html and imports this module: do NOT give it its own <script type="module">
// tag, or the browser fetches it under a second URL (.js?v=N vs .jsx) and evaluates
// it twice (double-load; same class as mp-zd1v).
//
// Cycle note: viewer.jsx imports from this file and re-exports every symbol
// here (plus window.* assignments) so the public surface of viewer.jsx is
// unchanged. WinnerBadge is also used internally by viewer.jsx (ViewerOverview,
// AwardsView) and is imported back from here.

import { withNumber } from './match_scoreboard.jsx';
import { matchParticipantIds, matchParticipantNames, isPlayerWatched } from './viewer_watchlist_core.jsx';
import { poolNameOf, isSupplementaryBout, isPoolDaihyosenBout } from './pool_ids.jsx';

const { useState } = React;
const EmptyState = window.EmptyState;

// DH-winner detection uses isPoolDaihyosenBout (pool_ids.jsx): a suffix match
// (…-DH-N) so a pool name that happens to contain "-DH-" can't false-positive a
// regular match as a daihyosen win.

// DHBadge: the "DH" tag marking a team that won a daihyosen play-off. The label
// is a kendo-glossary term (tap/focus to define daihyosen), matching the
// bracket's DH chip and staying explainable on touch where a title tooltip
// isn't. Falls back to a plain badge before glossary.jsx attaches window.Term.
export function DHBadge() {
  const label = (typeof window !== "undefined" && window.Term)
    ? React.createElement(window.Term, { name: "daihyosen" }, "DH")
    : "DH";
  return <span className="tag-badge">{label}</span>;
}

// ---------------------------------------------------------------------------
// isSwissFinalStandings
// testable without mounting the component. Mirrors the
// admin_competition.jsx swiss helpers pattern.

// `comp.swissCurrentRound >= comp.swissRounds` is the precondition for
// "final standings"; we also require every match in the final round
// to be completed so a half-finished final round doesn't prematurely
// claim a winner.
export function isSwissFinalStandings(comp, poolMatches) {
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
export function swissStandingsHeading(comp, poolMatches) {
  if (isSwissFinalStandings(comp, poolMatches)) return "Final standings";
  const current = (comp && comp.swissCurrentRound) || 0;
  if (current === 0) return "Standings: pending";
  return `Standings after round ${current}`;
}

export function WinnerBadge({ name, isFs = false, testId, marginBottom }) {
  return (
    <div
      className={`winner-badge${isFs ? " winner-badge--lg" : ""}`}
      data-testid={testId}
      style={marginBottom != null ? { marginBottom } : undefined}
    >
      <span className="winner-badge__icon" aria-hidden="true">🏆</span>
      <span>Winner: {name}</span>
    </div>
  );
}

export function SwissStandingsViewer({ competition, poolMatches, tweaks }) {
  const c = competition;
  const [standings, setStandings] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  // Re-fetch whenever the round counter advances (SSE-driven) so the
  // standings table reflects the latest cumulative state. Also depend
  // on poolMatches length so a fresh round's matches landing triggers
  // a refresh: the round may have completed even when swissCurrentRound
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

  if (loading) return <window.LoadingSpinner text="Loading standings…" />;
  if (error) return <div className="alert alert--error">{error}</div>;

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      {isFinal && winner && <WinnerBadge name={winner.player?.name || ""} />}
      <div className="pool">
        <div className="pool__head">
          <div className="pool__name">{heading}</div>
          <div className="pool__match-count">
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
                {/* Rank-ordered: "#" is the authoritative standing rank (s.rank),
                    DRY with the backend tiebreak/override logic, not the row index. */}
                <td className={`pool-standings__draw-pos${s.isOverridden ? " pool-standings__draw-pos--override" : ""}`}>{s.rank || i + 1}{s.isOverridden ? "*" : ""}</td>
                <td>
                  <div className="pool__player-name">{s.player?.name || ""}</div>
                  {tweaks?.showDojo ? <div className="pool__dojo-name">{s.player?.dojo || ""}</div> : null}
                </td>
                <td className="num">{s.wins || 0}</td>
                <td className="num">{s.losses || 0}</td>
                <td className="num">{s.draws || 0}</td>
                <td className="num">{s.ipponsGiven || 0}</td>
                <td className="num">{s.ipponsTaken || 0}</td>
              </tr>
            )) : (
              <tr><td colSpan={7} className="pool__table-empty">No matches scored yet.</td></tr>
            )}
          </tbody>
        </table>
        {standings.length > 0 && (
          <div className="league-matrix__legend hint--sm" style={{ marginTop: 8 }}>
            Ranked by: wins → points scored (PW) → head-to-head.
          </div>
        )}
      </div>
    </div>
  );
}

export const PoolMatchRow = React.memo(({ m, onClick }) => {
  const aRawName = typeof m.sideA === "object" ? m.sideA?.name : m.sideA;
  const bRawName = typeof m.sideB === "object" ? m.sideB?.name : m.sideB;
  // withNumber prepends the assigned competitor number (e.g. "K1") when
  // present; the winner comparison below still uses the bare name.
  const aName = typeof m.sideA === "object" ? withNumber(m.sideA) : aRawName;
  const bName = typeof m.sideB === "object" ? withNumber(m.sideB) : bRawName;
  const winnerName = typeof m.winner === "object" ? m.winner?.name : m.winner;
  // Winner comparison must use the bare name (numberPrefix not included).
  const aWin = winnerName && winnerName === aRawName;
  const bWin = winnerName && winnerName === bRawName;

  // Render a non-interactive <div> when there's no click handler (read-only
  // reuse, e.g. the operator console passes onClick=null) so we don't leave a
  // focusable button that does nothing for keyboard/screen-reader users.
  const Tag = onClick ? "button" : "div";
  const interactiveProps = onClick ? { type: "button", onClick } : {};
  return (
    <Tag className={`pool-match-row${m.status === "running" ? " is-running" : ""}`} {...interactiveProps}>
      <div className={`pool-match-row__side pool-match-row__side--right ${bWin ? "pool-match-row__side--win" : ""}`}>
        <span className="pool-match-row__name">{bName}</span>
        <span className="pool-match-row__badge pool-match-row__badge--shiro">SHIRO</span>
      </div>
      <span className="pool-match-row__score">
        {window.matchStateCell(m, m.ipponsB, m.ipponsA)}
      </span>
      <div className={`pool-match-row__side ${aWin ? "pool-match-row__side--win" : ""}`}>
        <span className="pool-match-row__badge pool-match-row__badge--aka">AKA</span>
        <span className="pool-match-row__name">{aName}</span>
      </div>
    </Tag>
  );
});
PoolMatchRow.displayName = "PoolMatchRow";

// Round-robin matrix for a single pool/league. Each off-diagonal cell shows the
// row player's result (W/L/X) against the column player; diagonal cells are self.
export function LeagueMatrix({ pool, matches, tweaks, onMatchClick, highlightPlayers }) {
  const players = pool.players || [];
  if (players.length < 2) return null;

  const isHighlighted = (p) => isPlayerWatched(p, highlightPlayers);

  // Stable participant key. A name is NOT unique within a competition:
  // two participants from different dojos may share one (the duplicate
  // check only rejects same-name AND same-dojo), so name-only indexing
  // collides and can show the wrong cell. Prefer the participant UUID,
  // falling back to name for legacy data that predates id persistence.
  const pkey = (x) => (x && x.id != null && x.id !== "" ? String(x.id) : (x && x.name) || "");
  // Winner id tolerant of BOTH the canonical object shape (`m.winner.id`
  // from api_serializers.jsx) and the flat `m.winnerId` shape some
  // fixtures/CSV round-trips use. (Side ids come from the shared
  // matchParticipantIds helper; names from matchParticipantNames.)
  const winnerId = (m) => (m.winner && typeof m.winner === "object" ? m.winner.id : null) || m.winnerId || "";

  // Index every match under BOTH its id-pair and its name-pair. The id
  // entries are collision-free and used first; the name entries are a
  // fallback for partially-migrated data (players carry ids but an older
  // pool-matches.csv does not, or vice versa). Same-name collisions only
  // affect the name fallback, which is the pre-existing behavior.
  const matchMap = {};
  matches.forEach(m => {
    const [aName, bName] = matchParticipantNames(m);
    const [aId, bId] = matchParticipantIds(m);
    const aKey = aId || aName;
    const bKey = bId || bName;
    if (aKey && bKey) {
      matchMap[`${aKey}||${bKey}`] = m;
      matchMap[`${bKey}||${aKey}`] = m;
    }
    if (aName && bName) {
      // Name fallback (does not clobber an existing id entry).
      if (!matchMap[`${aName}||${bName}`]) matchMap[`${aName}||${bName}`] = m;
      if (!matchMap[`${bName}||${aName}`]) matchMap[`${bName}||${aName}`] = m;
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

  // Label including the dojo when present so two same-name participants are
  // distinguishable. Used for BOTH the hover `title` AND the cell/header
  // `aria-label`: screen readers don't reliably announce `title`, so the
  // accessible name must carry the disambiguating dojo itself.
  const playerLabel = (p) => (p && p.dojo ? `${p.name} (${p.dojo})` : (p && p.name) || "");
  const cellLabel = (rowPlayer, colPlayer, result) => `Match: ${playerLabel(rowPlayer)} vs ${playerLabel(colPlayer)}: ${result}`;
  const cellTitle = (rowPlayer, colPlayer, result) => `${playerLabel(rowPlayer)} vs ${playerLabel(colPlayer)}: ${result}`;

  return (
    <div className="league-matrix-wrap">
      <table className="league-matrix">
        <thead>
          <tr>
            <th className="league-matrix__corner"></th>
            {players.map((p, i) => (
              <th key={`${pkey(p)}#${i}`} scope="col" aria-label={playerLabel(p)} className={`league-matrix__col-head${isHighlighted(p) ? " league-matrix__col--me" : ""}`} title={playerLabel(p)}>{p.number || (i + 1)}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {players.map((rowPlayer, ri) => (
            <tr key={`${pkey(rowPlayer)}#${ri}`} className={isHighlighted(rowPlayer) ? "league-matrix__row--me" : ""}>
              <td className="league-matrix__row-head" title={playerLabel(rowPlayer)} aria-label={playerLabel(rowPlayer)}>
                <span className="league-matrix__num">{rowPlayer.number || (ri + 1)}</span>
                <span className="league-matrix__pname">{tweaks.showDojo ? rowPlayer.name : shortName(rowPlayer)}</span>
              </td>
              {players.map((colPlayer, ci) => {
                const colMe = isHighlighted(colPlayer) ? " league-matrix__col--me" : "";
                if (ri === ci) return <td key={`${pkey(colPlayer)}#${ci}`} className={`league-matrix__cell league-matrix__cell--self${colMe}`}>&mdash;</td>;
                // Look up the match by id-pair first (collision-free),
                // then by name-pair for legacy/un-migrated data.
                const m = matchMap[`${pkey(rowPlayer)}||${pkey(colPlayer)}`] || matchMap[`${rowPlayer.name}||${colPlayer.name}`];
                if (!m) return <td key={`${pkey(colPlayer)}#${ci}`} title={cellTitle(rowPlayer, colPlayer, "not played")} className={`league-matrix__cell league-matrix__cell--empty${colMe}`}></td>;

                const [aName] = matchParticipantNames(m);
                const [aId] = matchParticipantIds(m);
                const winnerName = typeof m.winner === "object" ? m.winner?.name : m.winner;
                // Which side is the row player? Match on id when both the
                // match side and the player carry one; else by name.
                const rowIsAka = (aId && rowPlayer.id)
                  ? aId === rowPlayer.id
                  : aName === rowPlayer.name;

                const interactiveProps = onMatchClick ? {
                  role: "button",
                  tabIndex: 0,
                  onClick: () => handleCellClick(m),
                  onKeyDown: (e) => handleCellKeyDown(e, m),
                } : {};

                if (m.status !== "completed") {
                  return <td key={`${pkey(colPlayer)}#${ci}`} title={cellTitle(rowPlayer, colPlayer, m.status === "running" ? "Now" : "Pending")} className={`league-matrix__cell league-matrix__cell--pending ${m.status === "running" ? "league-matrix__cell--running" : ""}${colMe}`} aria-label={cellLabel(rowPlayer, colPlayer, m.status === "running" ? "Now" : "Pending")} {...interactiveProps}>
                    {m.status === "running" ? "" : "–"}
                  </td>;
                }

                const ipponsA = (m.ipponsA || []).filter(x => x && x !== "•");
                const ipponsB = (m.ipponsB || []).filter(x => x && x !== "•");
                const rowIppons = rowIsAka ? ipponsA : ipponsB;
                // Engi (flag-count scoring) is the ONLY competition type
                // where a matrix cell shows a NUMBER; every other type shows
                // the row player's own ippon letters above. Engi never
                // draws (odd flag totals), so this only ever feeds the
                // win/loss branches below.
                const isEngiCell = m.flagsA != null || m.flagsB != null;
                const rowFlags = rowIsAka ? (m.flagsA || 0) : (m.flagsB || 0);
                // Prefer the winner id: when two participants share a name,
                // the winner name alone can't tell them apart (e.g. the
                // head-to-head between two same-name/different-dojo players).
                // Fall back to name for legacy data without a winner id.
                const wId = winnerId(m);
                // When both sides share a name and there is no winner id, the
                // winner NAME is ambiguous: do NOT mark either row as winner
                // (that would light up BOTH cells green for the one match). The
                // id branch is the authoritative resolver; the name fallback is
                // only safe for distinct-name matchups. (Source path now sends
                // winnerId for same-name matches, so this is defense-in-depth
                // for legacy data without ids.)
                const namesAmbiguous = rowPlayer.name === colPlayer.name;
                const rowWon = (wId && rowPlayer.id)
                  ? wId === rowPlayer.id
                  : (!namesAmbiguous && winnerName && winnerName === rowPlayer.name);
                const isDraw = window.isHikiwake(m.decision) || window.isHikiwake(m.score?.type);

                let cellContent;
                let resultLabel;
                if (isDraw) {
                  // A scored draw shows the row player's own ippon letters
                  // (M/K/D/T/H); the draw colour (league-matrix__cell--draw)
                  // signals it was a tie, so we don't collapse a 1–1 to a bare
                  // "X". A scoreless draw has no letters → fall back to the "X"
                  // hikiwake glyph.
                  const drawLetters = rowIppons.join("");
                  cellContent = <span className="league-matrix__draw">{drawLetters || "X"}</span>;
                  resultLabel = "Draw";
                } else if (rowWon) {
                  // Show the row player's ippon letters (M/K/D/T/H); the green
                  // cell colour signals the win. No "W" fallback: W is not an
                  // ippon. A win with no recorded ippons (walkover/hantei)
                  // shows an empty green cell. Engi shows its own flag count
                  // (a number) instead: there are no ippon letters to show.
                  cellContent = <span className="league-matrix__win">{isEngiCell ? rowFlags : rowIppons.join("")}</span>;
                  resultLabel = "Win";
                } else {
                  // The loser's own ippons (red), or empty when they scored
                  // none. Engi shows its own flag count instead.
                  cellContent = <span className="league-matrix__loss">{isEngiCell ? rowFlags : rowIppons.join("")}</span>;
                  resultLabel = "Loss";
                }

                return (
                  <td key={`${pkey(colPlayer)}#${ci}`} title={cellTitle(rowPlayer, colPlayer, resultLabel)} className={`league-matrix__cell ${rowWon ? "league-matrix__cell--win" : isDraw ? "league-matrix__cell--draw" : "league-matrix__cell--loss"}${colMe}`} aria-label={cellLabel(rowPlayer, colPlayer, resultLabel)} {...interactiveProps}>
                    {cellContent}
                  </td>
                );
              })}
            </tr>
          ))}
        </tbody>
      </table>
      <div className="league-matrix__legend">
        <span className="league-matrix__legend-item league-matrix__legend-item--win">win</span>
        <span className="league-matrix__legend-item league-matrix__legend-item--loss">loss</span>
        <span className="league-matrix__legend-item league-matrix__legend-item--draw">draw</span>
        <span style={{ color: "var(--ink-3)", fontSize: 11 }}>letters = ippons (M/K/D/T/H/S, ○ = default win) · X = scoreless draw</span>
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
// sideB = Shiro (left), sideA = Aka (right): matches PoolMatchRow convention.
// isEngi: when true, each side is an engi pair — render member1 (name) over
// member2 (displayName) stacked, matching the engi score editor layout.
export const PoolNumberedMatchRow = React.memo(({ m, num, onMatchClick, isEngi }) => {
  // withNumber prepends the assigned competitor number (e.g. "K1") when present.
  const aName = typeof m.sideA === "object" ? withNumber(m.sideA) : m.sideA;
  const bName = typeof m.sideB === "object" ? withNumber(m.sideB) : m.sideB;
  // Engi pair: member2 lives in displayName.
  const aDN = isEngi && typeof m.sideA === "object" ? (m.sideA.displayName || "") : "";
  const bDN = isEngi && typeof m.sideB === "object" ? (m.sideB.displayName || "") : "";

  // DH badge: show which side won a completed daihyosen bout.
  const isDH = isPoolDaihyosenBout(m.id) && m.status === "completed";
  const winnerName = m.winner && typeof m.winner === "object" ? m.winner.name : m.winner;
  const nameOf = (side) => (typeof side === "object" ? side?.name : side) || "";
  const shiroWonDH = isDH && !!winnerName && winnerName === nameOf(m.sideB); // shiro = sideB
  const akaWonDH = isDH && !!winnerName && winnerName === nameOf(m.sideA);   // aka = sideA

  const handleClick = onMatchClick ? () => onMatchClick(m) : undefined;

  // Non-interactive <div> when there's no handler (read-only reuse) so the row
  // isn't a focusable control that does nothing.
  const Tag = handleClick ? "button" : "div";
  const interactiveProps = handleClick ? { type: "button", onClick: handleClick } : {};
  return (
    <Tag className={`pool-match-numbered-row${m.status === "running" ? " is-running" : ""}`} style={{ cursor: handleClick ? "pointer" : "default" }} {...interactiveProps}>
      <span className="pool-match-numbered-row__num">{num}</span>
      <div className="pool-match-numbered-row__side pool-match-numbered-row__side--shiro">
        <span className="sr-only">Shiro: </span>
        <span className="pool-match-numbered-row__name">{bName || "-"}</span>
        {shiroWonDH ? <DHBadge /> : null}
        {bDN ? <span className="pool-match-numbered-row__name">{bDN}</span> : null}
      </div>
      <span className="pool-match-numbered-row__score">
        {/* Running matches are signalled by the row highlight (shared .is-running),
            not a centre dot: matchStateCell shows "vs" for the live matchup,
            a score when completed, "–" when scheduled. */}
        {window.matchStateCell(m, m.ipponsB, m.ipponsA)}
      </span>
      <div className="pool-match-numbered-row__side pool-match-numbered-row__side--aka">
        <span className="sr-only">Aka: </span>
        <span className="pool-match-numbered-row__name">{aName || "-"}</span>
        {akaWonDH ? <DHBadge /> : null}
        {aDN ? <span className="pool-match-numbered-row__name">{aDN}</span> : null}
      </div>
    </Tag>
  );
});
PoolNumberedMatchRow.displayName = "PoolNumberedMatchRow";

export function PoolsViewer({ pools, standings, poolMatches, tweaks, competition, onMatchClick, highlightPlayers }) {
  const isTeam = competition && (competition.kind === "team" || competition.teamSize > 0);
  // Engi-Kyogi (kata demonstration): flag-count scoring. Standings show
  // Victories (V) and total Flags columns only.
  const isEngi = !!(competition && competition.engi);
  // FR-050 / FR-051: league competitions render Final standings instead of
  // pool standings, and surface a Winner badge once every match is complete.
  // Format check is value-based so older competitions without a Format
  // field render as before (no header relabel, no winner badge).
  const isLeague = competition && competition.format === "league";
  if (!pools || pools.length === 0) {
    return <EmptyState icon="⏳" title={isLeague ? "League not drawn yet" : "Pools not drawn yet"} />;
  }
  const allMatchesComplete = isLeague && (() => {
    const all = poolMatches || [];
    return all.length > 0 && all.every(m => m.status === "completed");
  })();
  // Winner is the top-ranked player from the (single) league standings.
  // pools[0] is the only pool for league formats: see internal/engine.
  const leagueWinner = (isLeague && allMatchesComplete && pools[0] && standings)
    ? (standings[pools[0].poolName] || [])[0]
    : null;
  const poolWinners = competition ? (competition.poolWinners || 2) : 2;

  return (
    <div className="pools-grid">
      {isLeague && leagueWinner && <div style={{ gridColumn: "1 / -1" }}><WinnerBadge name={leagueWinner.player?.name || ""} /></div>}
      {pools.map((pool) => {
        const poolStandings = standings ? standings[pool.poolName] : null;
        // Match ids belong to this pool by EXACT parsed pool name, not a raw
        // prefix: startsWith("Pool A-") would also swallow "Pool A-East-…"
        // ids when pool names overlap by prefix. poolNameOf strips any DH/TB
        // suffix and matches the backend's equality rule (daihyosen.go).
        const matches = poolMatches ? poolMatches.filter(m => poolNameOf(m.id) === pool.poolName) : [];

        // Teams that won a completed daihyosen (play-off) in this pool drive the
        // "DH" badge next to their name. The play-off decides order within the
        // tied group only (FIK 6.1.6.1.2) and never feeds the ranking cascade,
        // so the badge is the only visible signal that a play-off settled two
        // otherwise-identical rows.
        const dhWinnerNames = new Set(
          matches
            .filter(m => isPoolDaihyosenBout(m.id) && m.status === "completed" && m.winner)
            .map(m => (typeof m.winner === "string" ? m.winner : (m.winner && m.winner.name) || ""))
            .filter(Boolean)
        );

        // Build playerKey→{rank, standingEntry} map from the rank-sorted standings array.
        // Rank is the backend's authoritative PlayerStanding.rank (single source of
        // truth: it already folds in tiebreakers and manual rank overrides; see
        // engine/scoring.go). We do NOT re-derive it as index+1 here (DRY), falling
        // back to index+1 only if a legacy payload omitted the field.
        // Used to look up each draw-position row's rank without resorting the table
        // (mp-938b: table is draw-order, not rank-order).
        // Key = player.id when available; falls back to "name||dojo" composite so that
        // duplicate names across different dojos don't collide: mirrors the lookup below.
        const rankByPlayerKey = new Map();
        if (poolStandings) {
          poolStandings.forEach((s, i) => {
            const key = s.player.id || `${s.player.name}||${s.player.dojo || ""}`;
            rankByPlayerKey.set(key, { rank: s.rank || i + 1, standing: s });
          });
        }

        // A pool has nothing to rank until at least one bout is decided: every
        // competitor is tied at 0 and the backend rank is merely the seed/draw
        // fallback. Suppress rank badges + the "advancing" qualification
        // highlight until a result exists; provisional ranks then appear and
        // update live as more bouts finish. W/L/D cover both individual and
        // team standings (any decided encounter bumps one of them). Leagues are
        // always rank-ordered, so this gate is applied only to the non-league
        // pool badge/highlight below.
        const poolHasResults = !!poolStandings && poolStandings.some(
          (s) => (s.wins || 0) + (s.losses || 0) + (s.draws || 0) > 0
        );

        // Determine which players to iterate.
        // Both pools and leagues use pool.players draw order so operators can read
        // the fight-order chart alongside the standings. Fall back to rank-sorted
        // standings order only when pool.players is empty (e.g. legacy payloads).
        const drawOrderPlayers = (pool.players && pool.players.length)
          ? pool.players
          : (poolStandings ? poolStandings.map(s => s.player) : []);

        return (
          // A lone pool (every league has exactly one) must span the full grid
          // width. Otherwise the landscape 2-column `.pools-grid` renders it at
          // half width, mismatching the full-width Overview standings table.
          <div key={pool.poolName} className="pool" style={pools.length === 1 ? { gridColumn: "1 / -1" } : undefined}>
            <div className="pool__head">
              <div className="pool__name">{isLeague ? (allMatchesComplete ? "Final standings" : "Standings") : pool.poolName}</div>
              <div className="pool__match-count">
                {matches.filter(m => m.status === "completed").length}/{matches.length} matches
              </div>
            </div>

            {/* Standings table: rows in draw position order, rank looked up by player key (id or name||dojo) */}
            <table className={`pool__table${isTeam ? " pool__table--team" : ""}`}>
              {/* Clarifies the "#" column (draw order, not rank) for every reader
                  and doubles as the table's accessible caption. */}
              <caption className="pool__table-caption">Listed in draw order; badge shows current rank</caption>
              <thead>
                {isEngi ? (
                  <tr><th scope="col">#</th><th scope="col">Pair</th><th className="num" scope="col"><abbr title="Victories (bouts won)">V</abbr></th><th className="num" scope="col"><abbr title="Total flags received">Flags</abbr></th></tr>
                ) : isTeam ? (
                  <tr><th scope="col">#</th><th scope="col">Team</th><th className="num" scope="col"><abbr title="Team matches won">W</abbr></th><th className="num" scope="col"><abbr title="Team matches lost">L</abbr></th><th className="num" scope="col"><abbr title="Team matches tied">T</abbr></th><th className="num" scope="col"><abbr title="Individual victories">IV</abbr></th><th className="num" scope="col"><abbr title="Individual losses">IL</abbr></th><th className="num" scope="col"><abbr title="Individual ties (draws)">IT</abbr></th><th className="num" scope="col"><abbr title="Points won">PW</abbr></th><th className="num" scope="col"><abbr title="Points lost">PL</abbr></th></tr>
                ) : (
                  <tr><th scope="col">#</th><th scope="col">Player</th><th className="num" scope="col"><abbr title="Fights won">W</abbr></th><th className="num" scope="col"><abbr title="Fights lost">L</abbr></th><th className="num" scope="col"><abbr title="Draws (hikiwake)">D</abbr></th><th className="num" scope="col"><abbr title="Points won (ippon)">PW</abbr></th><th className="num" scope="col"><abbr title="Points lost">PL</abbr></th></tr>
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
                  const isAdvancing = !isLeague && poolHasResults && rank !== null && rank <= poolWinners;

                  const rowClasses = [
                    isPlayerWatched(p, highlightPlayers) ? "pool__row--me" : "",
                    isAdvancing ? "advancing" : "",
                    s && s.tied ? "pool__row--tied" : "",
                  ].filter(Boolean).join(" ");

                  return (
                    <tr key={p.id || `${p.name}||${p.dojo || ""}` || drawPos} className={rowClasses || undefined}>
                      {/* The "#" column is always the draw position. Both pools and
                          leagues use pool.players draw order; rank is surfaced
                          separately by the rank badge next to the name. */}
                      <td className="pool-standings__draw-pos">{drawPos}</td>
                      <td>
                        <div className="pool__player-name">
                          {p.number ? <span className="num-prefix">{p.number}</span> : null}
                          {p.name}
                          {isTeam && dhWinnerNames.has(p.name) && (!isLeague || (rank || drawPos) <= 3) && (
                            <DHBadge />
                          )}
                          {/* The rank badge shows for both pools and leagues: the "#"
                              column is always draw position, so the badge is the
                              authoritative standing rank signal for both formats. */}
                          {poolHasResults && rank !== null ? (
                            <span className={`rank-badge${isAdvancing ? " rank-badge--adv" : ""}`}>
                              {rankOrdinal(rank)}{s && s.isOverridden ? "*" : ""}
                            </span>
                          ) : null}
                        </div>
                        {/* Engi pair: member2 (displayName) stacked below member1. */}
                        {isEngi && p.displayName ? <div className="pool__player-name">{p.displayName}</div> : null}
                        {tweaks.showDojo ? <div className="pool__dojo-name">{p.dojo}</div> : null}
                      </td>
                      {s ? (
                        <>
                          {isEngi ? (
                            <>
                              <td className="num">{s.wins}</td>
                              <td className="num">{s.flags || 0}</td>
                            </>
                          ) : (
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
                          )}
                        </>
                      ) : (
                        Array.from({ length: isEngi ? 2 : isTeam ? 8 : 5 }, (_, j) => <td key={j} className="num">-</td>)
                      )}
                    </tr>
                  );
                })}
              </tbody>
            </table>

            {/* Numbered match list: shown for both individual and team pools */}
            {matches.length > 0 && (
              <>
                <div className="pool-match-section-label">Matches</div>
                <div className="pool-match-numbered-list">
                  {matches.map((m, idx) => {
                    if (isTeam) {
                      // Team: enrich match the same way as the legacy PoolMatchRow path.
                      // A daihyosen ('-DH-') or tiebreaker ('-TB-') is a single rep
                      // bout even in a team comp, so force compKind/teamSize to route
                      // it to the individual editor (matches enrichPoolMatchWithComp).
                      const isRepBout = isSupplementaryBout(m.id || "");
                      const enriched = { ...m, phase: "pool", poolName: pool.poolName, phaseName: pool.poolName, compFormat: competition.format, compId: competition.id, compName: competition.name, compKind: isRepBout ? "" : competition.kind, teamSize: isRepBout ? 0 : competition.teamSize };
                      return (
                        <PoolNumberedMatchRow
                          key={m.id}
                          m={m}
                          num={idx + 1}
                          onMatchClick={onMatchClick ? () => onMatchClick(enriched) : null}
                        />
                      );
                    } else {
                      // Individual or engi: delegates name/pair rendering to PoolNumberedMatchRow (isEngi passed through for name stacking)
                      const enriched = { ...m, phase: "pool", poolName: pool.poolName, phaseName: pool.poolName, compFormat: competition.format, compId: competition.id, compName: competition.name, compKind: "", teamSize: 0 };
                      return (
                        <PoolNumberedMatchRow
                          key={m.id}
                          m={m}
                          num={idx + 1}
                          isEngi={isEngi}
                          onMatchClick={onMatchClick ? () => onMatchClick(enriched) : null}
                        />
                      );
                    }
                  })}
                </div>
              </>
            )}

            {/* Round-robin matrix: optional grid below the match list (individual only) */}
            {matches.length > 0 && !isTeam && (
              <div style={{ marginTop: 4 }}>
                <LeagueMatrix pool={pool} matches={matches} tweaks={tweaks} onMatchClick={onMatchClick} highlightPlayers={highlightPlayers} />
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}
