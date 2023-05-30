package storage

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/binance-chain/tss-lib/ecdsa/keygen"
	tnet "github.com/libp2p/go-libp2p-testing/net"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	. "gopkg.in/check.v1"

	"github.com/joltify-finance/tss/conversion"
)

type FileStateMgrTestSuite struct{}

var _ = Suite(&FileStateMgrTestSuite{})

func TestPackage(t *testing.T) { TestingT(t) }

func (s *FileStateMgrTestSuite) SetUpTest(c *C) {
	conversion.SetupBech32Prefix()
}

func (s *FileStateMgrTestSuite) TestNewFileStateMgr(c *C) {
	folder := os.TempDir()
	f := filepath.Join(folder, "test", "test1", "test2")
	defer func() {
		err := os.RemoveAll(f)
		c.Assert(err, IsNil)
	}()
	fsm, err := NewFileStateMgr(f)
	c.Assert(err, IsNil)
	c.Assert(fsm, NotNil)
	_, err = os.Stat(f)
	c.Assert(err, IsNil)
	fileName, err := fsm.getFilePathName("whatever")
	c.Assert(err, NotNil)
	fileName, err = fsm.getFilePathName("oppypub1addwnpepqtmru87hylm9q0tcza8p0vze2zvmqk0wr0933qr472hggzw2tp4pvy3756g")
	c.Assert(err, IsNil)
	c.Assert(fileName, Equals, filepath.Join(f, "localstate-oppypub1addwnpepqtmru87hylm9q0tcza8p0vze2zvmqk0wr0933qr472hggzw2tp4pvy3756g.json"))
}

func (s *FileStateMgrTestSuite) TestSaveLocalState(c *C) {
	stateItem := KeygenLocalState{
		PubKey:    "wasdfasdfasdfasdfasdfasdf",
		LocalData: keygen.NewLocalPartySaveData(5),
		ParticipantKeys: []string{
			"A", "B", "C",
		},
		LocalPartyKey: "A",
	}
	folder := os.TempDir()
	f := filepath.Join(folder, "test", "test1", "test2")
	defer func() {
		err := os.RemoveAll(f)
		c.Assert(err, IsNil)
	}()
	fsm, err := NewFileStateMgr(f)
	c.Assert(err, IsNil)
	c.Assert(fsm, NotNil)
	c.Assert(fsm.SaveLocalState(stateItem), NotNil)
	stateItem.PubKey = "oppypub1addwnpepqtmru87hylm9q0tcza8p0vze2zvmqk0wr0933qr472hggzw2tp4pvy3756g"
	c.Assert(fsm.SaveLocalState(stateItem), IsNil)
	filePathName := filepath.Join(f, "localstate-"+stateItem.PubKey+".json")
	_, err = os.Stat(filePathName)
	c.Assert(err, IsNil)
	item, err := fsm.GetLocalState(stateItem.PubKey)
	c.Assert(err, IsNil)
	c.Assert(reflect.DeepEqual(stateItem, item), Equals, true)
}

func (s *FileStateMgrTestSuite) TestSaveAddressBook(c *C) {
	testAddresses := make(map[peer.ID][]ma.Multiaddr)
	var t *testing.T
	id1 := tnet.RandIdentityOrFatal(t)
	id2 := tnet.RandIdentityOrFatal(t)
	id3 := tnet.RandIdentityOrFatal(t)
	mockAddr, err := ma.NewMultiaddr("/ip4/192.168.3.5/tcp/6668")
	c.Assert(err, IsNil)
	peers := []peer.ID{id1.ID(), id2.ID(), id3.ID()}
	for _, each := range peers {
		testAddresses[each] = []ma.Multiaddr{mockAddr}
	}
	folder := os.TempDir()
	f := filepath.Join(folder, "test")
	defer func() {
		err := os.RemoveAll(f)
		c.Assert(err, IsNil)
	}()
	fsm, err := NewFileStateMgr(f)
	c.Assert(err, IsNil)
	c.Assert(fsm, NotNil)
	c.Assert(fsm.SaveAddressBook(testAddresses), IsNil)
	filePathName := filepath.Join(f, "address_book.seed")
	_, err = os.Stat(filePathName)
	c.Assert(err, IsNil)
	item, err := fsm.RetrieveP2PAddresses()
	c.Assert(err, IsNil)
	c.Assert(item, HasLen, 3)
}
