// admin_draw_review.jsx: the Overview's Draw review card (bc-drev FR2, FR8,
// FR9), shown while a pools + knockout or knockout competition is draw-ready.
//
// It reads, it does not decide. First, and on its own (operator note P0: "dojo
// summary first after the draw. The swaps should be suggested as a separate
// step"), Dojo separation: one row per pair of competitors from the same dojo
// and where they can first meet (GET .../dojo-separation, the server's order),
// grouped by dojo, with the facts in one line under the card's title and the
// conclusion in a second (does a swap help, and where to find it).
// Below it, optional, Change the draw: a button that opens the preview tab
// (Pools, or the Bracket for a knockout-only draw) in its Swap competitors
// mode, where any two competitors are swapped by hand, then the swaps the
// server suggests (GET .../draw/swap-suggestions). Every swap opens the "If
// you swap them" check first (admin_draw_swap_check.jsx), which is the only
// door that sends one.
//
// Both reads are recomputed by the server from the stored draw, so the card
// reads them again on every change of the draw: after a swap made here, and on
// a draw_generated event for the competition (another device's swap, or the
// draw made again). The swap controls go once a result is recorded
// (competitionHasRecordedResult, the browser's mirror of the server's
// recordedResultIn): from then on the draw is fixed.

import { competitionHasRecordedResult } from './recorded_result.jsx';
import {
  DrawSwapCheck, effectiveFormat, drawPlaceLabel, poolLabel, competitorLabel, meetingRoundName, meetingChange, swapDoneToast,
  bothWinRound, meetingLateness,
} from './admin_draw_swap_check.jsx';
import { askSwapMode } from './swap_mode_request.jsx';

const { useState, useEffect } = React;

// reviewsDraw: the formats whose draw the card reviews, the ones with a bracket.
export function reviewsDraw(comp) {
  return !!comp && (effectiveFormat(comp) === 'mixed' || effectiveFormat(comp) === 'knockout');
}

// canFirstMeet: the "Can first meet" cell of one summary row: its first line,
// and a second one when winning both pools makes them meet in a later round
// than the earliest they can meet in ("Final if both win their pools"). Another
// match of the same round is not a second line, and the table names rounds
// only, never match numbers.
export function canFirstMeet(row) {
  if (row.samePool) return { first: `Share ${poolLabel(row.samePool)}`, second: '' };
  if (!row.earliest) return { first: 'Cannot meet in the knockout', second: '' };
  const both = bothWinRound(row);
  return { first: meetingRoundName(row.earliest), second: both ? `${both} if both win their pools` : '' };
}

const count = (n, one, many) => `${n} ${n === 1 ? one : many}`;

// onlyInFinal: the pair cannot meet before the final (or not at all).
const onlyInFinal = (row) => !row.samePool && (!row.earliest || meetingLateness(row)[0] === 0);

// drawVerdict: the card's one line under its title, counted from the summary
// rows: "12 pairs from 4 dojos. None share a pool; the earliest any can meet
// is the Quarterfinals (4 pairs).", "12 pairs from 4 dojos. 2 pairs share a
// pool." or "12 pairs from 4 dojos. They can meet only in the Final." A
// knockout-only draw has no pools to share, so it says nothing of them. Empty
// with no rows: the table already says no two competitors share a dojo.
export function drawVerdict(meetings, { knockoutOnly = false } = {}) {
  const rows = meetings || [];
  if (rows.length === 0) return '';
  const one = rows.length === 1;
  const head = `${count(rows.length, 'pair', 'pairs')} from ${count(new Set(rows.map((r) => r.dojo)).size, 'dojo', 'dojos')}.`;
  const shared = rows.filter((r) => r.samePool);
  if (shared.length > 0) {
    if (one) return `${head} They share ${poolLabel(shared[0].samePool)}.`;
    return `${head} ${count(shared.length, 'pair shares', 'pairs share')} a pool.`;
  }
  const meeting = rows.filter((r) => r.earliest);
  if (meeting.length === 0) return `${head} ${one ? 'They cannot' : 'None can'} meet in the knockout.`;
  if (rows.every(onlyInFinal)) return `${head} They can meet only in the Final.`;
  const depth = Math.min(...meeting.map((r) => meetingLateness(r)[0]));
  const first = meeting.filter((r) => meetingLateness(r)[0] === depth);
  const round = meetingRoundName(first[0].earliest);
  if (one) return knockoutOnly ? `${head} They can first meet in the ${round}.` : `${head} They do not share a pool and can first meet in the ${round}.`;
  const earliest = `earliest any can meet is the ${round} (${count(first.length, 'pair', 'pairs')})`;
  return knockoutOnly ? `${head} The ${earliest}.` : `${head} None share a pool; the ${earliest}.`;
}

