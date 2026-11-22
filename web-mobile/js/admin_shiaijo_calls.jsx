// The court console's call to court and its withdrawal (admin_shiaijo.jsx),
// as a hook. Call to court announces a match to the spectators and
// competitors; it does NOT start the match. The console keeps the newest call
// it made per match and takes it down once that match leaves scheduled, moves
// to another court or leaves the court's feed (bc-cdbl).
//
// The page keeps callIdsRef and withdrawCallRef and passes them in, because
// its court feed handler, set up before this hook is called, reads them when
// a push names another court: callIdsRef says whether this console holds a
// call for the match, and withdrawCallRef, bound by this hook's layout effect
// to the render's withdrawCall, takes it down. The handler runs on an event,
// after the commit, so the ref is bound by then.

import { serverNowMs } from './server_clock.jsx';
import { acceptTap } from './tap_guard.jsx';
import { announcementExpiredToast } from './write_result.jsx';
import { matchKey } from './admin_shiaijo_feed.jsx';
import { competitionHidesNames } from './competition_shape.jsx';
import { numberOf } from './competitor_identity.jsx';

const { useState, useEffect, useRef } = React;

// How long a court call shows on the public screens, in minutes.
export const CALL_MINUTES = 5;

export function useCourtCalls({
    court, password, showToast, mountedRef, callIdsRef, withdrawCallRef,
    courtComps, courtCompetitions, courtMatchesRaw, tournament,
}) {
    // One timer per called match, for the end of the call kept for it
    // (keepCalled): the card reads as called only while that call is showing.
    const callEndsRef = useRef(new Map());
    useEffect(() => {
        // The Map itself never changes, only what it holds.
        const callEnds = callEndsRef.current;
        return () => {
            callEnds.forEach(clearTimeout);
            callEnds.clear();
        };
    }, []);
    // `calledKeys` is the render's copy of which matches read as called: the
    // keys whose call this console keeps (callIdsRef) still stands, taken
    // again by callsChanged whenever those calls change or one's time runs out
    // (keepCalled, withdrawCall). `callingKey` guards the in-flight announce
    // request. `callTapRef` guards against double taps on Call to court
    // (bc-cdbl).
    const [calledKeys, setCalledKeys] = useState(() => new Set());
    const [callingKey, setCallingKey] = useState(null);
    const callTapRef = useRef(null);

    // takeDown: withdraw the court call `id` (the device made it, so it is known
    // at once, sent or not). Fire and forget: the withdrawal is an outbox entry
    // that goes after the call's own send, and a refusal is told once by the
    // outbox (_tellRefusal), never here. `msg` names the call on a held-writes
    // list. Shared by withdrawCall and the earlier call callToCourt replaces.
    const takeDown = (id, msg) => {
        if (!id || !window.API || typeof window.API.deleteAnnouncement !== "function") return;
        window.API.deleteAnnouncement(id, password, { message: msg });
    };

    // A call stands while its time is not over.
    const callStands = (entry) => !!entry && entry.endsAt > serverNowMs();
    const clearCallEnd = (key) => {
        const t = callEndsRef.current.get(key);
        if (t === undefined) return;
        clearTimeout(t);
        callEndsRef.current.delete(key);
    };
    // isCalled: the match keyed `key` reads as called (the card's "Call again"
    // and its hint, a row's "Call again") exactly while this console keeps a
    // call for it that stands. Each match has its own, so more than one can
    // read as called at once. Render reads the snapshot, never the ref.
    const isCalled = (key) => calledKeys.has(key);
    // callsChanged: take the snapshot again from the calls kept now. The same
    // Set is kept while the keys are the same, so nothing re-renders for it.
    const callsChanged = () => {
        if (!mountedRef.current) return;
        const standing = new Set();
        for (const [k, entry] of callIdsRef.current.entries()) {
            if (callStands(entry)) standing.add(k);
        }
        setCalledKeys((prev) => (prev.size === standing.size && [...standing].every((k) => prev.has(k)) ? prev : standing));
    };
    // keepCalled: run whenever the call kept for the match keyed `key` changes
    // or its time runs out. With that call standing it arms the match's one
    // timer for the call's end, and runs again then. With none standing the
    // match reads as uncalled (it offers Call to court again), and a call the
    // server has answered is dropped: there is nothing left to withdraw. One
    // the server has not answered yet stays kept, so a start still takes it out
    // of the outbox, and it reads as uncalled since it most likely arrives
    // expired; should it land with time left (the server's floor), its answer
    // runs this again with the expiry answered, and it reads as called again.
    const keepCalled = (key) => {
        clearCallEnd(key);
        const entry = callIdsRef.current.get(key);
        if (callStands(entry)) {
            callEndsRef.current.set(key, setTimeout(() => {
                callEndsRef.current.delete(key);
                keepCalled(key);
            }, entry.endsAt - serverNowMs()));
        } else if (entry && entry.answered) {
            callIdsRef.current.delete(key);
        }
        callsChanged();
    };

    // Call to court: optional. Broadcasts a tournament announcement so the
    // competitors (and anyone watching the public app) are notified they're
    // being summoned to this shiaijo. It does NOT start the match; Start is
    // always available on its own.
    const callToCourt = async (m, ev) => {
        // bc-cdbl: double tap guard. acceptTap returns false for a bounce and
        // true otherwise, stamping the tap as it goes.
        if (!acceptTap(callTapRef, ev, matchKey(m))) return;
        if (!window.API || typeof window.API.sendAnnouncement !== "function") return;
        // A competition whose public pages hide names is called by number (the
        // side colour when a side has none yet): the announcement reaches every
        // public screen as typed, so a name here would undo the setting.
        const hides = competitionHidesNames(tournament, (courtCompetitions || []).find((c) => c.id === m.compId));
        const callLabel = (side, colour) => (hides ? numberOf(side) : (side && side.name)) || colour;
        const a = callLabel(m.sideA, "Aka");
        const b = callLabel(m.sideB, "Shiro");
        const msg = `Now calling ${b} and ${a} to Shiaijo ${court}.`.slice(0, 200);
        setCallingKey(matchKey(m));
        // The call already up for this match, if any: { id, msg }. It comes down
        // only once the new call has landed (below), so a send the server refuses
        // leaves it up. The server replaces only an announcement with identical
        // text, so a call whose text changed (a side's name resolved) would
        // otherwise leave the earlier banner up until it expired (bc-cdbl).
        const earlier = callIdsRef.current.get(matchKey(m)) || null;
        // When the call's time is over, as the server judges it: made-at, in
        // its clock frame, plus the minutes the call shows.
        const endsAt = serverNowMs() + CALL_MINUTES * 60000;
        // What stands once this call has not: the earlier call while its own
        // time is not over, otherwise nothing.
        const fallBack = (key, entry) => {
            if (callIdsRef.current.get(key) !== entry) return;
            if (callStands(earlier)) callIdsRef.current.set(key, earlier);
            else callIdsRef.current.delete(key);
            keepCalled(key);
        };
        try {
            // The answer is the outbox's pending one and carries the id the
            // device made, so starting the match can withdraw this call before
            // it has landed (the withdrawal then takes the unsent call out of the
            // outbox). `settled` is the server's word on it.
            const sent = await window.API.sendAnnouncement(msg, CALL_MINUTES, password);
            const key = matchKey(m);
            // Keep the newest call per match, with its text, so starting the match
            // can withdraw it (bc-cdbl). A refused send leaves the earlier call
            // and its entry in place while its time is not over.
            const entry = { compId: m.compId, matchId: m.id, msg, id: sent.id, endsAt, answered: false };
            callIdsRef.current.set(key, entry);
            keepCalled(key);
            sent.settled.then(
                (landed) => {
                    // Its time ran out before it reached the server (held offline
                    // too long): nothing was shown and nothing was replaced, so
                    // the earlier call stays the one kept while it is still
                    // showing, and the "Called" toast below is put right. With
                    // no call standing, the card reads as if no call was made.
                    if (landed && landed.expired) {
                        fallBack(key, entry);
                        if (showToast) showToast(announcementExpiredToast(CALL_MINUTES, "court call"), "error");
                        return;
                    }
                    // Shown: the call reads as called until the expiry the server
                    // answered (a late call shows for what is left of its time).
                    if (callIdsRef.current.get(key) === entry) {
                        entry.answered = true;
                        const until = landed && Date.parse(landed.expiresAt);
                        if (Number.isFinite(until)) entry.endsAt = until;
                        keepCalled(key);
                    }
                    // An identical text already replaced the earlier call on the
                    // server (AnnouncementStore.Add), so a withdrawal of it would
                    // 404; but a call withdrawn before it was sent replaced
                    // nothing, so the earlier one comes down with it.
                    if (earlier && (earlier.msg !== msg || (landed && landed.discarded))) takeDown(earlier.id, earlier.msg);
                },
                () => {
                    // The outbox tells the operator (_tellRefusal); here only the
                    // bookkeeping and the card go back to what stands.
                    fallBack(key, entry);
                },
            );
            if (!mountedRef.current) return;
            if (showToast) showToast(`Called ${b} and ${a} to Shiaijo ${court}`);
        } catch (e) {
            if (showToast) showToast((e && e.message) || "Could not send the call announcement", "error");
        } finally {
            if (mountedRef.current) setCallingKey(null);
        }
    };

    // withdrawCall: take this console's court call for the match keyed `key`
    // back down once that match has left scheduled (bc-cdbl). Fire
    // and forget: the withdrawal is an outbox entry, never allowed to fail the
    // start, and the 5-minute expiry still clears the banner.
    const withdrawCall = (key) => {
        const entry = callIdsRef.current.get(key);
        if (!entry) return;
        callIdsRef.current.delete(key);
        clearCallEnd(key);
        callsChanged();
        takeDown(entry.id, entry.msg);
    };
    React.useLayoutEffect(() => { withdrawCallRef.current = withdrawCall; });

    // A court call comes down once its match has left scheduled, however that
    // happened (started here, started elsewhere, closed by a decision), once the
    // match shows on another court, whatever its status, or once the loaded
    // court feed no longer holds the match (its competition dropped from this
    // court); a key no list holds is left alone until the court feed has loaded.
    useEffect(() => {
        if (callIdsRef.current.size === 0) return;
        const feedMatches = window.tournamentMatches({ competitions: courtCompetitions }) || [];
        for (const [key, entry] of callIdsRef.current.entries()) {
            const m = courtMatchesRaw.find((x) => matchKey(x) === key);
            if (m && m.status !== "scheduled") {
                withdrawCallRef.current(key);
                continue;
            }
            const moved = feedMatches.find((x) => matchKey(x) === key);
            // A court move still queued on this device alone does not withdraw
            // the call, however the feed came by it (a patch or a refetch
            // overlaid with the queue): only the server's court may.
            if (moved && (moved.court || "") !== (court || "")
                && !(window.API && window.API.courtMoveQueued(moved.compId, moved.id))) {
                withdrawCallRef.current(key);
                continue;
            }
            if (!m && !moved && courtComps !== null) {
                if (!(window.API && window.API.courtMoveQueued(entry.compId, entry.matchId))) {
                    withdrawCallRef.current(key);
                }
            }
        }
    }, [courtMatchesRaw, courtCompetitions, courtComps, court, callIdsRef, withdrawCallRef]);

    return { callingKey, isCalled, callToCourt, withdrawCall };
}
