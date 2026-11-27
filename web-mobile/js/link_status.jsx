// link_status.jsx: the ONE owner of "is this page connected", the state the
// admin top bar's pill shows ("Connected" or "Reconnecting…", admin_shell.jsx
// AdminTopbar). The page is connected while the event stream is open
// (API.subscribeToEvents' onStatus, 'open' or 'error') AND the outbox's writes
// do not find the server unreachable (subscribeSyncStatus 'offline'): while
// queued writes fail for network reasons the stream may still read open, and
// the pill says what the writes found (operator decision 2026-09-27).
//
// The court console's control bar (admin_shiaijo_control.jsx) asks the same
// hook, so the two never contradict each other (bc-sseq).
//
// A leaf: window.API, window.subscribeSyncStatus and React are read at call
// time, so a test may install them after the import.

/** Whether the page is connected: the event stream is open and the writes do not find the server unreachable. */
export function linkUpOf(streamOpen, syncStatus) {
    return !!streamOpen && syncStatus !== 'offline';
}

/**
 * The page's connection: { connected, syncStatus, linkUp }. `connected` is
 * the event stream's state (optimistically true until it says otherwise, as
 * the stream replays 'open' to a subscriber joining an open source), and
 * `linkUp` what the top bar's pill shows. `onStream(up)` is told each stream
 * status as it comes (the top bar's sustained-outage timer).
 */
export function useLinkStatus(onStream) {
    const { useState, useEffect, useRef } = React;
    const [connected, setConnected] = useState(true);
    const [syncStatus, setSyncStatus] = useState('synced');
    const onStreamRef = useRef(onStream);
    React.useLayoutEffect(() => { onStreamRef.current = onStream; });
    useEffect(() => {
        const api = typeof window !== 'undefined' ? window.API : null;
        if (!api || typeof api.subscribeToEvents !== 'function') return undefined;
        return api.subscribeToEvents(
            () => {},
            (status) => {
                const up = status === 'open';
                setConnected(up);
                if (typeof onStreamRef.current === 'function') onStreamRef.current(up);
            }
        );
    }, []);
    useEffect(() => {
        const subscribe = typeof window !== 'undefined' && window.subscribeSyncStatus;
        if (typeof subscribe !== 'function') return undefined;
        return subscribe((s) => setSyncStatus(s));
    }, []);
    return { connected, syncStatus, linkUp: linkUpOf(connected, syncStatus) };
}
