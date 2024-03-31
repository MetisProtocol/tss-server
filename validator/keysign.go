package validator

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
)

func (v *Validator) KeySignValidate(signId, keyId string, signMsg []byte) ([]byte, error) {
	fmt.Printf("KeySignValidate signId:%v,keyId:%v\n", signId, keyId)

	signInfo, err := v.getSignInfo(signId)
	if err != nil {
		fmt.Println("KeySignValidate get sign info err:", err)
		return nil, err
	}

	if len(signInfo.Result.Signature) > 0 {
		fmt.Println("KeySignValidate signature already generated")
		return signInfo.Result.Signature, nil
	}

	if signInfo.Result.MpcID != keyId {
		fmt.Println("KeySignValidate key id mismatch", "chainSignMpcId", signInfo.Result.MpcID, "validateMpcId", keyId)
		return nil, errors.New("KeySignValidate key id mismatch")
	}

	if !bytes.EqualFold(signInfo.Result.SignMsg, signMsg) {
		fmt.Println("KeySignValidate sign msg mismatch", "chainSignMsg", hex.EncodeToString(signInfo.Result.SignMsg), "validateMsg", hex.EncodeToString(signMsg))
		return nil, errors.New("KeySignValidate sign msg mismatch")
	}

	fmt.Println("KeySignValidate success")
	return nil, nil
}
