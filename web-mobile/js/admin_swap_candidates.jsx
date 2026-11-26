// admin_swap_candidates.jsx: the marks of the Swap competitors mode (bc-drev
// K4). Once the organiser picks a first competitor on Pools (preview) or
// Bracket (preview), every other drawn competitor is marked by what swapping
// the two would do to dojo-mates, each dojo judged as a whole: green
// "better" keeps a dojo apart for longer and leaves none worse off, amber
// "worse" leaves a dojo worse off (earlier, or into one pool), and a plain
// pick changes no dojo. The server judges each candidate (GET
// .../draw/swap-candidates, by the same rule as the swap check's before and
// after), this module only reads it and words it, and useSwapCompetitors
// (admin_competition_bracket.jsx) is its one caller, for both tabs.
//
// A mark is advice, never a gate: the swap itself still goes through "If you
// swap them", so a read that fails shows no marks and the swap works as
// before. The marks' words are drawn from the moment the mode is on, and the
// banner's key line from the pick on, each hidden until it shows, so the
// marks arriving move nothing under the finger.

import { DRAW_MOVED_CODES } from './admin_draw_swap_check.jsx';
import { useLatestRef } from './latest_ref.jsx';

const { useState, useEffect } = React;

// The refusals that mean the draw on screen is no longer the stored one, as
// for the swap, plus not_in_draw: the draw no longer holds the pick.
const CANDIDATES_DRAW_MOVED = new Set([...DRAW_MOVED_CODES, 'not_in_draw']);

// The words a mark carries beside its colour. "same" carries none.
const VERDICT_WORDS = { better: 'better', worse: 'worse' };

// candidateVerdicts: the server's answer for one pick as a map from each
// candidate's id to its mark ("better" or "worse"; "same" and anything else
// leave the candidate plain). An answer about another pick is no answer.
export function candidateVerdicts(answer, pickId) {
  const marks = new Map();
  if (!answer || !pickId || (answer.pick && answer.pick.id && answer.pick.id !== pickId)) return marks;
  for (const cand of answer.candidates || []) {
    if (cand && cand.id && cand.id !== pickId && VERDICT_WORDS[cand.verdict]) marks.set(cand.id, cand.verdict);
  }
  return marks;
}

// MARK_KEYS: every sentence the banner's key line can say: that the marks
// are on their way while the read is out, then which colours show and what
// they mean, that no swap changes anything, or that the swaps could not be
// marked (the first and the last two quiet).
export const MARK_KEYS = {
  loading: { text: 'Marking the swaps…', quiet: true },
  both: { text: 'Green keeps dojo-mates apart longer, amber brings some closer.', quiet: false },
  better: { text: 'Green keeps dojo-mates apart longer.', quiet: false },
  worse: { text: 'Amber brings some dojo-mates closer.', quiet: false },
  same: { text: 'No swap changes when dojo-mates can meet.', quiet: true },
  failed: { text: 'The swaps could not be marked.', quiet: true },
};

// marksKey: which of MARK_KEYS the key line shows for the marks as read, or
// 'loading' while the read is out.
export function marksKey({ marks, failed }) {
  if (failed) return 'failed';
  if (!marks) return 'loading';
  const vs = [...marks.values()];
  const better = vs.includes('better');
  const worse = vs.includes('worse');
  if (better && worse) return 'both';
  if (better) return 'better';
  return worse ? 'worse' : 'same';
}

// SwapMarksKey: the banner's key line, drawn from the pick on with EVERY
// sentence of MARK_KEYS stacked in one grid cell and only the current one
// visible ('loading' while the read is out; none while the swap check is
// open), so the line holds the height of its longest sentence from the pick
// on and nothing below it moves when the read answers, fails, or finds no
// swap that changes anything.
export function SwapMarksKey({ current }) {
  return (
    <div className="swap-mode__key" aria-live="polite" data-testid="swap-marks-key">
      {Object.entries(MARK_KEYS).map(([key, line]) => (
        <span key={key} data-key={key} aria-hidden={key === current ? undefined : 'true'}
          className={`swap-mode__key-line${line.quiet ? ' swap-mode__key-line--quiet' : ''}${key === current ? ' is-shown' : ''}`}>
          {line.text}
        </span>
      ))}
    </div>
  );
}

const EMPTY = { pickId: null, drawKey: null, marks: null, failed: false };

// useSwapCandidates: the marks for the competitor picked first, read when the
// pick changes or the draw changes (`drawKey`, where every competitor of the
// draw the host shows stands: a swap made on another device keeps the pick
// but moves places, so the old marks are dropped until the new read answers,
// while a re-read of the same draw keeps them). The answer of a read is kept only while its pick and draw are
// still the ones on screen (a newer pick, a cancel or a swap drops it), and
// a draw that moved under the page goes to onDrawChanged, the swap check's
// own path.
export function useSwapCandidates({ compId, pickId, drawKey = null, password, onDrawChanged }) {
  const [state, setState] = useState(EMPTY);
  const changedRef = useLatestRef(onDrawChanged);
  useEffect(() => {
    setState(EMPTY);
    if (!pickId) return undefined;
    let live = true;
    Promise.resolve().then(() => window.API.fetchSwapCandidates(compId, pickId, password))
      .then((answer) => { if (live) setState({ pickId, drawKey, marks: candidateVerdicts(answer, pickId), failed: false }); })
      .catch((e) => {
        if (!live) return;
        if (e && CANDIDATES_DRAW_MOVED.has(e.code)) { changedRef.current(e.message); return; }
        setState({ pickId, drawKey, marks: null, failed: true });
      });
    return () => { live = false; };
  }, [compId, pickId, drawKey, password, changedRef]);
  const current = pickId && state.pickId === pickId && state.drawKey === drawKey;
  return current ? { marks: state.marks, failed: state.failed } : { marks: null, failed: false };
}

// SwapVerdictWord: the word beside a candidate's colour, ", better" or ",
// worse" (`lead` false drops the comma, for a line with nothing before it).
// Both words are always drawn in one grid cell, only the shown one visible,
// so the space they take is the same before, during and after the marks.
export function SwapVerdictWord({ mark, lead }) {
  const comma = lead ? ', ' : '';
  return (
    <span className="swap-verdict" data-testid="swap-verdict-word">
      {Object.keys(VERDICT_WORDS).map((v) => (
        <span key={v} className={`swap-verdict__word swap-verdict__word--${v}${mark === v ? ' is-shown' : ''}`}
          aria-hidden={mark === v ? undefined : 'true'}>
          {comma}{VERDICT_WORDS[v]}
        </span>
      ))}
    </span>
  );
}
