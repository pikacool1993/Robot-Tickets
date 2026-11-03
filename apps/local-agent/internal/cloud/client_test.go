package cloud

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	protocol "robot/apps/cloud-server/pkg/clientprotocol"
	"testing"
	"time"
)

func TestRejectMalformedEncryptedResponses(t *testing.T) {
	key := bytes.Repeat([]byte{8}, 32)
	keys := protocol.Keyring{"client-v1": key}
	for _, mode := range []string{"short nonce", "wrong key ID", "plaintext", "redirect"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "plaintext":
					w.Write([]byte(`{"code":"ok","data":null}`))
				case "redirect":
					http.Redirect(w, r, "http://127.0.0.1:1", http.StatusFound)
				default:
					env, _ := keys.Seal("client-v1", []byte(`{"code":"ok","data":null}`))
					if mode == "short nonce" {
						env.Nonce = "AA=="
					} else {
						env.KeyID = "other"
					}
					_ = json.NewEncoder(w).Encode(env)
				}
			}))
			defer server.Close()
			client, err := New(protocol.Config{Version: 1, ServerURL: server.URL, TransportKeyID: "client-v1", TransportKey: base64.StdEncoding.EncodeToString(key), ExportID: "test", IssuedAt: time.Now()})
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			if err := client.Call(context.Background(), "GET", "/auth/me", "token", nil, nil); err == nil {
				t.Fatal("accepted invalid response")
			}
		})
	}
}
