package tss

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/ipfs/go-datastore"
	sync2 "github.com/ipfs/go-datastore/sync"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/libp2p/go-libp2p/core/protocol"
	routedhost "github.com/libp2p/go-libp2p/p2p/host/routed"
	connmgr "github.com/libp2p/go-libp2p/p2p/net/connmgr"
	"github.com/multiformats/go-multiaddr"
	pb "google.golang.org/protobuf/proto"
)

type P2PStream interface {
	ID() string
	RemotePartyId() string
	SendMsg(*TssP2PMsg) error
	RecvMsg() (*TssP2PMsg, error)
	Close() error
}

type P2PStreamHandler func(stream P2PStream)

type P2PCommunicator interface {
	LocalPartyId() *PartyID
	SetStreamHandler(protocolId string, handler P2PStreamHandler)
	RemoveStreamHandler(protocolId string)
	NewStream(ctx context.Context, remotePartyId string, protocolId string) (P2PStream, error)
}

type LibP2PManager struct {
	bootstrapPeers  []peer.AddrInfo
	listenAddr      multiaddr.Multiaddr
	privateKey      crypto.PrivKey
	localPartyId    *PartyID
	localHandlerMap *sync.Map // map[string]P2PStreamHandler
	host            host.Host
}

func NewLibP2PManager(
	bootstrapPeers []peer.AddrInfo,
	listenAddr multiaddr.Multiaddr,
	privateKey crypto.PrivKey,
	moniker string,
) (*LibP2PManager, error) {
	if len(bootstrapPeers) < 1 {
		return nil, errors.New("not enough bootstrap peers")
	}
	peerId, err := peer.IDFromPrivateKey(privateKey)
	if err != nil {
		return nil, err
	}
	pubKeyBytes, err := privateKey.GetPublic().Raw()
	if err != nil {
		return nil, err
	}
	return &LibP2PManager{
		bootstrapPeers: bootstrapPeers,
		listenAddr:     listenAddr,
		privateKey:     privateKey,
		localPartyId: &PartyID{
			Id:      peerId.String(),
			Moniker: moniker,
			Key:     pubKeyBytes,
		},
		localHandlerMap: &sync.Map{},
		host:            nil,
	}, nil
}

func (m *LibP2PManager) LocalPartyId() *PartyID {
	return m.localPartyId
}

func (m *LibP2PManager) Start() error {
	ctx := context.Background()

	cm, err := connmgr.NewConnManager(
		100,
		400,
		connmgr.WithGracePeriod(15*time.Second),
	)
	if err != nil {
		return err
	}

	h, err := libp2p.New(
		libp2p.ListenAddrs(m.listenAddr),
		libp2p.Identity(m.privateKey),
		libp2p.DefaultTransports,
		libp2p.DefaultMuxers,
		libp2p.DefaultSecurity,
		libp2p.NATPortMap(),
		libp2p.DefaultEnableRelay,
		libp2p.ConnectionManager(cm),
		// libp2p.DefaultConnectionManager,
	)
	if err != nil {
		return err
	}

	// Construct a datastore (needed by the DHT). This is just a simple, in-memory thread-safe datastore.
	dstore := sync2.MutexWrap(datastore.NewMapDatastore())

	// Make the DHT
	newDHT := dht.NewDHT(ctx, h, dstore)

	// Make the routed host
	routedHost := routedhost.Wrap(h, newDHT)

	m.host = routedHost

	// connect to the chosen ipfs nodes
	err = m.bootstrapConnect()
	if err != nil {
		return err
	}

	// start auto reconnect
	// go m.autoReconnect()
	return nil
}

// This code is borrowed from the go-ipfs bootstrap process
func (m *LibP2PManager) bootstrapConnect() error {
	if len(m.bootstrapPeers) < 1 {
		return errors.New("not enough bootstrap peers")
	}

	errs := make(chan error, len(m.bootstrapPeers))
	var wg sync.WaitGroup
	for _, p := range m.bootstrapPeers {

		// performed asynchronously because when performed synchronously, if
		// one `Connect` call hangs, subsequent calls are more likely to
		// fail/abort due to an expiring context.
		// Also, performed asynchronously for dial speed.

		wg.Add(1)
		go func(p peer.AddrInfo) {
			defer wg.Done()
			defer log.Println("bootstrapDial", m.host.ID(), p.ID)
			log.Printf("%s bootstrapping to %s", m.host.ID(), p.ID)

			m.host.Peerstore().AddAddrs(p.ID, p.Addrs, peerstore.PermanentAddrTTL)
			if err := m.host.Connect(context.TODO(), p); err != nil {
				log.Println("bootstrapDialFailed", p.ID)
				log.Printf("failed to bootstrap with %v: %s", p.ID, err)
				errs <- err
				return
			}
			log.Println("bootstrapDialSuccess", p.ID)
			log.Printf("bootstrapped with %v", p.ID)
		}(p)
	}
	wg.Wait()

	// our failure condition is when no connection attempt succeeded.
	// So drain the errs channel, counting the results.
	close(errs)
	count := 0
	var err error
	for err = range errs {
		if err != nil {
			count++
		}
	}
	return nil
}

