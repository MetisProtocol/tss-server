package tss

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	sync "sync"
	"time"

	"github.com/bnb-chain/tss-lib/v2/common"
	"github.com/bnb-chain/tss-lib/v2/ecdsa/keygen"
	"github.com/bnb-chain/tss-lib/v2/ecdsa/signing"
	tsslib "github.com/bnb-chain/tss-lib/v2/tss"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/google/uuid"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (m *Manager) KeySign(ctx context.Context, request *KeySignRequest) (*KeySignResponse, error) {
	// create sessionId
	newUUID, err := uuid.NewUUID()
	if err != nil {
		return nil, err
	}
	sessionId := newUUID.String()

	m.logger.Info("[keySign] start",
		"signId", request.SignId,
		"keyId", request.KeyId,
		"sessionId", sessionId)

	// get key from local storage
	keyData, err := m.getLatestReadyKeyDataByKeyIdFromStorage(request.KeyId)
	if err != nil {
		return nil, err
	}
	if keyData == nil {
		return nil, errors.New("key not found from local storage")
	}

	var deadline time.Time
	if d, ok := ctx.Deadline(); ok {
		deadline = d
	} else {
		deadline = time.Now().Add(10 * time.Minute)
	}
	partyCount := len(keyData.GetAllPartyId())
	errChan := make(chan error, partyCount)
	readyP2PStreamChan := make(chan P2PStream, partyCount)
	var wgNew sync.WaitGroup

	for _, pbPartyID := range keyData.GetAllPartyId() {
		wgNew.Add(1)
		go func(toPartyId *PartyID) {
			defer wgNew.Done()
			select {
			case <-ctx.Done():
				return
			default:
				p2pStream, err := m.p2pCommunicator.NewStream(ctx, toPartyId.Id, "/tss/keySign")
				if err != nil {
					select {
					case errChan <- err:
					case <-ctx.Done():
					}

					m.logger.Error("[keySign] connect p2p stream fail",
						"signId", request.SignId,
						"keyId", request.KeyId,
						"sessionId", sessionId,
						"remotePartyId", toPartyId.Id,
						"error", err)
					return
				}

				m.logger.Info("[keySign] connect p2p stream succ",
					"signId", request.SignId,
					"keyId", request.KeyId,
					"sessionId", sessionId,
					"remotePartyId", p2pStream.RemotePartyId(),
					"p2pStreamId", p2pStream.ID())

				var isReady = false
				defer func() {
					if !isReady {
						if err := p2pStream.Close(); err != nil {
							m.logger.Error("[keySign] close p2p stream fail",
								"signId", request.SignId,
								"keyId", request.KeyId,
								"sessionId", sessionId,
								"remotePartyId", p2pStream.RemotePartyId(),
								"p2pStreamId", p2pStream.ID(),
								"error", err)
						}
					}
				}()

				// send prepare msg and wait ready msg
				if err := p2pStream.SendMsg(&TssP2PMsg{
					MsgType: &TssP2PMsg_KeySignPrepareMsg{
						KeySignPrepareMsg: &KeySignPrepareMsg{
							SignId:       request.SignId,
							SessionId:    sessionId,
							KeyId:        request.KeyId,
							KeySessionId: keyData.KeySessionId,
							SignMsg:      request.SignMsg,
							Deadline:     timestamppb.New(deadline),
						},
					},
				}); err != nil {
					m.logger.Error("[keySign] send p2p prepare msg fail",
						"signId", request.SignId,
						"keyId", request.KeyId,
						"sessionId", sessionId,
						"remotePartyId", p2pStream.RemotePartyId(),
						"p2pStreamId", p2pStream.ID(),
						"error", err)

					errChan <- err
					return
				}

				m.logger.Info("[keySign] send p2p prepare msg succ",
					"signId", request.SignId,
					"keyId", request.KeyId,
					"sessionId", sessionId,
					"remotePartyId", p2pStream.RemotePartyId(),
					"p2pStreamId", p2pStream.ID())

				if p2pMsg, err := p2pStream.RecvMsg(); err != nil {
					errChan <- err

					m.logger.Error("[keySign] recv p2p ready msg fail",
						"signId", request.SignId,
						"keyId", request.KeyId,
						"sessionId", sessionId,
						"remotePartyId", p2pStream.RemotePartyId(),
						"p2pStreamId", p2pStream.ID(),
						"error", err)
				} else {
					switch p2pMsgType := p2pMsg.MsgType.(type) {
					case *TssP2PMsg_KeySignReadyMsg:
						isReady = true
						select {
						case readyP2PStreamChan <- p2pStream:
						case <-ctx.Done():
						}

						m.logger.Info("[keySign] recv p2p ready msg succ",
							"signId", request.SignId,
							"keyId", request.KeyId,
							"sessionId", sessionId,
							"remotePartyId", p2pStream.RemotePartyId(),
							"p2pStreamId", p2pStream.ID())
					case *TssP2PMsg_KeySignFinishMsg:
						select {
						case errChan <- errors.New(p2pMsgType.KeySignFinishMsg.FailureReason):
						case <-ctx.Done():
						}

						m.logger.Error("[keySign] recv p2p ready msg fail for finish msg",
							"signId", request.SignId,
							"keyId", request.KeyId,
							"sessionId", sessionId,
							"remotePartyId", p2pStream.RemotePartyId(),
							"p2pStreamId", p2pStream.ID(),
							"error", p2pMsgType.KeySignFinishMsg.FailureReason)
					default:
						errChan <- fmt.Errorf("invalid msg type: %s", p2pMsg.GetMsgType())

						m.logger.Error("[keySign] recv p2p ready msg fail for invalid msg type",
							"signId", request.SignId,
							"keyId", request.KeyId,
							"sessionId", sessionId,
							"remotePartyId", p2pStream.RemotePartyId(),
							"p2pStreamId", p2pStream.ID(),
							"error", fmt.Sprintf("invalid msg type: %s", p2pMsg.GetMsgType()))
					}
				}
			}
		}(pbPartyID)
	}

	wgNew.Wait()

	defer func() {
		close(errChan)
		close(readyP2PStreamChan)
		m.logger.Info("[keySign] close err and p2p chans",
			"signId", request.SignId,
			"keyId", request.KeyId,
			"sessionId", sessionId)
	}()

	// wait for all ready
	readyP2PStreamMap := make(map[string]P2PStream)

