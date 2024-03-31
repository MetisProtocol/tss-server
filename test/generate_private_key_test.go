package test

import (
	"encoding/base64"
	"testing"

	"github.com/libp2p/go-libp2p/core/crypto"
)

func TestGeneratePriKey(t *testing.T) {
	priKey, _, err := crypto.GenerateSecp256k1Key(nil)
	if err != nil {
		t.Fatal(err)
	}

	// print base64 private key
	raw, err := priKey.Raw()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("priKey: %s\n", base64.URLEncoding.EncodeToString(raw))
}
