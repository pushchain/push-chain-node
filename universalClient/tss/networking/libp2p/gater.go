package libp2p

import (
	"github.com/libp2p/go-libp2p/core/control"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

// validatorGater rejects inbound connections whose authenticated peer ID is
// not accepted by the authorizer. Outbound dials are not gated: this node only
// dials peers resolved from the validator set.
type validatorGater struct {
	authorizer func(peerID string) bool
}

func (g *validatorGater) InterceptPeerDial(peer.ID) bool               { return true }
func (g *validatorGater) InterceptAddrDial(peer.ID, ma.Multiaddr) bool { return true }
func (g *validatorGater) InterceptAccept(network.ConnMultiaddrs) bool  { return true }

func (g *validatorGater) InterceptSecured(dir network.Direction, p peer.ID, _ network.ConnMultiaddrs) bool {
	if dir == network.DirOutbound {
		return true
	}
	return g.authorizer(p.String())
}

func (g *validatorGater) InterceptUpgraded(network.Conn) (bool, control.DisconnectReason) {
	return true, 0
}
