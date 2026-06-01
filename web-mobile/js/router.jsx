// Thin wrapper around preact-router exposing a `<Router>` component, a
// `<Route>` helper, the imperative `route()` navigation function, and a
// `useQuery()` hook for query-string parameters.
//
// preact-router itself is loaded as a UMD bundle from vendor/, exposing
// itself as `window.preactRouter`. We re-export under the React-aliased
// names so the rest of the codebase can import from this module without
// reaching into globals scattered through every file.
//
// Route paths follow preact-router conventions: `:param` for a path
// segment, `:param?` for optional, `:rest*` for catch-all. Components
// rendered by a matched <Route> receive the path params plus a `matches`
// prop with the full match record.
//
// useQuery() returns the parsed `?key=value` parameters as a plain
// object. It re-runs on every history change because preact-router fires
// a popstate-equivalent on its own route() calls. To keep the hook cheap
// we re-parse on each render rather than caching — query strings here
// are short and the parse cost is negligible.

const PreactRouter = window.preactRouter;
const _Router = PreactRouter ? PreactRouter.Router : null;
const _route = PreactRouter ? PreactRouter.route : null;
const _Link = PreactRouter ? PreactRouter.Link : null;
const _getCurrentUrl = PreactRouter ? PreactRouter.getCurrentUrl : (() => (typeof window !== 'undefined' ? window.location.pathname + window.location.search : '/'));

// <Router> — wrapper that simply forwards to preact-router's Router.
// We keep it as a thin pass-through so callers can later swap the
// implementation (e.g. for SSR or static rendering) without changing
// import sites across the app.
function Router(props) {
    if (!_Router) {
        // Defensive fallback during the migration window when index.html
        // hasn't loaded preact-router yet (e.g. tests). Render children
        // directly so app.jsx still mounts.
        return React.createElement('div', null, props.children);
    }
    return React.createElement(_Router, props, props.children);
}

// <Route component={X} path="/foo" /> — preact-router accepts this shape
// when nested inside <Router>. Components matched by path receive the
// route params as props.
function Route(props) {
    return React.createElement(props.component || 'div', props);
}

// Imperative navigation.
//
// NOTE: we deliberately do NOT delegate to preact-router's route() here.
// preact-router's route() only mutates history when canRoute(url) finds a
// MOUNTED <Router> that matches the path. This app renders entirely from its
// own state machine (app.jsx) and never mounts a <Router>, so canRoute() is
// always false and preact-router's route() silently no-ops — leaving the
// address bar stuck while the view changes. That broke back/forward and
// shareable deep links (mp-dd3).
//
// Instead we drive the History API directly. app.jsx owns the routing
// state-of-record: it calls route() from a state->URL sync effect, and it
// handles browser back/forward via its own popstate listener.
//
// We dispatch a popstate event after mutating history so listeners that
// rely on a history-change signal — notably useQuery() below — re-parse
// and react to programmatic navigation. The native History API does NOT
// fire popstate on pushState/replaceState, but preact-router's route()
// does, and useQuery() / display surfaces were written against that
// contract. Dispatching here restores it.
//
// Safety against loops: app.jsx's popstate handler calls parsePath on
// location.pathname and setMode/setAdminView/setViewerCompId accordingly.
// Because route() is only called from the state->URL sync effect AFTER
// the state already matches the new URL, parsePath returns the same
// state that's already set — React bails on identical setState values,
// and the URL-sync effect's `location.pathname !== url` guard prevents
// any re-entry on a possible re-render.
function route(url, replace = false) {
    if (typeof window !== 'undefined' && window.history) {
        if (replace) window.history.replaceState(null, '', url);
        else window.history.pushState(null, '', url);
        try {
            window.dispatchEvent(new PopStateEvent('popstate'));
        } catch {
            // Older / non-browser environments without PopStateEvent.
            // useQuery() still works because its onChange just re-reads
            // location.search; missing this signal is harmless there.
        }
        return true;
    }
    return false;
}

// Pure parse of a search string into a plain object. Extracted so the
// useQuery hook below stays trivial and so the parser can be unit-tested
// without a DOM.
function parseSearch(search) {
    const params = {};
    if (!search || search.length < 2) return params;
    const trimmed = search.startsWith('?') ? search.slice(1) : search;
    for (const pair of trimmed.split('&')) {
        if (!pair) continue;
        const eq = pair.indexOf('=');
        if (eq === -1) {
            params[decodeURIComponent(pair)] = '';
        } else {
            const k = decodeURIComponent(pair.slice(0, eq));
            const v = decodeURIComponent(pair.slice(eq + 1));
            params[k] = v;
        }
    }
    return params;
}

// useQuery() — parse the current URL's query string into a plain object,
// re-rendering the calling component on history-stack changes. We
// subscribe to `popstate` explicitly rather than calling preact-router's
// useRouter() purely for its side effect, because conditional hook calls
// inside try/catch violate the Rules of Hooks and silently swallow real
// failures. `route()` from preact-router dispatches `popstate` after
// pushState, so this covers programmatic navigation too.
function useQuery() {
    const { useState, useEffect } = React;
    const getSearch = () => (typeof window !== 'undefined' && window.location)
        ? window.location.search
        : '';
    const [search, setSearch] = useState(getSearch);
    useEffect(() => {
        if (typeof window === 'undefined') return undefined;
        const onChange = () => setSearch(window.location.search);
        window.addEventListener('popstate', onChange);
        return () => window.removeEventListener('popstate', onChange);
    }, []);
    return parseSearch(search);
}

function getCurrentUrl() {
    return _getCurrentUrl();
}

const Link = _Link || ((props) => React.createElement('a', props, props.children));

export { Router, Route, route, Link, useQuery, getCurrentUrl };

if (typeof window !== 'undefined') {
    window.AppRouter = { Router, Route, route, Link, useQuery, getCurrentUrl };
}
