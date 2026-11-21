# Sample printouts

Reprints the sample PDFs the docs show on the Sample printouts page
(`docs/user-guide/organisers/sample-printouts.md`), and the picture of one page
of each.

    make docs/samples

It prints each sample workbook on its own with
`bin/bracket-creator print --type=all`, from the example workbook of the same
name at the repository root (`pools-example-medium.xlsx` and
`pools-example-medium-teams.xlsx`, made by `make examples`). It does not run
`make examples` itself: regenerate the examples first when the change is to the
workbooks.

Every page of every new PDF is turned into a picture, and so is every page of
the committed sample in `docs/samples/<sample>/`. A PDF whose pages all match is
left alone, so git stays quiet. A PDF with a new page count, or with any page
whose pixels moved, is copied over the committed one together with its
thumbnail, and the run lists it:

    8 unchanged, 1 copied into docs/samples/
      ...
    copied - look at these, and at the page that shows them:
      pools-example-medium/print_full_bracket.pdf: CHANGED, page 3, 4 of 11 (thumbnail: page 3)

The PDFs are compared as pictures, never as bytes: `print` stamps each file it
writes with its own date and ids, so two prints of the same workbook never have
the same bytes. The pictures are compared with the screenshot harness's pixel
compare (`scripts/screenshots/lib/png.mjs`), which allows the grey level or two
a render can move by.

## How a page becomes a picture

With the tools the repository already needs, nothing more:

- pdfcpu, the Go module the app uses to put its PDFs together, splits the PDF
  into one file per page (`go run github.com/pdfcpu/pdfcpu/cmd/pdfcpu split`, at
  the version `go.mod` names).
- LibreOffice, which `print` needs anyway, draws each page as a PNG at 96 dots
  per inch (an A4 page is 794 by 1123 pixels). Each one-page PDF is placed as a
  picture on a drawing page of its own size, and the drawing is converted:
  opening the PDF directly lays its text out again and moves letters. Its PNG
  export draws a document's first page only and ignores a page range, which is
  why the split comes first.

LibreOffice is found the way `print` finds it: `$LIBREOFFICE_PATH`, then
`soffice` on the path, then the usual install locations.

## Choosing the thumbnail page

`SAMPLES` in `run.mjs` names, for each PDF, the page the docs page shows. To
show another page, change the number and delete that thumbnail
(`docs/samples/<sample>/<pdf>.png`): the next run draws it again from the
committed PDF without touching the PDF. A run stops with an error when a PDF no
longer has the page its thumbnail names.
