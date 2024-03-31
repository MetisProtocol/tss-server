package tss

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/bnb-chain/tss-lib/v2/ecdsa/keygen"
	tsslib "github.com/bnb-chain/tss-lib/v2/tss"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (m *Manager) KeyGen(ctx context.Context, request *KeyGenRequest) (*KeyGenResponse, error) {
	// create sessionId
	newUUID, err := uuid.NewUUID()
	if err != nil {
		return nil, err
	}
	sessionId := newUUID.String()

	m.logger.Info("start keyGen", "sessionId", sessionId, "keyId", request.KeyId)

	var deadline time.Time
	if d, ok := ctx.Deadline(); ok {
		deadline = d
	} else {
		deadline = time.Now().Add(10 * time.Minute)
	}
	errChan := make(chan error)
	readyP2PStreamChan := make(chan P2PStream)
	for _, pbPartyID := range request.GetAllPartyId() {
		go func(toPartyId *PartyID) {
			p2pStream, err := m.p2pCommunicator.NewStream(ctx, toPartyId.Id, "/tss/keyGen")
			if err != nil {
				errChan <- err
				return
			}
			var isReady = false
			defer func() {
				if !isReady {
					if err := p2pStream.Close(); err != nil {
						m.logger.Error("close p2p stream error", "error", err)
					}
				}
			}()

			// send prepare msg and wait ready msg
			if err := p2pStream.SendMsg(&TssP2PMsg{
				MsgType: &TssP2PMsg_KeyGenPrepareMsg{
					KeyGenPrepareMsg: &KeyGenPrepareMsg{
						SessionId:  sessionId,
						KeyId:      request.KeyId,
						Threshold:  request.Threshold,
						AllPartyId: request.AllPartyId,
						Deadline:   timestamppb.New(deadline),
					},
				},
			}); err != nil {
				errChan <- err
				return
			}

			if p2pMsg, err := p2pStream.RecvMsg(); err != nil {
				errChan <- err
			} else {
				switch p2pMsgType := p2pMsg.MsgType.(type) {
				case *TssP2PMsg_KeyGenReadyMsg:
					isReady = true
					readyP2PStreamChan <- p2pStream
				case *TssP2PMsg_KeyGenFinishMsg:
					errChan <- errors.New(p2pMsgType.KeyGenFinishMsg.FailureReason)
				default:
					errChan <- fmt.Errorf("invalid msg type: %s", p2pMsg.GetMsgType())
				}
			}
		}(pbPartyID)
	}

	// wait for all ready
	readyP2PStreamMap := make(map[string]P2PStream)
	defer func() {
		for _, p2pStream := range readyP2PStreamMap {
			err := p2pStream.(P2PStream).Close()
			if err != nil {
				m.logger.Error("close p2p stream error", "error", err)
			}
		}
	}()
waitForReady:
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case err := <-errChan:
			return nil, err
		case readyP2PStream := <-readyP2PStreamChan:
			readyP2PStreamMap[readyP2PStream.RemotePartyId()] = readyP2PStream
			if len(readyP2PStreamMap) == len(request.GetAllPartyId()) {
				break waitForReady
			}
		}
	}

	finishMsgChan := make(chan *KeyGenFinishMsg)
	for _, p2pStream := range readyP2PStreamMap {
		go func(readyP2PStream P2PStream) {
			if err := readyP2PStream.SendMsg(&TssP2PMsg{
				MsgType: &TssP2PMsg_KeyGenStartMsg{
					KeyGenStartMsg: &KeyGenStartMsg{},
				},
			}); err != nil {
				errChan <- err
				return
			}

			if p2pMsg, err := readyP2PStream.RecvMsg(); err != nil {
				errChan <- err
			} else if finishMsg, ok := p2pMsg.MsgType.(*TssP2PMsg_KeyGenFinishMsg); !ok {
				errChan <- fmt.Errorf("invalid msg type: %s", p2pMsg.GetMsgType())
			} else {
				finishMsgChan <- finishMsg.KeyGenFinishMsg
			}
		}(p2pStream)
	}

	// step 3: wait for finish
	var finishCnt int32
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case err := <-errChan:
			return nil, err
		case finishMsg := <-finishMsgChan:
			if !finishMsg.IsSuccess {
				return nil, errors.New(finishMsg.FailureReason)
			}
			finishCnt++
			if finishCnt == int32(len(request.GetAllPartyId())) {
				// update pubKey field

				return &KeyGenResponse{
					SessionId: sessionId,
					PublicKey: finishMsg.PublicKey,
				}, nil
			}
		}
	}
}

