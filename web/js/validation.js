// Pure-logic validation helpers for the Excel CLI participant list and form.
// All functions here are side-effect free and accept their inputs explicitly,
// so they can be unit-tested without a DOM.

export function escapeHtml(text) {
    return String(text)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#039;');
}

export function getIssueLineNumber(message) {
    const match = String(message).match(/^Line (\d+):/);
    if (!match) {
        return null;
    }
    return parseInt(match[1], 10);
}

export function sanitizeNameForValidation(name) {
    const trimmedName = String(name).trim();
    if (!trimmedName) {
        return '';
    }

    const fullName = trimmedName.split(/\s+/);
    if (fullName.length === 1) {
        return fullName[0].toUpperCase();
    }

    const firstName = fullName[0].toUpperCase();
    const lastName = fullName[fullName.length - 1].toUpperCase();
    return `${firstName[0]}. ${lastName}`;
}

export function normalizeNameForValidation(name) {
    return String(name).trim().replace(/\s+/g, ' ').toLocaleLowerCase();
}

// Pure validation: takes the raw textarea text + zekken flag and returns an
// object describing the participant list state. No DOM access.
export function getParticipantValidationState(playerList, withZekkenName) {
    const lines = String(playerList || '').split('\n');
    const errors = [];
    const warnings = [];
    const infos = [];
    const seenEntries = new Map();
    let participantCount = 0;
    let metadataLineCount = 0;

    lines.forEach((rawLine, index) => {
        const lineNumber = index + 1;
        const trimmedLine = rawLine.trim();
        if (!trimmedLine) {
            return;
        }

        participantCount += 1;
        const columns = rawLine.split(',').map(part => part.trim());
        const name = columns[0] || '';

        if (!name) {
            errors.push(`Line ${lineNumber}: missing participant name in column 1.`);
            return;
        }

        if (withZekkenName) {
            if (columns.length < 3) {
                errors.push(`Line ${lineNumber}: expected format 'Name, ZekkenName, Dojo'.`);
                return;
            }

            if (!columns[2]) {
                errors.push(`Line ${lineNumber}: missing dojo in column 3.`);
                return;
            }

            if (columns.length > 3) {
                metadataLineCount += 1;
            }

            const displayName = columns[1] || sanitizeNameForValidation(name);
            const duplicateKey = `${normalizeNameForValidation(name)}|${displayName.toLocaleLowerCase()}|${columns[2].toLocaleLowerCase()}`;
            if (seenEntries.has(duplicateKey)) {
                errors.push(`Line ${lineNumber}: duplicate participant entry also appears on line ${seenEntries.get(duplicateKey)}.`);
                return;
            }

            seenEntries.set(duplicateKey, lineNumber);
            return;
        }

        const dojo = columns.length >= 2 ? columns[1] : '';
        if (!dojo) {
            warnings.push(`Line ${lineNumber}: dojo is missing in column 2; it will fall back to 'NA'.`);
        }

        if (columns.length > 2) {
            metadataLineCount += 1;
        }

        const duplicateKey = `${normalizeNameForValidation(name)}|${sanitizeNameForValidation(name)}|${(dojo || 'NA').toLocaleLowerCase()}`;
        if (seenEntries.has(duplicateKey)) {
            errors.push(`Line ${lineNumber}: duplicate participant entry also appears on line ${seenEntries.get(duplicateKey)}.`);
            return;
        }

        seenEntries.set(duplicateKey, lineNumber);
    });

    if (metadataLineCount > 0) {
        const limitColumn = withZekkenName ? 3 : 2;
        infos.push(`Extra columns were detected on ${metadataLineCount} line(s). Columns after column ${limitColumn} are treated as metadata.`);
    }

    return {
        isEmpty: String(playerList || '').trim() === '',
        participantCount,
        withZekkenName: !!withZekkenName,
        errors,
        warnings,
        infos
    };
}


// --- Shiaijo-count rule (spec 007 R9) --------------------------------------
//
// MAX_COURTS mirrors helper.MaxCourts (internal/helper/constants.go): Shiaijo
// are labelled A-Z, so 26 is the hard cap on the field itself.
const MAX_COURTS = 26;

// The legal shiaijo allocations for ONE competition: the powers of two that
// fit inside the A-Z label cap. Derived from MAX_COURTS rather than written
// out, so the cap and this list can never disagree -- 32 shiaijo cannot be
// labelled, which is why 16 is the ceiling.
export const VALID_SHIAIJO_COUNTS = (() => {
    const out = [];
    for (let p = 1; p <= MAX_COURTS; p *= 2) out.push(p);
    return out;
})();

