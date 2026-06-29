// AdminExport card extracted from admin_schedule.jsx (mp-d7tl).

export function AdminExport({ c, t }) {
  // The competition detail nests its real fields (roster, format, courts,
  // pool settings) under `c.config`; some other call sites pass an already
  // flattened object. Read from `c.config` when present, else from `c`, so
  // this works in both shapes.
  const cfg = c.config || c;
  const cid = c.id || cfg.id;
  const url = `${(window.linkBase || (() => window.location.origin))(t)}/viewer.html?id=${t.id}#comp-${cid}`;

  const downloadXlsx = async () => {
    try {
      // Generate the workbook through the SAME stateless generator the `web`
      // app uses (POST /create), instead of the engine's stored-pool export.
      // The engine path reloads pools from disk, which severs the player↔match
      // pointer link the renderer needs, so the pool W/L/T/RANK formulas
      // collapse to "=0". Re-deriving from the roster here keeps them live
      // (mp-x0u9). /create is a public, stateless endpoint (no auth header).
      // Swiss has no static-bracket generator; exporting it as a round-robin
      // pool would hand the operator a structurally wrong workbook. Block it
      // with a clear message instead (live results live in the scoring view).
      if (cfg.format === "swiss") {
        throw new Error("Swiss competitions can't be exported to a static Excel bracket: use the live scoring view to track Swiss rounds.");
      }

      const players = cfg.players || [];
      if (players.length < 2) throw new Error("competition has no participants to export");

      const isPlayoffs = cfg.format === "playoffs";
      const singlePool = cfg.format === "league";
      const playersPerPool = singlePool ? players.length : (cfg.poolSize || players.length);

      // /create requires 1 <= winnersPerPool < playersPerPool for pools.
      let winnersPerPool = cfg.poolWinners || 2;
      if (winnersPerPool >= playersPerPool) winnersPerPool = Math.max(1, playersPerPool - 1);

      // RFC-4180-quote any field containing a comma/quote/newline so the Go
      // parser (helper.CreatePlayers) routes the line through encoding/csv
      // instead of a naive strings.Split: otherwise a name or dojo with a
      // comma (e.g. "Smith, Jr") silently corrupts the roster.
      const csvField = (s) =>
        /[",\n]/.test(s) ? '"' + String(s).replace(/"/g, '""') + '"' : s;

      // Roster lines match the canonical participant CSV the generator parses:
      // with zekken → "Name, DisplayName, Dojo"; otherwise → "Name, Dojo".
      const rosterLine = (p) => cfg.withZekkenName
        ? [csvField(p.name), csvField(p.displayName || p.name), csvField(p.dojo || "NA")].join(", ")
        : [csvField(p.name), csvField(p.dojo || "NA")].join(", ");
      const playerList = players.map(rosterLine).join("\n");

      const seeded = players
        .filter((p) => p.seed && p.seed > 0)
        .map((p) => ({ name: p.name, seedRank: p.seed }));

      const body = new URLSearchParams({
        tournamentType: isPlayoffs ? "playoffs" : "pools",
        playerList,
        courts: String((cfg.courts && cfg.courts.length) || 1),
        winnersPerPool: String(winnersPerPool),
        playersPerPool: String(playersPerPool),
        poolSizeMode: cfg.poolSizeMode || "min",
        teamMatches: String(cfg.teamSize || 0),
        titlePrefix: cfg.name || c.name || "",
        numberPrefix: cfg.numberPrefix || "",
        determined: "on", // preserve the registered participant order (no shuffle)
      });
      if (singlePool || cfg.roundRobin) body.set("roundRobin", "on");
      // Honour the competition's pool format: "partial" → path-graph match set
      // (the generator otherwise defaults to full round-robin). Mirrors the
      // engine's PoolFormat switch (internal/engine/pools.go).
      if (cfg.poolFormat === "partial") body.set("poolFormat", "partial");
      if (cfg.withZekkenName) body.set("withZekkenName", "on");
      if (seeded.length) body.set("seeds", JSON.stringify(seeded));

      const resp = await fetch(`/create`, { method: "POST", body });
      if (!resp.ok) {
        let msg = await resp.text();
        try { msg = JSON.parse(msg).error || msg; } catch (_) { /* keep raw text */ }
        throw new Error(msg);
      }
      const blob = await resp.blob();
      const dlUrl = window.URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = dlUrl;
      a.download = `bracket-${cid}.xlsx`;
      document.body.appendChild(a);
      a.click();
      a.remove();
      window.URL.revokeObjectURL(dlUrl);
    } catch (err) {
      alert("Export failed: " + err.message);
    }
  };

  const copyUrl = async () => {
    try {
      await navigator.clipboard.writeText(url);
      alert("URL copied to clipboard!");
    } catch (err) {
      alert("Failed to copy URL: " + (err?.message || err));
    }
  };

  return (
    <div className="row">
      <div className="card">
        <div className="card__title" style={{ marginBottom: 8 }}>Export {c.name}</div>
        <div className="card__sub" style={{ marginBottom: 14 }}>Generate the official Excel workbook used during the day.</div>
        <button type="button" className="btn btn--primary btn--full" onClick={downloadXlsx}>Download .xlsx</button>
        <div className="field__hint" style={{ marginTop: 10 }}>Includes pool draws, pool matches, and elimination brackets with linked formulas.</div>
      </div>
      <div className="card">
        <div className="card__title" style={{ marginBottom: 8 }}>Public viewer link</div>
        <div className="card__sub" style={{ marginBottom: 14 }}>Players & spectators see this competition's bracket, schedule and results.</div>
        <div style={{ display: "flex", gap: 8 }}>
          <input className="input" value={url} readOnly style={{ flex: 1 }} />
          <button type="button" className="btn" onClick={copyUrl}>Copy</button>
        </div>
      </div>
    </div>
  );
}
