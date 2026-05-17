// Polling helper for the post-submit download flow. Kept in its own module so
// the interval/timeout bookkeeping is testable without a DOM, and so a rapid
// resubmit cancels the previous loop instead of stacking a second one beside
// it.

let activePoll = null;

export function stopActivePoll(timers = defaultTimers) {
    if (activePoll === null) return;
    if (activePoll.interval !== null) timers.clearInterval(activePoll.interval);
    if (activePoll.timeout !== null) timers.clearTimeout(activePoll.timeout);
    activePoll = null;
}

// startDownloadPoll starts a polling loop for the given token. Calling it
// again with a fresh token cancels the previous loop (interval + timeout)
// before starting the new one — so duplicate submits never produce two
// concurrent intervals.
//
// Required deps:
//   fetchStatus(token) -> Promise<{ready: boolean}>
//
// Optional deps (default to platform globals):
//   onReady()                   — called once when fetchStatus reports ready
//   onError()                   — called once after maxConsecutiveErrors
//   onTimeout()                 — called once when timeoutMs elapses without ready
//   intervalMs (default 100)
//   timeoutMs (default 60000)
//   maxConsecutiveErrors (default 5)
//   timers                       — { setInterval, clearInterval, setTimeout, clearTimeout }
//                                  (override for tests; defaults to globalThis)
export function startDownloadPoll(token, deps = {}) {
    const fetchStatus = deps.fetchStatus;
    if (typeof fetchStatus !== "function") {
        throw new Error("startDownloadPoll: fetchStatus is required");
    }
    const onReady = deps.onReady || (() => {});
    const onError = deps.onError || (() => {});
    const onTimeout = deps.onTimeout || (() => {});
    const intervalMs = deps.intervalMs || 100;
    const timeoutMs = deps.timeoutMs || 60000;
    const maxConsecutiveErrors = deps.maxConsecutiveErrors || 5;
    const timers = deps.timers || defaultTimers;

    stopActivePoll(timers);

    let consecutiveErrors = 0;
    const interval = timers.setInterval(function () {
        fetchStatus(token)
            .then(data => {
                consecutiveErrors = 0;
                if (data && data.ready) {
                    stopActivePoll(timers);
                    onReady();
                }
            })
            .catch(err => {
                consecutiveErrors++;
                if (typeof console !== "undefined" && console.error) {
                    console.error("Failed to poll download status:", err);
                }
                if (consecutiveErrors >= maxConsecutiveErrors) {
                    stopActivePoll(timers);
                    onError();
                }
            });
    }, intervalMs);

    const timeout = timers.setTimeout(function () {
        // Only fire the timeout path if the interval is still active —
        // otherwise we already completed (success or error path).
        if (activePoll !== null && activePoll.interval === interval) {
            stopActivePoll(timers);
            onTimeout();
        }
    }, timeoutMs);

    activePoll = { interval, timeout };
    return activePoll;
}

// Test hook: returns the current activePoll handle (null when idle).
export function getActivePoll() {
    return activePoll;
}

const defaultTimers = {
    setInterval: (fn, ms) => setInterval(fn, ms),
    clearInterval: (id) => clearInterval(id),
    setTimeout: (fn, ms) => setTimeout(fn, ms),
    clearTimeout: (id) => clearTimeout(id),
};
