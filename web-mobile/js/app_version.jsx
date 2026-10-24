// app_version.jsx: the ONE owner of "is this page running the server's build,
// and may it reload onto the new one" (bc-evts stage 3, S2). A leaf with no
// imports: api_client.jsx feeds it the server's build (the X-App-Build header
// of every answer, the SSE hello frame's `build`) and the outbox's state, the
// score editors and every form holding unsaved input register while they do
// (registerUnsavedInput, useUnsavedInput), and the app shell renders the
// banner from versionState(). The app and every display follow this one rule:
//
//   builds match (or either is "unknown")       -> 'current'
//   mismatch, no unsaved input, outbox idle     -> persist the outbox, reload
//                                                  ONCE -> 'reloading'
//   mismatch otherwise                          -> 'stale' (the banner), and
//                                                  re-checked when an editor
//                                                  closes or the outbox drains
//
// The page's own build is the <meta name="app-build"> the server writes into
// the page it serves (internal/mobileapp/index_meta.go, appBuildMeta; the
// operator's decision S2 a). The page (no-store) names its modules under the
// build's own directory, /dist/<content hash>/, served immutable
// (internal/mobileapp/server.go, staticCacheControlScoped; review 3, F3), so a
// reload brings the page and its code from one build (review 2, R7) without
// asking for a module it holds. As a backstop against a loop all the same (a
// cache that serves an old page), a page reloads ONCE for a given server build
// (_RELOADED_FOR, in sessionStorage), and a page still behind it after that
// shows the banner. A tab whose sessionStorage refuses the note notes it on
// the address it reloads to instead (_RELOAD_FLAG, stage 3 review 8, L2), which
// the page it loads reads and takes off its address.

export const VERSION_BANNER_TEXT = 'A new version of the app is installed. Reload to keep scoring.';
export const VERSION_RELOAD_LABEL = 'Reload';

const UNKNOWN = 'unknown';

let _bundleBuild = null;
let _serverBuild = null;
let _unsavedHolders = 0;
let _state = 'current';
let _reloaded = false;
let _outboxIdle = () => true;
let _beforeReload = () => {};
let _reload = _reloadPage;
const _subscribers = new Set();

// _reloadPage: the reload, to `to` when given (an address noting the build,
// _flaggedAddress), else of the page as it is.
function _reloadPage(to) {
    if (to) location.replace(to);
    else location.reload();
}

// The server build this tab last reloaded for (sessionStorage: per tab, kept
// across the reload).
const _RELOADED_FOR = 'bc_app_reloaded_for_build';

// The query parameter that notes the reload on the address when
// sessionStorage refuses it (L2). Read once as the page loads, then taken off
// the address, so it lasts this page's life and no further.
const _RELOAD_FLAG = 'bc_reloaded_for';
let _flaggedFor = _takeReloadFlag();

function _takeReloadFlag() {
    try {
        const url = new URL(location.href);
        const build = url.searchParams.get(_RELOAD_FLAG);
        if (build === null) return '';
        url.searchParams.delete(_RELOAD_FLAG);
        history.replaceState(history.state, '', url.pathname + url.search + url.hash);
        return build;
    } catch (_e) {
        return '';
    }
}

// _flaggedAddress: this page's address, noting a reload for `build`.
function _flaggedAddress(build) {
    const url = new URL(location.href);
    url.searchParams.set(_RELOAD_FLAG, build);
    return url.toString();
}

// bundleBuild is the build this page was served with, read once from the page
// meta the server wrote (unknown without it, which never forces a reload).
export function bundleBuild() {
    if (_bundleBuild === null) {
        let content = '';
        try {
            content = document.querySelector('meta[name="app-build"]')?.content || '';
        } catch (_e) {
            content = '';
        }
        _bundleBuild = content.trim() || UNKNOWN;
    }
    return _bundleBuild;
}

function _reloadedFor(build) {
    if (_flaggedFor === build) return true;
    try {
        return sessionStorage.getItem(_RELOADED_FOR) === build;
    } catch (_e) {
        return false;
    }
}

function _noteReloadFor(build) {
    try {
        sessionStorage.setItem(_RELOADED_FOR, build);
    } catch (e) {
        // The note then rides on the address the page reloads to (L2), so
        // the page it loads still knows, and shows the banner rather than
        // reloading again.
        console.warn('app_version: could not note the reload in sessionStorage, noting it on the address:', e);
        return false;
    }
    return true;
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

// registerUnsavedInput: the ONE registry of "this page holds input the
// operator has not saved", which an automatic reload would lose. A score
// editor registers while it is open (admin_scoring_modal.jsx); a form
// registers while it holds typed input not yet saved (useUnsavedInput): the
// participants paste box, a settings form, a lineup rename, a prompt's reason.
// While any holder is registered the automatic reload waits and the banner
// shows. The function it returns unregisters once however often it is called
// (StrictMode runs an effect's cleanup twice), then re-checks.
export function registerUnsavedInput() {
    _unsavedHolders += 1;
    let done = false;
    return () => {
        if (done) return;
        done = true;
        _unsavedHolders = Math.max(0, _unsavedHolders - 1);
        checkVersion();
    };
}

// useUnsavedInput: a form's hook into registerUnsavedInput, registered while
// `holds` is true (typed input not yet saved) and unregistered as it saves,
// clears or unmounts. React is the page's global (the vendored Preact compat),
// so this leaf still imports nothing.
export function useUnsavedInput(holds) {
    const on = !!holds;
    React.useEffect(() => (on ? registerUnsavedInput() : undefined), [on]);
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

// setReloader replaces the reload, for tests: called with the address to go to
// (a reload noted on it, L2), or with nothing to reload the page as it is.
export function setReloader(fn) {
    _reload = typeof fn === 'function' ? fn : _reloadPage;
}

// reloadApp: the operator's Reload on the banner (version_banner.jsx), through
// the same reloader the automatic reload uses.
export function reloadApp() {
    _reload();
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

// outboxIsIdle: the outbox is empty and nothing is in flight, so a reload
// loses nothing of this page's whatever browser storage answers (L2).
export function outboxIsIdle() {
    return _outboxIsIdle();
}

// checkVersion applies the rule above and answers the state.
export function checkVersion() {
    if (_reloaded) return _state;
    if (_serverBuild === null || buildsMatch(bundleBuild(), _serverBuild)) {
        _setState('current');
        return _state;
    }
    // Reloaded for this server build already, and still behind it: the banner,
    // not another reload (R8). `_reloaded` keeps it to one reload per page.
    if (_unsavedHolders === 0 && _outboxIsIdle() && !_reloadedFor(_serverBuild)) {
        const noted = _noteReloadFor(_serverBuild);
        let to;
        try {
            to = noted ? undefined : _flaggedAddress(_serverBuild);
        } catch (e) {
            // No address to note it on either: nothing would stop a loop.
            console.error('app_version: could not note the reload, showing the banner instead:', e);
            _setState('stale');
            return _state;
        }
        _reloaded = true;
        try {
            _beforeReload();
        } catch (e) {
            console.error('app_version: saving before the reload failed:', e);
        }
        _setState('reloading');
        _reload(to);
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
    _unsavedHolders = 0;
    _state = 'current';
    _reloaded = false;
    _flaggedFor = '';
    _outboxIdle = () => true;
    _beforeReload = () => {};
    _reload = _reloadPage;
    _subscribers.clear();
    try { sessionStorage.removeItem(_RELOADED_FOR); } catch (_e) { /* no storage in this test */ }
}
