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
// (courtControlBar's tone): green "In control" ("running" means a match, never
// the control), saturated slate "View only" (another tab or device runs it),
// saturated amber "Control lost" (this device lost it), neutral "Checking",
// "Can't reach court control" and "Reconnecting" (the console still scores)
// and red for a sign-in or check this console must fix. When this console
// loses the shiaijo while open, the same band carries the one-time account of
// it (courtControlLossNote): who took it, when someone else holds it now, and
// what became of what this console entered (courtControlEntriesNote).

import { useCourtControl } from './court_control.jsx';
import { serverNowMs } from './server_clock.jsx';
import { useDialogFocus } from './dialog_focus.jsx';
import { useOpenedTapGuard, acceptTap } from './tap_guard.jsx';
import { courtNotHeldError } from './write_result.jsx';
import { publishHeight } from './published_height.jsx';
import { holdsUnsaved } from './unsaved_entries.jsx';
import { useLinkStatus } from './link_status.jsx';
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
 * Who has the shiaijo now, the ONE wording the bar, the reason lines and the
 * idle panel share: { grace, clause, noun }. While the server's release grace
 * keeps the shiaijo for the device that gave it back (`releasedAt`, never for
 * another tab of this device, which takes it back at once), the clause says it
 * was closed there just now ("shiaijo A was closed on a Mac just now");
 * otherwise who runs it ("an iPad runs shiaijo A"). `noun` names the device
 * without its article ("Mac", or "device" for one the app cannot recognise).
 */
