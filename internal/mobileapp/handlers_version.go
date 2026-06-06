package mobileapp

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/cmd/version"
)

type versionResponse struct {
	Version   string `json:"version"`
	BuildDate string `json:"buildDate"`
}

// RegisterVersionHandlers wires GET /api/version. The endpoint is
// public (no admin auth header required) and returns the version and
// buildDate of the running binary.
func RegisterVersionHandlers(r *gin.RouterGroup) {
	r.GET("/version", func(c *gin.Context) {
		c.JSON(http.StatusOK, versionResponse{
			Version:   version.GetVersion(),
			BuildDate: version.GetBuildDate(),
		})
	})
}
