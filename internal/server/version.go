package server

// VersionAtLeast reports whether a dotted version (such as the Docker Engine
// version, with or without a leading "v") is at least major.minor.
func VersionAtLeast(value string, major, minor int) bool { return versionAtLeast(value, major, minor) }
