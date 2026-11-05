// calendar_move.jsx: moving a competition on the calendar, by dragging its block
// or with its Move button. A competition that is not drawn moves by its date,
// start time and shiaijo; a drawn one (draw-ready) moves by date and start time
// only, and its matches move with it (the server re-times them in the same
// save). Undo puts back the settings and, for a drawn one, every match time
// exactly, or says that something changed since.

import { formatClockHHMM, parseClockTime } from './calendar_rules.jsx';
import { settingsPutBody } from './competition_put_body.jsx';
import { readRetimePreview, RetimeWarning } from './retime_warning.jsx';
import { PillButton } from './competition_fields.jsx';
import { useDialogFocus } from './dialog_focus.jsx';
import { useOpenedTapGuard } from './tap_guard.jsx';

const { useState, useEffect, useRef } = React;

export const SNAP_MINUTES = 5;
export const LONG_PRESS_MS = 450;
export const PX_PER_MINUTE = 1;

// A competition can be moved until it starts. A config with no status is one
// not yet drawn.
export function isDrawn(comp) {
  return comp?.status === 'draw-ready';
}

export function isMovable(comp) {
  const s = comp?.status;
  return s === undefined || s === null || s === '' || s === 'setup' || s === 'draw-ready';
}

/**
 * Shifts an array of courts by delta lanes, keeping court count and clamping at bounds.
 */
export function shiftLanes(courts, allCourts, delta) {
  if (!Array.isArray(courts) || courts.length === 0 || !Array.isArray(allCourts) || allCourts.length === 0) {
    return [];
  }
  const indices = courts.map(c => allCourts.indexOf(c)).filter(i => i !== -1);
  if (indices.length === 0) return [];
  const minIdx = Math.min(...indices);
  const maxIdx = Math.max(...indices);
  let shift = delta;
  if (minIdx + shift < 0) {
    shift = -minIdx;
  }
  if (maxIdx + shift >= allCourts.length) {
    shift = allCourts.length - 1 - maxIdx;
  }
  return indices.map(i => allCourts[i + shift]);
}

/**
 * Snaps a start to 5 minutes; a start that falls inside a pause moves to the
 * end of that pause. Stays within the day.
 */
export function snapStart(mins, breaks) {
  let m = Math.round(mins / SNAP_MINUTES) * SNAP_MINUTES;
  m = Math.max(0, Math.min(m, 24 * 60 - SNAP_MINUTES));
  const windows = (breaks || [])
    .map((b) => ({ start: b.startMins ?? b.StartMins, end: b.endMins ?? b.EndMins }))
    .filter((w) => Number.isFinite(w.start) && Number.isFinite(w.end))
    .sort((a, b) => a.start - b.start);
  for (const w of windows) {
    if (m >= w.start && m < w.end) m = w.end;
  }
  return Math.min(m, 24 * 60 - SNAP_MINUTES);
}

/**
 * Where a block would land if dropped now.
 *
 * geometry: {
 *   days: [{ day, left, top, right, bottom, breaks }]  // each day body on screen
 *   laneWidth, startHour, courts: all shiaijo labels in lane order
 * }
 * block: { left, top } the dragged block's top left corner on screen,
 *        { courts } its shiaijo labels, { minCourtIdx }
 *
 * Returns { day, startMins, courts } or null when the block is not over a day.
 */
export function dropTarget({ geometry, blockLeft, blockTop, pointerX, pointerY, blockCourts }) {
  const days = geometry?.days || [];
  const over = days.find((d) => pointerX >= d.left && pointerX < d.right && pointerY >= d.top - 40 && pointerY <= d.bottom + 40);
  if (!over) return null;
  const laneWidth = geometry.laneWidth || 1;
  const startMins = snapStart(geometry.startHour * 60 + (blockTop - over.top) / PX_PER_MINUTE, over.breaks);
  const allCourts = geometry.courts || [];
  const indices = (blockCourts || []).map((c) => allCourts.indexOf(c)).filter((i) => i !== -1);
  const currentMin = indices.length ? Math.min(...indices) : 0;
  const wantedMin = Math.round((blockLeft - over.left) / laneWidth);
  const courts = shiftLanes(blockCourts, allCourts, wantedMin - currentMin);
  return { day: over.day, startMins, courts };
}

/** Adds minutes to an "HH:MM" start, or null when it leaves the day. */
export function shiftClock(startTime, deltaMins) {
  const m = parseClockTime(startTime);
  if (m === null) return null;
  const n = m + deltaMins;
  if (n < 0 || n >= 24 * 60) return null;
  return formatClockHHMM(n);
}

function sameList(a, b) {
  return (a || []).length === (b || []).length && (a || []).every((v, i) => v === b[i]);
}

/**
 * Reads the competition again, then saves the move through the same settings
 * save the Settings page uses, under the version just read. For a drawn
 * competition the server moves its matches in that same save.
 *
 * Give either startTime ("HH:MM") or deltaMins (move the start by that many
 * minutes, as a drag does).
 */