waitForReady:
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case err := <-errChan:
			return nil, err
		case readyP2PStream, ok := <-readyP2PStreamChan:
			if !ok {
				return nil, fmt.Errorf("readyP2PStream closed")
			}
			readyP2PStreamMap[readyP2PStream.RemotePartyId()] = readyP2PStream
			if int32(len(readyP2PStreamMap)) > keyData.Threshold {
				break waitForReady
			}
		}
	}

	// cancel other party
	if len(readyP2PStreamMap) < len(keyData.AllPartyId) {
		go func(cnt int32) {
			for {
				select {
				case <-ctx.Done():
					return
				case readyP2PStream, ok := <-readyP2PStreamChan:
					if !ok {
						return
					}
					cnt++
					_ = readyP2PStream.SendMsg(&TssP2PMsg{
						MsgType: &TssP2PMsg_KeySignCancelMsg{
							KeySignCancelMsg: &KeySignCancelMsg{},
						},
					})
					_ = readyP2PStream.Close()
					if cnt == int32(len(keyData.AllPartyId)) {
						return
					}
				}
			}
		}(int32(len(readyP2PStreamMap)))
	}

	var signPartyIds = make([]*PartyID, 0, len(readyP2PStreamMap))
	for _, pbPartyID := range keyData.GetAllPartyId() {
		if _, ok := readyP2PStreamMap[pbPartyID.GetId()]; ok {
			signPartyIds = append(signPartyIds, pbPartyID)
		}
	}

	var wg sync.WaitGroup
	finishMsgChan := make(chan *KeySignFinishMsg, len(readyP2PStreamMap))
	for _, p2pStream := range readyP2PStreamMap {
		wg.Add(1)
		go func(readyP2PStream P2PStream) {
			defer wg.Done()
			select {
			case <-ctx.Done():
				return
			default:
				if err := readyP2PStream.SendMsg(&TssP2PMsg{
					MsgType: &TssP2PMsg_KeySignStartMsg{
						KeySignStartMsg: &KeySignStartMsg{
							SignPartyId: signPartyIds,
						},
					},
				}); err != nil {
					select {
					case errChan <- err:
					default:
					}

					m.logger.Error("[keySign] send p2p start msg fail",
						"signId", request.SignId,
						"keyId", request.KeyId,
						"sessionId", sessionId,
						"remotePartyId", readyP2PStream.RemotePartyId(),
						"p2pStreamId", readyP2PStream.ID(),
						"error", err)

					return
				}

				m.logger.Info("[keySign] send p2p start msg succ",
					"signId", request.SignId,
					"keyId", request.KeyId,
					"sessionId", sessionId,
					"remotePartyId", readyP2PStream.RemotePartyId(),
					"p2pStreamId", readyP2PStream.ID())

				if p2pMsg, err := readyP2PStream.RecvMsg(); err != nil {
					select {
					case errChan <- err:
					default:
					}

					m.logger.Error("[keySign] recv p2p finish msg fail",
						"signId", request.SignId,
						"keyId", request.KeyId,
						"sessionId", sessionId,
						"remotePartyId", readyP2PStream.RemotePartyId(),
						"p2pStreamId", readyP2PStream.ID(),
						"error", err)
				} else if finishMsg, ok := p2pMsg.MsgType.(*TssP2PMsg_KeySignFinishMsg); !ok {
					select {
					case errChan <- fmt.Errorf("invalid msg type: %s", p2pMsg.GetMsgType()):
					default:
					}
					m.logger.Error("[keySign] recv p2p finish msg fail for invalid msg type",
						"signId", request.SignId,
						"keyId", request.KeyId,
						"sessionId", sessionId,
						"remotePartyId", readyP2PStream.RemotePartyId(),
						"p2pStreamId", readyP2PStream.ID(),
						"error", fmt.Sprintf("invalid msg type: %s", p2pMsg.GetMsgType()))
				} else {
					finishMsgChan <- finishMsg.KeySignFinishMsg

					m.logger.Info("[keySign] recv p2p finish msg succ",
						"signId", request.SignId,
						"keyId", request.KeyId,
						"sessionId", sessionId,
						"remotePartyId", readyP2PStream.RemotePartyId(),
						"p2pStreamId", readyP2PStream.ID())
				}
			}
		}(p2pStream)
	}

	defer func() {
		for _, p2pStream := range readyP2PStreamMap {
			err := p2pStream.(P2PStream).Close()
			if err != nil {
				m.logger.Error("[keySign] close p2p stream fail",
					"signId", request.SignId,
					"keyId", request.KeyId,
					"sessionId", sessionId,
					"remotePartyId", p2pStream.RemotePartyId(),
					"p2pStreamId", p2pStream.ID(),
					"error", err)
			}
		}

		close(finishMsgChan)
		m.logger.Info("[keySign] close finish msg chan",
			"signId", request.SignId,
			"keyId", request.KeyId,
			"sessionId", sessionId)
	}()

	wg.Wait()

	// step 3: wait for finish
	var finishCnt int32
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case err := <-errChan:
			return nil, err
		case finishMsg, ok := <-finishMsgChan:
			if !ok {
				return nil, fmt.Errorf("finishMsgChan closed")
			}
			if finishMsg.IsSuccess {
				return &KeySignResponse{
					SessionId:  sessionId,
					SignatureR: finishMsg.SignatureR,
					SignatureS: finishMsg.SignatureS,
					SignatureV: finishMsg.SignatureV,
				}, nil
			}
			finishCnt++
			if finishCnt == int32(len(readyP2PStreamMap)) {
				return nil, status.Errorf(codes.Internal, fmt.Sprintf("sign all party failed. reason: %s", finishMsg.FailureReason))
			}
		}
	}
}

