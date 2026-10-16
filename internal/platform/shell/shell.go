// Package shell builds POSIX shell command lines without injection risk.
package shell

import "strings"

// Quote wraps a value in single quotes so the shell treats it as one literal word.
func Quote(value string) string {
	if value == "" {
		return "''"
	}
	if isSafe(value) {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// Join quotes every argument and joins them into a single command line.
func Join(args ...string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = Quote(arg)
	}
	return strings.Join(quoted, " ")
}

func isSafe(value string) bool {
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case strings.ContainsRune("-_./:=@,+", r):
		default:
			return false
		}
	}
	return true
}
