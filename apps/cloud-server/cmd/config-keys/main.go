// config-keys generates independent export keys. Output contains secrets.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log"
)

func main() {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		log.Fatal(err)
	}
	pub, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("# Server .env (keep encryption key and signing seed private)")
	fmt.Println("CLIENT_CONFIG_SERVER_URL=http://localhost:8080")
	fmt.Println("CLIENT_CONFIG_ENCRYPTION_KEY_ID=cfg-aes-v1")
	fmt.Println("CLIENT_CONFIG_ENCRYPTION_KEY=" + base64.StdEncoding.EncodeToString(key))
	fmt.Println("CLIENT_CONFIG_SIGNING_KEY_ID=cfg-sign-v1")
	fmt.Println("CLIENT_CONFIG_SIGNING_SEED=" + base64.StdEncoding.EncodeToString(private.Seed()))
	fmt.Println("# Client trust anchor: embed this PUBLIC key, never the signing seed")
	fmt.Println("# CLIENT_CONFIG_SIGNING_PUBLIC_KEY=" + base64.StdEncoding.EncodeToString(pub))
	fmt.Println("# Client also needs CLIENT_CONFIG_ENCRYPTION_KEY_ID/KEY to import .cfg files")
}
