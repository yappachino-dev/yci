package fs

type AppPaths struct {
	config  string
	library string
	staging string
}

// NewAppPaths builds application paths from resolved configuration values.
func NewAppPaths(configDir, libraryDir, stagingDir string) AppPaths {
	return AppPaths{config: configDir, library: libraryDir, staging: stagingDir}
}

func (p AppPaths) ConfigDir() string {
	return p.config
}

func (p AppPaths) StagingDir() string {
	return p.staging
}

func (p AppPaths) LibraryDir() string {
	return p.library
}
