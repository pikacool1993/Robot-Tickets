package store

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestIsolationPersistenceAndProtectedSecrets(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.InstanceID()
	if err != nil || len(id) != 36 {
		t.Fatal("missing instance identity")
	}
	secret := []byte("secret-password-that-must-not-be-plaintext")
	if err := s.Put("form", secret); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("form")
	if err != nil || !bytes.Equal(got, secret) {
		t.Fatal("DPAPI round trip failed")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	file, err := os.ReadFile(filepath.Join(dir, "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(file, secret) {
		t.Fatal("plaintext secret on disk")
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	restored, _ := s.InstanceID()
	if restored != id {
		t.Fatal("identity changed on restart")
	}
	other, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	otherID, _ := other.InstanceID()
	if id == otherID {
		t.Fatal("independent instances share identity")
	}
	if err := s.ReplaceConfig([]byte("config secret")); err != nil {
		t.Fatal(err)
	}
	got, err = s.Get("form")
	if err != nil || got != nil {
		t.Fatal("replaced config must clear remembered credentials")
	}
}
