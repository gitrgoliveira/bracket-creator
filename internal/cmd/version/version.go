//go:generate bash get_build_info.sh

package version

import (
	// using blank import for embed as it is only used inside comments
	_ "embed"
	"fmt"
	"runtime"
)

var (
	// GitCommit returns the git commit that was compiled.
	//go:embed commit.txt
	gitCommit string

	// Version returns the main version number that is being run at the moment.
	//go:embed version.txt
	version string

	// BuildDate returns the date the binary was built
	//go:embed build_date.txt
	buildDate string
)

// GoVersion returns the version of the go runtime used to compile the binary
var goVersion = runtime.Version()

// OsArch returns the os and arch used to build the binary
var osArch = fmt.Sprintf("%s %s", runtime.GOOS, runtime.GOARCH)

// generateOutput return the output of the version command
func generateOutput() string {
	return fmt.Sprintf(`bracket-creator - %s

Git Commit: %s
Build date: %s
Go version: %s
OS / Arch : %s
`, version, gitCommit, buildDate, goVersion, osArch)
}

// Print the current version
func Print() {
	fmt.Println(generateOutput())
}

// GetVersion returns the current version
func GetVersion() string {
	return version
}

// GetGitCommit returns the git commit
func GetGitCommit() string {
	return gitCommit
}

// GetBuildDate returns the build date
func GetBuildDate() string {
	return buildDate
}

// GetGoVersion returns the go version
func GetGoVersion() string {
	return goVersion
}

// GetOsArch returns the OS and architecture
func GetOsArch() string {
	return osArch
}
