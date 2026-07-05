// timer_pool.jsx: a self-pruning setTimeout pool for long-lived effects.
//
// schedule() wraps the callback so a FIRED timer deletes its own id from the
// pool; clearAll() cancels whatever is still pending. Without the self-prune,
// an effect that stays mounted for an entire tournament day (a /display TV
// wall, a viewer parked on one competition, the operator console) accumulates
// one entry per scheduled refetch, i.e. one or two per SSE event, for the
// tab's lifetime (mp-wng6).
//
// Leaf module by design: app.jsx, admin.jsx, and admin_shiaijo.jsx all import
// it. It must NOT live in app.jsx: the admin modules importing app.jsx would
// evaluate it a second time under a different URL (script tag app.js?v=N vs
// bare import app.js), the mp-zd1v double-load class.
export function createTimerPool() {
  const pending = new Set();
  return {
    schedule(fn, delay) {
      const id = setTimeout(() => {
        pending.delete(id);
        fn();
      }, delay);
      pending.add(id);
    },
    clearAll() {
      pending.forEach(clearTimeout);
      pending.clear();
    },
    // pendingCount: pool-size observability. Currently read only by the
    // timer_pool unit tests, which use it to assert the self-prune drains.
    pendingCount() {
      return pending.size;
    },
  };
}
