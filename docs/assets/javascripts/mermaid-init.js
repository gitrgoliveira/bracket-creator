// Render Mermaid diagrams in the MkDocs Material site.
//
// Material 8.5's built-in Mermaid loader does not fire reliably from the
// superfences custom fence, so we load Mermaid explicitly (extra_javascript in
// mkdocs.yaml) and run it here.
//
// IMPORTANT: pymdownx superfences emits `<pre class="mermaid"><code>…</code></pre>`.
// Vanilla mermaid.run() chokes on that nested `<code>` (every diagram fails with
// "Syntax error in text"). We unwrap it first — replacing the host's content with
// the `<code>`'s decoded text — so each `.mermaid` holds the raw diagram source,
// the structure mermaid expects.
//
// `navigation.instant` is enabled (XHR page swaps, no full reload), so we re-run
// on Material's `document$` observable — it emits on first load AND every instant
// navigation — rather than once on DOMContentLoaded, else diagrams are blank after
// an in-site link click. We only process not-yet-rendered nodes to avoid
// re-running mermaid over already-drawn SVGs (which throws during page swaps).
(function () {
  function render() {
    if (!window.mermaid) return;
    var pending = Array.prototype.slice
      .call(document.querySelectorAll(".mermaid"))
      .filter(function (el) {
        return !el.querySelector("svg") && el.dataset.processed !== "true";
      });
    if (!pending.length) return;
    pending.forEach(function (el) {
      var code = el.querySelector("code");
      if (code) el.textContent = code.textContent; // unwrap <code> → raw source
    });
    window.mermaid.initialize({ startOnLoad: false, securityLevel: "loose" });
    try {
      window.mermaid.run({ nodes: pending });
    } catch (e) {
      // A single bad diagram shouldn't blank the page; mermaid logs its own error.
      console.error("mermaid.run failed:", e);
    }
  }
  if (window.document$ && typeof window.document$.subscribe === "function") {
    window.document$.subscribe(render); // Material instant-nav aware
  } else {
    document.addEventListener("DOMContentLoaded", render);
  }
})();