function holderNow(view, c) {
    const dev = devicePhrase(view.holderDevice);
    const noun = dev ? dev.replace(/^an? /, '') : 'device';
    if (!view.sameDevice && view.releasedAt > 0) {
        return { grace: true, noun, clause: `shiaijo ${c} was closed on ${dev || 'another device'} just now` };
    }
    return { grace: false, noun, clause: `${holderPhrase(view)} runs shiaijo ${c}` };
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
 * names the match the console's feed shows running on the shiaijo; `linkUp`
 * is the page's connection as the top bar shows it (link_status.jsx), so a
 * console with no answer says "Reconnecting" only when the top bar does.
 */
export function courtControlBar(view, court, { now = serverNowMs(), checked = true, online = true, running = '', linkUp = true } = {}) {
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
        return bar('holds', 'circle-check', 'In control', `This device runs shiaijo ${c}.`);
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
        const held = holderNow(view, c);
        if (view.youWereDisplaced) {
            // Only who has it NOW: whoever took it from this device may have
            // lost it to a third since (the one-time note says who took it).
            return offline(bar('lost', 'triangle-alert', 'Control lost', `${cap(held.clause)}${held.grace ? '' : ' now'}.`, {
                take: true,
                primary: true,
                ask,
            }));
        }
        if (held.grace) {
            // A tab that lost the shiaijo to another tab of this device earlier
            // is on the displaced list, so the claim at the grace's end takes
            // nothing for it (court_control.go Claim): it promises no take.
            const after = view.tabWasDisplaced ? '' : ` This console takes it within about 10 seconds unless that ${held.noun} comes back.`;
            return offline(bar('view', 'eye', 'View only', `${cap(held.clause)}.${after}`, { take: true, ask }));
        }
        return offline(bar('view', 'eye', 'View only', `${cap(held.clause)}.`, {
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
    // The page is connected (the top bar says so) but the control check has
    // had no answer: say that, never "not connected". The heartbeat claims
    // again, so it retries by itself.
    if (linkUp && online) {
        return bar('pending', 'refresh', "Can't reach court control", 'Retrying; scoring here still works.', {
            more: `This device runs shiaijo ${c} once it gets an answer, unless another device already does.`,
        });
    }
    // The page itself is offline: the same word as the top bar's pill.
    return bar('pending', 'wifi-off', 'Reconnecting', 'Scoring here still works.', {
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
    if (view.state === 'other') return `${holderNow(view, c).clause}.`;
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
        const held = holderNow(view, c);
        // In the grace nobody starts a match there: the device closed it.
        return { title: 'View only', body: held.grace ? `${cap(held.clause)}.` : `${cap(held.clause)}. The match it starts shows here.` };
    }
    if (view.state === 'free-displaced') {
        return { title: 'View only', body: `Nobody runs shiaijo ${c} now. Tap Take control at the top to start its next match here.` };
    }
    return null;
}

/**
 * What the console knew the moment it stopped running the shiaijo while open,
 * frozen then, since the shiaijo may change hands again before the operator
 * reads it: { taker, match, draft }. `taker` is who took it (holderPhrase, ''
 * when nobody runs it now); `match` the match the feed showed running there
 * ({compId, id, label}, or null); `draft` the label of the match whose
 * entries the inline editor held unsaved, '' for an editor with no match
 * label, or null when it held none.
 */
export function courtControlLoss(view, { running = null, editorUnsaved = null, editorLabel = '' } = {}) {
    const taker = view && view.state === 'other' ? holderPhrase(view) : '';
    const match = running && running.compId && running.id
        ? { compId: running.compId, id: running.id, label: String(running.label || '') }
        : null;
    const draft = holdsUnsaved(editorUnsaved) ? String(editorLabel || '') : null;
    return { taker, match, draft };
}

/** Whose operator to tell: "the Mac's operator", "the other device's operator", or '' (another tab of this device, or nobody). */
function operatorToTell(view) {
    if (!view || view.state !== 'other' || view.sameDevice) return '';
    const dev = devicePhrase(view.holderDevice);
    return dev ? `the ${dev.replace(/^an? /, '')}'s operator` : "the other device's operator";
}

/**
 * What became of what this console entered, for the loss band. `queued` is
 * how many entries this tab's outbox still holds for the shiaijo's matches
 * (API.queuedWritesOfMatches: not answered by the server yet; the outbox keeps
 * sending them whoever runs the shiaijo, and the server judges each on
 * arrival, FR8: after another tablet's take they count as this device's, not
 * the court's, and a take back does not change that, since the court's
 * standing starts again at the take's own time, so the band never says a take
 * saves them), `forMatch` whether every one of them is for the
 * running match `loss.match` (then named). `loss.draft` is the editor's
 * entries that were never sent: a take back enables the editor again, and its
 * Finish or Save then sends them as the court's.
 */
export function courtControlEntriesNote(loss, view, queued = 0, forMatch = false) {
    const tell = operatorToTell(view);
    const parts = [];
    if (loss && loss.draft !== null && loss.draft !== undefined) {
        const what = loss.draft ? `Your entries for ${loss.draft}` : 'Your entries on screen';
        parts.push(`${what} were not saved. Take control to save them${tell ? `, or tell ${tell}` : ''}.`);
    }
    const n = Math.max(0, Number(queued) || 0);
    if (n > 0) {
        const label = forMatch && loss && loss.match && loss.match.label ? ` for ${loss.match.label}` : '';
        parts.push(`${n} ${n === 1 ? 'entry' : 'entries'}${label} ${n === 1 ? 'is' : 'are'} still waiting to be sent from this device.${tell && parts.length === 0 ? ` Tell ${tell}.` : ''}`);
    }
    if (parts.length === 0) parts.push('Nothing you entered is waiting to be sent.');
    return parts.join(' ');
}

/**
 * The one-time account in the band when this console stops running the
 * shiaijo while open: who took it, when that is not who has it now (the bar
 * says who has it now), then courtControlEntriesNote.
 */
export function courtControlLossNote(loss, view, queued = 0, forMatch = false) {
    const parts = [];
    const nowPhrase = view && view.state === 'other' ? holderPhrase(view) : '';
    if (loss && loss.taker && loss.taker !== nowPhrase) {
        parts.push(loss.taker === 'another tab on this device'
            ? 'Another tab on this device took it.'
            : `${cap(loss.taker)} took it from this device.`);
    }
    parts.push(courtControlEntriesNote(loss, view, queued, forMatch));
    return parts.join(' ');
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
const NO_MATCHES = Object.freeze([]);

// How many entries this tab's outbox holds for `matches` (window.API, read at
// call time like queued_writes_wait.jsx; 0 when the outbox is not there).
function queuedFor(matches) {
    const api = typeof window !== 'undefined' ? window.API : null;
    if (!api || typeof api.queuedWritesOfMatches !== 'function') return 0;
    return Number(api.queuedWritesOfMatches(matches)) || 0;
}

/**
 * The mark under the top bar, sticky with it so it is on screen however far
 * the operator scrolls. `control` is useShiaijoControl's answer; `running`
 * names the match the console's feed shows running on the shiaijo, for the
 * confirm, and `runningMatch` is that match. When this console stops running
 * the shiaijo while it is open, the same band (one live region, so a screen
 * reader hears the loss once) carries the one-time account of it
 * (courtControlLossNote), until the operator dismisses it or the console runs
 * the shiaijo again: `courtMatches` are the shiaijo's matches whose queued
 * entries it counts, `editorUnsavedRef` and `editorLabel` the inline editor's
 * unsaved entries and the match they are for.
 */
export function ShiaijoControlBar({ court, control, showToast, running = '', runningMatch = null, courtMatches = NO_MATCHES, editorUnsavedRef = null, editorLabel = '' }) {
    const { view, checked } = control;
    const isOnline = useOnline();
    const { linkUp } = useLinkStatus();
    const own = courtControlBar(view, court, { checked, online: isOnline, running, linkUp });
    const [asking, setAsking] = useState(null); // the statement, while the confirm is open
    const [taking, setTaking] = useState(false);
    const [lost, setLost] = useState(null); // courtControlLoss's, while the account shows
    const heldRef = useRef({ court, held: false, seq: view.answerSeq || 0 });
    const mountedRef = useRef(true);
    useEffect(() => () => { mountedRef.current = false; }, []);
    const boxRef = useRef(null);
    const barRef = useRef(null);
    const confirmTapRef = useRef(null);
    // What the loss freezes, read as it is the moment the control goes.
    const latestRef = useRef(null);
    useLayoutEffect(() => { latestRef.current = { view, running, runningMatch, editorUnsavedRef, editorLabel }; });
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
        if (asking && !own.take && !taking) setAsking(null);
    }, [asking, own.take, taking]);
    // The loss, told once: this console ran the shiaijo, by an answer that came
    // while it is open (never the copy a reload starts from, so a console
    // opened already lost tells nothing: its bar says it), and now another tab
    // or device does (or nobody does and this one lost it). No answer in
    // between (offline) keeps what was known; another shiaijo starts afresh. A
    // layout effect, so the band changes in one step and is announced once.
    const state = view.state;
    const answerSeq = view.answerSeq || 0;
    useLayoutEffect(() => {
        const h = heldRef.current;
        if (h.court !== court) {
            heldRef.current = { court, held: false, seq: answerSeq };
            setLost(null);
            return;
        }
        if (state === 'holds') {
            if (answerSeq > h.seq) h.held = true;
            setLost(null);
        } else if (CANNOT_WRITE.has(state)) {
            if (h.held) {
                const l = latestRef.current;
                const runningAt = l.runningMatch ? { ...l.runningMatch, label: l.running } : null;
                setLost(courtControlLoss(l.view, { running: runningAt, editorUnsaved: l.editorUnsavedRef, editorLabel: l.editorLabel }));
            }
            h.held = false;
        }
    }, [court, state, answerSeq]);
    // While the account shows, its count follows the outbox: an entry that
    // lands leaves it.
    const [, bumpQueue] = useState(0);
    const showing = !!lost;
    useEffect(() => {
        if (!showing) return undefined;
        const subscribe = typeof window !== 'undefined' && window.subscribeUnsentWrites;
        if (typeof subscribe !== 'function') return undefined;
        return subscribe(() => { if (mountedRef.current) bumpQueue((x) => x + 1); });
    }, [showing]);
    let note = '';
    if (lost) {
        const counted = lost.match ? [...(courtMatches || []), lost.match] : (courtMatches || []);
        const queued = queuedFor(counted);
        const forMatch = !!lost.match && queued > 0 && queuedFor([lost.match]) === queued;
        note = courtControlLossNote(lost, view, queued, forMatch);
    }
    // One band: while the account shows it is the amber loss band whatever
    // the bar's own state (another tab of this device took it, too).
    const bar = lost ? { ...own, tone: 'lost', glyph: 'triangle-alert', lead: 'Control lost', primary: own.take } : own;

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
                            {note && <span className="shiaijo-control__lost" data-testid="court-control-lost"> {note}</span>}
                        </span>
                    </p>
                    {bar.take && (
                        <button type="button" className={`btn shiaijo-control__take${bar.primary ? ' btn--primary' : ''}`}
                            onClick={tapTake} disabled={taking || !isOnline}>
                            {taking && !asking ? 'Taking control…' : 'Take control'}
                        </button>
                    )}
                    {note && (
                        <button type="button" className="btn btn--ghost shiaijo-control__dismiss" data-testid="court-control-dismiss"
                            onClick={() => setLost(null)}>
                            Dismiss
                        </button>
                    )}
                </div>
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
