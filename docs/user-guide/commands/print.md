# print

Renders bracket Excel workbooks into grouped, print-ready **PDFs** (competitor tags, name sheets, bracket trees, and more) using **LibreOffice**.

```
bracket-creator print --type <type> (--input <dir> | --tournament-data <dir>) [flags]
```

This is the command-line counterpart of the tournament app's **Export PDFs** button. Both share the same rendering engine; use the CLI when you want PDFs without running the server, or when you want to generate them on a separate, LibreOffice-equipped machine (see [When to use the CLI](#when-to-use-the-cli)).

## Input modes

Provide **exactly one**:

| Flag | What it reads |
|------|---------------|
| `--input <dir>` | A directory of pre-existing bracket `.xlsx` files (for example, output from [`create-pools`](create-pools.md) or [`create-playoffs`](create-playoffs.md)). |
| `--tournament-data <dir>` | A [tournament app](../organisers/run-tournament.md) data directory; the workbooks are generated on the fly from competition state. |

## Types

`--type` selects which sheets to render (required):

| Type | Output |
|------|--------|
| `registration` | The data sheet from every workbook |
| `names` | The "Names to Print" sheets, A3 landscape, with title pages |
| `tags` | The competitor "Tags" sheets, with title pages (team workbooks excluded). Tags carry a [QR code](../organisers/run-tournament.md#export-and-print) when the tournament's public URL is set. |
| `pools-trees` | Pool Draw + Tree sheets (a participant booklet), page-numbered |
| `full-bracket` | Pool Draw + Pool/Elimination Matches + Trees, page-numbered |
| `all` | Every type, written into `--output-dir` |

## Other flags

| Flag | Short | Description |
|------|-------|-------------|
| `--output` | `-o` | Output PDF path for a single `--type`. Mutually exclusive with `--output-dir`. |
| `--output-dir` | | Output directory. Required for `--type=all`; for a single `--type`, give this or `--output`. |
| `--team-file` | | An `.xlsx` basename to treat as a team workbook (excluded from tags). Repeatable; defaults to any filename containing `team`. |

An output target is always required: for a single `--type`, provide exactly one of `--output` or `--output-dir`; `--type=all` requires `--output-dir` (and rejects `--output`).

## Usage

```bash
# Generate every PDF group from a folder of bracket workbooks
bracket-creator print --type=all --input=./xlsx/ --output-dir=./pdfs

# Generate everything from a tournament-data directory
bracket-creator print --type=all --tournament-data=tournament-data/ --output-dir=./pdfs

# Competitor tags only, to a single file
bracket-creator print --type=tags --input=./xlsx/ -o ./tags.pdf
```

## When to use the CLI

The tournament app's in-app export needs LibreOffice **in the running server**. The CLI lets you keep the server on the lean default image (no LibreOffice) and render PDFs elsewhere:

- **Offline / CLI-only events**: you built brackets with `create-pools` or the [legacy web UI](../organisers/web-ui.md) and never ran the tournament app. Only the CLI can render those into PDFs (`--input`).
- **Out-of-band generation**: run `print --tournament-data ...` on a LibreOffice-equipped machine instead of bundling LibreOffice into every deployment.
- **Batch or scripted** PDF generation.

## LibreOffice requirement

`print` renders through **LibreOffice** (`soffice`). Install it with your platform's package manager (ensuring `soffice` is on `PATH`), or set `$LIBREOFFICE_PATH` to the `soffice` binary. If LibreOffice is not found, the command exits with installation instructions. The published [`bracket-creator-mobile-pdf`](../install/hosting.md) image bundles it.
