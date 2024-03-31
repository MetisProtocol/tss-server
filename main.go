package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/metis-protocol/tss-server/tss"
	"github.com/metis-protocol/tss-server/validator"
	"github.com/multiformats/go-multiaddr"
	"github.com/tendermint/tendermint/libs/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var (
	argStoragePath       = flag.String("storagePath", "/app/data/leveldb", "storage path")
	argP2PBootstrapPeers = flag.String("p2pBootstrapPeers", "", "p2p bootstrap peers")
	argP2PListenAddr     = flag.String("p2pListenAddr", "/ip4/0.0.0.0/tcp/4001", "p2p listen addr")
	argP2PPrivateKeyFile = flag.String("p2pPrivateKeyFile", "/app/data/priv_validator_key.json", "p2p secp256k1 private key pem file")
	argThemisRestUrl     = flag.String("themisRest", "http://127.0.0.1:1317", "themis rest url")
	argGrpcPort          = flag.Int("grpcPort", 9001, "grpc port")
	argDev               = flag.Bool("dev", false, "dev model")
)

func main() {
	flag.Parse()

	if p2p := os.Getenv("p2p"); p2p != "" {
		*argP2PBootstrapPeers = p2p
	}

	logger := log.NewTMLogger(log.NewSyncWriter(os.Stdout)).With("module", "tss")

	// start local storage
	levelDBManager := tss.NewLevelDBStorage(*argStoragePath)
	if err := levelDBManager.Start(); err != nil {
		logger.Error("failed start storage", "error", err)
		return
	}
	defer levelDBManager.Stop()

	p2pBootstrapPeers, err := parseP2PBootstrapPeers(*argP2PBootstrapPeers)
	if err != nil {
		logger.Error("failed parse p2p bootstrap peers", "error", err)
		return
	}
	p2pListenAddr, err := multiaddr.NewMultiaddr(*argP2PListenAddr)
	if err != nil {
		logger.Error("failed parse p2p listen addr", "error", err)
		return
	}
	p2pPrivateKey, p2pAddress, err := validator.ParseP2PPrivateKeyFile(*argP2PPrivateKeyFile)
	if err != nil {
		logger.Error("failed parse p2p private key", "error", err)
		return
	}

	// start p2p communicator, use address as moniker
	libP2PManager, err := tss.NewLibP2PManager(
		p2pBootstrapPeers,
		p2pListenAddr,
		p2pPrivateKey,
		p2pAddress)
	if err != nil {
		logger.Error("failed NewLibP2PManager", "error", err)
		return
	}
	if err := libP2PManager.Start(); err != nil {
		logger.Error("failed start storage", "error", err)
		return
	}
	defer func() {
		if err := libP2PManager.Stop(); err != nil {
			logger.Error("failed stop libp2p", "error", err)
		}
	}()

	// start mpc manager
	mpcManager := tss.NewManager(
		logger,
		levelDBManager,
		libP2PManager,
		validator.New(*argThemisRestUrl),
		*argDev)
	mpcManager.Start()
	defer mpcManager.Stop()
	logger.Info("mpc manager started", "themisRestUrl", *argThemisRestUrl, "argDev", *argDev, "p2pBootstrapPeers", argP2PBootstrapPeers)

	server := grpc.NewServer()
	tss.RegisterTssServiceServer(server, mpcManager)

	// register reclection to support grpcurl
	reflection.Register(server)

	basectx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go func() {
		defer cancel()

		// start grpc server
		lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *argGrpcPort))
		if err != nil {
			logger.Error("failed to listen grpc port", "error", err)
			return
		}

		logger.Info("server listening at " + lis.Addr().String())
		if err := server.Serve(lis); err != nil {
			logger.Error("failed to serve", "error", err)
			return
		}
	}()

	<-basectx.Done()
	server.GracefulStop()
}

func parseP2PBootstrapPeers(p2pBootstrapPeers string) ([]peer.AddrInfo, error) {
	bootstrapPeers := make([]peer.AddrInfo, 0)
	for _, addr := range strings.Split(p2pBootstrapPeers, ",") {
		peerInfo, err := peer.AddrInfoFromString(addr)
		if err != nil {
			return nil, err
		}
		bootstrapPeers = append(bootstrapPeers, *peerInfo)
	}
	return bootstrapPeers, nil
}
