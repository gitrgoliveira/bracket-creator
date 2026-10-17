// version_banner.jsx: the ONE banner that says a new build of the app is
// installed (bc-evts S2). app_version.jsx owns the rule (the page reloads by
// itself once no score editor is open and the outbox is idle); this is what
// shows while it cannot: an editor is open or a write is still unsent. One
// banner for the whole app, displays included (operator S2 a): app.jsx mounts
// it once, beside the App.
import { subscribeVersionState, versionState, reloadApp, outboxIsIdle, VERSION_BANNER_TEXT, VERSION_RELOAD_LABEL } from './app_version.jsx';
import { QUEUED_UNSAVED_NOTICE } from './write_result.jsx';

// Reload stores the outbox, then reloads (the reloaded page sends what is
// still held), but only once the outbox IS stored (stage 3 review 7, K3):
// browser storage can refuse it (full), and then the writes this page holds
// live only in its memory, which a reload would lose. An outbox empty and
// idle holds nothing to lose, so Reload goes whatever storage answers (stage 3
// review 8, L2: storage that refuses every access kept such a page on the old
// build for good). Answers whether it reloaded.
function reloadKeepingTheOutbox() {
  let stored = true;
  try {
    if (window.API && typeof window.API.persistQueue === 'function') stored = window.API.persistQueue() !== false;
  } catch (_e) {
    stored = false;
  }
  if (!stored && !outboxIsIdle()) return false;
  reloadApp();
  return true;
}

export function VersionBanner({ onReload = reloadKeepingTheOutbox } = {}) {
  const [state, setState] = React.useState(versionState);
  // The last Reload found the outbox could not be stored: the page stays, and
  // says to keep it open (the one wording of that, write_result.jsx).
  const [notStored, setNotStored] = React.useState(false);
  React.useEffect(() => {
    setState(versionState());
    return subscribeVersionState(setState);
  }, []);
  if (state !== 'stale') return null;
  return (
    <div className="version-banner" role="status" data-testid="version-banner">
      <span className="version-banner__text">
        {VERSION_BANNER_TEXT}
        {notStored ? <span className="version-banner__note">{QUEUED_UNSAVED_NOTICE}</span> : null}
      </span>
      <button type="button" className="btn btn--sm version-banner__reload" onClick={() => setNotStored(onReload() === false)}>
        {VERSION_RELOAD_LABEL}
      </button>
    </div>
  );
}
