---
description: "Use when planning or editing the Web UI (especially web/index.html), including CSV input UX, validation messaging, and client-side interaction flows."
name: "Web UI Workflow Rules"
applyTo: "web/index.html"
---
# Web UI Workflow Rules

- Keep the participant input model headerless unless explicitly requested otherwise.
- Keep `Dojo` terminology in UI copy unless explicitly requested otherwise.
- Do not add downloadable CSV templates unless explicitly requested.
- For metadata-column notices, prefer a single aggregated informational message instead of repeated per-line warnings.

## Validation requirement (mandatory)

When Web UI changes are made:

1. Run the app with `make run` (or `PORT=<port> make run` if 8080 is occupied).
2. Validate in the running UI using Playwright interactions, including at least one click flow.
3. Confirm the updated behavior in the live page, not only via static file review.
4. Summarize the validation actions and outcomes.

## UX consistency checks

- If `withZekkenName`/Zekken mode toggles format expectations, keep placeholders/help/format guide/sample behavior consistent.
- Avoid introducing noisy or repetitive warnings when a concise informational message is sufficient.
