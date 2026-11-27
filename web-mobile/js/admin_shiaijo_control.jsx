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
//     `canWrite` is true while this tab runs the shiaijo, has had no answer
//     yet (offline: an unconnected console still scores), or the shiaijo is
//     not listed. `controlRef` is what the console's hooks
//     read, so a handler judges the control as it is when tapped.
//   - refusedNotHeld(controlRef, { quiet }) is the ONE check those handlers
//     make: true (and a toast, unless quiet) when this tab may not write.
//   - ShiaijoControlBar draws the mark for every state, with Take control.
//
//   - ShiaijoViewOnlyReason, courtControlReason and courtControlIdle say why
//     the console's write controls are greyed out, beside each disabled group
//     and in the idle panel.
//
// The bar is a static banner, never a pulse (DESIGN.md principle 3: a holder is
// ordinary ongoing state). Every state opens with a glyph and a bold word, so
// its meaning never rests on hue alone, and each meaning has one treatment
// (courtControlBar's tone): green "Running", saturated slate "View only"
// (another tab or device runs it), saturated amber "Control lost" (this device
// lost it), neutral "Checking" and "Not connected" (the console still scores)
// and red for a sign-in or check this console must fix.

import { useCourtControl } from './court_control.jsx';
import { serverNowMs } from './server_clock.jsx';
import { useDialogFocus } from './dialog_focus.jsx';
import { useOpenedTapGuard, acceptTap } from './tap_guard.jsx';
import { courtNotHeldError } from './write_result.jsx';
import { publishHeight } from './published_height.jsx';
import { Icon } from './icon.jsx';

const { useState, useEffect, useLayoutEffect, useRef } = React;

// How long a console that has had no answer yet shows "Checking" before it
// says it is not connected: a claim on a working connection answers well
// inside it, so an ordinary open never flashes the not-connected line.
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

/** "an iPad", ... or '' for a device the server reads as `device` (unrecognised). */
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

/** Whether the console may write: it runs the shiaijo, has had no answer yet, or the shiaijo is not listed. */
export function courtControlWrites(view) {
    return !!view && (view.state === 'holds' || view.state === 'unknown' || view.state === 'not-listed');
}

const cap = (s) => (s ? s.charAt(0).toUpperCase() + s.slice(1) : s);

/** Who runs the shiaijo, as the bar says it: "an iPad", "another tab on this device" or "another device". */
function holderPhrase(view) {
    if (view.sameDevice) return 'another tab on this device';
    return devicePhrase(view.holderDevice) || 'another device';
}

/**
 * The confirm's statement for a take (never a second question: the title
 * asks it): who runs the shiaijo, when it was last active, the match the
 * console's own feed shows running there, and what the take does to it.
 */
function takeStatement(view, active, running) {
    const when = active ? ` (${active})` : '';
    const has = running ? ` and has ${running} running` : '';
    if (view.sameDevice) {
        return `${cap(holderPhrase(view))} runs it${when}${has}. That tab becomes view only; results queued on this device still count.`;
    }
    if (view.releasedAt > 0 && !view.youWereDisplaced) {
        return `${cap(holderPhrase(view))} closed it just now and may come back. Taking it now makes that device view only; results it already saved are kept.`;
    }
    return `${cap(holderPhrase(view))} runs it${when}${has}. That device becomes view only; results it already saved are kept.`;
}

/**
 * What the bar shows for `view` (useCourtControl's) on `court`:
 * { tone, glyph, lead, text, more, offline, take, primary, ask }.
 *   tone: one treatment per meaning. 'holds' (green: this tab runs it),
 *     'view' (saturated slate: another tab or device runs it, so view only),
 *     'lost' (saturated amber: this device lost it, so view only until it
 *     takes it back), 'pending' (neutral: no answer yet; the console still
 *     scores) and 'error' (red: a sign-in or check this console must fix).
 *   glyph and lead: the icon and the bold word that open the bar, so the
 *     meaning never rests on hue alone.
 *   text: always shown; more: the detail a phone leaves out; offline: that
 *     Take control needs the connection (shown on every screen size).
 *   take: offers Take control; primary: as the recovery (this device lost
 *     it) rather than a plain option; ask: the statement the confirm shows
 *     (null: nobody runs the shiaijo, so taking it stops no one).
 * `checked` is false while a console that has had no answer is still inside
 * its grace; `online` false says Take control needs the connection; `running`
 * names the match the console's feed shows running on the shiaijo.
 */
