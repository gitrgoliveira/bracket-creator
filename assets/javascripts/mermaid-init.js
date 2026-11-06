// Render Mermaid diagrams in the MkDocs Material site.
//
// Material 8.5's built-in Mermaid loader does not fire reliably from the
// superfences custom fence; we load Mermaid explicitly (extra_javascript in
// mkdocs.yaml) and run it here.
//
// IMPORTANT: pymdownx superfences emits `<pre class="mermaid"><code>…</code></pre>`.
// Vanilla mermaid.run() chokes on that nested `<code>` (every diagram fails with
// "Syntax error in text"). We unwrap it first; replacing the host's content with
// the `<code>`'s decoded text; so each `.mermaid` holds the raw diagram source,
// the structure mermaid expects.
//
// `navigation.instant` is enabled (XHR page swaps, no full reload); we re-run
// on Material's `document$` observable. It emits on first load AND every instant
// navigation, rather than once on DOMContentLoaded, else diagrams are blank after
// an in-site link click. We only process not-yet-rendered nodes to avoid
// re-running mermaid over already-drawn SVGs (which throws during page swaps).
(function () {
  function render() {
    if (!window.mermaid) return;
    // Our own "attempted" marker; NOT mermaid's `data-processed`. mermaid.run()
    // is async and skips any node already flagged `data-processed` (even an
    // explicit `nodes` list), so touching that attribute ourselves would race its
    // per-node rendering and blank later diagrams. A separate attribute lets us
    // stop retrying a failed diagram (no <svg>) on every Material instant-nav swap
    // without interfering with mermaid's own bookkeeping.
    var pending = Array.prototype.slice
      .call(document.querySelectorAll(".mermaid"))
      .filter(function (el) {
        return !el.querySelector("svg") && el.dataset.mermaidAttempted !== "1";
      });
    if (!pending.length) return;
    pending.forEach(function (el) {
      var code = el.querySelector("code");
      if (code) el.textContent = code.textContent; // unwrap <code> → raw source
      el.dataset.mermaidAttempted = "1";
    });
    window.mermaid.initialize({ startOnLoad: false, securityLevel: "antiscript" });
    try {
      window.mermaid.run({ nodes: pending });
    } catch (e) {
      // A single bad diagram should not blank the page; mermaid logs its own error.
      console.error("mermaid.run failed:", e);
    }
  }
  if (window.document$ && typeof window.document$.subscribe === "function") {
    window.document$.subscribe(render); // Material instant-nav aware
  } else {
    document.addEventListener("DOMContentLoaded", render);
  }
})();
