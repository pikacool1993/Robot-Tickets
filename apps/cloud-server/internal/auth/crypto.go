package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

type Envelope struct {
	Version    int    `json:"version"`
	KeyID      string `json:"key_id"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

type Keyring map[string][]byte

func (k Keyring) Open(e Envelope) ([]byte, error) {
	key, ok := k[e.KeyID]
	if !ok || e.Version != 1 {
		return nil, errors.New("unknown encryption key or protocol version")
	}
	nonce, err := base64.StdEncoding.DecodeString(e.Nonce)
	if err != nil {
		return nil, errors.New("invalid nonce")
	}
	ct, err := base64.StdEncoding.DecodeString(e.Ciphertext)
	if err != nil {
		return nil, errors.New("invalid ciphertext")
	}
	aead, err := makeAEAD(key)
	if err != nil {
		return nil, err
	}
	if len(nonce) != aead.NonceSize() {
		return nil, errors.New("invalid nonce length")
	}
	return aead.Open(nil, nonce, ct, aad(e.Version, e.KeyID))
}

func (k Keyring) Seal(keyID string, plain []byte) (Envelope, error) {
	key, ok := k[keyID]
	if !ok {
		return Envelope{}, errors.New("unknown encryption key")
	}
	aead, err := makeAEAD(key)
	if err != nil {
		return Envelope{}, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return Envelope{}, err
	}
	ct := aead.Seal(nil, nonce, plain, aad(1, keyID))
	return Envelope{Version: 1, KeyID: keyID, Nonce: base64.StdEncoding.EncodeToString(nonce), Ciphertext: base64.StdEncoding.EncodeToString(ct)}, nil
}

func makeAEAD(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("AES-256 key must be 32 bytes")
	}
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}

func aad(version int, keyID string) []byte {
	return []byte(fmt.Sprintf("robot-http-json:%d:%s", version, keyID))
}

func DecodeKeyring(raw string) (Keyring, error) {
	if raw == "" {
		return Keyring{}, nil
	}
	var encoded map[string]string
	if err := json.Unmarshal([]byte(raw), &encoded); err != nil {
		return nil, err
	}
	keys := Keyring{}
	for id, value := range encoded {
		b, err := base64.StdEncoding.DecodeString(value)
		if err != nil || len(b) != 32 {
			return nil, fmt.Errorf("key %q must be base64-encoded 32-byte AES key", id)
		}
		keys[id] = b
	}
	return keys, nil
}

func SealSecret(key []byte, plaintext string) (ciphertext, nonce []byte, err error) {
	aead, err := makeAEAD(key)
	if err != nil {
		return nil, nil, err
	}
	nonce = make([]byte, aead.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, err
	}
	return aead.Seal(nil, nonce, []byte(plaintext), []byte("agent-credential:v1")), nonce, nil
}

func OpenSecret(key, ciphertext, nonce []byte) (string, error) {
	aead, err := makeAEAD(key)
	if err != nil {
		return "", err
	}
	if len(nonce) != aead.NonceSize() {
		return "", errors.New("invalid nonce length")
	}
	plain, err := aead.Open(nil, nonce, ciphertext, []byte("agent-credential:v1"))
	return string(plain), err
}
