// Thin wrappers over the Go server endpoints used by the Excel CLI page.
// Keeping the fetch calls here lets app.js focus on DOM wiring and lets the
// pure-logic modules stay free of network code.

export async function fetchAppStatus() {
    const response = await fetch('/api/status');
    if (!response.ok) {
        throw new Error(`Server returned ${response.status}`);
    }
    return response.json();
}

export async function fetchDownloadStatus(downloadToken) {
    const response = await fetch(
        '/api/download-status?token=' + encodeURIComponent(downloadToken),
        { cache: 'no-store' }
    );
    if (!response.ok) {
        throw new Error(`Server returned ${response.status}`);
    }
    return response.json();
}

export async function parseParticipants(playerList, withZekkenName) {
    const response = await fetch('/api/parse-participants', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
            playerList: playerList,
            withZekkenName: withZekkenName
        })
    });

    if (response.ok) {
        return { ok: true, data: await response.json() };
    }

    let errorMessage = 'Failed to parse participants.';
    try {
        const errorData = await response.json();
        if (errorData && typeof errorData.error === 'string' && errorData.error.trim()) {
            errorMessage = errorData.error;
        }
    } catch (_) {
        // Keep default fallback message when response is not JSON.
    }
    return { ok: false, error: errorMessage };
}
