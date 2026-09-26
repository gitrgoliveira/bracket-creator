// server_clock.jsx: this device's clock in the server's frame (bc-lww1).
//
// api_client.jsx learns the offset from GET /api/time and stamps every write
// with serverNowMs(), so a stored match's modifiedAt is in this frame. A score
// editor that asks "was this snapshot written before the operator's last
// edit?" stamps the edit with the same clock, so the two compare in one frame.
//
// A leaf with no imports, and import-only (no <script> tag of its own): every
// importer resolves the same module URL and so shares ONE offset. A module
// both script-tagged and ES-imported would load twice and split it (mp-zd1v).
let offsetMs = 0;

export function serverNowMs() {
  return Date.now() + offsetMs;
}

export function serverClockOffsetMs() {
  return offsetMs;
}

export function setServerClockOffsetMs(ms) {
  offsetMs = ms;
}
