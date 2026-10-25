// Command updatesign makes and uses the key that signs release checksum
// lists, which the app checks before installing an update (see package
// updater).
//
//	go run ./cmd/updatesign keygen
//	    prints a new key pair: the private key goes into the repository
//	    secret UPDATE_SIGNING_KEY, the public one into the repository
//	    variable UPDATE_PUBLIC_KEY.
//	UPDATE_SIGNING_KEY=... go run ./cmd/updatesign sign SHA256SUMS.txt
//	    writes SHA256SUMS.txt.sig (base64 Ed25519 signature).
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "updatesign:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: updatesign keygen | updatesign sign FILE")
	}
	switch args[0] {
	case "keygen":
		public, private, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return err
		}
		fmt.Println("UPDATE_SIGNING_KEY (secret):  ", base64.StdEncoding.EncodeToString(private.Seed()))
		fmt.Println("UPDATE_PUBLIC_KEY (variable): ", base64.StdEncoding.EncodeToString(public))
		return nil

	case "sign":
		if len(args) != 2 {
			return fmt.Errorf("usage: updatesign sign FILE")
		}
		seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(os.Getenv("UPDATE_SIGNING_KEY")))
		if err != nil || len(seed) != ed25519.SeedSize {
			return fmt.Errorf("UPDATE_SIGNING_KEY is not a base64 Ed25519 key from `updatesign keygen`")
		}
		data, err := os.ReadFile(args[1])
		if err != nil {
			return err
		}
		signature := ed25519.Sign(ed25519.NewKeyFromSeed(seed), data)
		return os.WriteFile(args[1]+".sig", []byte(base64.StdEncoding.EncodeToString(signature)+"\n"), 0o644)
	}
	return fmt.Errorf("unknown command %q", args[0])
}
