// The court console's shiaijo control (admin_shiaijo.jsx, bc-sseq FR9): the
// mark under the top bar that says whether this tab runs the shiaijo, its Take
// control with the one question it asks, and the check the console's own
// handlers make before they act.
//
// court_control.jsx owns the control itself (the ids, the claim and heartbeat,
// the release, the one write gate the outbox asks). This module only shows it
// and asks it:
//   - useShiaijoControl(court, password, showToast) mounts the control for the
//     console (useCourtControl) and answers { view, canWrite, controlRef }.
//     `canWrite` is true while this tab runs the shiaijo, or has had no answer
//     yet (offline: an unconnected console still scores, and its results claim
//     a free shiaijo on arrival, FR8). `controlRef` is what the console's hooks
//     read, so a handler judges the control as it is when tapped.
//   - refusedNotHeld(controlRef, { quiet }) is the ONE check those handlers
//     make: true (and a toast, unless quiet) when this tab may not write.
//   - ShiaijoControlBar draws the mark for every state, with Take control.
//
// The bar is a static banner, never a pulse (DESIGN.md principle 3: a holder is
// ordinary ongoing state): green (--ok) with a filled dot for the holder, grey
// for a shiaijo another tab or device runs, amber (--warn) for a shiaijo this
// tablet lost, a free one it lost earlier, and a console with no connection.

import { useCourtControl } from './court_control.jsx';
import { serverNowMs } from './server_clock.jsx';
import { useDialogFocus } from './dialog_focus.jsx';
import { useOpenedTapGuard, acceptTap } from './tap_guard.jsx';
import { courtNotHeldError } from './write_result.jsx';

const { useState, useEffect, useRef } = React;

// How long a console that has had no answer yet shows "Checking" before it
// says it is not connected: a claim on a working connection answers well
// inside it, so an ordinary open never flashes the amber line.
export const CONTROL_CHECK_GRACE_MS = 1500;

// The device word the server keeps (court_control.jsx deviceWord), as a phrase.
const DEVICE_PHRASE = {
    ipad: 'an iPad',
    'android-tablet': 'an Android tablet',
    iphone: 'an iPhone',
    'android-phone': 'an Android phone',
    mac: 'a Mac',
    windows: 'a Windows PC',
};

/** "an iPad", ... or '' for a device the server reads as `other`. */
export function devicePhrase(word) {
    return DEVICE_PHRASE[String(word || '')] || '';
}

/** "last active just now" / "last active 2 min ago" / "last active 3 h ago", or '' with no time. */
export function lastActivePhrase(at, now = serverNowMs()) {
    const t = Number(at) || 0;
    if (t <= 0) return '';
    const mins = Math.floor(Math.max(0, Number(now) - t) / 60_000);
    if (mins < 1) return 'last active just now';
    if (mins < 60) return `last active ${mins} min ago`;
    return `last active ${Math.floor(mins / 60)} h ago`;
}

/** Whether the console may write: it runs the shiaijo, or has had no answer yet. */
export function courtControlWrites(view) {
    return !!view && (view.state === 'holds' || view.state === 'unknown');
}

/**
 * What the bar shows for `view` (useCourtControl's) on `court`:
 * { tone: 'holds' | 'other' | 'warn' | 'checking', text, take, ask }.
 * `take` offers Take control; `ask` is the one question it asks first (null:
 * nobody runs the shiaijo, so taking it stops no one). `checked` is false
 * while a console that has had no answer is still inside its grace.
 */
export function courtControlBar(view, court, { now = serverNowMs(), checked = true, online = true } = {}) {
    const c = String(court || '');
    const state = view ? view.state : 'unknown';
    if (state === 'holds') {
        return { tone: 'holds', text: `This tablet runs shiaijo ${c}`, take: false, ask: null };
    }
    if (state === 'other' && view.sameDevice) {
        const active = lastActivePhrase(view.holderLastActiveAt, now);
        return {
            tone: 'other',
            text: `Shiaijo ${c} is run from another tab on this tablet${active ? ` (${active})` : ''}. Results this tablet has queued still count as the court's.`,
            take: true,
            ask: `The other tab on this tablet will stop running shiaijo ${c}. Take control?`,
        };
    }
    if (state === 'other' && view.youWereDisplaced) {
        return {
            tone: 'warn',
            text: `Shiaijo ${c} is now run from another device. Results this tablet still has queued count like any other device's.`,
            take: true,
            ask: `The other device will stop running shiaijo ${c}. Take control?`,
        };
    }
    if (state === 'other') {
        const parts = [devicePhrase(view.holderDevice), lastActivePhrase(view.holderLastActiveAt, now)].filter(Boolean);
        return {
            tone: 'other',
            text: `Shiaijo ${c} is run from another device${parts.length ? ` (${parts.join(', ')})` : ''}`,
            take: true,
            ask: `The other device will stop running shiaijo ${c}. Take control?`,
        };
    }
    if (state === 'free-displaced') {
        return {
            tone: 'warn',
            text: `Nobody runs shiaijo ${c} now. This tablet lost the control earlier; tap Take control to run it.`,
            take: true,
            ask: null,
        };
    }
    // unknown: no answer yet, or none that names a holder.
    if (view && view.connected) {
        return { tone: 'checking', text: `Checking who runs shiaijo ${c}…`, take: false, ask: null };
    }
    if (!checked && online) {
        return { tone: 'checking', text: `Checking who runs shiaijo ${c}…`, take: false, ask: null };
    }
    return {
        tone: 'warn',
        text: `Not connected: this tablet runs shiaijo ${c} once it connects, unless another device already does`,
        take: false,
        ask: null,
    };
}

