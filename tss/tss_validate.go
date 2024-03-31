package tss

type Validator interface {
	KeyGenValidate(keyId string, threshold int32, allPartyId []*PartyID) error
	KeySignValidate(signId, keyId string, signMsg []byte) ([]byte, error)
}
