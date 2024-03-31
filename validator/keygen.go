package validator

import (
	"errors"
	"fmt"
	"strings"

	"github.com/metis-protocol/tss-server/tss"
)

func (v *Validator) KeyGenValidate(keyId string, threshold int32, allPartyId []*tss.PartyID) error {
	outputs, err := v.getCurrentMpcSet()
	if err != nil {
		v.logger.Error("key gen validator", "err", err)
		return err
	}

	valAddressesMap := make(map[string]struct{})
	for _, val := range outputs.Result {
		valAddressesMap[strings.ToLower(val.Moniker)] = struct{}{}
		v.logger.Info("KeyGenValidate val signer", "signer", val.Moniker)
	}

	for _, partyId := range allPartyId {
		fmt.Println("party address:", partyId.Moniker)
		if _, exist := valAddressesMap[strings.ToLower(partyId.Moniker)]; !exist {
			return errors.New("not a validator party")
		}
	}
	return nil
}
