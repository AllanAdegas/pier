package updates

import (
	"fmt"
	"regexp"
	"strconv"
)

// release is a parsed MAJOR.MINOR.PATCH version, with an optional
// pre-release suffix. Build metadata is accepted and ignored.
type release struct {
	major, minor, patch uint64
	prerelease          string
}

var releasePattern = regexp.MustCompile(`^v?(\d{1,9})\.(\d{1,9})\.(\d{1,9})(?:-([0-9A-Za-z.-]{1,64}))?(?:\+[0-9A-Za-z.-]{1,64})?$`)

func parseRelease(text string) (release, error) {
	match := releasePattern.FindStringSubmatch(text)
	if match == nil {
		return release{}, fmt.Errorf("invalid version %q", text)
	}
	numbers := [3]uint64{}
	for i := range numbers {
		number, err := strconv.ParseUint(match[i+1], 10, 64)
		if err != nil {
			return release{}, fmt.Errorf("invalid version %q: %w", text, err)
		}
		numbers[i] = number
	}
	return release{major: numbers[0], minor: numbers[1], patch: numbers[2], prerelease: match[4]}, nil
}

// newerThan reports whether r is a later release than other. A pre-release
// sorts before the release it precedes; two pre-releases of the same
// version are not ordered (never an update).
func (r release) newerThan(other release) bool {
	if r.major != other.major {
		return r.major > other.major
	}
	if r.minor != other.minor {
		return r.minor > other.minor
	}
	if r.patch != other.patch {
		return r.patch > other.patch
	}
	return other.prerelease != "" && r.prerelease == ""
}
