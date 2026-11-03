package trust

import (
	"crypto/ed25519"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
)

//go:embed assets/*.json
var assets embed.FS

type Keys struct {
	Encryption map[string]string `json:"encryption_keys"`
	Signing    map[string]string `json:"signing_public_keys"`
}

func Load() (map[string][]byte, map[string]ed25519.PublicKey, error) {
	enc := map[string][]byte{}
	sign := map[string]ed25519.PublicKey{}
	files, err := assets.ReadDir("assets")
	if err != nil {
		return nil, nil, err
	}
	for _, f := range files {
		data, err := assets.ReadFile("assets/" + f.Name())
		if err != nil {
			return nil, nil, err
		}
		var keys Keys
		if err := json.Unmarshal(data, &keys); err != nil {
			return nil, nil, err
		}
		for id, value := range keys.Encryption {
			key, err := base64.StdEncoding.DecodeString(value)
			if err != nil || len(key) != 32 {
				return nil, nil, errors.New("invalid embedded encryption key")
			}
			enc[id] = key
		}
		for id, value := range keys.Signing {
			key, err := base64.StdEncoding.DecodeString(value)
			if err != nil || len(key) != ed25519.PublicKeySize {
				return nil, nil, errors.New("invalid embedded signing public key")
			}
			sign[id] = key
		}
	}
	return enc, sign, nil
}
