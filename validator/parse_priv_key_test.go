package validator

import "testing"

func TestParsePrivKey(t *testing.T) {
	pri, address, err := ParseP2PPrivateKeyFile("./priv_validator_key.json")
	t.Logf("pri:%v", pri)
	t.Logf("address:%v", address)
	t.Logf("err:%v", err)
}
