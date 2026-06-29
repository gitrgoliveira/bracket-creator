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


// Pure validator for the courts (Shiaijo) field, A-Z hard cap at 26.
export function validateCourtsValue(rawCourts) {
    const courts = parseInt(rawCourts, 10);
    if (Number.isNaN(courts) || courts < 1 || courts > 26) {
        return { ok: false, error: 'Number of Shiaijo (courts) must be between 1 and 26' };
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
