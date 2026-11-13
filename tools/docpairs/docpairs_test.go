package docpairs

import (
	"os"
	"path/filepath"
	"testing"
)

var pairedDocuments = []string{
	"README", "CONTRIBUTING", "CODE_OF_CONDUCT", "GOVERNANCE", "SECURITY", "RELEASE_NOTES",
}

// Proper nouns that carry Portuguese accents and may appear in English text.
var allowedInEnglish = []string{"Português", "São Paulo"}

func readRepoFile(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", name))
	if err != nil {
		t.Fatalf("%s: %v (every document needs an English primary and a .pt-BR.md counterpart)", name, err)
	}
	return string(data)
}

func firstLine(text string) string {
	for i, r := range text {
		if r == '\n' {
			return text[:i]
		}
	}
	return text
}

func TestEveryDocumentHasBothLanguagesLinkedToEachOther(t *testing.T) {
	for _, name := range pairedDocuments {
		english := readRepoFile(t, name+".md")
		portuguese := readRepoFile(t, name+".pt-BR.md")

		wantEnglish := "English | [Português](" + name + ".pt-BR.md)"
		if got := firstLine(english); got != wantEnglish {
			t.Errorf("%s.md first line = %q, want %q", name, got, wantEnglish)
		}
		wantPortuguese := "[English](" + name + ".md) | Português"
		if got := firstLine(portuguese); got != wantPortuguese {
			t.Errorf("%s.pt-BR.md first line = %q, want %q", name, got, wantPortuguese)
		}
	}
}

func TestEnglishDocumentsContainNoPortugueseProse(t *testing.T) {
	for _, name := range pairedDocuments {
		body := readRepoFile(t, name+".md")
		body = body[len(firstLine(body)):]
		if words := PortugueseWords(StripCode(body), allowedInEnglish); len(words) > 0 {
			t.Errorf("%s.md has Portuguese text outside code blocks: %v", name, words)
		}
	}
}

func TestPortugueseDocumentsAreTranslatedAndKeepTheStructure(t *testing.T) {
	for _, name := range pairedDocuments {
		english := readRepoFile(t, name+".md")
		portuguese := readRepoFile(t, name+".pt-BR.md")
		if !HasPortugueseLetters(StripCode(portuguese)) {
			t.Errorf("%s.pt-BR.md has no Portuguese text: was it left in English?", name)
		}
		if got, want := HeadingCount(portuguese), HeadingCount(english); got != want {
			t.Errorf("%s: %d headings in Portuguese, %d in English", name, got, want)
		}
	}
}

func TestInstallerHasBothLanguagesForEveryMessage(t *testing.T) {
	script := readRepoFile(t, "install.sh")
	arms := InstallerArms(script)
	if len(arms) == 0 {
		t.Fatal("install.sh has no `pt.key)` / `en.key)` message arms")
	}
	languages := map[string]map[string]bool{}
	for _, arm := range arms {
		if languages[arm.Key] == nil {
			languages[arm.Key] = map[string]bool{}
		}
		languages[arm.Key][arm.Language] = true
		if arm.Language == "en" && HasPortugueseLetters(arm.Text) {
			t.Errorf("install.sh en.%s contains Portuguese text: %s", arm.Key, arm.Text)
		}
	}
	for key, has := range languages {
		if !has["pt"] || !has["en"] {
			t.Errorf("install.sh message %q needs both a pt and an en string (pt=%v en=%v)", key, has["pt"], has["en"])
		}
	}
	for key := range InstallerKeysInUse(script) {
		if languages[key] == nil {
			t.Errorf("install.sh uses message %q, which has no pt/en strings", key)
		}
	}
}
