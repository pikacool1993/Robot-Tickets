// Package clientconfig defines the signed binary .cfg interchange format.
package clientconfig

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strings"
	"time"
)

const Magic = "RBTCFG\r\n"
const MaxFileSize = 64 * 1024

type Config struct {
	Version        int       `json:"version"`
	ServerURL      string    `json:"server_url"`
	TransportKeyID string    `json:"transport_key_id"`
	TransportKey   string    `json:"transport_key"`
	ExportID       string    `json:"export_id"`
	IssuedAt       time.Time `json:"issued_at"`
}

type Header struct {
	Version         int    `json:"version"`
	EncryptionKeyID string `json:"encryption_key_id"`
	SigningKeyID    string `json:"signing_key_id"`
	Nonce           string `json:"nonce"`
}

type Exporter struct {
	EncryptionKeyID string
	EncryptionKey   []byte
	SigningKeyID    string
	SigningKey      ed25519.PrivateKey
}

func ValidID(id string) bool {
	if len(id) == 0 || len(id) > 64 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return true
}

func NormalizeServerURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery || (u.Path != "" && u.Path != "/") {
		return "", errors.New("server URL must be an absolute HTTP(S) origin without credentials, query or fragment")
	}
	return strings.TrimRight(u.String(), "/"), nil
}

func (c Config) Validate() error {
	if c.Version != 1 {
		return errors.New("unsupported configuration version")
	}
	if _, err := NormalizeServerURL(c.ServerURL); err != nil {
		return err
	}
	key, err := base64.StdEncoding.DecodeString(c.TransportKey)
	if err != nil || len(key) != 32 || !ValidID(c.TransportKeyID) || c.ExportID == "" || c.IssuedAt.IsZero() {
		return errors.New("invalid configuration fields")
	}
	return nil
}

func (e Exporter) Seal(c Config) ([]byte, error) {
	if len(e.EncryptionKey) != 32 || len(e.SigningKey) != ed25519.PrivateKeySize || !ValidID(e.EncryptionKeyID) || !ValidID(e.SigningKeyID) {
		return nil, errors.New("invalid export keys")
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(e.EncryptionKey)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	header, err := json.Marshal(Header{1, e.EncryptionKeyID, e.SigningKeyID, base64.StdEncoding.EncodeToString(nonce)})
	if err != nil {
		return nil, err
	}
	prefix := append([]byte(Magic), binary.BigEndian.AppendUint32(nil, uint32(len(header)))...)
	prefix = append(prefix, header...)
	plain, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	file := append(prefix, aead.Seal(nil, nonce, plain, prefix)...)
	file = append(file, ed25519.Sign(e.SigningKey, file)...)
	if len(file) > MaxFileSize {
		return nil, errors.New("configuration too large")
	}
	return file, nil
}

// Open verifies the signature against locally trusted keys BEFORE decrypting.
// Header key IDs select trusted keys; a file never supplies its own trust anchor.
func Open(file []byte, encryptionKeys map[string][]byte, signingKeys map[string]ed25519.PublicKey) (Config, error) {
	var c Config
	invalid := errors.New("invalid or untrusted configuration file")
	if len(file) < 12+ed25519.SignatureSize+16 || len(file) > MaxFileSize || !bytes.Equal(file[:8], []byte(Magic)) {
		return c, invalid
	}
	n := int(binary.BigEndian.Uint32(file[8:12]))
	if n < 2 || n > 4096 || 12+n+16+ed25519.SignatureSize > len(file) {
		return c, invalid
	}
	var h Header
	if err := json.Unmarshal(file[12:12+n], &h); err != nil || h.Version != 1 || !ValidID(h.EncryptionKeyID) || !ValidID(h.SigningKeyID) {
		return c, invalid
	}
	pub := signingKeys[h.SigningKeyID]
	end := len(file) - ed25519.SignatureSize
	if len(pub) != ed25519.PublicKeySize || !ed25519.Verify(pub, file[:end], file[end:]) {
		return c, invalid
	}
	key := encryptionKeys[h.EncryptionKeyID]
	if len(key) != 32 {
		return c, invalid
	}
	nonce, err := base64.StdEncoding.DecodeString(h.Nonce)
	if err != nil || len(nonce) != 12 {
		return c, invalid
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return c, invalid
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return c, invalid
	}
	plain, err := aead.Open(nil, nonce, file[12+n:end], file[:12+n])
	if err != nil {
		return c, invalid
	}
	if err := json.Unmarshal(plain, &c); err != nil {
		return Config{}, invalid
	}
	if err := c.Validate(); err != nil {
		return Config{}, invalid
	}
	return c, nil
}
