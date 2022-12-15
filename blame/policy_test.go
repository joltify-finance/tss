package blame

import (
	"sort"
	"sync"
	"testing"

	bkg "github.com/binance-chain/tss-lib/ecdsa/keygen"
	btss "github.com/binance-chain/tss-lib/tss"
	"github.com/libp2p/go-libp2p-core/peer"
	. "gopkg.in/check.v1"

	"github.com/joltify-finance/tss/conversion"
	"github.com/joltify-finance/tss/messages"
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

func TestPackage(t *testing.T) { TestingT(t) }

type policyTestSuite struct {
	blameMgr *Manager
}

var _ = Suite(&policyTestSuite{})

func (p *policyTestSuite) SetUpTest(c *C) {
	p.blameMgr = NewBlameManager()
	conversion.SetupBech32Prefix()
	p1, err := peer.Decode(testPeers[0])
	c.Assert(err, IsNil)
	p2, err := peer.Decode(testPeers[1])
	c.Assert(err, IsNil)
	p3, err := peer.Decode(testPeers[2])
	c.Assert(err, IsNil)
	p.blameMgr.SetLastUnicastPeer(p1, "testType")
	p.blameMgr.SetLastUnicastPeer(p2, "testType")
	p.blameMgr.SetLastUnicastPeer(p3, "testType")
	localTestPubKeys := testPubKeys[:]
	sort.Strings(localTestPubKeys)
	partiesID, localPartyID, err := conversion.GetParties(localTestPubKeys, testPubKeys[0])
	c.Assert(err, IsNil)
	partyIDMap := conversion.SetupPartyIDMap(partiesID)
	err = conversion.SetupIDMaps(partyIDMap, p.blameMgr.PartyIDtoP2PID)
	c.Assert(err, IsNil)
	outCh := make(chan btss.Message, len(partiesID))
	endCh := make(chan bkg.LocalPartySaveData, len(partiesID))
	ctx := btss.NewPeerContext(partiesID)
	params := btss.NewParameters(ctx, localPartyID, len(partiesID), 3)
	keyGenParty := bkg.NewLocalParty(params, outCh, endCh)

	testPartyMap := new(sync.Map)
	testPartyMap.Store("", keyGenParty)
	p.blameMgr.SetPartyInfo(testPartyMap, partyIDMap)
}

func (p *policyTestSuite) TestGetUnicastBlame(c *C) {
	_, err := p.blameMgr.GetUnicastBlame("testTypeWrong")
	c.Assert(err, NotNil)
	_, err = p.blameMgr.GetUnicastBlame("testType")
	c.Assert(err, IsNil)
}

func (p *policyTestSuite) TestGetBroadcastBlame(c *C) {
	pi := p.blameMgr.partyInfo

	r1 := btss.MessageRouting{
		From:                    pi.PartyIDMap["1"],
		To:                      nil,
		IsBroadcast:             false,
		IsToOldCommittee:        false,
		IsToOldAndNewCommittees: false,
	}
	msg := messages.WireMessage{
		Routing:   &r1,
		RoundInfo: "key1",
		Message:   nil,
	}

	p.blameMgr.roundMgr.Set("key1", &msg)
	blames, err := p.blameMgr.GetBroadcastBlame("key1")
	c.Assert(err, IsNil)
	var blamePubKeys []string
	for _, el := range blames {
		blamePubKeys = append(blamePubKeys, el.Pubkey)
	}
	sort.Strings(blamePubKeys)
	expected := testPubKeys[2:]
	sort.Strings(expected)
	c.Assert(blamePubKeys, DeepEquals, expected)
}

func (p *policyTestSuite) TestTssWrongShareBlame(c *C) {
	pi := p.blameMgr.partyInfo

	r1 := btss.MessageRouting{
		From:                    pi.PartyIDMap["1"],
		To:                      nil,
		IsBroadcast:             false,
		IsToOldCommittee:        false,
		IsToOldAndNewCommittees: false,
	}
	msg := messages.WireMessage{
		Routing:   &r1,
		RoundInfo: "key2",
		Message:   nil,
	}
	target, err := p.blameMgr.TssWrongShareBlame(&msg)
	c.Assert(err, IsNil)
	c.Assert(target, Equals, "oppypub1zcjduepqjykgrc8kehvvauxuq9regzvueec2lz3xwtcpf9mw77pu5h8z9r2sysjdf9")
}

func (p *policyTestSuite) TestTssMissingShareBlame(c *C) {
	localTestPubKeys := testPubKeys[:]
	sort.Strings(localTestPubKeys)
	blameMgr := p.blameMgr
	acceptedShares := blameMgr.acceptedShares
	// we only allow a message be updated only once.
	blameMgr.acceptShareLocker.Lock()
	acceptedShares[RoundInfo{0, "testRound", "123:0"}] = []string{"1", "2"}
	acceptedShares[RoundInfo{1, "testRound", "123:0"}] = []string{"1"}
	blameMgr.acceptShareLocker.Unlock()
	nodes, _, err := blameMgr.TssMissingShareBlame(2)
	c.Assert(err, IsNil)
	c.Assert(nodes[0].Pubkey, Equals, localTestPubKeys[3])
	// we test if the missing share happens in round2
	blameMgr.acceptShareLocker.Lock()
	acceptedShares[RoundInfo{0, "testRound", "123:0"}] = []string{"1", "2", "3"}
	blameMgr.acceptShareLocker.Unlock()
	nodes, _, err = blameMgr.TssMissingShareBlame(2)
	c.Assert(err, IsNil)
	results := []string{nodes[0].Pubkey, nodes[1].Pubkey}
	sort.Strings(results)
	c.Assert(results, DeepEquals, localTestPubKeys[2:])
}
