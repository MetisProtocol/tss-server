package test

import (
	"encoding/base64"
	"testing"
)

func TestSignatureParse(t *testing.T) {
	msg := "MTIzCg=="
	msgV, err := base64.StdEncoding.DecodeString(msg)
	if err != nil {
		panic(err)
	}
	t.Logf("msg value:%x", string(msgV))

	r := "NHfnJT64UPnNE1CSZer+voIP9lQs/f8F08K5Frs7JAM="
	rV, err := base64.StdEncoding.DecodeString(r)
	if err != nil {
		panic(err)
	}
	t.Logf("r value:%x", rV)

	s := "EkHjHWI9rcyMDiB+yoNqnzjeDEJbAflcraRMAd1sktc="
	sV, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		panic(err)
	}
	t.Logf("s value:%x", sV)

	v := "AA=="
	vV, err := base64.StdEncoding.DecodeString(v)
	if err != nil {
		panic(err)
	}
	t.Logf("v value:%x", vV)

	pubkey := "Arg1G+qaRRt0GTsl83HFTKNj+jfDIx4ZQjTuRybeVR7z"
	pV, err := base64.StdEncoding.DecodeString(pubkey)
	if err != nil {
		panic(err)
	}
	t.Logf("pV value:%x", pV)

	sigHash := "HIr/lQaFwu1LwxdPNHIoe1bZUXuclIEnMZoJp6Nt6sg="
	hV, err := base64.StdEncoding.DecodeString(sigHash)
	if err != nil {
		panic(err)
	}
	t.Logf("hV value:%x", hV)
}
