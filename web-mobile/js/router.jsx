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
const _useRouter = PreactRouter ? PreactRouter.useRouter : null;

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

// Imperative navigation. Equivalent to history.pushState + manual route
// recompute — preact-router intercepts and dispatches to mounted Routers.
function route(url, replace = false) {
    if (_route) return _route(url, replace);
    // Tests / pre-load fallback: use the History API directly.
    if (typeof window !== 'undefined' && window.history) {
        if (replace) window.history.replaceState(null, '', url);
        else window.history.pushState(null, '', url);
    }
    return false;
}

// useQuery() — parse the current URL's query string into a plain object.
// We parse fresh on every render rather than memoising because (a) the
// query strings are short, (b) memoising would require a stable cache
// key derived from window.location.search which we'd have to subscribe
// to anyway, defeating the optimisation. preact-router's own router-
// context hook gives us the re-render trigger via `useRouter()`.
function useQuery() {
    // Subscribe to the router so this hook re-runs when the route
    // changes. _useRouter returns [args, route]; we only need it to
    // schedule re-renders when the URL updates.
    if (_useRouter) {
        try { _useRouter(); } catch { /* not under a Router; fall through */ }
    }
    const search = (typeof window !== 'undefined' && window.location)
        ? window.location.search
        : '';
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

function getCurrentUrl() {
    return _getCurrentUrl();
}

const Link = _Link || ((props) => React.createElement('a', props, props.children));

export { Router, Route, route, Link, useQuery, getCurrentUrl };

if (typeof window !== 'undefined') {
    window.AppRouter = { Router, Route, route, Link, useQuery, getCurrentUrl };
}
