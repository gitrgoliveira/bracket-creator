import { UNSAVED_ENTRIES_DISCARDED } from "./unsaved_entries.jsx";

// confirmMatchLabel: how a confirm names the match it acts on. BOTH
// competitors, never one: "Ito Rin leaves Shiaijo A" read as a person moving,
// not a match. Order mirrors the on-court display (Shiro/sideB vs Aka/sideA).
// Shared by the court move and Send back to queue.
export function confirmMatchLabel(m) {
    const shiro = (m.sideB && m.sideB.name) || "";
    const aka = (m.sideA && m.sideA.name) || "";
    return (shiro && aka) ? `${shiro} vs ${aka}` : (shiro || aka || "this match");
}

export const SCORE_KEPT_SENTENCE = "The score already saved is kept: starting it again carries on from there.";

export function runningCourtMoveText(opts = {}) {
    const { label = "", from, to, unsaved } = opts || {};
    const trimmedFrom = (from != null ? String(from) : "").trim();
    const trimmedTo = (to != null ? String(to) : "").trim();

    const message = [
        trimmedFrom ? `${label} is running on Shiaijo ${trimmedFrom}.` : `${label} is running.`,
        trimmedTo ? `It stops there and goes first in the queue on Shiaijo ${trimmedTo}.` : "It stops there and is taken off its shiaijo.",
        unsaved && UNSAVED_ENTRIES_DISCARDED,
        SCORE_KEPT_SENTENCE,
    ].filter(Boolean).join(" ");

    return {
        title: trimmedTo ? `Move to Shiaijo ${trimmedTo}?` : "Take it off its shiaijo?",
        message,
        confirmLabel: trimmedTo ? `Move to Shiaijo ${trimmedTo}` : "Take off its shiaijo",
        cancelLabel: "Cancel",
    };
}

export async function confirmRunningCourtMove(opts) {
    const { title, message, confirmLabel, cancelLabel } = runningCourtMoveText(opts);
    return Boolean(await window.confirmDialog({ title, message, confirmLabel, cancelLabel }));
}