func (m *Manager) handleKeyGen(p2pStream P2PStream) {
	// check msg type
	var keyGenPrepareMsg *KeyGenPrepareMsg
	var readKeyGenPrepareMsgErr error
	if p2pMsg, err := p2pStream.RecvMsg(); err != nil {
		readKeyGenPrepareMsgErr = err
	} else {
		keyGenPrepareMsg = p2pMsg.GetKeyGenPrepareMsg()
		if keyGenPrepareMsg == nil {
			readKeyGenPrepareMsgErr = fmt.Errorf("invalid msg type: %s", p2pMsg.GetMsgType())
		}
	}
	if readKeyGenPrepareMsgErr != nil {
		if err := p2pStream.SendMsg(&TssP2PMsg{
			MsgType: &TssP2PMsg_KeyGenFinishMsg{
				KeyGenFinishMsg: &KeyGenFinishMsg{
					IsSuccess:     false,
					FailureReason: readKeyGenPrepareMsgErr.Error(),
				},
			},
		}); err != nil {
			m.logger.Error("send msg error", "error", err)
		}
		return
	}

	ctx, cancel := context.WithDeadline(context.Background(), keyGenPrepareMsg.GetDeadline().AsTime())
	defer cancel()

	p2pProcessMsgChan := make(chan *TssP2PMsg)
	p2pSessionProtocolId := fmt.Sprintf("/tss/keyGen/session/%s", keyGenPrepareMsg.SessionId)
	m.p2pCommunicator.SetStreamHandler(p2pSessionProtocolId, func(p2pSessionStream P2PStream) {
		defer func() {
			if err := p2pSessionStream.Close(); err != nil {
				m.logger.Error("close p2p stream failed", "error", err)
			}
		}()
		p2pMsg, err := p2pSessionStream.RecvMsg()
		if err != nil {
			m.logger.Error("recv msg failed", "error", err)
			return
		}
		p2pProcessMsgChan <- p2pMsg
	})
	defer m.p2pCommunicator.RemoveStreamHandler(p2pSessionProtocolId)

	if finishMsg, keyGen0Err := m.handleKeyGen0(ctx, p2pStream, keyGenPrepareMsg, p2pProcessMsgChan, p2pSessionProtocolId); keyGen0Err != nil {
		m.logger.Error("handleKeyGen0 failed",
			"sessionId", keyGenPrepareMsg.SessionId,
			"keyId", keyGenPrepareMsg.KeyId,
			"err", keyGen0Err)

		if err := m.compareAndSetKeyStatusStorage(
			keyGenPrepareMsg.KeyId,
			keyGenPrepareMsg.SessionId,
			nil,
			KeyStatus_KEY_STATUS_PENDING,
			KeyStatus_KEY_STATUS_ERROR,
			"",
			timestamppb.Now()); err != nil {
			m.logger.Error("compareAndSetKeyStatusStorage failed",
				"sessionId", keyGenPrepareMsg.SessionId,
				"keyId", keyGenPrepareMsg.KeyId,
				"error", err)
		}
		for _, pbPartyID := range keyGenPrepareMsg.AllPartyId {
			if pbPartyID.GetId() != m.p2pCommunicator.LocalPartyId().GetId() {
				if p2pSessionStream, err := m.p2pCommunicator.NewStream(ctx, pbPartyID.Id, p2pSessionProtocolId); err != nil {
					m.logger.Error("new p2p session stream failed", "error", err)
				} else {
					if err := p2pSessionStream.SendMsg(&TssP2PMsg{
						MsgType: &TssP2PMsg_ProcessErrorMsg{
							ProcessErrorMsg: &ProcessErrorMsg{
								ErrorMsg: keyGen0Err.Error(),
							},
						},
					}); err != nil {
						m.logger.Error("keyGen send p2p ProcessErrorMsg msg fail", "error", err)
					} else {
						if err := p2pSessionStream.Close(); err != nil {
							m.logger.Error("p2p stream close fail", "error", err)
						}
					}
				}
			}
		}

		if err := p2pStream.SendMsg(&TssP2PMsg{
			MsgType: &TssP2PMsg_KeyGenFinishMsg{
				KeyGenFinishMsg: &KeyGenFinishMsg{
					IsSuccess:     false,
					FailureReason: keyGen0Err.Error(),
				},
			},
		}); err != nil {
			m.logger.Error("send msg error", "error", err)
		}
	} else {
		if err := p2pStream.SendMsg(&TssP2PMsg{
			MsgType: &TssP2PMsg_KeyGenFinishMsg{
				KeyGenFinishMsg: finishMsg,
			},
		}); err != nil {
			m.logger.Error("send msg error", "error", err)
		}
	}
}