export async function saveMove({ password, competitionId, date, startTime, deltaMins, courts }) {
  const comps = await window.API.fetchCompetitions();
  const fresh = (Array.isArray(comps) ? comps : []).find((c) => c.id === competitionId);
  if (!fresh) throw new Error('This competition is no longer there.');
  if (!isMovable(fresh)) throw new Error('This competition has started, so it can no longer be moved.');
  const start = startTime || shiftClock(fresh.startTime, deltaMins || 0);
  if (!start) throw new Error('That would move the start out of the day. Pick a start time on the same day.');
  const drawn = isDrawn(fresh);
  const edits = { date, startTime: start };
  if (!drawn && Array.isArray(courts) && courts.length > 0) edits.courts = courts;
  const built = settingsPutBody(fresh, edits, fresh.configVersion);
  if (built.error) throw new Error(built.error);
  const answer = await window.API.updateCompetition(competitionId, built.body, password);
  return {
    competitionId,
    name: fresh.name,
    drawn,
    answer,
    before: { date: fresh.date, startTime: fresh.startTime, courts: fresh.courts },
    after: { date, startTime: start, courts: edits.courts || fresh.courts },
  };
}

/**
 * Undoes a saved move: the previous date, start time and shiaijo, sent under
 * the version the move answered with. A drawn competition also sends the move's
 * previousTimes back, even when empty, so every match returns to its exact
 * time, or the server says that something changed since and writes nothing.
 */
export async function undoMove({ password, move }) {
  const comps = await window.API.fetchCompetitions();
  const fresh = (Array.isArray(comps) ? comps : []).find((c) => c.id === move.competitionId);
  if (!fresh) throw new Error('This competition is no longer there.');
  const edits = { date: move.before.date, startTime: move.before.startTime };
  if (!move.drawn) edits.courts = move.before.courts;
  const built = settingsPutBody(fresh, edits, move.answer.configVersion);
  if (built.error) throw new Error(built.error);
  const body = move.drawn ? { ...built.body, restoreMatchTimes: move.answer.previousTimes || [] } : built.body;
  return window.API.updateCompetition(move.competitionId, body, password);
}

/** The sentence an operator reads for a move or an undo that did not happen. */
export function moveErrorMessage(err) {
  if (!err) return 'Could not move the competition.';
  if (err.code === 'config_changed') return 'This competition changed, reload it.';
  return err.message || 'Could not move the competition.';
}

/** Whether a Move dialog's fields name a move the server can be asked about. */
export function moveFieldsReady({ date, startTime, courts }) {
  return !!date && parseClockTime(startTime) !== null && Array.isArray(courts) && courts.length > 0;
}

function placementSummary(placement) {
  const windows = Object.values(placement?.windows || {});
  const starts = windows.map((w) => w.start ?? w.Start).filter(Number.isFinite);
  const ends = windows.map((w) => w.end ?? w.End).filter(Number.isFinite);
  if (starts.length === 0 || ends.length === 0) return '';
  const about = placement.lengthSource === 'draw' ? '' : 'about ';
  return `Runs ${formatClockHHMM(Math.min(...starts))} to ${about}${formatClockHHMM(Math.max(...ends) % 1440)}.`;
}

/**
 * The Move dialog: day, start time and shiaijo, with what the move would do.
 * dayOptions: [{ date, label }]. onSave({ date, startTime, courts }) returns a
 * promise; a refusal stays in the dialog with the operator's edit.
 */
