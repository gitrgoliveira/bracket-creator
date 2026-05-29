// Announcement composer + modal. Extracted from admin_setup.jsx so the
// same broadcast UI can be reached two ways: inline on the Edit-details
// page (AdminEditTournament) and via a dedicated "Announce" button on the
// admin dashboard (mp-djc). Endpoint is unchanged — POST /api/tournament/announce
// (handlers_announcement.go), driven through window.API.sendAnnouncement.

const { useState: useStateAn, useEffect: useEffectAn, useCallback: useCallbackAn } = React;

// Pure helpers for the announcement-broadcast controls.
// isSendDisabled drives the button's `disabled` attribute.
// sendLabel drives the button's text (inFlight → "Sending...", three
// dots — kept ASCII to match the literal the tests assert on).
function isSendAnnouncementDisabled(message, inFlight) {
  return !message.trim() || inFlight;
}
function sendAnnouncementLabel(inFlight) {
  return inFlight ? "Sending..." : "Send announcement";
}

// AnnouncementComposer — the broadcast card body (active-announcements
// list + message + duration + send). Owns all announcement state and
// handlers. `password` is forwarded to the mutating API calls; `showToast`
// surfaces success/error feedback. Both are optional — guarded at call sites.
function AnnouncementComposer({ password, showToast }) {
  const [announcementMessage, setAnnouncementMessage] = useStateAn("");
  const [announcementDuration, setAnnouncementDuration] = useStateAn(5);
  const [announcementInFlight, setAnnouncementInFlight] = useStateAn(false);
  const [activeAnnouncements, setActiveAnnouncements] = useStateAn([]);

  const refreshAnnouncements = useCallbackAn(async () => {
    try {
      const list = await window.API.fetchAnnouncements();
      setActiveAnnouncements(list || []);
    } catch (_e) {
      // non-fatal
    }
  }, []);

  useEffectAn(() => { refreshAnnouncements(); }, [refreshAnnouncements]);

  const handleSendAnnouncement = async () => {
    const trimmed = announcementMessage.trim();
    if (!trimmed) return;
    setAnnouncementInFlight(true);
    try {
      await window.API.sendAnnouncement(trimmed, announcementDuration, password);
      setAnnouncementMessage("");
      if (showToast) {
        showToast(`Announcement broadcast for ${announcementDuration} minutes!`, "success");
      }
      await refreshAnnouncements();
    } catch (e) {
      if (showToast) {
        showToast(e.message, "error");
      }
    } finally {
      setAnnouncementInFlight(false);
    }
  };

  const handleDismissAnnouncement = async (id) => {
    try {
      await window.API.deleteAnnouncement(id, password);
      await refreshAnnouncements();
    } catch (e) {
      if (showToast) showToast(e.message, "error");
    }
  };

  const handleClearAnnouncements = async () => {
    try {
      await window.API.clearAnnouncements(password);
      await refreshAnnouncements();
    } catch (e) {
      if (showToast) showToast(e.message, "error");
    }
  };

  return (
    <>
      {activeAnnouncements.length > 0 && (
        <div className="field" style={{ marginBottom: 16 }}>
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 8 }}>
            <label className="field__label" style={{ marginBottom: 0 }}>Active announcements</label>
            <button className="btn btn--sm btn--danger" onClick={handleClearAnnouncements}>Clear all</button>
          </div>
          {activeAnnouncements.map(ann => (
            <div key={ann.id} style={{ display: "flex", alignItems: "center", gap: 8, padding: "6px 8px", background: "var(--color-surface-raised, #f5f5f5)", borderRadius: 4, marginBottom: 4 }}>
              <span style={{ flex: 1, fontSize: "0.9em" }}>{ann.message}</span>
              <button
                className="btn btn--sm"
                onClick={() => handleDismissAnnouncement(ann.id)}
                aria-label="Dismiss announcement"
              >&times;</button>
            </div>
          ))}
        </div>
      )}
      <div className="field">
        <label className="field__label">Message</label>
        <textarea
          className="input"
          style={{ width: "100%", height: 80, boxSizing: "border-box", padding: "8px 12px", resize: "vertical" }}
          maxLength={200}
          placeholder="e.g. Lunch break for 30 minutes, Delay on court B..."
          value={announcementMessage}
          onChange={(e) => setAnnouncementMessage(e.target.value)}
        />
        <div className="field__hint" style={{ display: "flex", justifyContent: "space-between", marginTop: 4 }}>
          <span>Maximum 200 characters. Adds to the active announcement queue on all viewer screens.</span>
          <span>{announcementMessage.length}/200</span>
        </div>
      </div>
      <div className="field">
        <label className="field__label">Duration</label>
        <div style={{ display: "flex", gap: 16, marginTop: 8 }}>
          {[5, 10, 15, 30].map((m) => (
            <label key={m} style={{ display: "flex", alignItems: "center", gap: 6, cursor: "pointer", fontWeight: 500 }}>
              <input
                type="radio"
                name="duration"
                value={m}
                checked={announcementDuration === m}
                onChange={() => setAnnouncementDuration(m)}
              />
              <span>{m} min</span>
            </label>
          ))}
        </div>
      </div>
      <div style={{ display: "flex", justifyContent: "flex-end", marginTop: 16 }}>
        <button
          className="btn btn--primary"
          disabled={isSendAnnouncementDisabled(announcementMessage, announcementInFlight)}
          onClick={handleSendAnnouncement}
        >
          {sendAnnouncementLabel(announcementInFlight)}
        </button>
      </div>
    </>
  );
}

// AnnouncementModal — dashboard entry point (mp-djc). Wraps the composer
// in the shared .modal-backdrop / .modal pattern (mirrors AuthModal in
// app.jsx). Backdrop click + Escape close.
function AnnouncementModal({ password, showToast, onClose }) {
  window.useEscapeToClose(onClose);
  return (
    <div className="modal-backdrop" onClick={onClose}>
      {/* modal--lg (720px) matches the width the composer was designed for
          on the Edit-details page (its card is maxWidth 720). At the default
          460px the message-hint row's space-between flex wraps the char
          counter into the middle of the sentence. */}
      <div className="modal modal--lg" onClick={(e) => e.stopPropagation()}>
        <div className="modal__head">
          <div className="modal__title">Broadcast announcement</div>
          <button className="modal__close" onClick={onClose} aria-label="Close">&times;</button>
        </div>
        <div className="modal__body">
          <AnnouncementComposer password={password} showToast={showToast} />
        </div>
      </div>
    </div>
  );
}

window.AnnouncementComposer = AnnouncementComposer;
window.AnnouncementModal = AnnouncementModal;

// ES export for the vitest suite — pure helpers only. Components stay
// behind the window.* pattern to match the rest of admin_*.jsx.
export { isSendAnnouncementDisabled, sendAnnouncementLabel };
