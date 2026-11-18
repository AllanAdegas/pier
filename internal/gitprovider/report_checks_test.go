package gitprovider

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestCheckRunCreateUpdateAndPermissionFallback(t *testing.T) {
	s := fixture(t)
	var inputs []checkRunInput
	deny := false
	mock(s, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/app/installations/123/access_tokens" {
			io.WriteString(w, `{"token":"scoped-token","expires_at":"2099-01-01T00:00:00Z"}`)
			return
		}
		if deny {
			w.WriteHeader(403)
			return
		}
		var input checkRunInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
		}
		inputs = append(inputs, input)
		if len(inputs) == 1 && (r.Method != "POST" || r.URL.Path != "/repos/acme/app/check-runs") {
			t.Error(r.Method, r.URL.Path)
		}
		if len(inputs) == 2 && (r.Method != "PATCH" || r.URL.Path != "/repos/acme/app/check-runs/7") {
			t.Error(r.Method, r.URL.Path)
		}
		io.WriteString(w, `{"id":7}`)
	})
	r := &githubReporter{service: s, selection: Selection{InstallationID: 123, Repository: "acme/app"}}
	report := RunReport{SHA: sha, Context: "pier/app", Run: "deployment", Phase: PhaseInProgress, Description: "Deploy"}
	id, err := r.UpsertCheckRun(t.Context(), report, 0)
	if err != nil || id != 7 {
		t.Fatal(id, err)
	}
	report.Phase = PhaseSucceeded
	id, err = r.UpsertCheckRun(t.Context(), report, id)
	if err != nil || id != 7 {
		t.Fatal(id, err)
	}
	if inputs[0].Status != "in_progress" || inputs[0].HeadSHA != sha || inputs[1].Status != "completed" || inputs[1].Conclusion != "success" {
		t.Fatal(inputs)
	}
	deny = true
	if _, err = r.UpsertCheckRun(t.Context(), report, id); err != ErrChecksUnavailable {
		t.Fatal(err)
	}
	if !s.checksDenied(123) {
		t.Fatal("permission refusal was not cached")
	}
}