func (m *Manager) handleKeyGen0(
	ctx context.Context,
	p2pStream P2PStream,
	keyGenPrepareMsg *KeyGenPrepareMsg,
	p2pProcessMsgChan <-chan *TssP2PMsg,
	p2pProcessProtocolId string,
) (*KeyGenFinishMsg, error) {
	m.logger.Info("handleKeyGen0 start",
		"sessionId", keyGenPrepareMsg.SessionId,
		"keyId", keyGenPrepareMsg.KeyId)
	defer func() {
		m.logger.Info("handleKeyGen0 finished",
			"sessionId", keyGenPrepareMsg.SessionId,
			"keyId", keyGenPrepareMsg.KeyId)
	}()

	// step 1: check and set key gen storage
	createdTime := timestamppb.Now()
	if err := m.checkAndSetKeyGenStorage(
		keyGenPrepareMsg.KeyId,
		keyGenPrepareMsg.SessionId,
		keyGenPrepareMsg.Threshold,
		keyGenPrepareMsg.AllPartyId,
		createdTime); err != nil {
		return nil, err
	}

	if !m.dev {
		if err := m.validator.KeyGenValidate(
			keyGenPrepareMsg.KeyId,
			keyGenPrepareMsg.Threshold,
			keyGenPrepareMsg.AllPartyId); err != nil {
			return nil, err
		}
	}

	if err := p2pStream.SendMsg(&TssP2PMsg{
		MsgType: &TssP2PMsg_KeyGenReadyMsg{
			KeyGenReadyMsg: &KeyGenReadyMsg{},
		},
	}); err != nil {
		m.logger.Error("send msg error", "error", err)
		return nil, err
	}

	var keyGenStartMsg *KeyGenStartMsg
	if p2pMsg, err := p2pStream.RecvMsg(); err != nil {
		return nil, err
	} else {
		keyGenStartMsg = p2pMsg.GetKeyGenStartMsg()
		if keyGenStartMsg == nil {
			return nil, fmt.Errorf("invalid msg type: %s", p2pMsg.GetMsgType())
		}
	}

	// step 2: create local party and start
	tssPreParams, _ := keygen.GeneratePreParams(1 * time.Minute)
	var tssAllPartyIDs = make(tsslib.UnSortedPartyIDs, len(keyGenPrepareMsg.GetAllPartyId()))
	for i, pbPartyID := range keyGenPrepareMsg.GetAllPartyId() {
		tssAllPartyIDs[i] = convertToTssPartyId(pbPartyID)
	}

	tssAllSortedPartyIDs := tsslib.SortPartyIDs(tssAllPartyIDs)
	idToTssSortedPartyIdMap := make(map[string]*tsslib.PartyID)
	for _, tssSortedPartyID := range tssAllSortedPartyIDs {
		idToTssSortedPartyIdMap[tssSortedPartyID.Id] = tssSortedPartyID
	}

	tssLocalSortedPartyID := idToTssSortedPartyIdMap[m.p2pCommunicator.LocalPartyId().GetId()]
	if tssLocalSortedPartyID == nil {
		return nil, errors.New("localPartyID not found")
	}

	tssPeerCtx := tsslib.NewPeerContext(tssAllSortedPartyIDs)
	tssParams := tsslib.NewParameters(
		btcec.S256(),
		tssPeerCtx,
		tssLocalSortedPartyID,
		len(tssAllSortedPartyIDs),
		int(keyGenPrepareMsg.Threshold))

	outCh := make(chan tsslib.Message)
	endCh := make(chan *keygen.LocalPartySaveData)
	tssLocalParty := keygen.NewLocalParty(
		tssParams,
		outCh,
		endCh,
		*tssPreParams).(*keygen.LocalParty)

	errCh := make(chan error)
	go func() {
		if err := tssLocalParty.Start(); err != nil {
			errCh <- err
		}
	}()

	// step 3: handle event loop
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case p2pProcessMsg := <-p2pProcessMsgChan:
			switch p2pMsgType := p2pProcessMsg.MsgType.(type) {
			case *TssP2PMsg_ProcessUpdateMsg:
				updateMsg := p2pMsgType.ProcessUpdateMsg

				fromSortedPartyID := idToTssSortedPartyIdMap[updateMsg.GetFromPartyId()]
				if fromSortedPartyID == nil {
					return nil, errors.New("fromPartyID not found in sortedPartyId")
				}

				msg, err := tsslib.ParseWireMessage(
					updateMsg.MsgWireBytes,
					fromSortedPartyID,
					updateMsg.IsBroadcast)
				if err != nil {
					return nil, err
				}
				go func() {
					if _, err := tssLocalParty.Update(msg); err != nil {
						errCh <- err
					}
				}()
			case *TssP2PMsg_ProcessErrorMsg:
				// save error status
				if err := m.compareAndSetKeyStatusStorage(
					keyGenPrepareMsg.KeyId,
					keyGenPrepareMsg.SessionId,
					nil,
					KeyStatus_KEY_STATUS_PENDING,
					KeyStatus_KEY_STATUS_ERROR,
					"",
					timestamppb.Now()); err != nil {

					m.logger.Error("compareAndSetKeyStatusStorage started",
						"sessionId", keyGenPrepareMsg.SessionId,
						"keyId", keyGenPrepareMsg.KeyId,
						"error", err)
				}

				return &KeyGenFinishMsg{
					IsSuccess:     false,
					FailureReason: p2pMsgType.ProcessErrorMsg.ErrorMsg,
				}, nil
			default:
				return nil, errors.New("invalid p2pMsgType")
			}
		case msg := <-outCh:
			msgWireBytes, _, err := msg.WireBytes()
			if err != nil {
				return nil, err
			}

			toPbPartyIds := make([]*PartyID, 0, len(keyGenPrepareMsg.GetAllPartyId()))
			if msg.GetTo() == nil {
				// broadcast
				for _, p := range keyGenPrepareMsg.GetAllPartyId() {
					if p.GetId() != m.p2pCommunicator.LocalPartyId().GetId() {
						toPbPartyIds = append(toPbPartyIds, p)
					}
				}
			} else {
				for _, p := range msg.GetTo() {
					toPbPartyIds = append(toPbPartyIds, convertToPbPartyId(p))
				}
			}
			for _, toPbPartyId := range toPbPartyIds {
				p2pSessionStream, err := m.p2pCommunicator.NewStream(ctx, toPbPartyId.Id, p2pProcessProtocolId)
				if err != nil {
					return nil, err
				}
				if err := p2pSessionStream.SendMsg(&TssP2PMsg{
					MsgType: &TssP2PMsg_ProcessUpdateMsg{
						ProcessUpdateMsg: &ProcessUpdateMsg{
							FromPartyId:  m.p2pCommunicator.LocalPartyId().GetId(),
							MsgWireBytes: msgWireBytes,
							IsBroadcast:  msg.GetTo() == nil,
						},
					},
				}); err != nil {
					return nil, err
				}
				if err := p2pSessionStream.Close(); err != nil {
					m.logger.Error("keyGen send p2p ProcessUpdateMsg msg fail", "error", err)
					return nil, err
				}
			}
		case tssLocalPartySaveData := <-endCh:
			saveDataJson, err := json.Marshal(tssLocalPartySaveData)
			if err != nil {
				return nil, err
			}

			pubKey, err := m.ConvertECDSAPubKeyToBTCECPubKey(tssLocalPartySaveData.ECDSAPub.ToECDSAPubKey())
			if err != nil {
				return nil, err
			}

			if err := m.compareAndSetKeyStatusStorage(
				keyGenPrepareMsg.KeyId,
				keyGenPrepareMsg.SessionId,
				pubKey.SerializeCompressed(),
				KeyStatus_KEY_STATUS_PENDING,
				KeyStatus_KEY_STATUS_READY,
				string(saveDataJson),
				timestamppb.Now()); err != nil {
				return nil, err
			}

			return &KeyGenFinishMsg{
				IsSuccess: true,
				PublicKey: pubKey.SerializeCompressed(),
			}, nil
		case err := <-errCh:
			return nil, err
		}
	}
}

func (m *Manager) ConvertECDSAPubKeyToBTCECPubKey(ecdsaPubKey *ecdsa.PublicKey) (*btcec.PublicKey, error) {
    // Serialize the ECDSA public key to a compressed format (33 bytes).
    serializedPubKey := elliptic.MarshalCompressed(ecdsaPubKey.Curve, ecdsaPubKey.X, ecdsaPubKey.Y)

    // Parse the serialized public key into a btcec.PublicKey.
    btcecPubKey, err := btcec.ParsePubKey(serializedPubKey)
    if err != nil {
        return nil, err
    }

    return btcecPubKey, nil
}
