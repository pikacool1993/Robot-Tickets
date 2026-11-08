package agent

import (
	"bytes"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	protocol "robot/apps/cloud-server/pkg/clientprotocol"
	"robot/apps/local-agent/internal/store"
	"sync/atomic"
	"testing"
	"time"
)

func TestProtocolMustStartBeforeCloudLogin(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(500) }))
	defer server.Close()
	root := t.TempDir()
	db, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service, err := New(root, db, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	service.useConfig(protocol.Config{Version: 1, ServerURL: server.URL, TransportKeyID: "key", TransportKey: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)), ExportID: "test", IssuedAt: time.Now()})
	if err = service.EnableProtocols(); err != nil {
		t.Fatal(err)
	}
	err = service.Login(Form{Username: "agent", Password: "password", ClientNumber: "A", AuthProtocol: "yyb"})
	if err == nil {
		t.Fatal("missing service executable accepted")
	}
	if requests.Load() != 0 || service.Snapshot().LoggedIn {
		t.Fatal("cloud login ran before protocol startup succeeded")
	}
}
