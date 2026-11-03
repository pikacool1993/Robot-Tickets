package clientconfig

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"testing"
	"time"
)

func TestSignedConfig(t *testing.T) {
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{1}, 32))
	key := bytes.Repeat([]byte{2}, 32)
	e := Exporter{"enc-v1", key, "sign-v1", private}
	c := Config{1, "http://example.test:8080", "client-v1", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32)), "test-export", time.Now().UTC()}
	file, err := e.Seal(c)
	if err != nil {
		t.Fatal(err)
	}
	enc := map[string][]byte{"enc-v1": key}
	sign := map[string]ed25519.PublicKey{"sign-v1": private.Public().(ed25519.PublicKey)}
	got, err := Open(file, enc, sign)
	if err != nil || got != c {
		t.Fatalf("round trip failed: %v", err)
	}
	other, err := e.Seal(c)
	if err != nil || bytes.Equal(file, other) {
		t.Fatal("exports must use fresh nonces")
	}
	if bytes.Contains(file, []byte(c.TransportKey)) || bytes.Contains(file, []byte(c.ServerURL)) {
		t.Fatal("plaintext exposed")
	}
	for i := range file {
		bad := bytes.Clone(file)
		bad[i] ^= 1
		if _, err := Open(bad, enc, sign); err == nil {
			t.Fatalf("accepted modified byte %d", i)
		}
	}
	for i := range file {
		if _, err := Open(file[:i], enc, sign); err == nil {
			t.Fatalf("accepted truncated file at %d", i)
		}
	}
	if _, err := Open(append(bytes.Clone(file), 0), enc, sign); err == nil {
		t.Fatal("accepted trailing data")
	}
	if _, err := Open(file, map[string][]byte{"enc-v1": bytes.Repeat([]byte{9}, 32)}, sign); err == nil {
		t.Fatal("accepted wrong encryption key")
	}
	if _, err := Open(file, enc, nil); err == nil {
		t.Fatal("accepted missing trust anchor")
	}
	wrong := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{9}, 32)).Public().(ed25519.PublicKey)
	if _, err := Open(file, enc, map[string]ed25519.PublicKey{"sign-v1": wrong}); err == nil {
		t.Fatal("accepted wrong public key")
	}
	bad := bytes.Clone(file)
	binary.BigEndian.PutUint32(bad[8:12], 0xffffffff)
	if _, err := Open(bad, enc, sign); err == nil {
		t.Fatal("accepted invalid header size")
	}
}

func TestValidation(t *testing.T) {
	for _, raw := range []string{"", "file:///tmp", "http://user:pass@host", "http://host/api", "http://host?key=secret", "http://host#fragment", "http://host?"} {
		if _, err := NormalizeServerURL(raw); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
	if got, err := NormalizeServerURL(" http://localhost:8080/ "); err != nil || got != "http://localhost:8080" {
		t.Fatal("valid origin rejected")
	}
}
