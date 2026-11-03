// Package clientprotocol exposes the wire contracts shared with local-agent.
package clientprotocol

import (
	"crypto/ed25519"
	"robot/apps/cloud-server/internal/auth"
	"robot/apps/cloud-server/internal/clientconfig"
)

type Config = clientconfig.Config
type Envelope = auth.Envelope
type Keyring = auth.Keyring

const MaxConfigSize = clientconfig.MaxFileSize

func OpenConfig(file []byte, encryptionKeys map[string][]byte, signingKeys map[string]ed25519.PublicKey) (Config, error) {
	return clientconfig.Open(file, encryptionKeys, signingKeys)
}