export function courtControlBar(view, court, { now = serverNowMs(), checked = true, online = true, running = '' } = {}) {
    const c = String(court || '');
    const state = view ? view.state : 'unknown';
    const bar = (tone, glyph, lead, text, extra = {}) => ({ tone, glyph, lead, text, more: '', offline: '', take: false, primary: false, ask: null, ...extra });
    // Offline, Take control cannot act (it needs the connection): the bar says
    // so after the detail, on every screen size.
    const offline = (b) => (b.take && !online ? { ...b, offline: 'Taking control needs a connection.' } : b);
    if (view && view.refusedStatus === 401) {
        return bar('error', 'alert-circle', 'Sign in again', `Your sign-in no longer works for shiaijo ${c}.`);
    }
    if (state === 'holds') {
        return bar('holds', 'circle-check', 'Running', `This device runs shiaijo ${c}.`);
    }
    if (state === 'other') {
        const active = lastActivePhrase(view.holderLastActiveAt, now);
        const ask = takeStatement(view, active, running);
        if (view.sameDevice) {
            return offline(bar('view', 'eye', 'View only', `Another tab on this device runs shiaijo ${c}.`, {
                more: `${active ? `${cap(active)}. ` : ''}Results queued on this device still count.`,
                take: true,
                ask,
            }));
        }
        if (view.youWereDisplaced) {
            return offline(bar('lost', 'triangle-alert', 'Control lost', `${cap(holderPhrase(view))} took shiaijo ${c} from this device.`, {
                take: true,
                primary: true,
                ask,
            }));
        }
        if (view.releasedAt > 0) {
            const dev = devicePhrase(view.holderDevice);
            const noun = dev ? dev.replace(/^an? /, '') : 'device';
            return offline(bar('view', 'eye', 'View only',
                `Shiaijo ${c} was closed on ${dev || 'another device'} just now. This console takes it within about 10 seconds unless that ${noun} comes back.`,
                { take: true, ask }));
        }
        return offline(bar('view', 'eye', 'View only', `${cap(holderPhrase(view))} runs shiaijo ${c}.`, {
            more: active ? `${cap(active)}.` : '',
            take: true,
            ask,
        }));
    }
    if (state === 'free-displaced') {
        return offline(bar('lost', 'triangle-alert', 'Control lost', view.youWereDisplaced
            ? `Nobody runs shiaijo ${c} now; this device lost it earlier.`
            : `Nobody runs shiaijo ${c} now; another tab on this device ran it last.`,
        { take: true, primary: true }));
    }
    if (state === 'not-listed') {
        return bar('error', 'alert-circle', 'Not found', `Shiaijo ${c} is no longer in this tournament. Go back to the schedule and pick one that is.`);
    }
    // unknown: no answer yet, a refused check, or none that names a holder.
    if (view && view.refusedStatus > 0) {
        return bar('error', 'alert-circle', 'Check failed', `Could not check who runs shiaijo ${c}. Reload the page.`);
    }
    if ((view && view.connected) || (!checked && online)) {
        return bar('pending', 'hourglass', 'Checking', `Finding out who runs shiaijo ${c}…`);
    }
    return bar('pending', 'wifi-off', 'Not connected', 'Scoring here still works.', {
        more: `This device runs shiaijo ${c} once it connects, unless another device already does.`,
    });
}

/**
 * Why the console's write controls are greyed out, for the line beside each
 * disabled group ("an iPad runs shiaijo B."; the component adds "View only:"),
 * or '' while it may write.
 */
