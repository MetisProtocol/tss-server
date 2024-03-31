package tss

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"math/big"
	"sync"

	tsslib "github.com/bnb-chain/tss-lib/v2/tss"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/tendermint/tendermint/libs/log"
	codes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Manager struct {
	UnimplementedTssServiceServer
	logger          log.Logger
	localStorage    LocalStorage
	p2pCommunicator P2PCommunicator
	validator       Validator
	dev             bool
	lock            *sync.Mutex
}

func NewManager(
	logger log.Logger,
	localStorage LocalStorage,
	p2pCommunicator P2PCommunicator,
	validator Validator,
	dev bool,
) *Manager {
	return &Manager{
		logger:          logger,
		localStorage:    localStorage,
		p2pCommunicator: p2pCommunicator,
		validator:       validator,
		dev:             dev,
		lock:            &sync.Mutex{},
	}
}

func (m *Manager) Start() {
	m.p2pCommunicator.SetStreamHandler("/tss/keyGen", m.handleKeyGen)
	m.p2pCommunicator.SetStreamHandler("/tss/keySign", m.handleKeySign)
}

func (m *Manager) Stop() {
	m.p2pCommunicator.RemoveStreamHandler("/tss/keySign")
	m.p2pCommunicator.RemoveStreamHandler("/tss/keyGen")
}

func (m *Manager) GetLocalPartyId(context.Context, *GetLocalPartyIdRequest) (*GetLocalPartyIdResponse, error) {
	return &GetLocalPartyIdResponse{
		PartyId: m.p2pCommunicator.LocalPartyId(),
	}, nil
}

func (m *Manager) GetKey(ctx context.Context, request *GetKeyRequest) (*GetKeyResponse, error) {
	keyData, err := m.getLatestReadyKeyDataByKeyIdFromStorage(request.KeyId)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, err.Error())
	}

	if keyData == nil {
		return nil, status.Errorf(codes.NotFound, "key data is nil")
	}

	return &GetKeyResponse{
		KeyId:      request.KeyId,
		Threshold:  keyData.Threshold,
		AllPartyId: keyData.AllPartyId,
		PublicKey:  keyData.Pubkey,
		Status:     keyData.Status,
	}, nil
}

func (m *Manager) VerifySignature(ctx context.Context, request *VerifySignatureRequest) (*VerifySignatureResponse, error) {
	pubKey, err := btcec.ParsePubKey(request.PublicKey)
	if err != nil {
		return nil, status.Errorf(codes.Internal, err.Error())
	}
	isValid := ecdsa.Verify(
		pubKey.ToECDSA(),
		request.GetSignMsg(),
		new(big.Int).SetBytes(request.GetSignatureR()),
		new(big.Int).SetBytes(request.GetSignatureS()))
	return &VerifySignatureResponse{
		IsValid: isValid,
	}, nil
}

func (m *Manager) checkAndSetKeyGenStorage(
	keyId string,
	sessionId string,
	threshold int32,
	allPartyId []*PartyID,
	createdTime *timestamppb.Timestamp,
) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	keyDataList, err := m.localStorage.GetKeyDataListByKeyId(keyId)
	if err != nil {
		return err
	}

	for _, d := range keyDataList {
		if d.Status == KeyStatus_KEY_STATUS_READY {
			return errors.New("key already exist")
		} else if d.Status == KeyStatus_KEY_STATUS_PENDING {
			return errors.New("key is generating, please do not repeat")
		}
	}

	if err := m.localStorage.SetKeyData(&KeyData{
		KeyId:        keyId,
		KeySessionId: sessionId,
		Threshold:    threshold,
		AllPartyId:   allPartyId,
		Status:       KeyStatus_KEY_STATUS_PENDING,
		SaveDataJson: "",
		CreatedTime:  createdTime,
		UpdatedTime:  createdTime,
	}); err != nil {
		return err
	}
	return nil
}

func (m *Manager) checkAndSetKeyResharingStorage(
	keyId string,
	sessionId string,
	threshold int32,
	allPartyId []*PartyID,
	createdTime *timestamppb.Timestamp,
) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	keyDataList, err := m.localStorage.GetKeyDataListByKeyId(keyId)
	if err != nil {
		return err
	}

	for _, d := range keyDataList {
		if d.KeySessionId == sessionId {
			return errors.New("key already exist")
		}
	}

	if err := m.localStorage.SetKeyData(&KeyData{
		KeyId:        keyId,
		KeySessionId: sessionId,
		Threshold:    threshold,
		AllPartyId:   allPartyId,
		Status:       KeyStatus_KEY_STATUS_PENDING,
		SaveDataJson: "",
		CreatedTime:  createdTime,
		UpdatedTime:  createdTime,
	}); err != nil {
		return err
	}
	return nil
}

func (m *Manager) getLatestReadyKeyDataByKeyIdFromStorage(
	keyId string,
) (*KeyData, error) {
	m.lock.Lock()
	defer m.lock.Unlock()

	keyDataList, err := m.localStorage.GetKeyDataListByKeyId(keyId)
	if err != nil {
		return nil, err
	}
	var latestKeyData *KeyData
	for _, d := range keyDataList {
		if d.Status != KeyStatus_KEY_STATUS_READY {
			continue
		}
		if latestKeyData == nil || d.CreatedTime.AsTime().After(latestKeyData.CreatedTime.AsTime()) {
			latestKeyData = d
		}
	}
	return latestKeyData, nil
}

func (m *Manager) getKeyDataByKeyIdAndKeySessionIdFromStorage(
	keyId string,
	keySessionId string,
) (*KeyData, error) {
	m.lock.Lock()
	defer m.lock.Unlock()
	return m.localStorage.GetKeyDataByKeyIdAndKeySessionId(keyId, keySessionId)
}

func (m *Manager) compareAndSetKeyStatusStorage(
	keyId string,
	keySessionId string,
	pubKey []byte,
	expectStatus KeyStatus,
	updateStatus KeyStatus,
	updateSavedDataJson string,
	updatedTime *timestamppb.Timestamp,
) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	keyData, err := m.localStorage.GetKeyDataByKeyIdAndKeySessionId(keyId, keySessionId)
	if err != nil {
		return err
	}
	if keyData == nil {
		return errors.New("key data not found")
	}
	if keyData.Status != expectStatus {
		return errors.New("key data status not equal expect status")
	}
	keyData.Status = updateStatus
	keyData.SaveDataJson = updateSavedDataJson
	keyData.UpdatedTime = updatedTime
	keyData.Pubkey = pubKey
	if err := m.localStorage.SetKeyData(keyData); err != nil {
		return err
	}
	return nil
}

func convertToTssPartyId(pbPartyID *PartyID) *tsslib.PartyID {
	return tsslib.NewPartyID(pbPartyID.Id, pbPartyID.Moniker, new(big.Int).SetBytes(pbPartyID.Key))
}

func convertToPbPartyId(tssPartyId *tsslib.PartyID) *PartyID {
	return &PartyID{
		Id:      tssPartyId.GetId(),
		Moniker: tssPartyId.GetMoniker(),
		Key:     tssPartyId.GetKey(),
	}
}
