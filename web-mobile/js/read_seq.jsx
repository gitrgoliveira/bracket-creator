// read_seq.jsx: the one owner of an answer's place in the event stream
// (bc-sseq FR4).
//
// The four reads a page fetches again after an event (the tournament, the
// competitions list, one competition's detail, the court feed) answer with
// `X-Event-Seq`: the seq of the newest event the server had sent when the read
// began (internal/mobileapp/event_seq.go). An answer at seq N already holds
// every change announced at or below N, so a page that applied it needs no
// refetch for those events (createRefetchGate, patch.jsx).
//
// The seq rides beside the value a fetcher returns, never inside it: no
// payload or normaliser changes, and a host that merges what it fetched
// (keep(prev, data)) reads the seq off the value the fetcher returned before
// merging, since the merged object carries none.
//   - noteReadSeq(value, res): keeps the answer's seq for `value` (the object
//     the fetcher finally returns) and returns `value`;
//   - holdReadSeq(value, seq): the same for a seq the host worked out;
//   - readSeqOf(value): that seq, or 0 when the answer carried none (a
//     missing or unreadable header covers no event).
//
// Leaf module: it imports nothing, so patch.jsx and the API pieces can import
// it without a load cycle.

const EVENT_SEQ_HEADER = 'X-Event-Seq';

const seqs = new WeakMap();

const keyable = (value) => value !== null && (typeof value === 'object' || typeof value === 'function');

// headerSeq reads the header off a fetch Response. A response with no
// readable headers (a test double, a non-fetch caller) or a value that is not
// a positive whole number reads as 0.
function headerSeq(res) {
    const headers = res && res.headers;
    if (!headers || typeof headers.get !== 'function') return 0;
    const raw = headers.get(EVENT_SEQ_HEADER);
    if (raw == null || String(raw).trim() === '') return 0;
    const n = Number(raw);
    return Number.isSafeInteger(n) && n > 0 ? n : 0;
}

// holdReadSeq keeps `seq` as `value`'s place in the stream (0 clears it) and
// returns `value`: for a host that joins two answers into one value, which is
// covered only up to the lower of their seqs.
function holdReadSeq(value, seq) {
    if (!keyable(value)) return value;
    if (seq > 0) seqs.set(value, seq);
    else seqs.delete(value);
    return value;
}

function noteReadSeq(value, res) {
    return holdReadSeq(value, headerSeq(res));
}

function readSeqOf(value) {
    return keyable(value) ? seqs.get(value) || 0 : 0;
}

export { EVENT_SEQ_HEADER, holdReadSeq, noteReadSeq, readSeqOf };
