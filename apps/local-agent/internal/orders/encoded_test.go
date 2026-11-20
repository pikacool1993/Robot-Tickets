package orders

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"encoding/json"
	"robot/apps/local-agent/internal/business"
	"robot/apps/local-agent/internal/userkey"
	"strings"
	"testing"
	"time"
)

func TestEncodedRequestMatchesLegacyBytes(t *testing.T) {
	plan, people, selected := orderFixture()
	body, err := FromPlan(plan, people, selected)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	key, err := userkey.Parse([]byte(`{"encrypt_key":"MDEyMzQ1Njc4OWFiY2RlZg==","iv":"0123456789abcdef","version":54,"create_time":1000,"expire_in":3600}`), now)
	if err != nil {
		t.Fatal(err)
	}
	session := business.Session{Token: "test-token", UserID: "99", AuthCode: "test-code"}
	for n := 0; n < 32; n++ {
		body.Users[0].Name = "中文\"\\\n<>&\u2028" + strings.Repeat("x", n)
		legacy, path, err := Build(body, key, session, now)
		if err != nil {
			t.Fatal(err)
		}
		want, err := json.Marshal(legacy)
		if err != nil {
			t.Fatal(err)
		}
		encoded, gotPath, err := BuildEncodedWithExpiredKey(body, key, session, now, false)
		if err != nil {
			t.Fatal(err)
		}
		got, err := encoded.OrderJSON()
		if err != nil || !bytes.Equal(got, want) || gotPath != path || !json.Valid(got) {
			t.Fatalf("wire mismatch, padding case %d", n)
		}
		encrypted, err := hex.DecodeString(legacy.EncryptedData)
		if err != nil {
			t.Fatal(err)
		}
		block, _ := aes.NewCipher([]byte("0123456789abcdef"))
		plain := make([]byte, len(encrypted))
		cipher.NewCBCDecrypter(block, []byte(key.IV)).CryptBlocks(plain, encrypted)
		padding := int(plain[len(plain)-1])
		original, _ := json.Marshal(body)
		if !bytes.Equal(plain[:len(plain)-padding], original) {
			t.Fatal("padding polluted plaintext")
		}
		// Encoding owns the submission bytes, independent of later object edits.
		body.Users[0].Signature = "changed"
		again, _ := encoded.OrderJSON()
		if !bytes.Equal(again, want) {
			t.Fatal("source mutation changed wire snapshot")
		}
	}
	if _, err := (EncodedRequest{}).OrderJSON(); err == nil {
		t.Fatal("empty encoded request accepted")
	}
}

var wireBenchmarkSink []byte

func BenchmarkOrderEncoding(b *testing.B) {
	plan, people, selected := orderFixture()
	body, err := FromPlan(plan, people, selected)
	if err != nil {
		b.Fatal(err)
	}
	now := time.Now()
	key, err := userkey.Parse([]byte(`{"encrypt_key":"MDEyMzQ1Njc4OWFiY2RlZg==","iv":"0123456789abcdef","version":54,"create_time":1000,"expire_in":3600}`), now)
	if err != nil {
		b.Fatal(err)
	}
	session := business.Session{Token: "test-token", UserID: "99", AuthCode: "test-code"}
	b.Run("build-then-marshal", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			req, _, err := Build(body, key, session, now)
			if err != nil {
				b.Fatal(err)
			}
			wireBenchmarkSink, err = json.Marshal(req)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("reuse-plaintext", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			req, _, err := BuildEncodedWithExpiredKey(body, key, session, now, false)
			if err != nil {
				b.Fatal(err)
			}
			wireBenchmarkSink, err = req.OrderJSON()
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}
