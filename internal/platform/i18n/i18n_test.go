package i18n

import (
	"context"
	"errors"
	"testing"
)

func TestTheFirstLanguageTheClientSpeaksWins(t *testing.T) {
	cases := map[string]Language{
		"en-US,en;q=0.9":          English,
		"pt-BR,pt;q=0.9,en;q=0.8": Portuguese,
		"fr-FR, en;q=0.5":         English,
		"de-DE":                   Portuguese,
		"":                        Portuguese,
	}
	for header, want := range cases {
		if got := FromAcceptLanguage(header, Portuguese); got != want {
			t.Errorf("%q: got %s, want %s", header, got, want)
		}
	}
}

func TestMessagesAreTranslatedWithTheirArguments(t *testing.T) {
	message := M("%s não encontrado(a).", Text("Aplicação"))
	if got := message.Localize(English); got != "Application not found." {
		t.Errorf("English: %q", got)
	}
	if got := message.String(); got != "Aplicação não encontrado(a)." {
		t.Errorf("Portuguese: %q", got)
	}
	if got := English.Sprintf("texto que ninguém traduziu: %d", 3); got != "texto que ninguém traduziu: 3" {
		t.Errorf("an untranslated message must stay as written, got %q", got)
	}
}

func TestErrorsKeepTheirCauseAndTranslateInsideOtherMessages(t *testing.T) {
	cause := errors.New("connection refused")
	err := Errorf("não foi possível contatar o cluster: %w", cause)
	if !errors.Is(err, cause) {
		t.Error("the wrapped error was lost")
	}
	if got := err.Error(); got != "não foi possível contatar o cluster: connection refused" {
		t.Errorf("Portuguese: %q", got)
	}
	outer := M("O kubeconfig armazenado não pode ser usado: %s", err)
	if got := outer.Localize(English); got != "The stored kubeconfig cannot be used: could not reach the cluster: connection refused" {
		t.Errorf("nested English: %q", got)
	}
}

func TestARequestWithoutLanguageUsesTheInstances(t *testing.T) {
	defer SetInstance(Instance())
	SetInstance(English)
	if got := FromContext(context.Background()); got != English {
		t.Errorf("got %s", got)
	}
	if got := FromContext(WithLanguage(context.Background(), Portuguese)); got != Portuguese {
		t.Errorf("the request's language must win, got %s", got)
	}
}

func TestATerminalSpeaksItsLocaleAndEnglishByDefault(t *testing.T) {
	cases := []struct {
		environment map[string]string
		want        Language
	}{
		{map[string]string{"LANG": "pt_BR.UTF-8"}, Portuguese},
		{map[string]string{"LANG": "pt_PT"}, Portuguese},
		{map[string]string{"LANG": "en_US.UTF-8"}, English},
		{map[string]string{"LC_ALL": "en_GB.UTF-8", "LANG": "pt_BR.UTF-8"}, English},
		{map[string]string{"LC_MESSAGES": "pt_BR.UTF-8", "LANG": "en_US.UTF-8"}, Portuguese},
		{map[string]string{"LANG": "C.UTF-8"}, English},
		{map[string]string{"LANG": "de_DE.UTF-8"}, English},
		{map[string]string{}, English},
	}
	for _, c := range cases {
		lookup := func(name string) string { return c.environment[name] }
		if got := FromEnvironment(lookup); got != c.want {
			t.Errorf("%v: got %s, want %s", c.environment, got, c.want)
		}
	}
}