export function courtControlReason(view, court) {
    const c = String(court || '');
    if (!view) return '';
    if (view.state === 'other') return `${holderPhrase(view)} runs shiaijo ${c}.`;
    if (view.state === 'free-displaced') return `nobody runs shiaijo ${c}. Tap Take control at the top to run it here.`;
    return '';
}

/**
 * The console's idle panel (no match selected) while it may not write:
 * { title, body }, or null while it may (the console keeps its own words).
 */
export function courtControlIdle(view, court) {
    const c = String(court || '');
    if (!view) return null;
    if (view.state === 'other') {
        return { title: 'View only', body: `${cap(holderPhrase(view))} runs shiaijo ${c}. The match it starts shows here.` };
    }
    if (view.state === 'free-displaced') {
        return { title: 'View only', body: `Nobody runs shiaijo ${c} now. Tap Take control at the top to start its next match here.` };
    }
    return null;
}

/**
 * The one-time note when this console stops running the shiaijo while it is
 * open (another device took it, or another tab on this device did): what
 * happened and that the screen is now view only.
 */
export function courtControlLostNote(view, court) {
    const c = String(court || '');
    const after = 'This screen is now view only; entries you had not saved stay on screen.';
    if (view && view.state === 'other') return `Shiaijo ${c} was taken by ${holderPhrase(view)}. ${after}`;
    return `This screen no longer runs shiaijo ${c}. ${after}`;
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
    useLayoutEffect(() => {
        controlRef.current = {
            canWrite,
            tell: () => { if (typeof showToast === 'function') showToast(courtNotHeldError().message, 'error'); },
        };
    });
    return { view, canWrite, checked, controlRef };
}

/**
 * The ONE check the console's handlers make before they act: true when this
 * tab may not write (another tab or device runs the shiaijo, or nobody does and
 * this device lost it), toasting why unless `quiet` (an automatic step after a
 * write the door gate already refused and told). A missing ref never refuses.
 */
export function refusedNotHeld(controlRef, { quiet = false } = {}) {
    const c = controlRef && controlRef.current;
    if (!c || c.canWrite) return false;
    if (!quiet && typeof c.tell === 'function') c.tell();
    return true;
}

const online = () => typeof navigator === 'undefined' || navigator.onLine !== false;

/** Whether the browser says it is online, kept current by its online and offline events. */
function useOnline() {
    const [on, setOn] = useState(online);
    useEffect(() => {
        const sync = () => setOn(online());
        window.addEventListener('online', sync);
        window.addEventListener('offline', sync);
        return () => {
            window.removeEventListener('online', sync);
            window.removeEventListener('offline', sync);
        };
    }, []);
    return on;
}

const CANNOT_WRITE = new Set(['other', 'free-displaced']);

/**
 * The mark under the top bar, sticky with it so it is on screen however far
 * the operator scrolls. `control` is useShiaijoControl's answer; `running`
 * names the match the console's feed shows running on the shiaijo, for the
 * confirm. Under the mark, the one-time note when this console stops running
 * the shiaijo while it is open, until the operator dismisses it or the
 * console runs the shiaijo again.
 */