func (m *Manager) handleKeySign(p2pStream P2PStream) {
	defer p2pStream.Close()
	// check msg type
	var keySignPrepareMsg *KeySignPrepareMsg
	var readKeySignPrepareMsgErr error
	if p2pMsg, err := p2pStream.RecvMsg(); err != nil {
		readKeySignPrepareMsgErr = err
	} else {
		keySignPrepareMsg = p2pMsg.GetKeySignPrepareMsg()
		if keySignPrepareMsg == nil {
			readKeySignPrepareMsgErr = fmt.Errorf("invalid msg type: %s", p2pMsg.GetMsgType())
		}
	}
	if readKeySignPrepareMsgErr != nil {

		m.logger.Error("[handleKeySign] recv p2p prepare msg fail",
			"remotePartyId", p2pStream.RemotePartyId(),
			"p2pStreamId", p2pStream.ID())

		if err := p2pStream.SendMsg(&TssP2PMsg{
			MsgType: &TssP2PMsg_KeySignFinishMsg{
				KeySignFinishMsg: &KeySignFinishMsg{
					IsSuccess:     false,
					FailureReason: readKeySignPrepareMsgErr.Error(),
				},
			},
		}); err != nil {
			m.logger.Error("send msg error", "error", err)
		}
		return
	}

	m.logger.Info("[handleKeySign] recv p2p prepare msg succ",
		"signId", keySignPrepareMsg.SignId,
		"keyId", keySignPrepareMsg.KeyId,
		"sessionId", keySignPrepareMsg.SessionId,
		"remotePartyId", p2pStream.RemotePartyId(),
		"p2pStreamId", p2pStream.ID())

	ctx, cancel := context.WithDeadline(context.Background(), keySignPrepareMsg.GetDeadline().AsTime())
	defer cancel()

	p2pProcessMsgChan := make(chan *TssP2PMsg)
	defer close(p2pProcessMsgChan)
	p2pSessionProtocolId := fmt.Sprintf("/tss/keySign/session/%s", keySignPrepareMsg.SessionId)
	m.p2pCommunicator.SetStreamHandler(p2pSessionProtocolId, func(p2pSessionStream P2PStream) {
		defer func() {
			if err := p2pSessionStream.Close(); err != nil {
				m.logger.Error("[handleKeySign] close session p2p stream fail",
					"signId", keySignPrepareMsg.SignId,
					"keyId", keySignPrepareMsg.KeyId,
					"sessionId", keySignPrepareMsg.SessionId,
					"remotePartyId", p2pSessionStream.RemotePartyId(),
					"p2pStreamId", p2pSessionStream.ID(),
					"error", err)
			}
		}()
		p2pMsg, err := p2pSessionStream.RecvMsg()
		if err != nil {
			m.logger.Error("[handleKeySign] recv session p2p msg fail",
				"signId", keySignPrepareMsg.SignId,
				"keyId", keySignPrepareMsg.KeyId,
				"sessionId", keySignPrepareMsg.SessionId,
				"remotePartyId", p2pSessionStream.RemotePartyId(),
				"p2pStreamId", p2pSessionStream.ID(),
				"error", err)
			return
		}
		p2pProcessMsgChan <- p2pMsg
	})
	defer m.p2pCommunicator.RemoveStreamHandler(p2pSessionProtocolId)

	if finishMsg, err := m.handleKeySign0(ctx, p2pStream, keySignPrepareMsg, p2pProcessMsgChan, p2pSessionProtocolId); err != nil {
		m.logger.Error("[handleKeySign] handleKeySign0 fail",
			"signId", keySignPrepareMsg.SignId,
			"keyId", keySignPrepareMsg.KeyId,
			"sessionId", keySignPrepareMsg.SessionId,
			"error", err)

		keySignFinishMsg := &KeySignFinishMsg{
			IsSuccess:     false,
			FailureReason: err.Error(),
		}

		if err := p2pStream.SendMsg(&TssP2PMsg{
			MsgType: &TssP2PMsg_KeySignFinishMsg{
				KeySignFinishMsg: keySignFinishMsg,
			},
		}); err != nil {
			m.logger.Error("[handleKeySign] send p2p finish fail msg fail",
				"signId", keySignPrepareMsg.SignId,
				"keyId", keySignPrepareMsg.KeyId,
				"sessionId", keySignPrepareMsg.SessionId,
				"remotePartyId", p2pStream.RemotePartyId(),
				"p2pStreamId", p2pStream.ID(),
				"failureReason", keySignFinishMsg.GetFailureReason(),
				"error", err)
		}
	} else {
		if err := p2pStream.SendMsg(&TssP2PMsg{
			MsgType: &TssP2PMsg_KeySignFinishMsg{
				KeySignFinishMsg: finishMsg,
			},
		}); err != nil {
			m.logger.Error("[handleKeySign] send p2p finish succ msg fail",
				"signId", keySignPrepareMsg.SignId,
				"keyId", keySignPrepareMsg.KeyId,
				"sessionId", keySignPrepareMsg.SessionId,
				"remotePartyId", p2pStream.RemotePartyId(),
				"p2pStreamId", p2pStream.ID(),
				"error", err)
		}

		m.logger.Info("[handleKeySign] send p2p finish succ msg",
			"signId", keySignPrepareMsg.SignId,
			"keyId", keySignPrepareMsg.KeyId,
			"sessionId", keySignPrepareMsg.SessionId,
			"remotePartyId", p2pStream.RemotePartyId(),
			"p2pStreamId", p2pStream.ID())
	}
}