// noSwapNeeded: no pair shares a pool and none can meet before the final, so
// there is nothing a swap could put later.
export function noSwapNeeded(meetings) {
  return Array.isArray(meetings) && meetings.every(onlyInFinal);
}

// drawConclusion: the card's second line under its title, what the facts above
// it mean for the organiser: "No swap is needed." when no pair could meet any
// later, "No swap can do better." when the server suggests none, otherwise how
// many it suggests and where they are. Empty while what it needs is not read,
// or when the suggestions could not be read (their section says so).
export function drawConclusion(meetings, suggestions) {
  if (noSwapNeeded(meetings)) return 'No swap is needed.';
  if (!Array.isArray(meetings) || !suggestions || !Array.isArray(suggestions.suggestions)) return '';
  const n = suggestions.suggestions.length;
  if (n === 0) return 'No swap can do better.';
  return n === 1
    ? '1 swap would keep dojo-mates apart longer. It is under Change the draw.'
    : `${n} swaps would keep dojo-mates apart longer. They are under Change the draw.`;
}

// swapModeSection: the preview tab whose Swap competitors mode swaps this
// competition's draw by hand: its pools, or its bracket for a knockout-only
// draw (swapModeAvailable, admin_competition_bracket.jsx).
export function swapModeSection(comp) {
  return effectiveFormat(comp) === 'knockout' ? 'bracket' : 'pools';
}

// byDojo: the summary rows grouped by dojo, the dojos in the order they first
// appear in the server's list (earliest meeting first, so the earliest
// meeting stays the first row) and each dojo's pairs in the server's order.
export function byDojo(meetings) {
  const groups = [];
  const index = new Map();
  for (const row of meetings || []) {
    if (!index.has(row.dojo)) { index.set(row.dojo, { dojo: row.dojo, rows: [] }); groups.push(index.get(row.dojo)); }
    index.get(row.dojo).rows.push(row);
  }
  return groups;
}

// improvementSentence: one pair a suggestion keeps apart longer.
export function improvementSentence(imp) {
  return `${imp.dojo}: ${meetingChange(imp.before, imp.after, true)}.`;
}

// useDrawReviewReads: the summary and the suggestions of one competition,
// read again whenever `generation` moves or a draw_generated event names it.
// Nothing is read while `enabled` is false (no draw to review).
function useDrawReviewReads(compId, password, generation, enabled) {
  const [separation, setSeparation] = useState({ id: null, meetings: null, error: null });
  const [suggestions, setSuggestions] = useState({ id: null, answer: null, error: null });
  const [eventGen, setEventGen] = useState(0);

  useEffect(() => {
    if (!enabled || !window.API || typeof window.API.subscribeToEvents !== 'function') return undefined;
    return window.API.subscribeToEvents((event) => {
      if (event && event.type === 'draw_generated' && event.data && event.data.competitionId === compId) setEventGen((g) => g + 1);
    });
  }, [compId, enabled]);

  useEffect(() => {
    if (!enabled) return undefined;
    let live = true;
    // Each read starts inside a promise, so whatever goes wrong with it is
    // said in its section and the page around the card never breaks over it.
    Promise.resolve().then(() => window.API.fetchDojoSeparation(compId, password))
      .then((meetings) => { if (live) setSeparation({ id: compId, meetings, error: null }); })
      .catch((e) => { if (live) setSeparation({ id: compId, meetings: null, error: e.message || 'Could not read the dojo separation.' }); });
    Promise.resolve().then(() => window.API.fetchSwapSuggestions(compId, password))
      .then((answer) => { if (live) setSuggestions({ id: compId, answer, error: null }); })
      .catch((e) => { if (live) setSuggestions({ id: compId, answer: null, error: e.message || 'Could not read the swap suggestions.' }); });
    return () => { live = false; };
  }, [compId, password, generation, eventGen, enabled]);

  // Held with the competition they describe, so another competition's rows
  // never show under this one's name while its reads are out.
  return {
    separation: separation.id === compId ? separation : { meetings: null, error: null },
    suggestions: suggestions.id === compId ? suggestions : { answer: null, error: null },
  };
}

