package diagnose

import "regexp"

// maxHints bounds the explanations of one failure: more than that buries the
// one that matters.
const maxHints = 3

// Explain returns the hints that match a failed build or deploy log, the most
// specific first. exitCode is the container's exit status, or 0 when unknown.
func Explain(log string, exitCode int) []Hint {
	var hints []Hint
	for _, candidate := range failureRules {
		if len(hints) == maxHints {
			break
		}
		if candidate.matches(log, exitCode) {
			hints = append(hints, candidate.hint)
		}
	}
	return hints
}

// rule ties a hint to the evidence that triggers it: an exit status, a log
// pattern, or either one.
type rule struct {
	hint     Hint
	exitCode int
	pattern  *regexp.Regexp
}

func (r rule) matches(log string, exitCode int) bool {
	if r.exitCode != 0 && exitCode == r.exitCode {
		return true
	}
	return r.pattern != nil && r.pattern.MatchString(log)
}
