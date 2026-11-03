// generate-keys generates independent transport and credential encryption keys.
// Output contains secrets intended for the server .env file.
package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
)

func newKey() string {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		log.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(key)
}

func main() {
	clientKey, credentialKey := newKey(), newKey()
	transportKeys, err := json.Marshal(map[string]string{"client-v1": clientKey})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("# Copy these lines into apps/cloud-server/.env")
	fmt.Println("TRANSPORT_KEYS_JSON=" + string(transportKeys))
	fmt.Println("AGENT_TRANSPORT_KEY_ID=client-v1")
	fmt.Println("AGENT_CREDENTIAL_ENCRYPTION_KEY=" + credentialKey)
	fmt.Println("AGENT_CREDENTIAL_KEY_VERSION=v1")
	fmt.Println()
	fmt.Println("# Keep these values private and do not commit .env")
}
