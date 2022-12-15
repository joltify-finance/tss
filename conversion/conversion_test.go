package conversion

import (
	"encoding/json"
	"github.com/cosmos/cosmos-sdk/types/bech32/legacybech32"
	"math/big"
	"sort"
	"testing"

	"github.com/binance-chain/tss-lib/crypto"
	"github.com/btcsuite/btcd/btcec"
	coskey "github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	"github.com/libp2p/go-libp2p/core/peer"
	. "gopkg.in/check.v1"
)

var (
	testPubKeys = []string{"oppypub1zcjduepqjykgrc8kehvvauxuq9regzvueec2lz3xwtcpf9mw77pu5h8z9r2sysjdf9", "oppypub1zcjduepqspqxa9406qvr0jrtxdlc52tul6lfx4ppxctgaxefyccdvk95e8eqklumzy", "oppypub1zcjduepqxhdum6ce45xympd2kw7dz64lkkvvul6ck2zlh6de22xs4k64039sjv9l3p", "oppypub1zcjduepqeu2qzchm86zxhf2jw9jqj40u86wh7cyk7dv2qdlc3reuvm8pwc5q0nqqwx"}
	testPeers   = []string{
		"12D3KooWKb4eWT3mxHCvMGp3pzYfi3R22BAQ6LG5AebsqjFGYJsN",
		"12D3KooWJT1LZcwCJ321umRW2mWFE3ooiKzkuzHJVmtUPjzfYbfw",
		"12D3KooWDScAyrV1SnUPD4PrcE2PpFvu2aoMHUddpaJSfkyBGGVY",
		"12D3KooWPkiFkYHxgUhfahdSVh23rnFxXVUT5k1RpLqPTNzkpCb1",
	}
)

type ConversionTestSuite struct {
	testPubKeys []string
	localPeerID peer.ID
}

var _ = Suite(&ConversionTestSuite{})

func (p *ConversionTestSuite) SetUpTest(c *C) {
	var err error
	SetupBech32Prefix()
	p.testPubKeys = testPubKeys[:]
	sort.Strings(p.testPubKeys)
	p.localPeerID, err = peer.Decode("12D3KooWPkiFkYHxgUhfahdSVh23rnFxXVUT5k1RpLqPTNzkpCb1")
	c.Assert(err, IsNil)
}
func TestPackage(t *testing.T) { TestingT(t) }

func (p *ConversionTestSuite) TestAccPubKeysFromPartyIDs(c *C) {
	partiesID, _, err := GetParties(p.testPubKeys, p.testPubKeys[0])
	c.Assert(err, IsNil)
	partyIDMap := SetupPartyIDMap(partiesID)
	var keys []string
	for k := range partyIDMap {
		keys = append(keys, k)
	}

	got, err := AccPubKeysFromPartyIDs(keys, partyIDMap)
	c.Assert(err, IsNil)
	sort.Strings(got)
	c.Assert(got, DeepEquals, p.testPubKeys)
	got, err = AccPubKeysFromPartyIDs(nil, partyIDMap)
	c.Assert(err, Equals, nil)
	c.Assert(len(got), Equals, 0)
}

func (p *ConversionTestSuite) TestGetParties(c *C) {
	partiesID, localParty, err := GetParties(p.testPubKeys, p.testPubKeys[0])
	c.Assert(err, IsNil)
	pk := coskey.PubKey{
		Key: localParty.Key[:],
	}
	c.Assert(err, IsNil)
	got, err := legacybech32.MarshalPubKey(legacybech32.AccPK, &pk)
	c.Assert(err, IsNil)

	c.Assert(got, Equals, p.testPubKeys[0])
	var gotKeys []string
	for _, val := range partiesID {
		pk := coskey.PubKey{
			Key: val.Key,
		}
		got, err := legacybech32.MarshalPubKey(legacybech32.AccPK, &pk)
		c.Assert(err, IsNil)
		gotKeys = append(gotKeys, got)
	}
	sort.Strings(gotKeys)
	c.Assert(gotKeys, DeepEquals, p.testPubKeys)

	_, _, err = GetParties(p.testPubKeys, "")
	c.Assert(err, NotNil)
	_, _, err = GetParties(p.testPubKeys, "12")
	c.Assert(err, NotNil)
	_, _, err = GetParties(nil, "12")
	c.Assert(err, NotNil)
}

//
func (p *ConversionTestSuite) TestGetPeerIDFromPartyID(c *C) {
	_, localParty, err := GetParties(p.testPubKeys, p.testPubKeys[0])
	c.Assert(err, IsNil)
	peerID, err := GetPeerIDFromPartyID(localParty)
	c.Assert(err, IsNil)
	c.Assert(peerID, Equals, p.localPeerID)
	_, err = GetPeerIDFromPartyID(nil)
	c.Assert(err, NotNil)
	localParty.Index = -1
	_, err = GetPeerIDFromPartyID(localParty)
	c.Assert(err, NotNil)
}

func (p *ConversionTestSuite) TestGetPeerIDFromSecp256PubKey(c *C) {
	sort.Strings(p.testPubKeys)
	_, localParty, err := GetParties(p.testPubKeys, p.testPubKeys[0])
	c.Assert(err, IsNil)
	got, err := GetPeerIDFromEd25519PubKey(localParty.Key[:])
	c.Assert(err, IsNil)
	c.Assert(got, Equals, p.localPeerID)
	_, err = GetPeerIDFromEd25519PubKey(nil)
	c.Assert(err, NotNil)
}

