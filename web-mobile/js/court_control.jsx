// court_control.jsx: the ONE client owner of "does this tab run the shiaijo"
// (bc-sseq FR9). One tab runs each shiaijo; the court's standing belongs to the
// tablet that tab is on.
//
// The server keeps who runs each shiaijo (court-control.json, the routes
// GET/PUT/DELETE /api/courts/:court/control). This module holds the ids a court
// console sends, the claim, the heartbeat and the release, the last answer per
// shiaijo, and the two mount-scoped questions the outbox asks:
//   - consoleWriteRefusal(): the ONE write gate. While a court console is on
//     screen in this tab (a mount counter useCourtControl raises and lowers)
//     and another device runs its shiaijo, or nobody does and this tablet lost
//     it earlier, every new write is refused before it is queued
//     (_placeWrite, api_client_enqueue.jsx). With no console mounted, or one
//     that holds, it never refuses: the Scores tab and page, Pools, Bracket and
//     the public page share this module but are never on screen beside the
//     console, and keep today's rules.
//   - courtControlFor(court): the `{tabId, deviceId}` a court write carries
//     (FR8), only while the console is mounted and holds the shiaijo or has had
//     no answer yet (an unconnected console's results claim a free shiaijo on
//     arrival). Put the object on the payload AS IT IS: the outbox reads which
//     shiaijo it was given for (courtOfControlStamp) to record it on the entry,
//     which the release asks.
//
// The ids, sent only to the server, which stores and compares only their
// one-way keys (no answer or event carries one):
//   - the TAB id: a UUID in sessionStorage, so a reload keeps it and a new tab
//     gets its own. A duplicated tab copies sessionStorage, so a
//     BroadcastChannel check mints a fresh one for the younger copy. It is NOT
//     the outbox's base-lease tab (base_sender_lease.jsx), which is minted per
//     page load: a reload must keep running the shiaijo.
//   - the TABLET id (deviceId): a UUID in localStorage, shared by every tab of
//     the browser as the outbox is. Always present: where localStorage cannot
//     be used the tab mints one into its own sessionStorage and counts as a
//     tablet of its own (its outbox is not shared then either).
//   - the PAGE id: one per page load, so a reload's late release never frees
//     the new page's claim.
//
// Declaration-only at load: nothing runs but literals; the ids are minted and
// the listeners installed on first use, so the import cycles with the outbox
// pieces are safe (check-load-cycles.mjs).

import { _randomId } from './api_client_ids.jsx';
import { serverNowMs } from './server_clock.jsx';
import { _directFetch } from './api_client_requests.jsx';
import { _storedQueue, _writeQueue } from './api_client_queue.jsx';
import { _subscribeToEvents } from './api_client_events.jsx';
import { courtNotHeldAnswer } from './write_result.jsx';

const UUID_V4 = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
export const COURT_TAB_KEY = 'bc_court_tab_v1';
export const COURT_DEVICE_KEY = 'bc_court_device_v1';
// The tablet id of a tab whose localStorage cannot be used: its own.
export const COURT_DEVICE_TAB_KEY = 'bc_court_device_tab_v1';
// The last answer per shiaijo, so a reload offline still shows it.
export const COURT_ANSWER_PREFIX = 'bc_court_control_answer_v1:';
const TAB_CHANNEL = 'bc_court_tab_v1';
// How long the first claim waits for an older copy of this tab to say the
// tab id is its own (a duplicated tab).
export const TAB_CHECK_MS = 150;
export const COURT_HEARTBEAT_MS = 30_000;
// A claim that hangs is given up, so heartbeats never pile up behind it.
const CLAIM_DEADLINE_MS = 12_000;

// The sentence Take control answers with when there is no connection: taking
// control means nothing offline (the plan's assumption).
export const TAKE_NEEDS_CONNECTION = 'Taking control needs a connection. Nothing changed; try again once connected.';

const _pageId = { v: '' };
let _tabId = '';
let _deviceId = '';
let _tabCheck = null; // { ready: Promise, channel, loadedAt }
let _listening = false;
let _reqSeq = 0;
// court -> { mounts, listeners:Set, answer, connected, answeredSeq, timer,
//            unsubEvents, getPassword, releaseTimer }
const _courts = new Map();
// The stamps courtControlFor handed out, to the shiaijo each was for.
const _stampCourts = new WeakMap();

// ---------------------------------------------------------------------------
// The ids
// ---------------------------------------------------------------------------

function _readOrMint(storage, key) {
    let v = storage.getItem(key);
    if (!UUID_V4.test(String(v || ''))) {
        v = _randomId();
        storage.setItem(key, v);
    }
    return v;
}

