// Package resources manages embedded resources for the application
package resources

import "embed"

// Resources holds embedded application resources
type Resources struct {
	WebFiles     embed.FS
	TemplateFile embed.FS
}

// NewResources creates a new resources handler
func NewResources(webFiles, templateFile embed.FS) *Resources {
	return &Resources{
		WebFiles:     webFiles,
		TemplateFile: templateFile,
	}
}

// GetWebFS returns the embedded web file system
func (r *Resources) GetWebFS() embed.FS {
	return r.WebFiles
}

// GetTemplateFS returns the embedded template file system
func (r *Resources) GetTemplateFS() embed.FS {
	return r.TemplateFile
}
