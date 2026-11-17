// The court console's start, pick and correct flow (admin_shiaijo.jsx), as a
// hook: a Start tap, a pick of another bout (which sends the running one back
// first), the one-start-at-a-time guard and the refusals it shows, the
// question a pick asks when the server cannot check it (bc-stpf), and opening
// a finished match to correct it. The state two concerns share (startingKey,
// pickedKey, correctingKey, startedFrom, startError) stays in the page and is
// passed in; the page renders what this hook returns.
//
// useCourtStart is called on every render, after the page has worked out the
// court's live bout (liveHere, liveBout), the open correction and the selected
// match, so its functions are rebuilt with the current render exactly as the
// inline ones were. latestRef, which carries this render's pickMatch and
// correctMatch with the court's matches to a tap judged again after an await
// (pickMatch, correctMatch, deferAndStart), is bound by this hook's own layout
// effect, the last hook call in it, so it holds the functions of the render
// that committed before any continuation reads it.

import { acceptTap } from './tap_guard.jsx';
import {
    startWhileCorrectingMessage, startWhileStartingMessage, startWasBlockedByStartMessage,
    correctWhileRunningMessage, wasToasted, writePending, writeQueueKey,
} from './write_result.jsx';
// What a Start came back with (a clock_skew refusal, a thrown 409) is
// start_match.jsx's to classify, shared with the Scores tab's own automatic
// start (admin_schedule_score_editor.jsx). startPatch, the write itself, comes
// from the same leaf, not from `window.startPatch`. The state wiring stays here:
// startingRef/pickingRef carry the pick exemption (pickMatch), which that
// leaf's guard has no word for.
import { startPatch, classifyStartOutcome, startFailureMessage, startRefusalStands } from './start_match.jsx';
// swissRoundLabel: single owner is pool_ids.jsx (mp-dej2); the console used to
// carry its own copy. scoreRowMatchName names a match in the refusal notices.
import { scoreRowMatchName } from './pool_ids.jsx';
import { holdsUnsaved } from './unsaved_entries.jsx';
import { confirmMatchLabel } from './court_move.jsx';
import { paceDisplayOf, PaceTime } from './court_pace.jsx';
import { matchKey } from './admin_shiaijo_feed.jsx';
import { isTeamMatch, shiaijoMatchNo, TieBreakerReason, MatchSides } from './admin_shiaijo_queue.jsx';

const { useState, useEffect, useRef, useCallback } = React;

const Icon = window.Icon;

