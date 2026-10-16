// version_banner.jsx: the ONE banner that says a new build of the app is
// installed (bc-evts S2). app_version.jsx owns the rule (the page reloads by
// itself once no score editor is open and the outbox is idle); this is what
// shows while it cannot: an editor is open or a write is still unsent. One
// banner for the whole app, displays included (operator S2 a): app.jsx mounts
// it once, beside the App.
import { subscribeVersionState, versionState, VERSION_BANNER_TEXT, VERSION_RELOAD_LABEL } from './app_version.jsx';

// The queue is persisted at every change already; Reload persists it once
// more, then reloads (the reloaded page sends what is still held).
function reloadKeepingTheOutbox() {
  try {
    if (window.API && typeof window.API.persistQueue === 'function') window.API.persistQueue();
  } catch (_e) { /* the reload still goes: every entry was stored as it was made */ }
  location.reload();
}

export function VersionBanner({ onReload = reloadKeepingTheOutbox } = {}) {
  const [state, setState] = React.useState(versionState);
  React.useEffect(() => {
    setState(versionState());
    return subscribeVersionState(setState);
  }, []);
  if (state !== 'stale') return null;
  return (
    <div className="version-banner" role="status" data-testid="version-banner">
      <span className="version-banner__text">{VERSION_BANNER_TEXT}</span>
      <button type="button" className="btn btn--sm version-banner__reload" onClick={() => onReload()}>
        {VERSION_RELOAD_LABEL}
      </button>
    </div>
  );
}
