package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"pier/internal/updates"
)

func updateRequest(t *testing.T, checker *updates.Checker) updates.Status {
	t.Helper()
	mux := http.NewServeMux()
	api := New(Dependencies{Updates: checker})
	mux.Handle("GET /api/v1/updates", api.adapt(api.updateStatus))
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/updates", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status %d: %s", response.Code, response.Body)
	}
	var status updates.Status
	if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	return status
}

func TestUpdatesRouteAnswersDisabledWithoutAChecker(t *testing.T) {
	if status := updateRequest(t, nil); status.Enabled || status.UpdateAvailable {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestUpdatesRouteHonoursTheOptOut(t *testing.T) {
	checker, err := updates.New(updates.Options{Enabled: false, Current: "0.5.0", URL: "http://127.0.0.1:1/never"})
	if err != nil {
		t.Fatal(err)
	}
	status := updateRequest(t, checker)
	if status.Enabled || status.Current != "0.5.0" {
		t.Fatalf("unexpected status: %+v", status)
	}
}
