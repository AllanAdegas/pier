package docker

import "regexp"

// Formats shared by every context that puts user input on a docker command
// line. They are deliberately strict: anything that matches is safe as a
// single argument and cannot be mistaken for an option.
var (
	imagePattern  = regexp.MustCompile(`^[a-z0-9]([a-z0-9._/:-]*[a-z0-9])?(:[A-Za-z0-9._-]+)?(@sha256:[0-9a-f]{64})?$`)
	objectPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)
)

// ValidImage reports whether the value is an acceptable image reference.
func ValidImage(reference string) bool { return imagePattern.MatchString(reference) }

// ValidObjectName reports whether the value can name (or identify) a
// container, volume or network.
func ValidObjectName(name string) bool { return objectPattern.MatchString(name) }
