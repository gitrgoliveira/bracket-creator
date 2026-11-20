// The outbox's id leaf: _randomId, which the load-time initializers of the other
// api_client_* pieces call. It imports nothing from the family, so a piece may
// call it while it loads.

// _randomId: a random v4 UUID for every id and token this client mints
// (sessions, tabs, queue keys, team members, court calls). Built from
// getRandomValues, which unlike crypto.randomUUID also exists on a plain-http
// venue address, so it never falls back to Math.random. Declared before the
// first load-time initializer that calls it (_revSession).
export function _randomId() {
    const b = crypto.getRandomValues(new Uint8Array(16));
    b[6] = (b[6] & 0x0f) | 0x40;
    b[8] = (b[8] & 0x3f) | 0x80;
    const h = Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('');
    return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`;
}
