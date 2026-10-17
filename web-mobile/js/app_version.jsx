// app_version.jsx: the ONE owner of "is this page running the server's build,
// and may it reload onto the new one" (bc-evts stage 3, S2). A leaf with no
// imports: api_client.jsx feeds it the server's build (the X-App-Build header
// of every answer, the SSE hello frame's `build`) and the outbox's state, the
// score editors register while they are open, and the app shell renders the
// banner from versionState(). The app and every display follow this one rule:
//
//   builds match (or either is "unknown")       -> 'current'
//   mismatch, no editor open, outbox idle       -> persist the outbox, reload
//                                                  ONCE -> 'reloading'
//   mismatch otherwise                          -> 'stale' (the banner), and
//                                                  re-checked when an editor
//                                                  closes or the outbox drains
//
// The build the RUNNING CODE was compiled for is the bundle's own constant,
// __APP_BUILD__, which esbuild writes in (--define) from the commit
// internal/cmd/version/get_build_info.sh records, the same file the server
// reads for X-App-Build (AppBuild). Not a value in the page: /dist/ is cached
// for five minutes, so a reload can pair fresh HTML with the modules of the
// build before, and only the code can say which build it is (review 1, M2).
// For the same reason a reload can bring the same old modules back: a page
// reloads ONCE for a given server build (_RELOADED_FOR, in sessionStorage),
// and a page still behind it after that shows the banner instead of looping.

export const VERSION_BANNER_TEXT = 'A new version of the app is installed. Reload to keep scoring.';
export const VERSION_RELOAD_LABEL = 'Reload';

const UNKNOWN = 'unknown';

let _bundleBuild = null;
let _serverBuild = null;
let _openEditors = 0;
let _state = 'current';
let _reloaded = false;
let _outboxIdle = () => true;
let _beforeReload = () => {};
let _reload = () => { location.reload(); };
const _subscribers = new Set();

// The server build this tab last reloaded for (sessionStorage: per tab, kept
// across the reload).
const _RELOADED_FOR = 'bc_app_reloaded_for_build';

// bundleBuild is the build the running code was compiled for, read once:
// __APP_BUILD__, or unknown when the bundle was compiled without it.
export function bundleBuild() {
    if (_bundleBuild === null) {
        const built = typeof __APP_BUILD__ === 'string' ? __APP_BUILD__ : '';
        _bundleBuild = built.trim() || UNKNOWN;
    }
    return _bundleBuild;
}

function _reloadedFor() {
    try {
        return sessionStorage.getItem(_RELOADED_FOR) || '';
    } catch (_e) {
        return '';
    }
}

function _noteReloadFor(build) {
    try {
        sessionStorage.setItem(_RELOADED_FOR, build);
    } catch (e) {
        // Without the note a reload that brings the old modules back would
        // reload again; said so, and the reload still goes.
        console.error('app_version: could not note the reload:', e);
    }
}

// buildsMatch: equal, or either side unknown (a binary built without a commit
// cannot say, so it never forces a reload).
export function buildsMatch(a, b) {
    return a === b || a === UNKNOWN || b === UNKNOWN;
}

// noteServerBuild records the build an answer or the SSE hello named and
// re-checks. An empty or missing value (a header the answer did not carry)
// says nothing.
export function noteServerBuild(build) {
    if (typeof build !== 'string' || build.trim() === '') return;
    _serverBuild = build.trim();
    checkVersion();
}

// registerOpenEditor counts an open score editor; the function it returns
// unregisters it once however often it is called (StrictMode runs an effect's
// cleanup twice), then re-checks.
export function registerOpenEditor() {
    _openEditors += 1;
    let done = false;
    return () => {
        if (done) return;
        done = true;
        _openEditors = Math.max(0, _openEditors - 1);
        checkVersion();
    };
}

// setOutboxIdleProbe: () => boolean, true when the outbox is empty and nothing
// is in flight.
export function setOutboxIdleProbe(fn) {
    _outboxIdle = typeof fn === 'function' ? fn : () => true;
}

// setBeforeReload: called right before the reload (the outbox's persist).
export function setBeforeReload(fn) {
    _beforeReload = typeof fn === 'function' ? fn : () => {};
}

// setReloader replaces location.reload, for tests.
export function setReloader(fn) {
    _reload = typeof fn === 'function' ? fn : () => { location.reload(); };
}

// noteOutboxDrained: the outbox has emptied; a stale page may reload now.
export function noteOutboxDrained() {
    checkVersion();
}

function _setState(next) {
    if (next === _state) return;
    _state = next;
    for (const fn of _subscribers) {
        try {
            fn(next);
        } catch (e) {
            console.error('app_version: subscriber failed:', e);
        }
    }
}

function _outboxIsIdle() {
    try {
        return _outboxIdle() === true;
    } catch (_e) {
        return false;
    }
}

// checkVersion applies the rule above and answers the state.
export function checkVersion() {
    if (_reloaded) return _state;
    if (_serverBuild === null || buildsMatch(bundleBuild(), _serverBuild)) {
        _setState('current');
        return _state;
    }
    // Reloaded for this server build already, and still behind it: the reload
    // brought the cached modules back. The banner, not another reload.
    if (_openEditors === 0 && _outboxIsIdle() && _reloadedFor() !== _serverBuild) {
        _reloaded = true;
        _noteReloadFor(_serverBuild);
        try {
            _beforeReload();
        } catch (e) {
            console.error('app_version: saving before the reload failed:', e);
        }
        _setState('reloading');
        _reload();
        return _state;
    }
    _setState('stale');
    return _state;
}

export function versionState() {
    return _state;
}

// subscribeVersionState(fn) calls fn(state) on each change; returns the
// unsubscribe.
export function subscribeVersionState(fn) {
    _subscribers.add(fn);
    return () => { _subscribers.delete(fn); };
}

// resetVersionForTests puts every piece of module state back.
export function resetVersionForTests() {
    _bundleBuild = null;
    _serverBuild = null;
    _openEditors = 0;
    _state = 'current';
    _reloaded = false;
    _outboxIdle = () => true;
    _beforeReload = () => {};
    _reload = () => { location.reload(); };
    _subscribers.clear();
    try { sessionStorage.removeItem(_RELOADED_FOR); } catch (_e) { /* no storage in this test */ }
}
