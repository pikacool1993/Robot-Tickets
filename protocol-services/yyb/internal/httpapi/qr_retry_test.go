package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"yyb_go/internal/store"
)

func TestConfirmedQRCanBePolledAndConfirmedAgain(t *testing.T) {
	app, err := NewApp(Config{ResourceRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	// Model a successful confirmation whose HTTP response never reached the
	// client. No live session/upstream credentials remain available for retries.
	entry := &qrEntry{confirmed: &store.AccountPublic{ID: 42, OpenID: "test-openid"}, confirmedUntil: time.Now().Add(confirmationRetention)}
	app.qrSessions["retry"] = entry
	handler := app.Handler()
	for i := 0; i < 3; i++ {
		poll := httptest.NewRecorder()
		handler.ServeHTTP(poll, httptest.NewRequest(http.MethodGet, "/qr/retry/poll", nil))
		var status struct{ Data struct{ Status string } }
		if err := json.Unmarshal(poll.Body.Bytes(), &status); err != nil || poll.Code != 200 || status.Data.Status != "confirmed" {
			t.Fatalf("poll: %s", poll.Body.String())
		}
		confirm := httptest.NewRecorder()
		handler.ServeHTTP(confirm, httptest.NewRequest(http.MethodPost, "/qr/retry/confirm", nil))
		var result struct{ Data store.AccountPublic }
		if err := json.Unmarshal(confirm.Body.Bytes(), &result); err != nil || confirm.Code != 200 || result.Data.ID != 42 || result.Data.OpenID != "test-openid" {
			t.Fatalf("confirm: %s", confirm.Body.String())
		}
	}
	app.pruneQR()
	if app.getQRSession("retry") == nil {
		t.Fatal("removed unexpired confirmation")
	}
	entry.confirmedUntil = time.Now().Add(-time.Second)
	for _, request := range []struct{ method, path string }{{http.MethodGet, "/qr/retry/poll"}, {http.MethodPost, "/qr/retry/confirm"}} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(request.method, request.path, nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("expired result returned %d", w.Code)
		}
	}
	app.pruneQR()
	if app.getQRSession("retry") != nil {
		t.Fatal("expired confirmation not pruned")
	}
}
