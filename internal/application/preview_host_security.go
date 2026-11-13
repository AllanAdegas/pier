package application

// stripPreviewHostOptions applies equally to own-repository and fork previews,
// and runs again after repository configuration has been applied.
func stripPreviewHostOptions(config *Config) {
	config.DockerOptions.Privileged = false
	config.DockerOptions.PidMode = ""
	config.DockerOptions.NetworkMode = ""
	config.DockerOptions.Devices = nil
	config.DockerOptions.CapAdd = nil
}
