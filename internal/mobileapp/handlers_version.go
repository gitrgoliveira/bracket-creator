package mobileapp

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/cmd/version"
)

type versionResponse struct {
	Version   string `json:"version"`
	GitCommit string `json:"gitCommit"`
	BuildDate string `json:"buildDate"`
}

// RegisterVersionHandlers wires GET /api/version. The endpoint is
// public (no admin auth header required) and returns version, gitCommit,
// and buildDate of the running binary.
func RegisterVersionHandlers(r *gin.RouterGroup) {
	r.GET("/version", func(c *gin.Context) {
		// Prevent intermediate proxies from caching build metadata,
		// a stale version footer would mislead operators about which binary
		// is actually running.
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, versionResponse{
			Version:   version.GetVersion(),
			GitCommit: version.GetGitCommit(),
			BuildDate: version.GetBuildDate(),
		})
	})
}

// BuildHeader is the response header naming the build that answered, set on
// every answer (bc-evts F2): a tab running another build's bundle reloads, or
// says it must. The SSE stream's hello carries the same value.
const BuildHeader = "X-App-Build"

// AppBuild is the build this server runs: its git commit, or "unknown" when
// the binary was built without one (a client treats unknown as a match).
func AppBuild() string {
	if b := strings.TrimSpace(version.GetGitCommit()); b != "" {
		return b
	}
	return "unknown"
}
