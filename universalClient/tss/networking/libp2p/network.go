package libp2p

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/rs/zerolog"

	"github.com/pushchain/push-chain-node/universalClient/tss/networking"
)

// Network implements networking.Network using libp2p.
type Network struct {
	cfg        Config
	host       host.Host
	protocolID protocol.ID

	handlerMu sync.RWMutex
	handler   networking.MessageHandler

	peerMu sync.RWMutex
	peers  map[string]peer.AddrInfo

	logger zerolog.Logger
}

// New creates a new libp2p network instance.
func New(ctx context.Context, cfg Config, logger zerolog.Logger) (*Network, error) {
	cfg.setDefaults()
	if logger.GetLevel() == zerolog.Disabled {
		logger = zerolog.New(io.Discard)
	}

	priv, err := loadIdentity(cfg.PrivateKeyBase64)
	if err != nil {
		return nil, err
	}

	host, err := libp2p.New(
		libp2p.Identity(priv),
		libp2p.ListenAddrStrings(cfg.ListenAddrs...),
	)
	if err != nil {
		return nil, err
	}

	n := &Network{
		cfg:        cfg,
		host:       host,
		protocolID: protocol.ID(cfg.ProtocolID),
		peers:      make(map[string]peer.AddrInfo),
		logger:     logger.With().Str("component", "networking_libp2p").Logger(),
	}

	host.SetStreamHandler(n.protocolID, n.handleStream)
	return n, nil
}

// ID implements networking.Network.
func (n *Network) ID() string {
	return n.host.ID().String()
}

// ListenAddrs implements networking.Network.
func (n *Network) ListenAddrs() []string {
	addrs := n.host.Addrs()
	var filtered []string
	for _, addr := range addrs {
		if isUnspecified(addr) {
			continue
		}
		filtered = append(filtered, addr.String()+"/p2p/"+n.host.ID().String())
	}
	if len(filtered) == 0 {
		out := make([]string, len(addrs))
		for i, addr := range addrs {
			out[i] = addr.String() + "/p2p/" + n.host.ID().String()
		}
		return out
	}
	return filtered
}

// RegisterHandler implements networking.Network.
func (n *Network) RegisterHandler(handler networking.MessageHandler) error {
	n.handlerMu.Lock()
	defer n.handlerMu.Unlock()
	if n.handler != nil {
		return fmt.Errorf("handler already registered")
	}
	n.handler = handler
	return nil
}

// EnsurePeer implements networking.Network.
func (n *Network) EnsurePeer(peerID string, addrs []string) error {
	if peerID == "" || len(addrs) == 0 {
		return fmt.Errorf("invalid peer info")
	}
	id, err := peer.Decode(peerID)
	if err != nil {
		return err
	}

	multiaddrs, err := normalizeAddrs(addrs, id)
	if err != nil {
		return err
	}

	n.peerMu.Lock()
	n.peers[peerID] = peer.AddrInfo{ID: id, Addrs: multiaddrs}
	n.peerMu.Unlock()
	return nil
}

// Send implements networking.Network.
func (n *Network) Send(ctx context.Context, peerID string, data []byte) error {
	info, err := n.lookupPeer(peerID)
	if err != nil {
		return err
	}

	dialCtx, cancel := context.WithTimeout(ctx, n.cfg.DialTimeout)
	defer cancel()

	// Try to connect (libp2p will reuse existing connections)
	if err := n.host.Connect(dialCtx, info); err != nil {
		return fmt.Errorf("failed to connect to peer %s: %w", peerID, err)
	}

	// Create stream with timeout
	streamCtx, streamCancel := context.WithTimeout(ctx, n.cfg.DialTimeout)
	defer streamCancel()

	stream, err := n.host.NewStream(streamCtx, info.ID, n.protocolID)
	if err != nil {
		return fmt.Errorf("failed to create stream to peer %s: %w", peerID, err)
	}
	defer stream.Close()

	// Set write deadline
	deadline := time.Now().Add(n.cfg.IOTimeout)
	if err := stream.SetWriteDeadline(deadline); err != nil {
		return fmt.Errorf("failed to set write deadline: %w", err)
	}

	if err := writeFramed(stream, data); err != nil {
		return fmt.Errorf("failed to write data to peer %s: %w", peerID, err)
	}

	// Set read deadline for response (if any)
	if err := stream.SetReadDeadline(deadline); err != nil {
		// Non-fatal, log and continue
		n.logger.Debug().Err(err).Str("peer_id", peerID).Msg("failed to set read deadline")
	}

	return nil
}

func (n *Network) Close() error {
	return n.host.Close()
}

func (n *Network) lookupPeer(peerID string) (peer.AddrInfo, error) {
	n.peerMu.RLock()
	info, ok := n.peers[peerID]
	n.peerMu.RUnlock()
	if !ok {
		return peer.AddrInfo{}, fmt.Errorf("unknown peer %s", peerID)
	}
	return info, nil
}

func (n *Network) handleStream(stream network.Stream) {
	defer stream.Close()

	if deadline := time.Now().Add(n.cfg.IOTimeout); true {
		_ = stream.SetReadDeadline(deadline)
	}

	data, err := readFramed(stream)
	if err != nil {
		n.logger.Warn().Err(err).Msg("read failed")
		return
	}

	n.handlerMu.RLock()
	handler := n.handler
	n.handlerMu.RUnlock()
	if handler == nil {
		return
	}

	// Call handler in a goroutine to avoid blocking
	go handler(stream.Conn().RemotePeer().String(), data)
}

func loadIdentity(base64Key string) (crypto.PrivKey, error) {
	if base64Key == "" {
		priv, _, err := crypto.GenerateEd25519Key(rand.Reader)
		return priv, err
	}
	raw, err := base64.StdEncoding.DecodeString(base64Key)
	if err != nil {
		return nil, err
	}
	return crypto.UnmarshalPrivateKey(raw)
}

func writeFramed(w io.Writer, data []byte) error {
	bw := bufio.NewWriter(w)
	if err := binary.Write(bw, binary.BigEndian, uint32(len(data))); err != nil {
		return err
	}
	if _, err := bw.Write(data); err != nil {
		return err
	}
	return bw.Flush()
}

func readFramed(r io.Reader) ([]byte, error) {
	br := bufio.NewReader(r)
	var length uint32
	if err := binary.Read(br, binary.BigEndian, &length); err != nil {
		return nil, err
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(br, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func normalizeAddrs(raw []string, expected peer.ID) ([]ma.Multiaddr, error) {
	var results []ma.Multiaddr
	for _, addr := range raw {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			continue
		}
		maddr, err := ma.NewMultiaddr(addr)
		if err != nil {
			return nil, err
		}
		if _, err := maddr.ValueForProtocol(ma.P_P2P); err == nil {
			info, err := peer.AddrInfoFromP2pAddr(maddr)
			if err != nil {
				return nil, err
			}
			if info.ID != expected {
				return nil, fmt.Errorf("multiaddr peer mismatch: expected %s got %s", expected, info.ID)
			}
			results = append(results, info.Addrs...)
			continue
		}
		results = append(results, maddr)
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("no usable addresses provided")
	}
	return results, nil
}

func isUnspecified(addr ma.Multiaddr) bool {
	if ip, err := manet.ToIP(addr); err == nil {
		return ip.IsUnspecified()
	}
	return false
}
