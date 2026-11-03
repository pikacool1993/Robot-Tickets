// prepare-trust embeds only the .cfg decryption key and signature PUBLIC key.
package main

import (
	"bufio"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	path := flag.String("env", "../cloud-server/.env", "server environment file")
	flag.Parse()
	f, err := os.Open(*path)
	if err != nil {
		log.Fatal("cannot open environment file")
	}
	defer f.Close()
	values := map[string]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, ok := strings.Cut(line, "=")
		if ok {
			values[strings.TrimSpace(name)] = strings.Trim(strings.TrimSpace(value), "\"'")
		}
	}
	if scanner.Err() != nil {
		log.Fatal("cannot read environment file")
	}
	encID, signID := values["CLIENT_CONFIG_ENCRYPTION_KEY_ID"], values["CLIENT_CONFIG_SIGNING_KEY_ID"]
	key, err := base64.StdEncoding.DecodeString(values["CLIENT_CONFIG_ENCRYPTION_KEY"])
	if err != nil || len(key) != 32 || encID == "" {
		log.Fatal("missing or invalid CLIENT_CONFIG_ENCRYPTION_KEY_ID/KEY")
	}
	seed, err := base64.StdEncoding.DecodeString(values["CLIENT_CONFIG_SIGNING_SEED"])
	if err != nil || len(seed) != 32 || signID == "" {
		log.Fatal("missing or invalid CLIENT_CONFIG_SIGNING_KEY_ID/SEED")
	}
	pub := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	output := filepath.Join("internal", "trust", "assets", "local.json")
	// Merge to retain older trusted keys during rotation; never embed private seeds.
	keys := struct {
		Encryption map[string]string `json:"encryption_keys"`
		Signing    map[string]string `json:"signing_public_keys"`
	}{map[string]string{}, map[string]string{}}
	if data, err := os.ReadFile(output); err == nil {
		if json.Unmarshal(data, &keys) != nil {
			log.Fatal("existing trust file is invalid")
		}
	}
	if keys.Encryption == nil {
		keys.Encryption = map[string]string{}
	}
	if keys.Signing == nil {
		keys.Signing = map[string]string{}
	}
	keys.Encryption[encID] = base64.StdEncoding.EncodeToString(key)
	keys.Signing[signID] = base64.StdEncoding.EncodeToString(pub)
	data, err := json.MarshalIndent(keys, "", "  ")
	if err != nil {
		log.Fatal("cannot encode trust file")
	}
	if err := os.WriteFile(output, data, 0600); err != nil {
		log.Fatal("cannot write trust file")
	}
	fmt.Println("Prepared ignored local trust file; no signing private key included. Rebuild the client to apply.")
}
