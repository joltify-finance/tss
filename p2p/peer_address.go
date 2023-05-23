package p2p

import (
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

func (c *Communication) ExportPeerAddress() map[peer.ID][]ma.Multiaddr {
	peerStore := c.dht.Host().Peerstore()
	peers := peerStore.Peers()
	addressBook := make(map[peer.ID][]ma.Multiaddr)
	for _, el := range peers {
		addrs := peerStore.Addrs(el)
		addressBook[el] = addrs
	}
	return addressBook
}