/**
 * The console's control: { view, canWrite, checked, controlRef }. `court` is
 * '' for a shiaijo the tournament does not list (nothing is claimed).
 */
export function useShiaijoControl(court, password, showToast) {
    const view = useCourtControl(court, password);
    const [checked, setChecked] = useState(false);
    useEffect(() => {
        setChecked(false);
        if (!court) return undefined;
        const t = setTimeout(() => setChecked(true), CONTROL_CHECK_GRACE_MS);
        return () => clearTimeout(t);
    }, [court]);
    const canWrite = !court || courtControlWrites(view);
    const controlRef = useRef(null);
    controlRef.current = {
        canWrite,
        tell: () => { if (typeof showToast === 'function') showToast(courtNotHeldError().message, 'error'); },
    };
    return { view, canWrite, checked, controlRef };
}

/**
 * The ONE check the console's handlers make before they act: true when this
 * tab may not write (another tab or device runs the shiaijo, or nobody does and
 * this tablet lost it), toasting why unless `quiet` (an automatic step after a
 * write the door gate already refused and told). A missing ref never refuses.
 */
export function refusedNotHeld(controlRef, { quiet = false } = {}) {
    const c = controlRef && controlRef.current;
    if (!c || c.canWrite) return false;
    if (!quiet && typeof c.tell === 'function') c.tell();
    return true;
}

const online = () => typeof navigator === 'undefined' || navigator.onLine !== false;

/**
 * The mark under the top bar, sticky with it so it is on screen however far
 * the operator scrolls. `control` is useShiaijoControl's answer.
 */
export function ShiaijoControlBar({ court, control, showToast }) {
    const { view, checked } = control;
    const bar = courtControlBar(view, court, { checked, online: online() });
    const [asking, setAsking] = useState(null); // the question, while it is open
    const [taking, setTaking] = useState(false);
    const mountedRef = useRef(true);
    useEffect(() => () => { mountedRef.current = false; }, []);
    const boxRef = useRef(null);
    const confirmTapRef = useRef(null);
    const { openedRef, onClickCapture } = useOpenedTapGuard();
    const open = !!asking;
    window.useEscapeToClose(open && !taking ? () => setAsking(null) : undefined);
    useDialogFocus(boxRef, open);
    // The question goes when there is nothing left to take (this tab took it,
    // or another answer came meanwhile).
    useEffect(() => {
        if (asking && !bar.take && !taking) setAsking(null);
    }, [asking, bar.take, taking]);

    const take = async () => {
        if (taking) return;
        setTaking(true);
        try {
            await view.take();
            if (mountedRef.current) setAsking(null);
        } catch (e) {
            if (typeof showToast === 'function') showToast((e && e.message) || 'Control was not taken. Try again.', 'error');
            if (mountedRef.current) setAsking(null);
        } finally {
            if (mountedRef.current) setTaking(false);
        }
    };
    const tapTake = () => {
        if (bar.ask) setAsking(bar.ask);
        else take();
    };

    return (
        <>
            <div className={`shiaijo-control shiaijo-control--${bar.tone}`} data-testid="court-control" data-state={view.state}>
                <p className="shiaijo-control__text" role="status">
                    {bar.tone === 'holds' && <span className="shiaijo-control__dot" aria-hidden="true" />}
                    {bar.text}
                </p>
                {bar.take && (
                    <button type="button" className="btn btn--sm shiaijo-control__take" onClick={tapTake} disabled={taking}>
                        {taking && !asking ? 'Taking control…' : 'Take control'}
                    </button>
                )}
            </div>
            {asking && (
                <div className="modal-backdrop" ref={openedRef} onClickCapture={onClickCapture}
                    onClick={() => !taking && setAsking(null)}>
                    <div className="shiaijo-move-confirm" role="dialog" aria-modal="true" ref={boxRef}
                        aria-labelledby="shiaijo-take-title" data-testid="take-control-ask" onClick={(e) => e.stopPropagation()}>
                        <h3 id="shiaijo-take-title" className="shiaijo-move-confirm__title">Take control of shiaijo {court}?</h3>
                        <p className="shiaijo-move-confirm__body">{asking}</p>
                        <div className="shiaijo-move-confirm__actions">
                            <button type="button" className="btn" onClick={() => setAsking(null)} disabled={taking}>
                                Cancel
                            </button>
                            <button type="button" className="btn btn--primary" disabled={taking}
                                onClick={(ev) => { if (acceptTap(confirmTapRef, ev)) take(); }}>
                                {taking ? 'Taking control…' : 'Take control'}
                            </button>
                        </div>
                    </div>
                </div>
            )}
        </>
    );
}