export function MoveDialog({ comp, dayOptions, allCourts, password, initial, onSave, onClose }) {
  const drawn = isDrawn(comp);
  const [fresh, setFresh] = useState(null);
  const [loadErr, setLoadErr] = useState(null);
  const [date, setDate] = useState(initial?.date || '');
  const [startTime, setStartTime] = useState(initial?.startTime || '');
  const [courts, setCourts] = useState(initial?.courts || []);
  const [preview, setPreview] = useState(null);
  const [previewErr, setPreviewErr] = useState(null);
  const [saving, setSaving] = useState(false);
  const [saveErr, setSaveErr] = useState(null);
  const boxRef = useRef(null);
  const { openedRef, onClickCapture } = useOpenedTapGuard();
  useDialogFocus(boxRef, true);

  useEffect(() => {
    let cancelled = false;
    window.API.fetchCompetitions().then((comps) => {
      if (cancelled) return;
      const c = (Array.isArray(comps) ? comps : []).find((x) => x.id === comp.id);
      if (!c) { setLoadErr('This competition is no longer there.'); return; }
      setFresh(c);
      setDate((d) => d || c.date || '');
      setStartTime((t) => t || c.startTime || '');
      setCourts((cs) => (cs.length ? cs : (c.courts || [])));
    }).catch((e) => { if (!cancelled) setLoadErr(e.message || 'Could not read the competition.'); });
    return () => { cancelled = true; };
  }, [comp.id]);

  const shownCourts = drawn && fresh ? (fresh.courts || []) : courts;
  const ready = moveFieldsReady({ date, startTime, courts: shownCourts });
  const courtsKey = shownCourts.join(',');

  useEffect(() => {
    setPreview(null);
    setPreviewErr(null);
    if (!fresh || !ready) return undefined;
    const controller = new AbortController();
    const timer = setTimeout(() => {
      window.API.previewMove({ competitionId: comp.id, date, startTime, courts: courtsKey.split(',') }, password, controller.signal)
        .then((answer) => { if (!controller.signal.aborted) setPreview(answer); })
        .catch((e) => { if (!controller.signal.aborted) setPreviewErr(e.message || 'Could not work out the new times.'); });
    }, 250);
    return () => { clearTimeout(timer); controller.abort(); };
  }, [fresh, ready, date, startTime, courtsKey, comp.id, password]);

  const retime = drawn ? readRetimePreview(preview) : null;
  const unchanged = fresh && date === fresh.date && startTime === fresh.startTime && (drawn || sameList(courts, fresh.courts));

  const save = async () => {
    setSaving(true);
    setSaveErr(null);
    try {
      await onSave({ date, startTime, courts: shownCourts });
    } catch (e) {
      setSaveErr(moveErrorMessage(e));
      setSaving(false);
    }
  };

  const toggleCourt = (court) => {
    setCourts((cs) => (cs.includes(court) ? cs.filter((c) => c !== court) : allCourts.filter((c) => cs.includes(c) || c === court)));
  };

  return (
    <div className="modal-backdrop" ref={openedRef} onClickCapture={onClickCapture} onClick={() => !saving && onClose()}>
      <div className="shiaijo-move-confirm cal-move-dialog" role="dialog" aria-modal="true" ref={boxRef}
        aria-labelledby="cal-move-title" data-testid="cal-move-dialog" onClick={(e) => e.stopPropagation()}>
        <h3 id="cal-move-title" className="shiaijo-move-confirm__title">Move {comp.name}</h3>
        {loadErr && <div className="alert alert--error" role="alert">{loadErr}</div>}
        {!fresh && !loadErr && <p className="shiaijo-move-confirm__body">Reading the competition...</p>}
        {fresh && (
          <div className="cal-move-dialog__fields">
            <div className="field">
              <label className="field__label" htmlFor="cal-move-day">Day</label>
              <select id="cal-move-day" className="input" value={date} onChange={(e) => setDate(e.target.value)} disabled={saving}>
                {dayOptions.map((o) => <option key={o.date} value={o.date}>{o.label}</option>)}
              </select>
            </div>
            <div className="field">
              <label className="field__label" htmlFor="cal-move-start">Start time</label>
              <input id="cal-move-start" className="input" type="text" inputMode="numeric" placeholder="09:00"
                value={startTime} onChange={(e) => setStartTime(e.target.value.trim())} disabled={saving} />
              {startTime !== '' && parseClockTime(startTime) === null && (
                <div className="field__hint field__hint--error">Write the start as hours and minutes, like 09:30.</div>
              )}
            </div>
            <div className="field">
              <span className="field__label">Shiaijo</span>
              <div className="radio-group" role="group" aria-label="Shiaijo">
                {allCourts.map((court) => {
                  const on = shownCourts.includes(court);
                  return (
                    <PillButton key={court} active={on} disabled={drawn || saving} onClick={() => toggleCourt(court)}>{court}</PillButton>
                  );
                })}
              </div>
              {drawn && (
                <div className="field__hint">
                  Its shiaijo stay as drawn. To use other shiaijo, discard the draw first.
                </div>
              )}
            </div>
            {previewErr && <div className="alert alert--warn" role="status">{previewErr}</div>}
            {!drawn && preview && placementSummary(preview.placement) && (
              <p className="shiaijo-move-confirm__body" aria-live="polite">{placementSummary(preview.placement)}</p>
            )}
            {!drawn && preview && (preview.overlaps || []).length > 0 && (
              <div className="alert alert--warn" role="status">
                {`Overlaps ${preview.overlaps.map((o) => `${o.name} on Shiaijo ${o.court}`).join(', ')}. You can still move it.`}
              </div>
            )}
            {drawn && <RetimeWarning preview={retime} lead="Moving it moves" />}
            {drawn && retime && !retime.anyChange && !unchanged && (
              <p className="shiaijo-move-confirm__body">No match changes its time.</p>
            )}
          </div>
        )}
        {saveErr && <div className="alert alert--error" role="alert">{saveErr}</div>}
        <div className="shiaijo-move-confirm__actions">
          <button type="button" className="btn" onClick={onClose} disabled={saving}>Cancel</button>
          <button type="button" className="btn btn--primary" onClick={save} disabled={!fresh || !ready || unchanged || saving}>
            {saving ? 'Moving...' : 'Move'}
          </button>
        </div>
      </div>
    </div>
  );
}
