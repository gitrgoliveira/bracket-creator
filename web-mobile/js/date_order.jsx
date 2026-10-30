// A leaf with no imports: the court queue (court_queue.jsx, under the display
// graph) and admin_helpers.jsx both order days by it.
//
// Chronological comparator for DD-MM-YYYY date strings. JS's default
// `Array.sort()` does lexical compare, which works for ISO YYYY-MM-DD
// (lex == chronological) but produces wrong order for DMY: "01-06-2026"
// (June 1) sorts before "12-05-2026" (May 12) lexically. This helper
// converts each value to an ISO sort key so lex compare matches
// chronological order. Non-DMY inputs (e.g. "") fall back to string
// compare so a mix of valid + empty dates still sorts deterministically.

export function compareDmy(a, b) {
  const toKey = (d) => {
    if (!d) return "";
    const m = /^(\d{2})-(\d{2})-(\d{4})$/.exec(d);
    return m ? `${m[3]}-${m[2]}-${m[1]}` : d;
  };
  return toKey(a).localeCompare(toKey(b));
}