func (p *ConversionTestSuite) TestGetPeersID(c *C) {
	localTestPubKeys := testPubKeys[:]
	sort.Strings(localTestPubKeys)
	partiesID, _, err := GetParties(p.testPubKeys, p.testPubKeys[0])
	c.Assert(err, IsNil)
	partyIDMap := SetupPartyIDMap(partiesID)
	partyIDtoP2PID := make(map[string]peer.ID)
	err = SetupIDMaps(partyIDMap, partyIDtoP2PID)
	c.Assert(err, IsNil)
	retPeers := GetPeersID(partyIDtoP2PID, p.localPeerID.String())
	var expectedPeers []string
	var gotPeers []string
	counter := 0
	for _, el := range testPeers {
		if el == p.localPeerID.String() {
			continue
		}
		expectedPeers = append(expectedPeers, el)
		gotPeers = append(gotPeers, retPeers[counter].String())
		counter++
	}
	sort.Strings(expectedPeers)
	sort.Strings(gotPeers)
	c.Assert(gotPeers, DeepEquals, expectedPeers)

	retPeers = GetPeersID(partyIDtoP2PID, "123")
	c.Assert(len(retPeers), Equals, 4)
	retPeers = GetPeersID(nil, "123")
	c.Assert(len(retPeers), Equals, 0)
}

func (p *ConversionTestSuite) TestPartyIDtoPubKey(c *C) {
	_, localParty, err := GetParties(p.testPubKeys, p.testPubKeys[0])
	c.Assert(err, IsNil)
	got, err := PartyIDtoPubKey(localParty)
	c.Assert(err, IsNil)
	c.Assert(got, Equals, p.testPubKeys[0])
	_, err = PartyIDtoPubKey(nil)
	c.Assert(err, NotNil)
	localParty.Index = -1
	_, err = PartyIDtoPubKey(nil)
	c.Assert(err, NotNil)
}

func (p *ConversionTestSuite) TestSetupIDMaps(c *C) {
	localTestPubKeys := testPubKeys[:]
	sort.Strings(localTestPubKeys)
	partiesID, _, err := GetParties(p.testPubKeys, p.testPubKeys[0])
	c.Assert(err, IsNil)
	partyIDMap := SetupPartyIDMap(partiesID)
	partyIDtoP2PID := make(map[string]peer.ID)
	err = SetupIDMaps(partyIDMap, partyIDtoP2PID)
	c.Assert(err, IsNil)
	var got []string

	for _, val := range partyIDtoP2PID {
		got = append(got, val.String())
	}
	sort.Strings(got)
	sort.Strings(testPeers)
	c.Assert(got, DeepEquals, testPeers)
	emptyPartyIDtoP2PID := make(map[string]peer.ID)
	SetupIDMaps(nil, emptyPartyIDtoP2PID)
	c.Assert(emptyPartyIDtoP2PID, HasLen, 0)
}

func (p *ConversionTestSuite) TestSetupPartyIDMap(c *C) {
	localTestPubKeys := testPubKeys[:]
	sort.Strings(localTestPubKeys)
	partiesID, _, err := GetParties(p.testPubKeys, p.testPubKeys[0])
	c.Assert(err, IsNil)
	partyIDMap := SetupPartyIDMap(partiesID)
	var pubKeys []string
	for _, el := range partyIDMap {
		pk := coskey.PubKey{
			Key: el.Key,
		}
		got, err := legacybech32.MarshalPubKey(legacybech32.AccPK, &pk)
		c.Assert(err, IsNil)
		pubKeys = append(pubKeys, got)
	}
	sort.Strings(pubKeys)
	c.Assert(p.testPubKeys, DeepEquals, pubKeys)

	ret := SetupPartyIDMap(nil)
	c.Assert(ret, HasLen, 0)
}

func (p *ConversionTestSuite) TestTssPubKey(c *C) {
	sk, err := btcec.NewPrivateKey(btcec.S256())
	c.Assert(err, IsNil)
	point, err := crypto.NewECPoint(btcec.S256(), sk.X, sk.Y)
	c.Assert(err, IsNil)
	_, _, err = GetTssPubKey(point)
	c.Assert(err, IsNil)

	// create an invalid point
	invalidPoint := crypto.NewECPointNoCurveCheck(btcec.S256(), sk.X, new(big.Int).Add(sk.Y, big.NewInt(1)))
	_, _, err = GetTssPubKey(invalidPoint)
	c.Assert(err, NotNil)

	pk, addr, err := GetTssPubKey(nil)
	c.Assert(err, NotNil)
	c.Assert(pk, Equals, "")
	c.Assert(addr.Bytes(), HasLen, 0)
	SetupBech32Prefix()
	// var point crypto.ECPoint
	c.Assert(json.Unmarshal([]byte(`{"Coords":[70074650318631491136896111706876206496089700125696166275258483716815143842813,72125378038650252881868972131323661098816214918201601489154946637636730727892]}`), &point), IsNil)
	pk, addr, err = GetTssPubKey(point)
	c.Assert(err, IsNil)
	c.Assert(pk, Equals, "oppypub1addwnpepq2dwek9hkrlxjxadrlmy9fr42gqyq6029q0hked46l3u6a9fxqel6v0rcq9")
	c.Assert(addr.String(), Equals, "oppy17l7cyxqzg4xymnl0alrhqwja276s3rns9ep2zu")
}
