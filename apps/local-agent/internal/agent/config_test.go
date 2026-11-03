package agent

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	protocol "robot/apps/cloud-server/pkg/clientprotocol"
	"robot/apps/local-agent/internal/store"
	"testing"
	"time"
)

func TestBootstrapImportAndFailedReplacement(t *testing.T) {
	root := t.TempDir()
	db, err := store.Open(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	key := bytes.Repeat([]byte{1}, 32)
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{2}, 32))
	c := protocol.Config{Version: 1, ServerURL: "http://localhost:8080", TransportKeyID: "client-v1", TransportKey: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32)), ExportID: "test", IssuedAt: time.Now()}
	nonce := bytes.Repeat([]byte{4}, 12)
	header, _ := json.Marshal(map[string]any{"version": 1, "encryption_key_id": "enc-v1", "signing_key_id": "sign-v1", "nonce": base64.StdEncoding.EncodeToString(nonce)})
	prefix := append([]byte("RBTCFG\r\n"), binary.BigEndian.AppendUint32(nil, uint32(len(header)))...)
	prefix = append(prefix, header...)
	block, _ := aes.NewCipher(key)
	aead, _ := cipher.NewGCM(block)
	plain, _ := json.Marshal(c)
	file := append(prefix, aead.Seal(nil, nonce, plain, prefix)...)
	file = append(file, ed25519.Sign(private, file)...)
	dir := filepath.Join(root, "config")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "bootstrap.cfg")
	if err := os.WriteFile(path, file, 0600); err != nil {
		t.Fatal(err)
	}
	enc := map[string][]byte{"enc-v1": key}
	sign := map[string]ed25519.PublicKey{"sign-v1": private.Public().(ed25519.PublicKey)}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := New(root, db, enc, sign, logger)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Snapshot().Configured || s.Snapshot().ServerURL != c.ServerURL {
		t.Fatal("bundled bootstrap was not imported")
	}
	if err := os.WriteFile(path, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Import(path); err == nil {
		t.Fatal("accepted corrupt replacement")
	}
	if s.Snapshot().ServerURL != c.ServerURL {
		t.Fatal("failed import replaced existing configuration")
	}
	s.Close()
	// Valid saved configuration takes precedence over an invalid bundled file.
	s, err = New(root, db, enc, sign, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if !s.Snapshot().Configured {
		t.Fatal("saved configuration was not restored")
	}
	if state := s.Snapshot(); state.LoggedIn {
		t.Fatal("import restored a session")
	}
}