// The canonical reason, shared by every surface that states the rule (this
// rejection message and the standing hint on the courts field in index.html).
const SHIAIJO_RULE_REASON = 'the knockout draw gives each shiaijo its own block of the bracket and the blocks merge in pairs, so the count has to halve cleanly';

// "1, 2 or 4" from [1, 2, 4].
export function joinShiaijoCounts(list) {
    if (list.length <= 1) return String(list[0] ?? '');
    return `${list.slice(0, -1).join(', ')} or ${list[list.length - 1]}`;
}

// Shiaijo-count rule for one tournament, mirrored from
// helper.ValidateShiaijoCount (internal/helper/shiaijo_count.go) and worded
// identically to shiaijoCountError (web-mobile/js/admin_helpers.jsx): a
// tournament that builds a knockout bracket runs on 1, 2, 4, 8 or 16 shiaijo.
// Anything else (3, 5, 6, 10, ...) is invalid, because the draw gives each
// shiaijo its own block of the bracket and merges those blocks in PAIRS: the
// count has to halve cleanly all the way down, which only a power of two
// does. 6 halves to 3 and stops.
//
// 1 shiaijo is explicitly VALID (its single block splits into two halves that
// merge like any other pair), so the message always offers 1 and must never
// read as "at least 2 shiaijo".
//
// This form posts to /create, which runs the SAME generator as the CLI and
// enforces the same rule server-side (helper.ValidateShiaijoCount, called
// from cmd/create_handler.go). The server's rejection is a JSON body and this
// is a native <form> POST, so an unvalidated bad count REPLACES the page with
// raw JSON and destroys the pasted participant list, the seeds and every
// other option. Catching it here is what keeps the form on screen.
//
// Pinned against the Go message by web/tests/validation.spec.js, which
// asserts the same fragments as internal/helper/shiaijo_count_test.go and
// web-mobile/js/__tests__/shiaijo_count.test.jsx.
export function shiaijoCountError(n) {
    if (!Number.isFinite(n) || n <= 1) return null;
    if (VALID_SHIAIJO_COUNTS.includes(n)) return null;
    const below = VALID_SHIAIJO_COUNTS.filter((p) => p < n).pop();
    const above = VALID_SHIAIJO_COUNTS.find((p) => p > n);
    // `above` is undefined past the ceiling (17+ shiaijo): there is no higher
    // valid count to offer, so the message names only the one below. `below`
    // is always at least 2 here, because n > 1 and every n <= 2 is valid.
    const options = above ? `${below} or ${above}` : `${below}`;
    return `${n} shiaijo cannot be paired down to a single bracket. Use ${options}, or 1: ${SHIAIJO_RULE_REASON}.`;
}

// Pure validator for the courts (Shiaijo) field: A-Z hard cap at 26, then the
// power-of-two rule above. The cap is checked FIRST so the order matches the
// server's (helper.ValidateCourts then helper.ValidateShiaijoCount), which
// keeps the message an operator sees the same whichever side rejects.
export function validateCourtsValue(rawCourts) {
    const courts = parseInt(rawCourts, 10);
    if (Number.isNaN(courts) || courts < 1 || courts > MAX_COURTS) {
        return { ok: false, error: `Number of Shiaijo (courts) must be between 1 and ${MAX_COURTS}` };
    }
    const ruleError = shiaijoCountError(courts);
    if (ruleError) {
        return { ok: false, error: ruleError };
    }
    return { ok: true, value: courts };
}

// Pure validator for pool settings; called only when tournamentType === 'pools'.
export function validatePoolSettings(winnersPerPool, playersPerPool, isMaxMode) {
    const poolSizeModeLabel = isMaxMode ? 'Maximum players' : 'Minimum players';
    if (!(winnersPerPool > 0)) {
        return { ok: false, error: 'Winners per pool must be at least 1' };
    }
    if (!(playersPerPool > 0)) {
        return { ok: false, error: `${poolSizeModeLabel} per pool must be at least 1` };
    }
    if (winnersPerPool >= playersPerPool) {
        return { ok: false, error: `Winners per pool must be less than ${poolSizeModeLabel.toLowerCase()} per pool` };
    }
    return { ok: true };
}
