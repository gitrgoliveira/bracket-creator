// The court console's match moves (admin_shiaijo.jsx), as a hook: Send back
// to queue, a court move and a reorder within the queue, with the confirms
// they open. The console's three confirms (the court move, Send back to
// queue, and the pick's question from the start flow, bc-stpf) are one set of
// dialogs: one tap guard, one Confirm bounce guard, one Escape and one focus
// rule serve them all, so they live here, and the start hook hands this one
// its question (pendingStartAsk, answerStartAsk).
//
// Called after useCourtStart and useCourtCalls, so functions flow one way: it
// reads the pick in flight (pickingRef) from the start hook and takes a court
// call down (withdrawCall) through the calls hook.

import { useOpenedTapGuard, acceptTap } from './tap_guard.jsx';
import { useDialogFocus } from './dialog_focus.jsx';
import { courtChangeWhileStartingMessage } from './write_result.jsx';
import { scoreRowMatchName } from './pool_ids.jsx';
import { holdsUnsaved, discardUnsaved, UNSAVED_ENTRIES_DISCARDED } from './unsaved_entries.jsx';
import { confirmMatchLabel, SCORE_KEPT_SENTENCE } from './court_move.jsx';
import { matchKey } from './admin_shiaijo_feed.jsx';
// A tab that does not run the shiaijo moves nothing (bc-sseq FR9), and a
// confirm left open when the control went elsewhere sends nothing.
import { refusedNotHeld } from './admin_shiaijo_control.jsx';

const { useState, useRef } = React;