// SeparationTable: one row per pair (operator ruling D6), each dojo named
// once: its pairs are a group of their own, the dojo cell spanning them.
function SeparationTable({ meetings }) {
  if (meetings.length === 0) {
    return <p className="draw-review__empty" data-testid="dojo-separation-empty">No two competitors share a dojo.</p>;
  }
  return (
    <div className="draw-review__table-wrap">
      <table className="table draw-review__table">
        <thead>
          <tr><th scope="col">Dojo</th><th scope="col">Pair</th><th scope="col">Can first meet</th></tr>
        </thead>
        {byDojo(meetings).map((group) => (
          <tbody key={group.dojo} className="draw-review__group" data-testid="dojo-separation-group">
            {group.rows.map((row, i) => {
              const meet = canFirstMeet(row);
              return (
                <tr key={`${row.a.id}\u001f${row.b.id}`} data-testid="dojo-separation-row" className={row.samePool ? 'draw-review__row--same-pool' : undefined}>
                  {i === 0 && <td className="draw-review__dojo" rowSpan={group.rows.length}>{group.dojo}</td>}
                  <td>
                    <div className="draw-review__who">{competitorLabel(row.a)}</div>
                    <div className="draw-review__who">{competitorLabel(row.b)}</div>
                  </td>
                  <td>
                    <div>{meet.first}</div>
                    {meet.second && <div className="draw-review__second">{meet.second}</div>}
                  </td>
                </tr>
              );
            })}
          </tbody>
        ))}
      </table>
    </div>
  );
}

