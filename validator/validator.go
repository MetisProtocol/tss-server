package validator

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/tendermint/tendermint/libs/log"
)

type Validator struct {
	themisRestUrl string
	logger        log.Logger
}

func New(themisRestUrl string) *Validator {
	logger := log.NewTMLogger(log.NewSyncWriter(os.Stdout)).With("module", "validator")
	return &Validator{themisRestUrl, logger}
}

type MpcPartyID struct {
	ID      string `json:"id,omitempty"`
	Moniker string `json:"moniker,omitempty"`
	Key     string `json:"key,omitempty"`
}

type ResultMpcSet struct {
	Height string       `json:"height,omitempty"`
	Result []MpcPartyID `json:"result,omitempty"`
}

func (v *Validator) getCurrentMpcSet() (*ResultMpcSet, error) {
	url := v.themisRestUrl + "/mpc/set"
	method := "GET"

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	var output ResultMpcSet
	err = json.Unmarshal(body, &output)
	if err != nil {
		return nil, err
	}

	return &output, nil
}

type Sign struct {
	SignID    string `json:"sign_id,omitempty"`
	MpcID     string `json:"mpc_id,omitempty"`
	SignType  int    `json:"sign_type,omitempty"`
	SignData  []byte `json:"sign_data,omitempty"`
	SignMsg   []byte `json:"sign_msg,omitempty"`
	Proposer  string `json:"proposer,omitempty"`
	Signature []byte `json:"signature,omitempty"`
}

type ResultSignInfo struct {
	Height string `json:"height,omitempty"`
	Result Sign   `json:"result,omitempty"`
}

func (v *Validator) getSignInfo(signId string) (*ResultSignInfo, error) {
	url := v.themisRestUrl + "/mpc/sign/" + signId
	method := "GET"

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	var output ResultSignInfo
	err = json.Unmarshal(body, &output)
	if err != nil {
		return nil, err
	}

	return &output, nil
}