export function useCourtStart({
    court, password, showToast, onEditScore, mountedRef, editorUnsavedRef, leaveEditorConfirmed,
    allMatches, courtMatchesRaw, liveHere, liveBout, openCorrection, selectedMatch, isStartedSnapshot,
    startingKey, setStartingKey, startError, setStartError, setPickedKey, setCorrectingKey, setStartedFrom,
    markSentBackHere,
}) {
    // The latest render's pick and correct and the court's matches, for a tap
    // judged again once the operator has answered the unsaved-entries
    // question (pickMatch, correctMatch).
    const latestRef = useRef(null);
    // The start in flight, as { key, match } (null = none). The guards READ this
    // ref, never startingKey: an advance awaits its finish write and then starts
    // the next match from the render that began the await, so a closure's state
    // is stale by then (a start begun meanwhile was not seen; one that has
    // landed still was). The state (startingKey, the page's) only drives the
    // rendering.
    const startingRef = useRef(null);
    // The scheduled match a pick (pickMatch) is starting, from the moment its
    // guards pass until the pick settles: { key, match } or null. It is a start in
    // flight for the one-start guard, because the pick awaits the defer of the
    // running bout before it reaches startMatch (and so before startingRef is
    // set). startMatch is handed its own pick, which is exempt from the guard.
    const pickingRef = useRef(null);
    // bc-sdtp: one bounce guard for every Start match on this console (Up next
    // and the queue rows), keyless, because the bounce lands on a DIFFERENT
    // button: the first start can land inside the bounce window, the card then
    // shows the next match, and a per-match key would let the bounce start it
    // and send the match just started back to the queue.
    const startTapRef = useRef(null);
    // The last tap the console refused because a match is live (a Correct), a
    // correction is open (a Start) or another start is still out (a Start):
    // { key, compId, why: "running" | "correcting" | "starting", against, label, start }
    // (`against` is the blocker's key, `label` its name as the operator sees it, and
    // `start` the in-flight start or pick record for "starting", null otherwise).
    // Only the tap is stored; the notice text is derived at render while the
    // condition still holds (refusalNotice below), so it cannot go stale. The one
    // that outlives its condition is "starting": when the start it named lands,
    // the effect beside startMatch turns it into a stored startError for the
    // refused match, in the past tense.
    const [refusedTap, setRefusedTap] = useState(null);
    // bc-stpf (operator ruling 2026-10-27, Q1): the question a pick of another
    // bout asks when the app cannot check that the picked match can start (no
    // answer in time, offline, or writes of its competition not sent yet):
    // send the running bout back and start the pick, or keep it running. It is
    // local and never waits on the network, so the court keeps working without
    // the server. Shape: {curLabel, pickLabel}; the answer goes to the promise
    // askStartWithoutCheck handed out (startAskResolveRef).
    const [pendingStartAsk, setPendingStartAsk] = useState(null);
    const startAskResolveRef = useRef(null);
    // Memoised on mountedRef, the one value from outside that it reads (a ref
    // that never changes), so the unmount answer below is set up once.
    const answerStartAsk = useCallback((sendBack) => {
        const resolve = startAskResolveRef.current;
        startAskResolveRef.current = null;
        if (mountedRef.current) setPendingStartAsk(null);
        if (resolve) resolve(sendBack);
    }, [mountedRef]);
    const askStartWithoutCheck = (cur, m) => new Promise((resolve) => {
        startAskResolveRef.current = resolve;
        setPendingStartAsk({ curLabel: confirmMatchLabel(cur), pickLabel: confirmMatchLabel(m) });
    });
    // A console that goes away with the question open answers it "keep it
    // running": nothing is sent, and the pick's finally runs.
    useEffect(() => () => answerStartAsk(false), [answerStartAsk]);

    // What refuses a tap right now, by kind: a live bout refuses a Correct, an
    // open correction refuses a Start, and a start still out refuses a Start of
    // another match. ONE place builds each sentence, for the toast and the
    // notice alike.
    // "starting" names `startOut`, the start (or pick) record that is out: a
    // tap's refusal reads it from the refs, so a refusal made from an advance's
    // async continuation names the start that is out NOW (the render's copy may
    // be stale there), and a refusal made during a pick's defer names that pick.
    // The render reads the state, which follows the refs (syncStartingKey).
    const refusalBlocker = (why, startOut) => {
        if (why === "running") return liveBout;
        if (why === "starting") return startOut ? startOut.match : null;
        return openCorrection;
    };
    const refusalText = (why, blocker) => {
        if (!blocker) return null;
        if (why === "running") return correctWhileRunningMessage({ court, label: scoreRowMatchName(blocker) });
        if (why === "starting") return startWhileStartingMessage({ label: scoreRowMatchName(blocker) });
        return startWhileCorrectingMessage({ label: scoreRowMatchName(blocker) });
    };
    // The notice for a refused tap, derived: shown only while the match that
    // refused it still does, so it goes by itself when that match is finished,
    // sent back or cancelled, and a later, unrelated one never revives it.
    // A "starting" refusal holds the record of the start that refused it, which
    // is still the one out while startingKey names it; another start out names
    // a different match, so the notice goes either way.
    const refusalNotice = (key) => {
        if (!refusedTap || refusedTap.key !== key) return null;
        const startOut = refusedTap.start && refusedTap.start.key === startingKey ? refusedTap.start : null;
        const blocker = refusalBlocker(refusedTap.why, startOut);
        return blocker && matchKey(blocker) === refusedTap.against ? refusalText(refusedTap.why, blocker) : null;
    };
    // Refuse a tap: remember it against the match that blocks it, and toast.
    const refuseTap = (m, why) => {
        const startOut = startingRef.current || pickingRef.current;
        const blocker = refusalBlocker(why, startOut);
        setRefusedTap({
            key: matchKey(m), compId: m.compId, why, against: matchKey(blocker), label: scoreRowMatchName(blocker),
            // The start (or pick) record itself, which startMatch marks `landed` once the
            // start went out: the effect beside startMatch asks it before writing the
            // past-tense sentence.
            start: why === "starting" ? startOut : null,
        });
        if (showToast) showToast(refusalText(why, blocker), "error");
    };
    // The stored refusal is read against the refused match's LIVE row in the
    // court's list (the same list the court-call withdrawal reads), never against
    // a snapshot: the notice shows only while that row is still scheduled, so it
    // cannot outlive the match for the render before the effect below drops it.
    const startErrorKey = startError ? startError.key : null;
    const startErrorRow = startErrorKey ? courtMatchesRaw.find((x) => matchKey(x) === startErrorKey) : null;
    const startErrorStatus = startErrorRow ? startErrorRow.status : null;
    // The Start notice a scheduled match's card or queue row shows: a refused
    // tap's derived notice, else the stored Start refusal for that match.
    const startNoticeFor = (key) => refusalNotice(key)
        || (startError && startError.key === key && startErrorStatus === "scheduled" ? startError.msg : null);

    // A stored Start refusal describes ONE match at one moment, so it is judged
    // against that match's live row and nothing else (startErrorStatus above;
    // start_match.jsx startRefusalStands owns the rule, and the Scores tab asks
    // the same one, admin_schedule_score_editor.jsx). It is
    // dropped when the match leaves scheduled (started here or elsewhere, closed
    // by a decision), or when the court's list is non-empty and no longer holds
    // it (the match moved). Up next changing by itself drops nothing: a refusal
    // for a match picked from further down the queue stays on its row, and is
    // there on the card if the match later reaches Up next. A refusal is about
    // one match at one moment, so a stale one never revives when that match is
    // later sent back to the queue: the next tap asks the server again. A start
    // by the operator clears it too (startMatch), and so does an eligibility
    // change in its competition (the competitor_status_updated handler).
    //
    // The list is EMPTY while it loads and across a transient empty refetch, and
    // an empty list says nothing about the match, so the refusal is left where it
    // is then (the court-call withdrawal leaves an absent key alone for the
    // same reason). Only a list that holds matches yet lacks this one counts.
    const startErrorStands = startRefusalStands({ row: startErrorRow, listHoldsMatches: courtMatchesRaw.length > 0 });
    useEffect(() => {
        if (!startErrorKey) return;
        if (!startErrorStands) setStartError(null);
    }, [startErrorKey, startErrorStands, setStartError]);

    // The one place the Starting… state follows the refs: the start in flight,
    // else the pick still deferring the running bout (so a start settling inside
    // a pick does not blank the button the pick is still holding).
    const syncStartingKey = () => {
        const s = startingRef.current || pickingRef.current;
        if (mountedRef.current) setStartingKey(s ? s.key : null);
    };
    // A refusal for a start that was still out ("starting") is derived while that
    // start is out: "X is still being started". When it has landed (or failed),
    // the sentence stops being true, but nothing else would say the match the
    // operator asked for was never started (a 2.7s toast was the only sign,
    // bc-aadv). So it becomes a STORED refusal for the refused match, in the past
    // tense, bound to that match like every stored refusal (the effect beside
    // upNext). The refused match is NOT started on its own: the court now has a
    // running bout, so a second automatic start would draw court_busy or put two
    // bouts on the court unasked, and one start at a time is the rule (fce5866c).
    // The operator starts it, which clears the sentence. The effect re-runs when
    // the start in flight changes (startingKey), and reads the refs for the truth.
    //
    // A start that did NOT land leaves the court free, so the past-tense sentence
    // would be false, and that start's own refusal (a thrown 409, a clock_skew
    // answer or a superseded one that held its result group) is what the
    // operator needs. It is stored for the
    // blocker before this runs (startMatch's refusal branch or its catch, ahead
    // of its finally), and startMatch clears
    // startError when a start passes the guard, so any value present when the
    // blocker ends is the blocker's own: it is kept, and the waiting match shows
    // nothing, as it did before the sentence existed.
    //
    // The sentence also needs a start that WENT OUT. The blocker can be a pick whose
    // deferral of the running bout failed (the revert threw or was unavailable): it
    // never started, nothing was written and the court is as it was, so "was being
    // started" would be false. The start record the refusal holds says whether a start
    // landed (`landed`, set by startMatch, also on the pick's record), not the court's
    // list: the list is refreshed a moment after the write and may still show the match
    // scheduled, and a start queued offline stays scheduled until the connection
    // returns, yet both were sent. Otherwise the refused tap is dropped, with no sentence.
    useEffect(() => {
        if (!refusedTap || refusedTap.why !== "starting") return;
        if (startingRef.current || pickingRef.current) return;
        if (refusedTap.start && refusedTap.start.landed) {
            const blocked = { key: refusedTap.key, compId: refusedTap.compId, msg: startWasBlockedByStartMessage({ label: refusedTap.label }) };
            setStartError((prev) => prev ?? blocked);
        }
        setRefusedTap(null);
    }, [refusedTap, startingKey, setStartError]);
    // The stored Start refusal for match `m` (startError), which
    // startRefusalStands binds to that match: a start's own refusal and a
    // pick's refused start check (bc-stpf) store it alike.
    const refusalFor = (m, msg) => ({ key: matchKey(m), compId: m.compId, msg });
    // Returns true when the start went out (landed, or was queued offline), false
    // otherwise: pickMatch relies on this so it only pins pickedKey for a match that
    // actually started (a blocked-by-eligibility start, a clock_skew refusal or a
    // superseded answer that held the result group must not steal the panel; a
    // superseded answer that held nothing is an echo of a start that landed and
    // does pin it).
    // `pick` is the pickingRef entry pickMatch made for THIS start, if it is one:
    // that pick is the start itself, not a start in flight against it.
    const startMatch = async (m, { pick = null, after = null } = {}) => {
        const key = matchKey(m);
        // bc-evts: the start is the outbox's (a running score write), answered
        // pending at once; a refusal arrives later, through `settled`. It is
        // stored on the refused match and toasted once, here, as a refusal the
        // start came back with always was, and claimed so the queue's own
        // alert does not toast it a second time.
        //
        // The pending answer pins the panel on the started match at once
        // (`startedFrom`, `pickedKey`, below); a start refused AFTER that takes
        // its pin back, so the console shows what the server holds and a tap
        // is not aimed at a match that never started (Z1). `refused` also
        // covers a refusal that comes before the pin is set.
        let refused = false;
        const unpin = () => {
            refused = true;
            if (pick) pick.refused = true;
            if (!mountedRef.current) return;
            setStartedFrom((s) => (s && s.key === key ? null : s));
            setPickedKey((k) => (k === key ? null : k));
        };
        const refusedLater = (msg) => {
            unpin();
            if (mountedRef.current) setStartError(refusalFor(m, msg));
            if (showToast) showToast(msg, "error");
        };
        // Claimed only when it was said, on the row or by the (app-wide) toast (R2).
        const onRefusal = (e) => { refusedLater(startFailureMessage(e)); return mountedRef.current || !!showToast; };
        const inFlight = startingRef.current || (pickingRef.current && pickingRef.current !== pick ? pickingRef.current : null);
        if (inFlight) {
            // One start at a time. An advance (Finish + Start Next, or the one
            // after a decision) that asks for another match while a start is out
            // is refused, and the refusal says so where the refused match is
            // shown (refuseTap: its Up next card or its queue row) and toasts,
            // rather than doing nothing (bc-aadv). A repeat for the match already
            // being started is silent: it is on its way.
            if (inFlight.key !== key && mountedRef.current) refuseTap(m, "starting");
            return false;
        }
        setStartError(null);
        const started = { key, match: m };
        startingRef.current = started;
        setStartingKey(key);
        try {
            // Starting makes the match running; until a refetch shows it so,
            // the start pin below makes it the court's live bout (liveHere).
            // A pick's start follows its send back of the running bout (`after`,
            // request-only: never sent, deferAndStart).
            const res = await onEditScore(m.compId, m.id, after ? { ...startPatch(), after } : startPatch(), m, { onRefusal });
            // A clock_skew answer, or a superseded one that held the result
            // group, means the server stored NOTHING of the start and, unlike a
            // queued start, nothing will land later, so returning true here would
            // pin the panel on a match that never started while the tap looked
            // like it worked. Found in browser verification: this card button is
            // a start path none of the review sweeps enumerated (it is not one of
            // the editor call sites). (A superseded answer that held NOTHING is an
            // echo of a start that already landed: classifyStartOutcome answers
            // ok for it and it takes the landed path below.)
            // Both are a REFUSED start (start_match.jsx classifyStartOutcome): the
            // sentence is stored on the refused match and toasted once, and
            // `landed` stays unset, so a refusal that waited on this start is not
            // worded as "was being started". The sentences differ in their advice
            // (after a clock refusal a SECOND tap normally succeeds; after a
            // held-group superseded one the operator checks the match first), which is why
            // the toast and the card carry the sentence and not a bare failure.
            //
            // For a PICK the court is not "as it was": the running bout was
            // already sent back to the queue (deferAndStart), and this refusal
            // leaves the court idle with that bout waiting in the queue.
            const outcome = classifyStartOutcome(res);
            if (!outcome.ok) {
                if (mountedRef.current) setStartError(refusalFor(m, outcome.msg));
                if (showToast) showToast(outcome.msg, "error");
                return false;
            }
            // The start went out. A refusal that waited on it (or on the pick that
            // made it) reads this, whichever record it holds.
            started.landed = true;
            if (pick) pick.landed = true;
            // The started match is the court's live bout from here, before any
            // push or refetch shows it running (bc-strt; X3: an advance after a
            // finish or a decision too, not only a pick): the editor treats this
            // snapshot as running, so a point struck at once saves at once. A
            // start that answered direct came back after its refresh, which
            // already shows it; a pick pins itself (deferAndStart).
            if (!refused && mountedRef.current && (pick || writePending(res))) {
                setStartedFrom({ key, at: m.modifiedAt });
                if (!pick) setPickedKey(key);
            }
            if (writePending(res)) {
                res.settled.then((body) => {
                    // A discarded start (the operator took it off the held-writes
                    // list) reads as ok, but the server never started the match.
                    if (body && body.discarded) { unpin(); return; }
                    const late = classifyStartOutcome(body);
                    if (!late.ok) refusedLater(late.msg);
                }, () => {});
            }
            return true;
        } catch (e) {
            const msg = startFailureMessage(e);
            if (mountedRef.current) setStartError(refusalFor(m, msg));
            // A thrown refusal onEditScore (editMatchScore, admin.jsx) already
            // toasted is not toasted again: the toast is a single slot, so a
            // second one only restarts its timer. The card or row says it either way.
            if (showToast && !wasToasted(e)) showToast(msg, "error");
            return false;
        } finally {
            // Only the start that holds the ref clears it; the state follows the refs.
            if (startingRef.current && startingRef.current.key === key) startingRef.current = null;
            syncStartingKey();
        }
    };

    // pickMatch: run an upcoming match out of order. Rules:
    //   • Completed matches are never picked here — correcting them is a
    //     separate deliberate action (see correctMatch), so this returns early.
    //   • If a DIFFERENT bout is running, defer it: send it back to the queue
    //     so the court is never left with two running bouts. It keeps its
    //     score (operator ruling 2026-09-26, bc-sbq: switch, keeping the
    //     score), so a mistaken switch is undone with one tap on its Start,
    //     and it runs after the picked one.
    //   • A scheduled pick is started (through the eligibility gate); a pick
    //     that's already running just takes the panel.
    //   • While a correction is open nothing starts (operator ruling
    //     2026-09-27, bc-crpn): the tap is refused with a notice, BEFORE the
    //     defer above, so a refused start sends nothing back to the queue.
    //   • One start at a time (bc-aadv): while another match's start is still
    //     out, a Start of a different scheduled match is refused with a notice
    //     on its row, also BEFORE the defer, so the running bout is not sent
    //     back for a start that never happens. A repeat for the match already
    //     being started is silent: it is on its way.
    const pickMatch = async (m, asked = false) => {
        if (!m || m.status === "completed") return;
        if (openCorrection) {
            refuseTap(m, "correcting");
            return;
        }
        const inFlight = startingRef.current || pickingRef.current;
        if (inFlight && m.status === "scheduled") {
            if (inFlight.key !== matchKey(m)) refuseTap(m, "starting");
            return;
        }
        // A pick that puts another match in the editor asks first over its
        // unsaved entries (R1). The court may move while the operator answers
        // (another device finishes the running bout, a start lands), so the
        // pick is judged again from the top on the court as it is then, by
        // the latest render's pickMatch, never on this tap's copy (stage 5
        // review 9, m3).
        if (!asked && selectedMatch && matchKey(selectedMatch) !== matchKey(m) && holdsUnsaved(editorUnsavedRef)) {
            if (!(await leaveEditorConfirmed())) return;
            const latest = latestRef.current;
            const fresh = latest.allMatches.find((x) => matchKey(x) === matchKey(m));
            if (fresh) await latest.pickMatch(fresh, true);
            return;
        }
        setRefusedTap(null);
        // A scheduled pick is a start in flight from here, before the defer below
        // awaits: a second Start tapped during that wait is refused, and does not
        // send the running bout back a second time.
        const pick = m.status === "scheduled" ? { key: matchKey(m), match: m } : null;
        if (pick) {
            pickingRef.current = pick;
            syncStartingKey();
        }
        try {
            await deferAndStart(m, pick);
        } finally {
            if (pick && pickingRef.current === pick) pickingRef.current = null;
            syncStartingKey();
        }
    };

    // A Start match tap: the bounce of the previous Start tap is ignored
    // (startTapRef, bc-sdtp), anything else goes to pickMatch.
    const tapStart = (m, ev) => {
        if (!acceptTap(startTapRef, ev)) return;
        pickMatch(m);
    };

    // bc-stpf (operator ruling 2026-10-12, option A): before a pick sends the
    // running bout `cur` back, ask the server whether the picked match `m`
    // would start once it has gone (API.checkStart, at most
    // START_CHECK_TIMEOUT_MS; pickMatch's pickingRef holds the pick, so the
    // button reads "Starting…" and a second Start is refused as in flight
    // meanwhile). True goes on with today's send back and start; false leaves
    // the court as it is. A refusal is stored on the picked match and toasted
    // once, and the running bout keeps running with its score. With no answer,
    // or when the device already knows an answer cannot be trusted (offline,
    // or writes of the pick's competition not sent yet that move a match or a
    // bar, API.startCheckUntrusted), the operator is asked
    // (Q1, 2026-10-27: the court must keep working without the server).
    // Console stubs without the check keep today's path.
    const startMayFollowSendBack = async (m, cur, pick) => {
        const api = window.API;
        if (typeof api.checkStart !== "function") return true;
        const untrusted = typeof api.startCheckUntrusted === "function" && api.startCheckUntrusted(m.compId);
        const answer = untrusted ? { unknown: true } : await api.checkStart(m.compId, m.id, court, password);
        if (!mountedRef.current) return false;
        if (answer && answer.ok) return true;
        if (answer && answer.refused) {
            const msg = startFailureMessage(answer.refused);
            if (pick) pick.refused = true;
            setStartError(refusalFor(m, msg));
            if (showToast) showToast(msg, "error");
            return false;
        }
        return askStartWithoutCheck(cur, m);
    };
    // The defer of the running bout and the start of the picked one, after
    // pickMatch's guards and its pickingRef entry (see pickMatch).
    const deferAndStart = async (m, pick) => {
        // The bout sent back is the court's live bout as this console knows
        // it, its own start the feed does not show yet included (liveHere,
        // bc-sssf).
        let cur = liveHere;
        const isSame = cur && matchKey(cur) === matchKey(m);
        // A pick of the match this console has just started, the feed still
        // reading it scheduled, is a pick of a running bout: it is started
        // once, never a second time.
        const startsPick = m.status === "scheduled" && !isStartedSnapshot(m);
        // The queue key of the send back the start must follow, if any.
        let after = null;
        if (cur && !isSame) {
            // Send the current bout back to the queue, otherwise the pick below
            // would leave the court with two running bouts. Route through the
            // dedicated revert-to-queue endpoint (not a scheduled /score write):
            // it keeps the bout's score server-side AND skips the StartMatchTx
            // eligibility gate, so a stale cross-match ineligibility can't
            // silently 409 the defer. If the revert fails we surface a toast and
            // must NOT start the pick.
            if (!window.API || typeof window.API.revertMatchToQueue !== "function") return;
            // bc-stpf: a start the server would refuse leaves the running bout
            // running, so the check comes before anything is sent back.
            if (startsPick) {
                if (!(await startMayFollowSendBack(m, cur, pick))) return;
                if (!mountedRef.current) return;
                // The check can wait on the operator for as long as they take
                // to answer (askStartWithoutCheck), and the court may move
                // meanwhile (another device finishes the running bout, or
                // starts another): the bout sent back and the match started
                // are the court's as it is now, by the latest render, never
                // this tap's copies, as pickMatch does after its own question.
                // A court that changed is judged again from the top.
                const latest = latestRef.current;
                const fresh = latest.allMatches.find((x) => matchKey(x) === matchKey(m));
                const freshCur = latest.liveHere;
                if (!fresh || fresh.status !== "scheduled" || !freshCur || matchKey(freshCur) !== matchKey(cur)) {
                    if (pick && pickingRef.current === pick) pickingRef.current = null;
                    syncStartingKey();
                    if (fresh) await latest.pickMatch(fresh, true);
                    return;
                }
                cur = freshCur;
                m = fresh;
            }
            if (!mountedRef.current) return;
            // bc-evts (the governing rule): the send back is the outbox's and
            // nothing waits for its answer. The pick's start is queued right
            // after it, naming it (`after`), so the outbox sends the start only
            // once the send back has settled: this device never asks the
            // server to start a bout while its own send back of the running
            // one is still out. A send back the server refuses is said here
            // (claimed, so the queue raises no second alert), and the start
            // then goes and the server judges it.
            const deferFailed = (e) => {
                if (showToast) showToast((e && e.message) || "Could not defer the current bout", "error");
                // Claimed only when said (R2): with no toast it is the outbox's.
                return !!showToast;
            };
            try {
                const sent = await window.API.revertMatchToQueue(cur.compId, cur.id, password, { onRefusal: deferFailed, seen: cur.scoreVersion, match: cur });
                after = writeQueueKey(sent);
                // Shown queued at once, as the Send back confirm's is.
                if (mountedRef.current) markSentBackHere(matchKey(cur), sent);
            } catch (e) {
                deferFailed(e);
                return;
            }
            if (!mountedRef.current) return;
        }
        if (startsPick) {
            // A start that went out pins the panel and its started snapshot
            // itself (startMatch, bc-strt).
            const ok = await startMatch(m, { pick, after });
            // A start refused before this line (pick.refused) left nothing to pin.
            if (!ok || !mountedRef.current || (pick && pick.refused)) return;
        }
        setPickedKey(matchKey(m));
    };

    // correctMatch: open a COMPLETED match to correct it in place, mirroring the
    // Scores page's "Correct" button. The court operator often spots a wrong
    // score after the match closed (frequently once the court is otherwise
    // done), so they must be able to fix it here rather than leaving for the
    // competition admin view. It is REFUSED while a bout is live on this court
    // (operator ruling 2026-09-27, bc-crpn: the console never holds a running
    // match and an open correction together): the tap gets a notice naming
    // the bout, and the operator finishes it or sends it back to the queue
    // first. A correction never defers or reverts the live match. The
    // editor then offers Reopen (kachinuki: back to running, keep the bout log;
    // a withdrawal-decided match: Clear withdrawal and reopen) or a direct
    // score re-write (other formats), same as the Scores page. A reopen makes
    // the match the court's live bout and ends the correction (the effect
    // beside correctingMatch).
    const correctMatch = async (m, asked = false) => {
        if (!m || m.status !== "completed") return;
        if (liveBout) {
            refuseTap(m, "running");
            return;
        }
        // Judged again after the ask on the court as it is then (m3, as pickMatch).
        if (!asked && selectedMatch && matchKey(selectedMatch) !== matchKey(m) && holdsUnsaved(editorUnsavedRef)) {
            if (!(await leaveEditorConfirmed())) return;
            const latest = latestRef.current;
            const fresh = latest.allMatches.find((x) => matchKey(x) === matchKey(m));
            if (fresh) latest.correctMatch(fresh, true);
            return;
        }
        setRefusedTap(null);
        setCorrectingKey(matchKey(m));
    };
    // Cancel the correction without saving and return to the court's ordinary
    // view. Only offered on a COMPLETED correction: once it is reopened it is
    // the live bout, finished via End match / Finish or sent back to the
    // queue like any other.
    const stopCorrecting = async () => {
        if (holdsUnsaved(editorUnsavedRef) && !(await leaveEditorConfirmed())) return;
        setCorrectingKey(null);
    };
    // End the correction of the match keyed `key` only if it is still the one
    // open: a write's answer may arrive after another correction was opened.
    const endCorrectionOf = (key) => { setCorrectingKey((k) => (k === key ? null : k)); };
    React.useLayoutEffect(() => { latestRef.current = { pickMatch, correctMatch, allMatches, liveHere }; });

    return {
        pickingRef, refusalNotice, startNoticeFor, startMatch, tapStart, correctMatch, stopCorrecting,
        endCorrectionOf, pendingStartAsk, answerStartAsk,
    };
}

