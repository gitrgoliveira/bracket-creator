package pdf

// Test-only exports for the external pdf_test package. Its tests build their
// workbooks through the engine, which imports this package, so an internal
// test could not import the engine back.

// ResolveSheets is the selection buildGroup prints for one workbook.
func (g Group) ResolveSheets(present []string) []string { return g.resolveSheets(present) }

// RequireSoffice skips the calling test when LibreOffice is unavailable.
var RequireSoffice = requireSoffice