/** This page load's id. */
export function courtPageId() {
    if (!_pageId.v) _pageId.v = _randomId();
    return _pageId.v;
}

/** This tab's id (sessionStorage; in memory when that cannot be used). */
export function courtTabId() {
    if (_tabId) return _tabId;
    try {
        _tabId = _readOrMint(sessionStorage, COURT_TAB_KEY);
    } catch (_e) {
        _tabId = _randomId();
    }
    _startTabCheck();
    return _tabId;
}

/** This tablet's id: localStorage, else this tab's own (sessionStorage), else this page's. */
export function courtDeviceId() {
    if (_deviceId) return _deviceId;
    try {
        _deviceId = _readOrMint(localStorage, COURT_DEVICE_KEY);
        return _deviceId;
    } catch (_e) { /* no shared storage: this tab is a tablet of its own */ }
    try {
        _deviceId = _readOrMint(sessionStorage, COURT_DEVICE_TAB_KEY);
    } catch (_e) {
        _deviceId = _randomId();
    }
    return _deviceId;
}

// A duplicated tab carries the original's sessionStorage, so both would send
// one tab id. Each tab says which tab id it has as it starts; the older page
// with that id answers that it is its own, and the younger one mints another.
function _startTabCheck() {
    if (_tabCheck) return;
    const loadedAt = Date.now();
    let channel = null;
    try {
        channel = typeof BroadcastChannel === 'function' ? new BroadcastChannel(TAB_CHANNEL) : null;
    } catch (_e) {
        channel = null;
    }
    if (!channel) {
        _tabCheck = { ready: Promise.resolve(), channel: null, loadedAt };
        return;
    }
    let done;
    const ready = new Promise((res) => { done = res; });
    const timer = setTimeout(() => done(), TAB_CHECK_MS);
    if (timer && typeof timer.unref === 'function') timer.unref();
    channel.onmessage = (e) => {
        const m = e && e.data;
        if (!m || typeof m !== 'object' || m.tabId !== _tabId || m.pageId === courtPageId()) return;
        if (m.type === 'who' && !(Number(m.loadedAt) < loadedAt)) {
            channel.postMessage({ type: 'mine', tabId: _tabId, pageId: courtPageId() });
        } else if (m.type === 'mine') {
            _remintTabId();
        }
    };
    _tabCheck = { ready, channel, loadedAt };
    try {
        channel.postMessage({ type: 'who', tabId: _tabId, pageId: courtPageId(), loadedAt });
    } catch (_e) { done(); }
}

function _remintTabId() {
    _tabId = _randomId();
    try { sessionStorage.setItem(COURT_TAB_KEY, _tabId); } catch (_e) { /* kept in memory */ }
    // A claim already made under the copied id is made again under the new one.
    for (const court of _courts.keys()) if (_mounted(court)) _claim(court);
}

function _tabReady() {
    courtTabId();
    return _tabCheck ? _tabCheck.ready : Promise.resolve();
}

// ---------------------------------------------------------------------------
// The device word: the only "who" another screen is shown.
// ---------------------------------------------------------------------------

/**
 * The kind of device, read from the browser: one of ipad, android-tablet,
 * iphone, android-phone, mac, windows, other. iPadOS Safari reports a Mac by
 * default, so a "Macintosh" user agent with more than one touch point reads ipad.
 */
export function deviceWord(nav = (typeof navigator !== 'undefined' ? navigator : {})) {
    const ua = String((nav && nav.userAgent) || '');
    const touch = Number(nav && nav.maxTouchPoints) || 0;
    if (/iPad/.test(ua) || (/Macintosh/.test(ua) && touch > 1)) return 'ipad';
    if (/iPhone|iPod/.test(ua)) return 'iphone';
    if (/Android/.test(ua)) return /Mobile/.test(ua) ? 'android-phone' : 'android-tablet';
    if (/Macintosh|Mac OS X/.test(ua)) return 'mac';
    if (/Windows/.test(ua)) return 'windows';
    return 'other';
}

// ---------------------------------------------------------------------------
// The state of a shiaijo, as this tab sees it
// ---------------------------------------------------------------------------

/**
 * The state an answer says: holds (this tab runs the shiaijo), other (another
 * tab or device runs it), free-displaced (nobody does, and this tab or tablet
 * lost it by a take), or unknown (no answer yet: offline, or a free shiaijo
 * the claim has not taken).
 */
