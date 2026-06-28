// Render Mermaid diagrams in the MkDocs Material site.
//
// Material 8.5's built-in Mermaid loader does not fire reliably from the
// superfences custom-fence alone, so we load Mermaid explicitly (see
// extra_javascript in mkdocs.yaml) and run it here. The pymdownx superfences
// `mermaid` custom fence emits `<pre class="mermaid">` blocks; mermaid.run()
// turns those into SVG.
//
// `navigation.instant` is enabled, which swaps page content via XHR without a
// full reload — so we re-run on Material's `document$` observable (it emits on
// every page load AND every instant navigation) rather than once on
// DOMContentLoaded, otherwise diagrams would be blank after the first in-site
// link click.
(function () {
  function render() {
    if (!window.mermaid) return;
    window.mermaid.initialize({ startOnLoad: false });
    try {
      window.mermaid.run({ querySelector: ".mermaid" });
    } catch (e) {
      // A single bad diagram shouldn't blank the page; Mermaid logs its own error.
      console.error("mermaid.run failed:", e);
    }
  }
  if (window.document$ && typeof window.document$.subscribe === "function") {
    window.document$.subscribe(render); // Material instant-nav aware
  } else {
    document.addEventListener("DOMContentLoaded", render);
  }
})();
