package main

import (
	"bytes"
	"testing"
)

func TestTokenHashMatchesBearerToken(t *testing.T) {
	raw := bytes.Repeat([]byte{0x5a}, 32)
	token, storedHash, err := tokenFromRandom(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got := hashToken(token); !bytes.Equal(got, storedHash) {
		t.Fatal("stored token hash does not match hash used during bearer authentication")
	}
}
