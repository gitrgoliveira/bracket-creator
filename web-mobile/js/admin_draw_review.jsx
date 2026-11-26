// admin_draw_review.jsx: the Overview's Draw review card (bc-drev FR2, FR8,
// FR9), shown while a pools + knockout or knockout competition is draw-ready.
//
// It reads, it does not decide. First, and on its own (operator note P0: "dojo
// summary first after the draw. The swaps should be suggested as a separate
// step"), Dojo separation: one row per pair of competitors from the same dojo
// and where they can first meet (GET .../dojo-separation, the server's order).
// Below it, optional, Change the draw: the swaps the server suggests
// (GET .../draw/swap-suggestions), then two native selects to pick a swap by
// hand. Every swap opens the "If you swap them" check first
// (admin_draw_swap_check.jsx), which is the only door that sends one.
//
// Both reads are recomputed by the server from the stored draw, so the card
// reads them again on every change of the draw: after a swap made here, and on
// a draw_generated event for the competition (another device's swap, or the
// draw made again). The swap controls go once a result is recorded
// (competitionHasRecordedResult, the browser's mirror of the server's
// recordedResultIn): from then on the draw is fixed. The pickers have no typed
// filter, so the competitor-number search rule does not apply to them.

import { competitionHasRecordedResult } from './recorded_result.jsx';
import { andList } from './list_phrase.jsx';
import {
  DrawSwapCheck, effectiveFormat, drawEntrants, drawPlaceLabel, poolLabel, competitorLabel, meetingRoundName, meetingChange, swapDoneToast,
} from './admin_draw_swap_check.jsx';

const { useState, useEffect } = React;

// reviewsDraw: the formats whose draw the card reviews, the ones with a bracket.
export function reviewsDraw(comp) {
  return !!comp && (effectiveFormat(comp) === 'mixed' || effectiveFormat(comp) === 'knockout');
}

// canFirstMeet: the "Can first meet" cell of one summary row: its first line,
// and a second one when winning both pools makes them meet later than the
// earliest place they can meet at ("Final if both win their pools").
export function canFirstMeet(row) {
  if (row.samePool) return { first: `Share ${poolLabel(row.samePool)}`, second: '' };
  if (!row.earliest) return { first: 'Cannot meet in the knockout', second: '' };
  const first = meetingRoundName(row.earliest);
  const later = row.ifBothWin && row.ifBothWin.matchId !== row.earliest.matchId;
  return { first, second: later ? `${meetingRoundName(row.ifBothWin)} if both win their pools` : '' };
}

// stuckSentences: the dojos no suggested swap helps, and why that is all the
// app can say about them. A dojo whose pair shares a pool keeps a line of its
// own, since its pool is its reason; the others share one line, since the
// table above already gives each its round.
export function stuckSentences(stuck) {
  const lines = [];
  const meeting = [];
  for (const st of stuck || []) {
    const m = st.meeting || {};
    if (m.samePool) lines.push(`${st.dojo}: no suggested swap takes them out of ${poolLabel(m.samePool)} without making another dojo meet earlier.`);
    else meeting.push(st.dojo);
  }
  if (meeting.length > 0) lines.push(`For ${andList(meeting)}, no suggested swap makes their first meeting later without making another dojo meet earlier.`);
  return lines;
}

// improvementSentence: one pair a suggestion keeps apart longer.
export function improvementSentence(imp) {
  return `${imp.dojo}: ${meetingChange(imp.before, imp.after, true)}.`;
}