export function useCourtMoves({
    court, password, showToast, onMoveCourt, mountedRef, editorUnsavedRef, selectedMatch, sorted,
    filteredScheduled, pickingRef, markSentBackHere, setPickedKey, setCorrectingKey, withdrawCall,
    pendingStartAsk, answerStartAsk, controlRef,
}) {
    // Pending court reassignment, awaiting operator confirmation. Moving a match
    // off this shiaijo is disruptive (it leaves the court and joins another's
    // queue), so it's gated behind a confirm step. {compId, matchId, to, label, from}.
    const [pendingMove, setPendingMove] = useState(null);
    const [movingCourt, setMovingCourt] = useState(false);
    // Pending revert-to-queue confirmation. Set when the operator clicks
    // "Send back to queue" on a running bout. Cleared on confirm or cancel.
    // Shape: {compId, matchId, label}.
    const [pendingRevert, setPendingRevert] = useState(null);
    // bc-cfbd: the confirms below open from a tap; the bounce of that tap
    // must not land on the fresh backdrop or its buttons. One ref serves all
    // (only one is ever open).
    const { openedRef: confirmOpenedRef, onClickCapture: confirmOnClickCapture } = useOpenedTapGuard();
    // The Confirm button of each confirm: its bounce is not a second confirm
    // (acceptTap in the handlers below). One ref serves all, as above.
    const confirmTapRef = useRef(null);
    const [reverting, setReverting] = useState(false);
    // The confirms are this page's own dialogs, so they do what DialogHost does
    // for confirmDialog: focus moves into the open one (its first button, Cancel),
    // Escape cancels it while its request is not out, and focus goes back to the
    // control that opened it once it closes, without scrolling the page.
    const confirmOpen = Boolean(pendingMove || pendingRevert || pendingStartAsk);
    const confirmBoxRef = useRef(null);
    // Escape on the pick's question (bc-stpf) is "keep it running".
    window.useEscapeToClose(confirmOpen && !movingCourt && !reverting
        ? () => { setPendingMove(null); setPendingRevert(null); if (pendingStartAsk) answerStartAsk(false); }
        : undefined);
    useDialogFocus(confirmBoxRef, confirmOpen);

    // The editor shows the match a host confirm drops (Send back to queue, a
    // court move) and holds unsaved entries: the confirm says they are
    // discarded, and drops them on confirm (m1).
    const editorUnsavedFor = (compId, matchId) => !!selectedMatch && selectedMatch.compId === compId
        && selectedMatch.id === matchId && holdsUnsaved(editorUnsavedRef);

    // moveMatch: reorder by swapping scheduledAt with the adjacent row.
    // direction: "up" (earlier) or "down" (later). Only scheduled matches
    // can be reordered; running/completed are stable.
    //
    // The swap is two separate updateMatchTime PUTs (no server-side swap API),
    // so it is NOT atomic. Each is an entry of its match's outbox, handed over at
    // once (bc-evts M5), so nothing here waits; when the server refuses one, the
    // other match is put back to its original time, so a partial failure doesn't
    // leave both rows with the same/incorrect time. Times are sent as strings (""
    // for an unset time): never null, which the Go handler's `scheduledAt string`
    // rejects (400).
    const moveMatch = async (m, direction) => {
        if (!window.API || typeof window.API.updateMatchTime !== "function") return;
        if (m.status === "running" || m.status === "completed") return;
        if (refusedNotHeld(controlRef)) return;
        // Reorder WITHIN the filtered (selected-competition) queue: the same list
        // the rows render and compute first/last against. Swapping against the full
        // court list would exchange ScheduledAt with a hidden match from another
        // competition, silently reordering a queue the operator can't even see.
        const idx = filteredScheduled.findIndex((x) => matchKey(x) === matchKey(m));
        if (idx < 0) return;
        const neighbour = direction === "up" ? filteredScheduled[idx - 1] : filteredScheduled[idx + 1];
        if (!neighbour) return; // already first/last
        const label = (m.sideB && m.sideB.name) || (m.sideA && m.sideA.name) || "Match";
        const myTime = m.scheduledAt || "";
        const neighbourTime = neighbour.scheduledAt || "";
        // Says a refusal, and answers whether it was said (R2: an unsaid one is
        // left for the outbox's alert).
        const failed = (e) => {
            if (showToast) showToast((e && e.message) || "Could not reorder the match", "error");
            return !!showToast;
        };
        // A refusal of either half puts the OTHER match back to its own time, so
        // the queue isn't left half-swapped (a put-back the server refuses too
        // is toasted like any refusal).
        // Both halves show at once and a refused one shows its old time again
        // (`was`, R4); the put-back of the other half does the same.
        const putBack = (match, time, shown) => {
            Promise.resolve(window.API.updateMatchTime(match.compId, match.id, time, password, {
                onRefusal: (e) => failed(e), was: { scheduledAt: shown },
            })).catch(failed);
        };
        try {
            await window.API.updateMatchTime(m.compId, m.id, neighbourTime, password, {
                onRefusal: (e) => { putBack(neighbour, neighbourTime, myTime); return failed(e); },
                was: { scheduledAt: myTime },
            });
            await window.API.updateMatchTime(neighbour.compId, neighbour.id, myTime, password, {
                onRefusal: (e) => { putBack(m, myTime, neighbourTime); return failed(e); },
                was: { scheduledAt: neighbourTime },
            });
            if (showToast) showToast(`Moved ${label} ${direction === "up" ? "earlier" : "later"} in the queue`);
        } catch (e) {
            failed(e);
        }
    };

    // Revert a running bout back to the queue. Gated behind a confirm dialog
    // (same pattern as court reassignment) because discarding the running state
    // is disruptive: viewers drop the match from "Now" and it reappears in the
    // upcoming list. Always offered on a running bout, and the match keeps its
    // score (operator ruling 2026-09-26, bc-sbq): starting it again carries on
    // from it, and the operator removes a wrong mark themselves.
    // A pick is checking the start of another match (bc-stpf): it holds the
    // running bout it may send back, so a Send back or a court move tapped
    // now would race it. The tap is refused with a toast naming the pick.
    const refusedWhilePicking = () => {
        const pick = pickingRef.current;
        if (!pick) return false;
        if (showToast) showToast(courtChangeWhileStartingMessage({ label: scoreRowMatchName(pick.match) }), "error");
        return true;
    };
    const requestRevert = (m) => {
        if (refusedNotHeld(controlRef)) return;
        if (refusedWhilePicking()) return;
        // The editor shows this match: its unsaved entries go with the send
        // back, and the confirm says so (R1, the editor's local Mark draw or
        // hantei pick of a running match included).
        const unsaved = editorUnsavedFor(m.compId, m.id);
        setPendingRevert({ compId: m.compId, matchId: m.id, match: m, label: confirmMatchLabel(m), seen: m.scoreVersion, at: m.modifiedAt, key: matchKey(m), unsaved });
    };
    const confirmRevert = async (ev) => {
        if (!pendingRevert || reverting) return;
        if (!window.API || typeof window.API.revertMatchToQueue !== "function") return;
        // A bounce of the tap that turned this confirm into the warned one must
        // not confirm the warned version the operator has not read yet.
        if (!acceptTap(confirmTapRef, ev)) return;
        if (refusedNotHeld(controlRef)) { setPendingRevert(null); return; }
        const { compId, matchId, label } = pendingRevert;
        const heldNow = editorUnsavedFor(compId, matchId);
        // The confirm's wording was decided when it opened. Entries made while
        // it was open (a scoring key reaching a console whose focus fell to
        // the body) were never named in it: show the confirm again WITH the
        // warning instead of dropping them silently (R1, stage 5 review 11,
        // minor 3).
        if (heldNow && !pendingRevert.unsaved) { setPendingRevert({ ...pendingRevert, unsaved: true }); return; }
        // The confirm said the editor's unsaved entries are discarded: they go
        // now, as Discard entries does everywhere (m1), not once the feed
        // shows the match queued.
        if (heldNow) discardUnsaved(editorUnsavedRef);
        setReverting(true);
        try {
            // bc-evts: the outbox's, never awaited: the console moves on at
            // once, and a refusal the server gives later is toasted.
            const res = await window.API.revertMatchToQueue(compId, matchId, password, {
                seen: pendingRevert.seen,
                match: pendingRevert.match,
                onRefusal: (e) => {
                    if (!mountedRef.current || !showToast) return false;
                    showToast((e && e.message) || "Could not send match back to queue", "error");
                    return true;
                },
            });
            if (!mountedRef.current) return;
            // Queued (offline included): the bout leaves the live slot and the
            // editor at once and shows in the queue, as a finish shows finished
            // (withOwnStatusMove); a refused or discarded send back puts it back.
            markSentBackHere(pendingRevert.key, res);
            if (showToast) showToast(`${label} sent back to queue`);
            setPendingRevert(null);
            setPickedKey(null);
            // Release a correction pin too. A reopened correction hands its key
            // to pickedKey (cleared above) once the refetch shows it running, but
            // one sent back before that refetch landed would otherwise re-pin the
            // panel to a now-scheduled match with no exit. Harmless (no-op) when
            // reverting a plain running bout.
            setCorrectingKey(null);
        } catch (e) {
            if (mountedRef.current) {
                if (showToast) showToast((e && e.message) || "Could not send match back to queue", "error");
            }
        } finally {
            if (mountedRef.current) setReverting(false);
        }
    };

    // Court reassignment is gated behind a confirm: a CourtPicker change opens a
    // confirmation rather than moving immediately, because the match leaves this
    // shiaijo for another court's queue. `onMoveCourt` (the real move) only runs
    // once the operator confirms.
    const requestMoveCourt = (compId, matchId, toCourt) => {
        if (refusedNotHeld(controlRef)) return;
        if (refusedWhilePicking()) return;
        const mm = sorted.find((x) => x.compId === compId && x.id === matchId);
        setPendingMove({
            compId, matchId, to: toCourt,
            shown: mm,
            label: mm ? confirmMatchLabel(mm) : "this match",
            from: (mm && mm.court) || court,
            unsaved: editorUnsavedFor(compId, matchId),
        });
    };
    const confirmMoveCourt = async (ev) => {
        if (!pendingMove || movingCourt) return;
        // The bounce guard of confirmRevert, for the court move's Confirm.
        if (!acceptTap(confirmTapRef, ev)) return;
        if (refusedNotHeld(controlRef)) { setPendingMove(null); return; }
        const { compId, matchId, to } = pendingMove;
        const heldNow = editorUnsavedFor(compId, matchId);
        // Entries made while a confirm without the warning was open are asked
        // about, never dropped silently (see confirmRevert).
        if (heldNow && !pendingMove.unsaved) { setPendingMove({ ...pendingMove, unsaved: true }); return; }
        if (heldNow) discardUnsaved(editorUnsavedRef);
        setMovingCourt(true);
        try {
            // The host hands the move to the outbox and answers at once with
            // `{ landed }`, a promise of whether the move landed (the host
            // reports its own failure); an older host answers the boolean. A
            // move that did not land leaves this console's call for the match
            // up. The call names the court the match is leaving, so it comes
            // down once the move has landed (bc-cdbl).
            const handed = await onMoveCourt(compId, matchId, to, { confirmed: true, shown: pendingMove.shown });
            const withdraw = () => withdrawCall(matchKey({ compId, id: matchId }));
            if (handed === true) withdraw();
            else if (handed && handed.landed && typeof handed.landed.then === "function") {
                handed.landed.then((ok) => { if (ok === true) withdraw(); });
            }
            if (mountedRef.current) setPendingMove(null);
        } finally {
            if (mountedRef.current) setMovingCourt(false);
        }
    };

    return {
        moveMatch, requestRevert, requestMoveCourt,
        confirms: {
            pendingMove, setPendingMove, movingCourt, confirmMoveCourt,
            pendingRevert, setPendingRevert, reverting, confirmRevert,
            pendingStartAsk, answerStartAsk,
            confirmOpenedRef, confirmOnClickCapture, confirmBoxRef, confirmTapRef,
        },
    };
}