// The Up next card: the first scheduled match of the selected competition
// that can be fought, with its Start, Enter lineup, Call to court and Move
// down. `starting` is the page's startingKey reading this match; `notice` is
// the card's alert line (startNoticeFor).
export function ShiaijoUpNextCard({ upNext, paceLookup, notice, starting, called, calling, onStart, onEnterLineup, onCall, onMoveDown }) {
    return (
        <div className="shiaijo-upnext">
            <div className="section-title">Up next</div>
            <div className={`shiaijo-upnext__card ${called ? "is-called" : ""}`}>
                <div className="shiaijo-upnext__time">
                    <PaceTime display={paceDisplayOf(paceLookup, upNext.compId, upNext.id)} fallback={upNext.scheduledAt || "-"} /> · {upNext.compName}
                    {shiaijoMatchNo(upNext)}
                    {/* DH-only label: a tiebreaker ("-TB-") is also a rep bout
                        (isSupplementaryBout), but it is NOT a daihyosen, so the "DH"
                        tag gates on isPoolDaihyosenBout. Routing still uses
                        isSupplementaryBout (see editorMatch in admin_shiaijo.jsx). */}
                    {window.isPoolDaihyosenBout && window.isPoolDaihyosenBout(upNext.id) && (
                        <span className="tag-badge" style={{ marginLeft: 6 }}>
                            {window.Term ? React.createElement(window.Term, { name: "daihyosen" }, "DH") : "DH"}
                        </span>
                    )}
                </div>
                <MatchSides m={upNext} large />
                <TieBreakerReason m={upNext} className="shiaijo-upnext__hint" />
                <div className="shiaijo-upnext__actions">
                    {/* Route through pickMatch (not startMatch) so starting the
                        Up Next bout honours the same rules as the Upcoming
                        "Start match" buttons: refused while a correction is open,
                        and a running bout is sent back to the queue first,
                        keeping its score (bc-sbq). With neither, it simply
                        starts this one. */}
                    <button type="button" className="btn btn--primary" disabled={starting} onClick={onStart}>
                        {starting ? "Starting…" : "Start match"}
                    </button>
                    {isTeamMatch(upNext) && (
                        <button type="button" className="btn btn--sm" onClick={onEnterLineup}
                            title="Set the team lineup before starting">
                            Enter lineup
                        </button>
                    )}
                    {/* Optional: announce the call to spectators/competitors.
                        Never required: Start match works on its own. */}
                    {onCall && (
                        <button type="button"
                            className="btn btn--sm"
                            disabled={calling}
                            onClick={onCall}
                            title="Announce this match to spectators and competitors"
                        >
                            {Icon && <Icon name="megaphone" />}{" "}
                            {calling ? "Calling…" : (called ? "Call again" : "Call to court")}
                        </button>
                    )}
                    {onMoveDown && (
                        <button type="button" className="btn btn--sm btn--ghost" aria-label="Move down" onClick={onMoveDown} title="Move this match later in the queue">↓</button>
                    )}
                </div>
                {notice && <div className="shiaijo-upnext__error" role="alert">{notice}</div>}
                <div className="shiaijo-upnext__hint">
                    {called
                        ? "Announced to spectators. Start the match when both are at the line."
                        : "Start when both competitors are at the line. Call to court announces the match to spectators (optional)."}
                </div>
            </div>
        </div>
    );
}