func (m *LibP2PManager) autoReconnect() {
	autoReconnectTime := os.Getenv("AUTO_RECONNECT_TIME")
	log.Println("AUTO_RECONNECT_TIME", autoReconnectTime)
	autoReconnectTimeSec, _ := strconv.ParseInt(autoReconnectTime, 10, 64)
	if autoReconnectTimeSec == 0 {
		autoReconnectTimeSec = 3600 // default 1h
	}
	log.Println("autoReconnectTimeSec", autoReconnectTimeSec)

	ticker := time.NewTicker(time.Duration(autoReconnectTimeSec * int64(time.Second)))
	for {
		select {
		case <-ticker.C:
			for {
				err := m.bootstrapConnect()
				if err == nil {
					log.Println("autoReconnect success")
					break
				}

				log.Println("autoReconnect failed, try again")
				time.Sleep(5 * time.Second)
			}
		}
	}
}

func (m *LibP2PManager) Stop() error {
	err := m.host.Close()
	if err != nil {
		log.Printf("fail to close host network: %s", err)
	}
	return err
}

type libP2PStream struct {
	stream network.Stream
}

func (l *libP2PStream) ID() string {
	return l.stream.ID()
}

func (l *libP2PStream) RemotePartyId() string {
	return l.stream.Conn().RemotePeer().String()
}

func (l *libP2PStream) SendMsg(msg *TssP2PMsg) error {
	bytes, err := pb.Marshal(msg)
	if err != nil {
		return err
	}
	length := uint32(len(bytes))
	lengthBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(lengthBytes, length)

	writer := bufio.NewWriter(l.stream)
	if n, err := writer.Write(lengthBytes); err != nil {
		return err
	} else if n != len(lengthBytes) {
		return fmt.Errorf("failed to write all bytes to stream")
	}
	if n, err := writer.Write(bytes); err != nil {
		return err
	} else if n != len(bytes) {
		return fmt.Errorf("failed to write all bytes to stream")
	}
	err = writer.Flush()
	if err != nil {
		return fmt.Errorf("fail to flush stream: %w", err)
	}
	return nil
}

func (l *libP2PStream) RecvMsg() (*TssP2PMsg, error) {
	lengthBytes := make([]byte, 4)
	if n, err := io.ReadFull(l.stream, lengthBytes); err != nil {
		return nil, err
	} else if n != len(lengthBytes) {
		return nil, fmt.Errorf("failed to read all bytes from stream")
	}
	length := binary.LittleEndian.Uint32(lengthBytes)
	if length > 20*1024*1024 {
		return nil, fmt.Errorf("message length is too large")
	}
	bytes := make([]byte, length)
	if n, err := io.ReadFull(l.stream, bytes); err != nil {
		return nil, err
	} else if n != len(bytes) {
		return nil, fmt.Errorf("failed to read all bytes from stream")
	}
	msg := &TssP2PMsg{}
	err := pb.Unmarshal(bytes, msg)
	if err != nil {
		return nil, err
	}
	return msg, nil
}

func (l *libP2PStream) Close() error {
	return l.stream.Close()
}

type localP2PStream struct {
	remotePartyId string
	readChan      <-chan *TssP2PMsg
	writeChan     chan<- *TssP2PMsg
}

func (l *localP2PStream) ID() string {
	return "local"
}

func (l *localP2PStream) RemotePartyId() string {
	return l.remotePartyId
}

func (l *localP2PStream) SendMsg(msg *TssP2PMsg) error {
	l.writeChan <- msg
	return nil
}

func (l *localP2PStream) RecvMsg() (*TssP2PMsg, error) {
	msg := <-l.readChan
	if msg == nil {
		return nil, fmt.Errorf("recv msg is nil")
	}
	return msg, nil
}

func (l *localP2PStream) Close() error {
	close(l.writeChan)
	return nil
}

func (m *LibP2PManager) SetStreamHandler(protocolId string, handler P2PStreamHandler) {
	m.localHandlerMap.Store(protocolId, handler)
	m.host.SetStreamHandler(protocol.ID(protocolId), func(stream network.Stream) {
		handler(&libP2PStream{
			stream: stream,
		})
	})
}

func (m *LibP2PManager) RemoveStreamHandler(protocolId string) {
	m.localHandlerMap.Delete(protocolId)
	m.host.RemoveStreamHandler(protocol.ID(protocolId))
}

func (m *LibP2PManager) NewStream(ctx context.Context, remotePartyId string, protocolId string) (P2PStream, error) {
	if remotePartyId == m.localPartyId.Id {
		if localHandler, ok := m.localHandlerMap.Load(protocolId); ok {
			toRemoteChan := make(chan *TssP2PMsg, 10)
			toLocalChan := make(chan *TssP2PMsg, 10)
			go func() {
				localHandler.(P2PStreamHandler)(&localP2PStream{
					remotePartyId: remotePartyId,
					readChan:      toRemoteChan,
					writeChan:     toLocalChan,
				})
			}()
			return &localP2PStream{
				remotePartyId: remotePartyId,
				readChan:      toLocalChan,
				writeChan:     toRemoteChan,
			}, nil
		} else {
			return nil, fmt.Errorf("no local handler for protocol %s", protocolId)
		}
	}
	peerId, err := peer.Decode(remotePartyId)
	if err != nil {
		return nil, err
	}
	stream, err := m.host.NewStream(ctx, peerId, protocol.ID(protocolId))
	if err != nil {
		return nil, err
	}
	return &libP2PStream{
		stream: stream,
	}, nil
}
