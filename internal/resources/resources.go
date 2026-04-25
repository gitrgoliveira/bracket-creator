// Package resources manages embedded resources for the application
package resources

import "io/fs"

// Resources holds embedded application resources.
// Fields use the fs.FS interface so that both embed.FS (production) and
// fstest.MapFS (tests) can be used.
type Resources struct {
	WebFiles fs.FS
}

// NewResources creates a new resources handler
func NewResources(webFiles fs.FS) *Resources {
	return &Resources{
		WebFiles: webFiles,
	}
}

// GetWebFS returns the embedded web file system
func (r *Resources) GetWebFS() fs.FS {
	return r.WebFiles
}
