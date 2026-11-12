package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"pier/internal/platform/apperr"
	"pier/internal/platform/i18n"
)

// answer runs a handler that fails with err behind the language middleware.
func answer(t *testing.T, acceptLanguage string, err error) errorDetail {
	t.Helper()
	api := New(Dependencies{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	handler := withLanguage(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.writeError(w, r, err)
	}))
	request := httptest.NewRequest(http.MethodGet, "/api/v1/anything", nil)
	if acceptLanguage != "" {
		request.Header.Set("Accept-Language", acceptLanguage)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	var body errorBody
	if decodeErr := json.NewDecoder(recorder.Body).Decode(&body); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	return body.Error
}

func TestErrorsAnswerInTheLanguageTheClientAsks(t *testing.T) {
	fields := apperr.Fields{}
	fields.Add("name", "Informe o nome do projeto.")
	invalid := fields.Err()

	english := answer(t, "en-US,en;q=0.9", invalid)
	if english.Message != "Check the highlighted fields." || english.Fields["name"] != "Enter the project name." {
		t.Errorf("English answer: %+v", english)
	}
	portuguese := answer(t, "pt-BR", invalid)
	if portuguese.Message != "Revise os campos destacados." || portuguese.Fields["name"] != "Informe o nome do projeto." {
		t.Errorf("Portuguese answer: %+v", portuguese)
	}
	if got := answer(t, "en", apperr.NotFound("Aplicação")).Message; got != "Application not found." {
		t.Errorf("not found in English: %q", got)
	}
}

func TestClientsWithoutALanguageGetTheInstances(t *testing.T) {
	defer i18n.SetInstance(i18n.Instance())
	i18n.SetInstance(i18n.English)
	if got := answer(t, "", apperr.Conflict("Já existe uma operação em andamento nesta stack.")).Message; got != "An operation is already running on this stack." {
		t.Errorf("got %q", got)
	}
}
