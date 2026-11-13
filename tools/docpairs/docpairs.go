// Package docpairs holds the helpers used by the language-pair tests: the
// repository documents and the installer must exist in English and Portuguese.
package docpairs

import (
	"regexp"
	"strings"
)

// portugueseLetters matches the accented letters that appear in Portuguese
// prose and never in plain English prose.
var portugueseLetters = regexp.MustCompile(`[áàâãéêíóôõúçÁÀÂÃÉÊÍÓÔÕÚÇ]`)

var (
	fencedBlock = regexp.MustCompile("(?s)```.*?```")
	inlineCode  = regexp.MustCompile("`[^`\n]*`")
)

// StripCode removes fenced and inline code, where Portuguese is allowed
// (quoted catalog keys, shell comments, sample output).
func StripCode(markdown string) string {
	return inlineCode.ReplaceAllString(fencedBlock.ReplaceAllString(markdown, ""), "")
}

// PortugueseWords returns the words of text that contain Portuguese accented
// letters, after removing the allowed proper nouns.
func PortugueseWords(text string, allowed []string) []string {
	for _, name := range allowed {
		text = strings.ReplaceAll(text, name, "")
	}
	var found []string
	for _, word := range strings.Fields(text) {
		if portugueseLetters.MatchString(word) {
			found = append(found, word)
		}
	}
	return found
}

// HasPortugueseLetters reports whether text contains any Portuguese accented letter.
func HasPortugueseLetters(text string) bool {
	return portugueseLetters.MatchString(text)
}

// HeadingCount counts Markdown headings outside code blocks.
func HeadingCount(markdown string) int {
	count := 0
	for _, line := range strings.Split(fencedBlock.ReplaceAllString(markdown, ""), "\n") {
		if strings.HasPrefix(line, "#") {
			count++
		}
	}
	return count
}

var (
	messageArm   = regexp.MustCompile(`(?m)^\s*(pt|en)\.([a-z_]+)\)(.*)$`)
	messageUsage = regexp.MustCompile(`(?:\bsay|\bfail|\bmsg) ([a-z_]+)`)
)

// MessageArm is one language arm of the installer's msg function.
type MessageArm struct {
	Language string
	Key      string
	Text     string
}

// InstallerArms lists every `pt.key)` / `en.key)` arm of the installer.
func InstallerArms(script string) []MessageArm {
	var arms []MessageArm
	for _, match := range messageArm.FindAllStringSubmatch(script, -1) {
		arms = append(arms, MessageArm{Language: match[1], Key: match[2], Text: match[3]})
	}
	return arms
}

// InstallerKeysInUse lists the message keys that say, fail or msg are called with.
func InstallerKeysInUse(script string) map[string]bool {
	keys := map[string]bool{}
	for _, match := range messageUsage.FindAllStringSubmatch(script, -1) {
		keys[match[1]] = true
	}
	return keys
}