export function courtControlState(answer) {
    if (!answer || typeof answer !== 'object') return 'unknown';
    if (answer.youHold === true) return 'holds';
    if (answer.holderKey) return 'other';
    if (answer.youWereDisplaced === true) return 'free-displaced';
    return 'unknown';
}

const REFUSING_STATES = new Set(['other', 'free-displaced']);

function _rec(court) {
    let r = _courts.get(court);
    if (!r) {
        r = {
            mounts: 0, listeners: new Set(), answer: _cachedAnswer(court), connected: false,
            answeredSeq: 0, timer: null, unsubEvents: null, getPassword: () => '', releaseTimer: null,
        };
        _courts.set(court, r);
    }
    return r;
}

function _mounted(court) {
    const r = _courts.get(court);
    return !!r && r.mounts > 0;
}

function _cachedAnswer(court) {
    try {
        const raw = sessionStorage.getItem(COURT_ANSWER_PREFIX + court);
        const a = raw ? JSON.parse(raw) : null;
        return a && typeof a === 'object' && a.court === court ? a : null;
    } catch (_e) {
        return null;
    }
}

function _notify(court) {
    const r = _courts.get(court);
    if (!r) return;
    for (const fn of r.listeners) {
        try { fn(); } catch (e) { console.error('[court control] a listener threw:', e); }
    }
}

function _noteAnswer(court, seq, answer) {
    const r = _rec(court);
    if (seq < r.answeredSeq) return;
    r.answeredSeq = seq;
    r.connected = true;
    r.answer = answer;
    try {
        if (answer) sessionStorage.setItem(COURT_ANSWER_PREFIX + court, JSON.stringify(answer));
        else sessionStorage.removeItem(COURT_ANSWER_PREFIX + court);
    } catch (_e) { /* the answer stays in memory */ }
    _notify(court);
}

function _noteNoAnswer(court, seq) {
    const r = _rec(court);
    if (seq < r.answeredSeq) return;
    if (r.connected) {
        r.connected = false;
        _notify(court);
    }
}

// ---------------------------------------------------------------------------
// Claim, take, release
// ---------------------------------------------------------------------------

function _url(court) {
    return `/api/courts/${encodeURIComponent(court)}/control`;
}

function _headers(court) {
    const r = _courts.get(court);
    const password = (r && r.getPassword && r.getPassword()) || '';
    return { 'Content-Type': 'application/json', 'X-Tournament-Password': password };
}

function _deadline() {
    try {
        return typeof AbortSignal !== 'undefined' && typeof AbortSignal.timeout === 'function'
            ? AbortSignal.timeout(CLAIM_DEADLINE_MS) : undefined;
    } catch (_e) {
        return undefined;
    }
}

// _claim: a claim (take false: also the heartbeat) or a take of `court`.
// Answers the server's answer, or null when none came. A take throws its
// refusal; a claim never does.
async function _claim(court, { take = false } = {}) {
    await _tabReady();
    const seq = ++_reqSeq;
    const body = {
        tabId: courtTabId(), pageId: courtPageId(), deviceId: courtDeviceId(), take, device: deviceWord(),
    };
    // The take's time on this tablet's clock as the outbox stamps a write
    // (the server's clock as this tablet reads it): only stamps from the
    // taking tablet are ever compared with it.
    if (take) body.takenAt = Math.max(1, Math.floor(serverNowMs()));
    let res;
    try {
        const signal = take ? undefined : _deadline();
        res = await _directFetch(_url(court), {
            method: 'PUT', headers: _headers(court), body: JSON.stringify(body), ...(signal ? { signal } : {}),
        });
    } catch (e) {
        _noteNoAnswer(court, seq);
        if (take) throw e;
        return null;
    }
    let data = null;
    try { data = await res.json(); } catch (_e) { data = null; }
    if (!res.ok) {
        // A shiaijo the tournament no longer lists, or a password refused:
        // nothing this tab can run, and nothing it is told it lost.
        if (res.status === 404) _noteAnswer(court, seq, null);
        else _noteNoAnswer(court, seq);
        if (take) {
            throw Object.assign(new Error((data && data.error) || 'Control was not taken. Try again.'), {
                code: data && data.code, status: res.status,
            });
        }
        return null;
    }
    _noteAnswer(court, seq, data && typeof data === 'object' ? data : null);
    return data;
}

/**
 * Take control of `court` from whoever runs it (the console's Take control,
 * after its one question). Needs a connection: offline it throws
 * TAKE_NEEDS_CONNECTION and changes nothing. Answers the new answer.
 */