// The console's confirms, drawn from what useCourtMoves returns as `confirms`.
export function ShiaijoConfirms({
    pendingMove, setPendingMove, movingCourt, confirmMoveCourt,
    pendingRevert, setPendingRevert, reverting, confirmRevert,
    pendingStartAsk, answerStartAsk,
    confirmOpenedRef, confirmOnClickCapture, confirmBoxRef, confirmTapRef,
}) {
    return (
        <>
            {pendingMove && (
                <div className="modal-backdrop" ref={confirmOpenedRef} onClickCapture={confirmOnClickCapture}
                    onClick={() => !movingCourt && setPendingMove(null)}>
                    <div className="shiaijo-move-confirm" role="dialog" aria-modal="true" ref={confirmBoxRef}
                        aria-labelledby="shiaijo-move-title" onClick={(e) => e.stopPropagation()}>
                        <h3 id="shiaijo-move-title" className="shiaijo-move-confirm__title">
                            Move to Shiaijo {pendingMove.to}?
                        </h3>
                        <p className="shiaijo-move-confirm__body" aria-live="polite">
                            {/* Only a waiting row offers a move here (ShiaijoQueueRow's
                                showActions), and a waiting match goes last there. */}
                            <strong>{pendingMove.label}</strong> leaves Shiaijo {pendingMove.from} and goes
                            last in the queue on Shiaijo {pendingMove.to}.
                            {pendingMove.unsaved ? ` ${UNSAVED_ENTRIES_DISCARDED}` : ""}
                        </p>
                        <div className="shiaijo-move-confirm__actions">
                            <button type="button" className="btn" onClick={() => setPendingMove(null)} disabled={movingCourt}>
                                Cancel
                            </button>
                            <button type="button" className="btn btn--primary" onClick={confirmMoveCourt} disabled={movingCourt}>
                                {movingCourt ? "Moving…" : `Move to Shiaijo ${pendingMove.to}`}
                            </button>
                        </div>
                    </div>
                </div>
            )}

            {pendingRevert && (
                <div className="modal-backdrop" ref={confirmOpenedRef} onClickCapture={confirmOnClickCapture}
                    onClick={() => !reverting && setPendingRevert(null)}>
                    <div className="shiaijo-move-confirm" role="dialog" aria-modal="true" ref={confirmBoxRef}
                        aria-labelledby="shiaijo-revert-title" onClick={(e) => e.stopPropagation()}>
                        <h3 id="shiaijo-revert-title" className="shiaijo-move-confirm__title">
                            Send back to queue?
                        </h3>
                        <p className="shiaijo-move-confirm__body" aria-live="polite">
                            <strong>{pendingRevert.label}</strong> will be returned to the upcoming queue.
                            {pendingRevert.unsaved ? ` ${UNSAVED_ENTRIES_DISCARDED}` : ""} {SCORE_KEPT_SENTENCE}
                        </p>
                        <div className="shiaijo-move-confirm__actions">
                            <button type="button" className="btn" onClick={() => setPendingRevert(null)} disabled={reverting}>
                                Cancel
                            </button>
                            <button type="button" className="btn btn--primary" onClick={confirmRevert} disabled={reverting}>
                                {reverting ? "Sending…" : "Send back to queue"}
                            </button>
                        </div>
                    </div>
                </div>
            )}

            {pendingStartAsk && (
                <div className="modal-backdrop" ref={confirmOpenedRef} onClickCapture={confirmOnClickCapture}
                    onClick={() => answerStartAsk(false)}>
                    <div className="shiaijo-move-confirm" role="dialog" aria-modal="true" ref={confirmBoxRef}
                        aria-labelledby="shiaijo-start-ask-title" data-testid="start-ask" onClick={(e) => e.stopPropagation()}>
                        <h3 id="shiaijo-start-ask-title" className="shiaijo-move-confirm__title">
                            Start {pendingStartAsk.pickLabel}?
                        </h3>
                        <p className="shiaijo-move-confirm__body" aria-live="polite">
                            The app could not check with the server that <strong>{pendingStartAsk.pickLabel}</strong> can
                            start. <strong>{pendingStartAsk.curLabel}</strong> is still running: send it back to the
                            queue and start {pendingStartAsk.pickLabel}, or keep it running. {SCORE_KEPT_SENTENCE}
                        </p>
                        <div className="shiaijo-move-confirm__actions">
                            <button type="button" className="btn" onClick={() => answerStartAsk(false)}>
                                Keep it running
                            </button>
                            <button type="button" className="btn btn--primary"
                                onClick={(ev) => { if (acceptTap(confirmTapRef, ev)) answerStartAsk(true); }}>
                                Send back and start
                            </button>
                        </div>
                    </div>
                </div>
            )}
        </>
    );
}