// optionLabel: a competitor in a picker: number, name, dojo, a seed marked.
// The seed is set off by a comma, never a middle dot (list_phrase.jsx).
export function optionLabel(e) {
  const seed = e.seed > 0 ? `, seed ${e.seed}` : '';
  return `${competitorLabel(e)}${e.dojo ? ` (${e.dojo})` : ''}${seed}`;
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
        <tbody>
          {meetings.map((row) => {
            const meet = canFirstMeet(row);
            return (
              <tr key={`${row.a.id}\u001f${row.b.id}`} data-testid="dojo-separation-row" className={row.samePool ? 'draw-review__row--same-pool' : undefined}>
                <td>{row.dojo}</td>
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
      </table>
    </div>
  );
}

function Suggestions({ answer, bracket, onPick }) {
  const { suggestions, stuck } = answer;
  return (
    <div data-testid="swap-suggestions">
      {suggestions.length === 0 && <p className="draw-review__empty">No swap keeps dojo-mates apart for longer.</p>}
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
                <div className="draw-review__effect">Nobody else meets earlier.</div>
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
      {stuck.length > 0 && (
        <ul className="draw-review__stuck" data-testid="swap-stuck">
          {stuckSentences(stuck).map((line) => <li key={line}>{line}</li>)}
        </ul>
      )}
    </div>
  );
}

function EntrantSelect({ id, label, groups, value, onChange, disabled, testId }) {
  return (
    <div className="field draw-review__pick">
      <label className="field__label" htmlFor={id}>{label}</label>
      <select id={id} className="select" value={value} onChange={(e) => onChange(e.target.value)} disabled={disabled} data-testid={testId}>
        <option value="">Choose a competitor</option>
        {groups.map((g) => (
          <optgroup key={g.label} label={g.label}>
            {g.entrants.map((e) => <option key={e.id} value={e.id}>{optionLabel(e)}</option>)}
          </optgroup>
        ))}
      </select>
    </div>
  );
}

/**
 * DrawReviewPanel: the Draw review card. Renders nothing for a competition
 * whose draw has no bracket to review (league, Swiss) or that is not
 * draw-ready.
 */
export function DrawReviewPanel({ c, pools, poolMatches, bracket, password, showToast, onRefreshCompetition }) {
  const [generation, setGeneration] = useState(0);
  const [pickA, setPickA] = useState('');
  const [pickB, setPickB] = useState('');
  const [checking, setChecking] = useState(null);
  const shown = reviewsDraw(c) && c.status === 'draw-ready';
  const { separation, suggestions } = useDrawReviewReads(c.id, password, generation, shown);

  if (!shown) return null;

  const groups = drawEntrants(c, pools, bracket);
  const byId = new Map();
  groups.forEach((g) => g.entrants.forEach((e) => byId.set(e.id, e)));
  // A pick the draw no longer holds (it changed under the page) reads as none.
  const a = byId.get(pickA) || null;
  const b = byId.get(pickB) || null;
  const recorded = competitionHasRecordedResult(poolMatches, bracket);

  const readAgain = () => {
    setGeneration((g) => g + 1);
    if (onRefreshCompetition) onRefreshCompetition();
  };
  const resetPicks = () => { setPickA(''); setPickB(''); };
  const onSwapped = (answer) => {
    const pair = checking;
    setChecking(null);
    resetPicks();
    if (showToast) showToast(swapDoneToast(answer, pair && pair.a, pair && pair.b));
    readAgain();
  };
  const onDrawChanged = (message) => {
    setChecking(null);
    resetPicks();
    if (showToast) showToast(message || 'The draw has changed. It has been read again.', 'error');
    readAgain();
  };

  return (
    <div className="card draw-review" data-testid="draw-review">
      <div className="card__head">
        <div className="card__title">Draw review</div>
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
          {suggestions.error && <div className="alert alert--warn" role="alert">{suggestions.error}</div>}
          {!suggestions.error && !suggestions.answer && <p className="draw-review__empty" role="status">Looking for swaps…</p>}
          {suggestions.answer && (
            <Suggestions answer={suggestions.answer} bracket={bracket} onPick={(sa, sb) => setChecking({ a: sa, b: sb })} />
          )}

          <h4 className="draw-review__subheading">Or pick your own swap</h4>
          <div className="draw-review__picks">
            <EntrantSelect id="draw-swap-a" label="Competitor" groups={groups} value={a ? pickA : ''} onChange={setPickA}
              disabled={!!checking} testId="swap-pick-a" />
            <EntrantSelect id="draw-swap-b" label="Swap with" groups={groups} value={b ? pickB : ''} onChange={setPickB}
              disabled={!!checking} testId="swap-pick-b" />
          </div>
          <button type="button" className="btn btn--primary" data-testid="swap-check-btn"
            disabled={!a || !b || a.id === b.id || !!checking} onClick={() => setChecking({ a, b })}>
            Check this swap
          </button>
          {a && b && a.id === b.id && <div className="field__hint">Pick two different competitors.</div>}
        </section>
      )}

      {checking && (
        <DrawSwapCheck comp={c} bracket={bracket} a={checking.a} b={checking.b} password={password}
          onClose={() => setChecking(null)} onSwapped={onSwapped} onDrawChanged={onDrawChanged} />
      )}
    </div>
  );
}
