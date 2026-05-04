const API = {
    async fetchTournament() {
        const res = await fetch('/api/viewer/tournament');
        return res.json();
    },
    async fetchCompetitions() {
        const res = await fetch('/api/viewer/competitions');
        return res.json();
    },
    async fetchCompetitionDetails(id) {
        const res = await fetch(`/api/viewer/competitions/${id}`);
        return res.json();
    },
    async updateTournament(config, password) {
        const res = await fetch('/api/tournament', {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify(config)
        });
        return res.json();
    },
    async updateCompetition(id, config, password) {
        const res = await fetch(`/api/competitions/${id}`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify(config)
        });
        return res.json();
    },
    async createCompetition(config, password) {
        const res = await fetch('/api/competitions', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify(config)
        });
        return res.json();
    },
    async startCompetition(id, password) {
        const res = await fetch(`/api/competitions/${id}/start`, {
            method: 'POST',
            headers: {
                'X-Tournament-Password': password
            }
        });
        return res.ok;
    },
    subscribeToEvents(callback) {
        const source = new EventSource('/api/events');
        source.onmessage = (event) => {
            try {
                const data = JSON.parse(event.data);
                callback(data);
            } catch (err) {
                console.error("Error parsing SSE event:", err);
            }
        };
        source.onerror = (err) => {
            console.error("SSE connection error:", err);
            source.close();
            // Reconnect after 5 seconds
            setTimeout(() => this.subscribeToEvents(callback), 5000);
        };
        return () => source.close();
    },
    async recordScore(compID, matchID, result, password) {
        const res = await fetch(`/api/competitions/${compID}/matches/${matchID}/score`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify(result)
        });
        return res.json();
    }
};

window.API = API;