func (m *Manager) handleKeySign0(
	ctx context.Context,
	p2pStream P2PStream,
	keySignPrepareMsg *KeySignPrepareMsg,
	p2pProcessMsgChan <-chan *TssP2PMsg,
	p2pProcessProtocolId string,
) (*KeySignFinishMsg, error) {
	m.logger.Info("[handleKeySign0] start",
		"signId", keySignPrepareMsg.SignId,
		"keyId", keySignPrepareMsg.KeyId,
		"sessionId", keySignPrepareMsg.SessionId,
		"remotePartyId", p2pStream.RemotePartyId(),
		"p2pStreamId", p2pStream.ID())
	defer func() {
		m.logger.Info("[handleKeySign0] finish",
			"signId", keySignPrepareMsg.SignId,
			"keyId", keySignPrepareMsg.KeyId,
			"sessionId", keySignPrepareMsg.SessionId,
			"remotePartyId", p2pStream.RemotePartyId(),
			"p2pStreamId", p2pStream.ID())
	}()

	// get key data from storage
	keyData, err := m.getKeyDataByKeyIdAndKeySessionIdFromStorage(keySignPrepareMsg.KeyId, keySignPrepareMsg.KeySessionId)
	if err != nil {
		m.logger.Error("[handleKeySign0] getKeyDataByKeyIdAndKeySessionIdFromStorage failed",
			"signId", keySignPrepareMsg.SignId,
			"keyId", keySignPrepareMsg.KeyId,
			"sessionId", keySignPrepareMsg.SessionId,
			"remotePartyId", p2pStream.RemotePartyId(),
			"p2pStreamId", p2pStream.ID(),
			"err", err)
		return nil, err
	}
	if keyData == nil {
		m.logger.Error("[handleKeySign0] keyData is nil failed",
			"signId", keySignPrepareMsg.SignId,
			"keyId", keySignPrepareMsg.KeyId,
			"sessionId", keySignPrepareMsg.SessionId,
			"remotePartyId", p2pStream.RemotePartyId(),
			"p2pStreamId", p2pStream.ID())
		return nil, errors.New("key data not found")
	}

	// check sign msg
	// generatedSignature, err := m.validator.KeySignValidate(keySignPrepareMsg.GetSignId(), keySignPrepareMsg.GetKeyId(), keySignPrepareMsg.GetSignMsg())
	// if err != nil {
	// 	m.logger.Error("handleKeySign0 KeySignValidate failed", "err", err)
	// 	return nil, err
	// }
	if !m.dev {
		_, err = m.validator.KeySignValidate(keySignPrepareMsg.GetSignId(), keySignPrepareMsg.GetKeyId(), keySignPrepareMsg.GetSignMsg())
		if err != nil {
			m.logger.Error("[handleKeySign0] KeySignValidate failed",
				"signId", keySignPrepareMsg.SignId,
				"keyId", keySignPrepareMsg.KeyId,
				"sessionId", keySignPrepareMsg.SessionId,
				"remotePartyId", p2pStream.RemotePartyId(),
				"p2pStreamId", p2pStream.ID(),
				"err", err)
			return nil, err
		}
	}

	var tssLocalPartySaveData keygen.LocalPartySaveData
	if err := json.Unmarshal([]byte(keyData.SaveDataJson), &tssLocalPartySaveData); err != nil {
		m.logger.Error("[handleKeySign0] tssLocalPartySaveData parse failed",
			"signId", keySignPrepareMsg.SignId,
			"keyId", keySignPrepareMsg.KeyId,
			"sessionId", keySignPrepareMsg.SessionId,
			"remotePartyId", p2pStream.RemotePartyId(),
			"p2pStreamId", p2pStream.ID(),
			"err", err)
		return nil, err
	}

	if err := p2pStream.SendMsg(&TssP2PMsg{
		MsgType: &TssP2PMsg_KeySignReadyMsg{
			KeySignReadyMsg: &KeySignReadyMsg{},
		},
	}); err != nil {
		m.logger.Error("[handleKeySign0] send p2p ready msg fail",
			"signId", keySignPrepareMsg.SignId,
			"keyId", keySignPrepareMsg.KeyId,
			"sessionId", keySignPrepareMsg.SessionId,
			"remotePartyId", p2pStream.RemotePartyId(),
			"p2pStreamId", p2pStream.ID(),
			"err", err)
		return nil, err
	}

	m.logger.Info("[handleKeySign0] send p2p ready msg succ",
		"signId", keySignPrepareMsg.SignId,
		"keyId", keySignPrepareMsg.KeyId,
		"sessionId", keySignPrepareMsg.SessionId,
		"remotePartyId", p2pStream.RemotePartyId(),
		"p2pStreamId", p2pStream.ID())

	var keySignStartMsg *KeySignStartMsg
	if p2pMsg, err := p2pStream.RecvMsg(); err != nil {
		m.logger.Error("[handleKeySign0] recv p2p start msg fail",
			"signId", keySignPrepareMsg.SignId,
			"keyId", keySignPrepareMsg.KeyId,
			"sessionId", keySignPrepareMsg.SessionId,
			"remotePartyId", p2pStream.RemotePartyId(),
			"p2pStreamId", p2pStream.ID(),
			"err", err)

		return nil, err
	} else {
		switch p2pMsgType := p2pMsg.MsgType.(type) {
		case *TssP2PMsg_KeySignStartMsg:
			keySignStartMsg = p2pMsgType.KeySignStartMsg
		case *TssP2PMsg_KeySignCancelMsg:
			m.logger.Info("[handleKeySign0] key sign canceled",
				"signId", keySignPrepareMsg.SignId,
				"keyId", keySignPrepareMsg.KeyId,
				"sessionId", keySignPrepareMsg.SessionId,
				"remotePartyId", p2pStream.RemotePartyId(),
				"p2pStreamId", p2pStream.ID())

			return &KeySignFinishMsg{
				IsSuccess:     false,
				FailureReason: "key sign canceled",
			}, nil
		default:
			return nil, fmt.Errorf("invalid msg type: %s", p2pMsg.GetMsgType())
		}
	}

	m.logger.Info("[handleKeySign0] recv p2p start msg succ",
		"signId", keySignPrepareMsg.SignId,
		"keyId", keySignPrepareMsg.KeyId,
		"sessionId", keySignPrepareMsg.SessionId,
		"remotePartyId", p2pStream.RemotePartyId(),
		"p2pStreamId", p2pStream.ID())

	// return generated signature after KeySignFinishMsg
	// if len(generatedSignature) > 0 {
	// 	r, s, v := ParseSignature(generatedSignature)
	// 	// send finish msg
	// 	return &KeySignFinishMsg{
	// 		IsSuccess:  true,
	// 		SignatureR: r,
	// 		SignatureS: s,
	// 		SignatureV: v,
	// 	}, nil
	// }

	// step 2: create local party and start
	var tssAllPartyIDs = make(tsslib.UnSortedPartyIDs, len(keySignStartMsg.SignPartyId))
	for i, pbPartyID := range keySignStartMsg.SignPartyId {
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
		int(keyData.Threshold))

	outCh := make(chan tsslib.Message)
	endCh := make(chan *common.SignatureData)
	tssLocalParty := signing.NewLocalParty(
		new(big.Int).SetBytes(keySignPrepareMsg.GetSignMsg()),
		tssParams,
		tssLocalPartySaveData,
		outCh,
		endCh).(*signing.LocalParty)

	errCh := make(chan error)
	go func() {
		if err := tssLocalParty.Start(); err != nil {
			errCh <- err
		}
	}()

	// step 3: handle event loop
	for {
		var tssSignatureData *common.SignatureData
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
				// send finish msg
				return &KeySignFinishMsg{
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

			toPbPartyIds := make([]*PartyID, 0, len(keySignStartMsg.SignPartyId))
			if msg.GetTo() == nil {
				// broadcast
				for _, p := range keySignStartMsg.SignPartyId {
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
					m.logger.Error("keySign send p2p ProcessUpdateMsg msg fail", "error", err)

				}
			}
		case tssSignatureData = <-endCh:
			// send finish msg
			return &KeySignFinishMsg{
				IsSuccess:  true,
				SignatureR: tssSignatureData.GetR(),
				SignatureS: tssSignatureData.GetS(),
				SignatureV: tssSignatureData.GetSignatureRecovery(),
			}, nil
		case err := <-errCh:
			return nil, err
		}
	}
}

func ParseSignature(signature []byte) ([]byte, []byte, []byte) {
	var r, s, v []byte
	r = append(r, signature[:32]...)
	s = append(s, signature[32:64]...)
	v = append(v, signature[64:]...)

	if v[0] > 1 {
		v[0] -= 27
	}
	return r, s, v
}