export async function takeCourtControl(court) {
    const c = String(court || '');
    if (!c) throw new Error('No shiaijo to take.');
    if (typeof navigator !== 'undefined' && navigator.onLine === false) {
        throw Object.assign(new Error(TAKE_NEEDS_CONNECTION), { noConnection: true });
    }
    try {
        return await _claim(c, { take: true });
    } catch (e) {
        // Whatever happened (a dropped connection may or may not have taken
        // it: _directFetch's sentence says so), the next answer says who runs
        // it now.
        _claim(c);
        throw e;
    }
}

// _tabletQueuedFor: the tablet's outbox (every tab's entries, from storage,
// and this tab's own) holds an entry stamped with the court's control for
// `court`. Every such entry needs the tablet to keep running the shiaijo.
function _tabletQueuedFor(court) {
    const held = (d) => !!d && d.courtControlCourt === court && !!d.payload && !!d.payload.courtControl;
    for (const d of _writeQueue.values()) if (held(d)) return true;
    for (const d of _storedQueue().values()) if (held(d)) return true;
    return false;
}

/** Whether this tab may give `court` back now: it runs it and the tablet has nothing queued for it. */
export function courtReleasable(court) {
    const r = _courts.get(court);
    return !!r && courtControlState(r.answer) === 'holds' && !_tabletQueuedFor(court);
}

// _release: gives `court` back when this tab runs it and the tablet has
// nothing queued for it. `keepalive` for a page that is going.
function _release(court, { keepalive = false } = {}) {
    if (!courtReleasable(court)) return;
    const r = _courts.get(court);
    const headers = _headers(court);
    const init = {
        method: 'DELETE', headers, body: JSON.stringify({ tabId: courtTabId(), pageId: courtPageId() }),
        ...(keepalive ? { keepalive: true } : {}),
    };
    try {
        const p = typeof fetch === 'function' ? fetch(_url(court), init) : null;
        if (p && typeof p.catch === 'function') p.catch(() => { /* the next claim says who runs it */ });
    } catch (_e) { /* the next claim says who runs it */ }
    // This tab no longer runs it; a page that stays asks again when it mounts.
    r.answer = r.answer ? { ...r.answer, youHold: false, holderKey: '' } : null;
    try { sessionStorage.removeItem(COURT_ANSWER_PREFIX + court); } catch (_e) { /* nothing kept */ }
    _notify(court);
}

// ---------------------------------------------------------------------------
// The lifecycle: mount, heartbeat, events, the page going and coming back
// ---------------------------------------------------------------------------

function _visible() {
    return typeof document === 'undefined' || document.visibilityState !== 'hidden';
}
function _online() {
    return typeof navigator === 'undefined' || navigator.onLine !== false;
}

function _claimAllMounted() {
    for (const court of _courts.keys()) if (_mounted(court)) _claim(court);
}

function _listenOnce() {
    if (_listening || typeof window === 'undefined' || typeof window.addEventListener !== 'function') return;
    _listening = true;
    // Closing the tab gives the shiaijo back, unless the tablet still has
    // something queued for it (those results need the control).
    window.addEventListener('pagehide', () => {
        for (const court of _courts.keys()) if (_mounted(court)) _release(court, { keepalive: true });
    });
    // A page restored from the back-forward cache claims again.
    window.addEventListener('pageshow', (e) => { if (e && e.persisted) _claimAllMounted(); });
    window.addEventListener('online', _claimAllMounted);
    if (typeof document !== 'undefined' && typeof document.addEventListener === 'function') {
        document.addEventListener('visibilitychange', () => { if (_visible()) _claimAllMounted(); });
    }
}

function _startCourt(court) {
    const r = _rec(court);
    _listenOnce();
    const interval = setInterval(() => {
        if (_visible() && _online()) _claim(court);
    }, COURT_HEARTBEAT_MS);
    if (interval && typeof interval.unref === 'function') interval.unref();
    r.timer = interval;
    let wasDown = false;
    try {
        r.unsubEvents = _subscribeToEvents((event) => {
            if (!event || !_mounted(court)) return;
            // A new stream: what this tab knew may be stale.
            if (event.type === 'resync_required') { _claim(court); return; }
            // The holder changed: ask again (a free shiaijo is taken at once,
            // unless this tablet lost it by a take).
            if (event.type === 'court_control_updated' && event.data && event.data.court === court) _claim(court);
        }, (status) => {
            if (status === 'error') wasDown = true;
            else if (status === 'open' && wasDown) { wasDown = false; _claim(court); }
        });
    } catch (_e) {
        r.unsubEvents = null;
    }
    _claim(court);
}

