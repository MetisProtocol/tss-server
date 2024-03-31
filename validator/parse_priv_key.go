package validator

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"

	"github.com/btcsuite/btcd/btcec/v2"
	ethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/libp2p/go-libp2p/core/crypto"
)

type PrivKeyFile struct {
    Address  string `json:"address"`
    PubKey   Key    `json:"pub_key"`
    PrivKey  Key    `json:"priv_key"`
}

type Key struct {
    Type  string `json:"type"`
    Value string `json:"value"`
}

func loadFilePV(keyFilePath string) (*PrivKeyFile, error) {
    keyJSONBytes, err := ioutil.ReadFile(keyFilePath)
    if err != nil {
        return nil, err
    }

    var pvKey PrivKeyFile
    err = json.Unmarshal(keyJSONBytes, &pvKey)
    if err != nil {
        return nil, fmt.Errorf("error reading PrivValidator key from %v: %v", keyFilePath, err)
    }

    return &pvKey, nil
}

func ParseP2PPrivateKeyFile(p2pPrivateKeyFile string) (crypto.PrivKey, string, error) {
	privVal, err := loadFilePV(p2pPrivateKeyFile)
	if err != nil {
		return nil, "", fmt.Errorf("load private key from file: %v", err)
	}

	privKeyBytes, err := base64.StdEncoding.DecodeString(privVal.PrivKey.Value)
	if err != nil {
		return nil, "", fmt.Errorf("error decoding private key: %v", err)
	}

    privKey, _ := btcec.PrivKeyFromBytes(privKeyBytes[4:])
    pubKey := privKey.PubKey()
    p2pAddress := ethCommon.BytesToAddress(pubKey.SerializeCompressed()).String()
    return (*crypto.Secp256k1PrivateKey)(privKey), p2pAddress, nil
}