// Suggestions: the swaps the server suggests, each with what it does and its
// own check button. None is a list of nothing: the card's conclusion line
// already says that no swap can do better.
function Suggestions({ answer, bracket, onPick }) {
  const { suggestions } = answer;
  return (
    <div data-testid="swap-suggestions">
      {suggestions.length > 0 && (
        <ul className="draw-review__suggestions">
          {suggestions.map((s) => (
            <li key={`${s.a.id}\u001f${s.b.id}`} className="draw-review__suggestion" data-testid="swap-suggestion">
              <div className="draw-review__suggestion-text">
                <div>
                  Swap <strong>{competitorLabel(s.a)}</strong> ({drawPlaceLabel(s.a.place, bracket)}) and{' '}
                  <strong>{competitorLabel(s.b)}</strong> ({drawPlaceLabel(s.b.place, bracket)})
                </div>
                {(s.improves || []).map((imp) => <div key={improvementSentence(imp)} className="draw-review__effect">{improvementSentence(imp)}</div>)}
                <div className="draw-review__effect">No dojo meets earlier.</div>
                {(s.byes || []).map((b) => (
                  <div key={b.id} className="draw-review__effect">
                    {b.bye ? `${b.name} gets a bye in the first round.` : `${b.name} loses their first-round bye.`}
                  </div>
                ))}
              </div>
              <button type="button" className="btn btn--sm" onClick={() => onPick(s.a, s.b)}
                aria-label={`Swap ${s.a.name} and ${s.b.name}`}>Swap</button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

/**
 * DrawReviewPanel: the Draw review card. Renders nothing for a competition
 * whose draw has no bracket to review (league, Swiss) or that is not
 * draw-ready.
 */
export function DrawReviewPanel({ c, poolMatches, bracket, password, showToast, onRefreshCompetition, onSection }) {
  const [generation, setGeneration] = useState(0);
  const [checking, setChecking] = useState(null);
  const shown = reviewsDraw(c) && c.status === 'draw-ready';
  const { separation, suggestions } = useDrawReviewReads(c.id, password, generation, shown);

  if (!shown) return null;

  const recorded = competitionHasRecordedResult(poolMatches, bracket);
  const knockoutOnly = effectiveFormat(c) === 'knockout';
  const verdict = separation.meetings ? drawVerdict(separation.meetings, { knockoutOnly }) : '';
  const conclusion = drawConclusion(separation.meetings, suggestions.error ? null : suggestions.answer);

  const readAgain = () => {
    setGeneration((g) => g + 1);
    if (onRefreshCompetition) onRefreshCompetition();
  };
  const onSwapped = (answer) => {
    const pair = checking;
    setChecking(null);
    if (showToast) showToast(swapDoneToast(answer, pair && pair.a, pair && pair.b));
    readAgain();
  };
  const onDrawChanged = (message) => {
    setChecking(null);
    if (showToast) showToast(message || 'The draw has changed. It has been read again.', 'error');
    readAgain();
  };
  const openSwapMode = () => {
    askSwapMode(c.id);
    onSection(swapModeSection(c));
  };

  return (
    <div className="card draw-review" data-testid="draw-review">
      <div className="card__head">
        <div className="card__head-main">
          <h2 className="card__title draw-review__title">Draw review</h2>
          {verdict && <p className="draw-review__verdict" data-testid="draw-review-verdict">{verdict}</p>}
          {!recorded && (
            // Drawn from the start a line high, so the suggestions arriving
            // move nothing where the sentence fits on one line.
            <p className="draw-review__conclusion" aria-live="polite" data-testid="draw-review-conclusion">
              {conclusion || '\u00a0'}
            </p>
          )}
        </div>
      </div>

      <section className="draw-review__section" aria-labelledby="draw-review-separation" data-testid="dojo-separation">
        <h3 id="draw-review-separation" className="draw-review__heading">Dojo separation</h3>
        <p className="draw-review__lead">Every pair of competitors from the same dojo, earliest meeting first.</p>
        {separation.error && <div className="alert alert--warn" role="alert">{separation.error}</div>}
        {!separation.error && !separation.meetings && <p className="draw-review__empty" role="status">Reading the draw…</p>}
        {separation.meetings && <SeparationTable meetings={separation.meetings} />}
      </section>

      {!recorded && (
        <section className="draw-review__section" aria-labelledby="draw-review-change" data-testid="draw-change">
          <h3 id="draw-review-change" className="draw-review__heading">Change the draw (optional)</h3>
          <p className="draw-review__lead">Swap two competitors if a pairing is wrong. Each swap is checked before it is made.</p>
          <button type="button" className="btn btn--sm draw-review__swap-mode" data-testid="draw-review-swap-mode" onClick={openSwapMode}>
            {knockoutOnly ? 'Swap competitors on the Bracket' : 'Swap competitors on Pools'}
          </button>
          {suggestions.error && <div className="alert alert--warn" role="alert">{suggestions.error}</div>}
          {!suggestions.error && !suggestions.answer && <p className="draw-review__empty" role="status">Looking for swaps…</p>}
          {suggestions.answer && (
            <Suggestions answer={suggestions.answer} bracket={bracket} onPick={(sa, sb) => setChecking({ a: sa, b: sb })} />
          )}
        </section>
      )}

      {checking && (
        <DrawSwapCheck comp={c} bracket={bracket} a={checking.a} b={checking.b} password={password}
          onClose={() => setChecking(null)} onSwapped={onSwapped} onDrawChanged={onDrawChanged} />
      )}
    </div>
  );
}