function _stopCourt(court) {
    const r = _courts.get(court);
    if (!r) return;
    if (r.timer) clearInterval(r.timer);
    r.timer = null;
    if (typeof r.unsubEvents === 'function') r.unsubEvents();
    r.unsubEvents = null;
}

/**
 * Mount a court console for `court` in this tab (useCourtControl calls it).
 * `getPassword` is read at each request; `onChange` runs on every new answer.
 * Answers the unmount. The first mount claims the shiaijo, beats every 30 s
 * while the page is shown and online, and claims again on resync_required, a
 * reconnect, the page shown again or restored from the back-forward cache.
 * The last unmount gives the shiaijo back on the same rule as closing the tab.
 */
export function mountCourtControl(court, getPassword, onChange) {
    const c = String(court || '');
    if (!c) return () => {};
    const r = _rec(c);
    if (typeof getPassword === 'function') r.getPassword = getPassword;
    if (typeof onChange === 'function') r.listeners.add(onChange);
    r.mounts++;
    if (r.releaseTimer) {
        // Mounted again at once (a re-render, a quick switch back): nothing to give back.
        clearTimeout(r.releaseTimer);
        r.releaseTimer = null;
    }
    if (r.mounts === 1 && !r.timer) _startCourt(c);
    let done = false;
    return () => {
        if (done) return;
        done = true;
        if (typeof onChange === 'function') r.listeners.delete(onChange);
        r.mounts = Math.max(0, r.mounts - 1);
        if (r.mounts > 0) return;
        _stopCourt(c);
        r.releaseTimer = setTimeout(() => {
            r.releaseTimer = null;
            if (r.mounts === 0) _release(c);
        }, 0);
    };
}

// ---------------------------------------------------------------------------
// What the console and the outbox ask
// ---------------------------------------------------------------------------

/**
 * The court console's view of `court`: { holds, state, holderDevice,
 * sameDevice, youWereDisplaced, holderLastActiveAt, holderSince, connected,
 * take }. `state` is courtControlState's; `take()` is takeCourtControl.
 */
export function useCourtControl(court, password) {
    const c = String(court || '');
    const [, bump] = React.useReducer((x) => x + 1, 0);
    const passwordRef = React.useRef(password);
    passwordRef.current = password;
    React.useEffect(() => {
        if (!c) return undefined;
        return mountCourtControl(c, () => passwordRef.current, bump);
    }, [c]);
    const take = React.useCallback(() => takeCourtControl(c), [c]);
    return courtControlView(c, take);
}

/** The view useCourtControl answers, for a court, without React. */
export function courtControlView(court, take = () => takeCourtControl(court)) {
    const r = court ? _courts.get(court) : null;
    const a = r ? r.answer : null;
    const state = courtControlState(a);
    return {
        holds: state === 'holds',
        state,
        holderDevice: (a && a.device) || '',
        sameDevice: !!(a && a.sameDevice),
        youWereDisplaced: !!(a && a.youWereDisplaced),
        holderLastActiveAt: (a && Number(a.lastActiveAt)) || 0,
        holderSince: (a && Number(a.since)) || 0,
        connected: !!(r && r.connected),
        take,
    };
}

/**
 * The ONE write gate: courtNotHeldAnswer() while a court console is mounted in
 * this tab and its shiaijo is run by another tab or device, or by nobody after
 * this tablet lost it; null otherwise. Asked by _placeWrite for every new
 * write except the withdrawal of a court call (cleanup the new holder wants
 * too). Entries already queued are never touched.
 */
export function consoleWriteRefusal() {
    for (const [, r] of _courts) {
        if (r.mounts > 0 && REFUSING_STATES.has(courtControlState(r.answer))) return courtNotHeldAnswer();
    }
    return null;
}

/**
 * The court control a write of a match on `court` carries: { tabId, deviceId }
 * while the court console is mounted and holds the shiaijo or has had no
 * answer yet, else null (an admin host's write is never stamped).
 */
export function courtControlFor(court) {
    const c = String(court || '');
    const r = c ? _courts.get(c) : null;
    if (!r || r.mounts <= 0) return null;
    const state = courtControlState(r.answer);
    if (state !== 'holds' && state !== 'unknown') return null;
    const stamp = { tabId: courtTabId(), deviceId: courtDeviceId() };
    _stampCourts.set(stamp, c);
    return stamp;
}

/** The shiaijo a stamp courtControlFor handed out was for, or ''. */
export function courtOfControlStamp(stamp) {
    return (stamp && typeof stamp === 'object' && _stampCourts.get(stamp)) || '';
}