export function ShiaijoControlBar({ court, control, showToast, running = '' }) {
    const { view, checked } = control;
    const isOnline = useOnline();
    const bar = courtControlBar(view, court, { checked, online: isOnline, running });
    const [asking, setAsking] = useState(null); // the statement, while the confirm is open
    const [taking, setTaking] = useState(false);
    const [lost, setLost] = useState(null); // the one-time note, while it shows
    const heldRef = useRef({ court, held: false });
    const mountedRef = useRef(true);
    useEffect(() => () => { mountedRef.current = false; }, []);
    const boxRef = useRef(null);
    const barRef = useRef(null);
    const confirmTapRef = useRef(null);
    // The bar sticks under the top bar and covers whatever scrolls under it, so
    // the inline team sheet sizes its pinned header and scroll margin to it too.
    useLayoutEffect(() => publishHeight(barRef.current, document.documentElement, '--court-control-h'), []);
    const { openedRef, onClickCapture } = useOpenedTapGuard();
    const open = !!asking;
    window.useEscapeToClose(open && !taking ? () => setAsking(null) : undefined);
    useDialogFocus(boxRef, open);
    // The question goes when there is nothing left to take (this tab took it,
    // or another answer came meanwhile).
    useEffect(() => {
        if (asking && !bar.take && !taking) setAsking(null);
    }, [asking, bar.take, taking]);
    // The loss, told once: this console ran the shiaijo and now another tab or
    // device does (or nobody does and this one lost it). No answer in between
    // (offline) keeps what was known; another shiaijo starts afresh.
    const state = view.state;
    const lostNote = courtControlLostNote(view, court);
    useEffect(() => {
        const h = heldRef.current;
        if (h.court !== court) {
            heldRef.current = { court, held: state === 'holds' };
            setLost(null);
            return;
        }
        if (state === 'holds') {
            h.held = true;
            setLost(null);
        } else if (CANNOT_WRITE.has(state)) {
            if (h.held) setLost(lostNote);
            h.held = false;
        }
    }, [court, state, lostNote]);

    const take = async () => {
        if (taking) return;
        setTaking(true);
        try {
            await view.take();
            if (mountedRef.current) setAsking(null);
        } catch (e) {
            if (typeof showToast === 'function') showToast((e && e.message) || 'Control was not taken. Try again.', 'error');
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
            <div ref={barRef} className={`shiaijo-control shiaijo-control--${bar.tone}`} data-testid="court-control"
                data-state={view.state} data-tone={bar.tone}>
                <div className="shiaijo-control__row">
                    <p className="shiaijo-control__text" role="status">
                        <Icon name={bar.glyph} size={18} className="shiaijo-control__glyph" />
                        <span className="shiaijo-control__words">
                            <strong className="shiaijo-control__lead">{bar.lead}</strong>{' '}
                            {bar.text}
                            {bar.more && <span className="shiaijo-control__more"> {bar.more}</span>}
                            {bar.offline && <span className="shiaijo-control__offline"> {bar.offline}</span>}
                        </span>
                    </p>
                    {bar.take && (
                        <button type="button" className={`btn shiaijo-control__take${bar.primary ? ' btn--primary' : ''}`}
                            onClick={tapTake} disabled={taking || !isOnline}>
                            {taking && !asking ? 'Taking control…' : 'Take control'}
                        </button>
                    )}
                </div>
                {lost && (
                    <div className="shiaijo-control__lost" role="alert" data-testid="court-control-lost">
                        <Icon name="triangle-alert" size={18} className="shiaijo-control__glyph" />
                        <p className="shiaijo-control__lost-text">{lost}</p>
                        <button type="button" className="btn btn--ghost shiaijo-control__dismiss" onClick={() => setLost(null)}>
                            Dismiss
                        </button>
                    </div>
                )}
            </div>
            {asking && (
                <div className="modal-backdrop" ref={openedRef} onClickCapture={onClickCapture}
                    onClick={() => !taking && setAsking(null)}>
                    <div className="shiaijo-move-confirm" role="dialog" aria-modal="true" ref={boxRef}
                        aria-labelledby="shiaijo-take-title" aria-describedby="shiaijo-take-body"
                        data-testid="take-control-ask" onClick={(e) => e.stopPropagation()}>
                        <h3 id="shiaijo-take-title" className="shiaijo-move-confirm__title">Take control of shiaijo {court}?</h3>
                        <p id="shiaijo-take-body" className="shiaijo-move-confirm__body">{asking}</p>
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

/**
 * The line beside a disabled group of the console's write controls, saying
 * why they are greyed out ("View only: an iPad runs shiaijo B."), so a tap on
 * one is never unexplained. Nothing while the console may write.
 */
export function ShiaijoViewOnlyReason({ court, control }) {
    if (!control || control.canWrite) return null;
    const text = courtControlReason(control.view, court);
    if (!text) return null;
    return (
        <p className="shiaijo-control-reason" data-testid="view-only-reason">
            <Icon name="eye" size={16} className="shiaijo-control-reason__glyph" />
            <span><strong>View only:</strong> {text}</span>
        </p>
    );
}
