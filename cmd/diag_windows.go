//go:build windows

package cmd

// diagnoseFolderError is a no-op on Windows: syscall.Stat_t doesn't carry
// Uid/Gid fields there, and Docker bind-mount UID mismatches are a
// Linux/macOS concern. The empty string tells the caller to omit the hint
// block from the error message.
func diagnoseFolderError(_ string) string { return "" }
