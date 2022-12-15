package p2p

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/protocol"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"google.golang.org/protobuf/proto"

	"github.com/joltify-finance/tss/messages"
)

var (
	ErrJoinPartyTimeout = errors.New("fail to join party, timeout")
	ErrLeaderNotReady   = errors.New("leader not reachable")
	ErrSignReceived     = errors.New("signature received")
	ErrNotActiveSigner  = errors.New("not active signer")
	ErrSigGenerated     = errors.New("signature generated")
)

type PartyCoordinator struct {
	logger             zerolog.Logger
	host               host.Host
	stopChan           chan struct{}
	timeout            time.Duration
	peersGroup         map[string]*PeerStatus
	joinPartyGroupLock *sync.RWMutex
	streamMgr          *StreamMgr
	wg                 *sync.WaitGroup
}

// NewPartyCoordinator create a new instance of PartyCoordinator
func NewPartyCoordinator(host host.Host, timeout time.Duration) *PartyCoordinator {
	// if no timeout is given, default to 10 seconds
	if timeout.Nanoseconds() == 0 {
		timeout = 10 * time.Second
	}
	pc := &PartyCoordinator{
		logger:             log.With().Str("module", "party_coordinator").Logger(),
		host:               host,
		stopChan:           make(chan struct{}),
		timeout:            timeout,
		peersGroup:         make(map[string]*PeerStatus),
		joinPartyGroupLock: &sync.RWMutex{},
		streamMgr:          NewStreamMgr(),
		wg:                 &sync.WaitGroup{},
	}
	host.SetStreamHandler(joinPartyProtocol, pc.HandleStream)
	host.SetStreamHandler(joinPartyProtocolWithLeader, pc.HandleStreamWithLeader)
	return pc
}

// Stop the PartyCoordinator rune
func (pc *PartyCoordinator) Stop() {
	defer pc.logger.Info().Msg("stop party coordinator")
	pc.host.RemoveStreamHandler(joinPartyProtocol)
	pc.host.RemoveStreamHandler(joinPartyProtocolWithLeader)
	close(pc.stopChan)
	pc.wg.Wait()
}

// Start the PartyCoordinator rune
func (pc *PartyCoordinator) Start() {
	// we may put some debug routine here
}

func (pc *PartyCoordinator) processRespMsg(respMsg *messages.JoinPartyLeaderComm, stream network.Stream) {

	remotePeer := stream.Conn().RemotePeer().String()
	pc.joinPartyGroupLock.RLock()
	defer pc.joinPartyGroupLock.RUnlock()
	peerGroup, ok := pc.peersGroup[respMsg.ID]
	if !ok {
		pc.logger.Info().Msgf("message ID from peer(%s) can not be found", remotePeer)
		return
	}
	peerGroup.leaderSetLock.RLock()
	leader := peerGroup.leader
	peerGroup.leaderSetLock.RUnlock()
	if remotePeer == leader {
		peerGroup.setLeaderResponse(respMsg)
		peerGroup.notify <- true
		return
	}
	pc.logger.Info().Msgf("this party(%s) is not the leader(%s) as expected", remotePeer, peerGroup.leader)
	return
}

func (pc *PartyCoordinator) processReqMsg(requestMsg *messages.JoinPartyLeaderComm, stream network.Stream) error {
	pc.joinPartyGroupLock.Lock()
	peerGroup, ok := pc.peersGroup[requestMsg.ID]
	pc.joinPartyGroupLock.Unlock()
	if !ok {
		pc.logger.Info().Msg("this party is not ready")
		return errors.New("party not ready")
	}
	remotePeer := stream.Conn().RemotePeer()
	partyFormed, err := peerGroup.updatePeer(remotePeer, stream)
	if err != nil {
		pc.logger.Error().Err(err).Msg("receive msg from unknown peer")
		return errors.New("msg from unknown")
	}
	if partyFormed {
		peerGroup.notify <- true
	}
	return nil
}

func (pc *PartyCoordinator) HandleStream(stream network.Stream) {
	remotePeer := stream.Conn().RemotePeer()
	logger := pc.logger.With().Str("remote peer", remotePeer.String()).Logger()
	logger.Debug().Msg("reading from join party request")
	payload, err := ReadStreamWithBuffer(stream)
	if err != nil {
		logger.Err(err).Msgf("fail to read payload from stream")
		pc.streamMgr.AddStream("UNKNOWN", stream)
		return
	}
	var msg messages.JoinPartyRequest
	if err := proto.Unmarshal(payload, &msg); err != nil {
		logger.Err(err).Msg("fail to unmarshal join party request")
		pc.streamMgr.AddStream("UNKNOWN", stream)
		return
	}
	pc.streamMgr.AddStream(msg.ID, stream)
	pc.joinPartyGroupLock.Lock()
	peerGroup, ok := pc.peersGroup[msg.ID]
	pc.joinPartyGroupLock.Unlock()
	if !ok {
		pc.logger.Info().Msg("this party is not ready")
		return
	}
	newFound, err := peerGroup.updatePeer(remotePeer, stream)
	if err != nil {
		pc.logger.Error().Err(err).Msg("receive msg from unknown peer")
		return
	}
	if newFound {
		peerGroup.newFound <- true
	}
}

// HandleStream handle party coordinate stream
func (pc *PartyCoordinator) HandleStreamWithLeader(stream network.Stream) {
	remotePeer := stream.Conn().RemotePeer()
	logger := pc.logger.With().Str("remote peer", remotePeer.String()).Logger()
	logger.Debug().Msg("reading from join party request")
	payload, err := ReadStreamWithBuffer(stream)
	if err != nil {
		logger.Err(err).Msgf("fail to read payload from stream")
		pc.streamMgr.AddStream("UNKNOWN", stream)
		return
	}

	var msg messages.JoinPartyLeaderComm
	err = proto.Unmarshal(payload, &msg)
	if err != nil {
		logger.Err(err).Msg("fail to unmarshal party data")
		pc.streamMgr.AddStream("UNKNOWN", stream)
		return
	}
	switch msg.MsgType {
	case "request":
		stream.Scope().ReserveMemory(JOINPARTYSIZE, network.ReservationPriorityAlways)
		err := pc.processReqMsg(&msg, stream)
		respMsg := "request received"
		if err != nil {
			respMsg = "invalid request"
		}
		err = WriteStreamWithBuffer([]byte(respMsg), stream)
		if err != nil {
			pc.logger.Error().Err(err).Msgf("fail to send response to leader")
		}
		pc.streamMgr.AddStream(msg.ID, stream)
		return
	default:
		logger.Err(err).Msg("fail to process this message")
		pc.streamMgr.AddStream("UNKNOWN", stream)
		return
	}
}

func (pc *PartyCoordinator) RemoveJoinPartyGroups(messageID string) {

	pc.joinPartyGroupLock.Lock()
	defer pc.joinPartyGroupLock.Unlock()
	status, ok := pc.peersGroup[messageID]
	if !ok {
		return
	}

	delete(pc.peersGroup, messageID)
	done := false
	for {
		select {
		case <-status.notify:
		case <-time.After(time.Second):
			done = true
			break
		}
		if done {
			break
		}
	}
	done = false
	for {
		select {
		case <-status.newFound:
		case <-time.After(time.Second):
			done = true
			break
		}
		if done {
			break
		}
	}

}

func (pc *PartyCoordinator) createJoinPartyGroups(messageID, leader string, peers []string, threshold int) (*PeerStatus, error) {
	pIDs, err := pc.getPeerIDs(peers)
	if err != nil {
		pc.logger.Error().Err(err).Msg("fail to parse peer id")
		return nil, err
	}
	pc.joinPartyGroupLock.Lock()
	defer pc.joinPartyGroupLock.Unlock()
	peerStatus := NewPeerStatus(pIDs, pc.host.ID(), leader, threshold)
	pc.peersGroup[messageID] = peerStatus
	return peerStatus, nil
}

func (pc *PartyCoordinator) getPeerIDs(ids []string) ([]peer.ID, error) {
	result := make([]peer.ID, len(ids))
	for i, item := range ids {
		pid, err := peer.Decode(item)
		if err != nil {
			return nil, fmt.Errorf("fail to decode peer id(%s):%w", item, err)
		}
		result[i] = pid
	}
	return result, nil
}

func (pc *PartyCoordinator) sendResponseToAll(msg *messages.JoinPartyLeaderComm, peers []peer.ID, p *sync.Map) {
	msg.MsgType = "response"
	msgSend, err := proto.Marshal(msg)
	if err != nil {
		pc.logger.Error().Msg("fail to marshal the message")
		return
	}
	var wg sync.WaitGroup

	if peers == nil {
		p.Range(func(key, value any) bool {
			wg.Add(1)
			go func() {
				defer wg.Done()
				stream := value.(network.Stream)
				if _, err := pc.sendMsgToPeerWithStream(msgSend, msg.ID, stream, true); err != nil {
					pc.logger.Error().Err(err).Msg("error in send the join party request to peer")
				}
			}()
			return true
		})
		wg.Wait()
		return
	}

	wg.Add(len(peers))
	for _, el := range peers {
		go func(peer peer.ID) {
			defer wg.Done()
			if peer == pc.host.ID() {
				return
			}
			if _, err := pc.sendMsgToPeer(msgSend, msg.ID, peer, joinPartyProtocolWithLeader, true); err != nil {
				pc.logger.Error().Err(err).Msg("error in send the join party request to peer")
			}
		}(el)
	}
	wg.Wait()
}

func (pc *PartyCoordinator) sendRequestToLeader(msg *messages.JoinPartyLeaderComm, leader peer.ID) (bool, error) {
	msg.MsgType = "request"
	msgSend, err := proto.Marshal(msg)
	if err != nil {
		pc.logger.Error().Msg("fail to marshal the message")
		return true, err
	}
	var resp string
	if resp, err = pc.sendMsgToPeer(msgSend, msg.ID, leader, joinPartyProtocolWithLeader, true); err != nil {
		pc.logger.Error().Err(err).Msg("error in send the join party request to leader")
		return true, errors.New("fail to send request to leader")
	}
	if resp == "request received" {
		pc.logger.Info().Msgf("we have got the confirmation from the leader")
		return false, nil
	}
	return true, nil
}

func (pc *PartyCoordinator) sendRequestToAll(msgID string, msgSend []byte, peers []peer.ID) {
	var wg sync.WaitGroup
	wg.Add(len(peers))
	for _, el := range peers {
		go func(peer peer.ID) {
			defer wg.Done()
			if peer == pc.host.ID() {
				return
			}
			if _, err := pc.sendMsgToPeer(msgSend, msgID, peer, joinPartyProtocol, false); err != nil {
				pc.logger.Error().Err(err).Msg("error in send the join party request to peer")
			}
		}(el)
	}
	wg.Wait()
}

func (pc *PartyCoordinator) sendMsgToPeerWithStream(msgBuf []byte, msgID string, stream network.Stream, needResponse bool) (string, error) {
	var err error
	defer pc.streamMgr.AddStream(msgID, stream)
	err = WriteStreamWithBuffer(msgBuf, stream)
	if err != nil {
		return "", fmt.Errorf("fail to write message to stream:%w", err)
	}
	pc.logger.Info().Msgf("write to stream to (%s) successfully", stream.Conn().RemotePeer().String())

	var resp string
	if needResponse {
		data, err := ReadStreamWithBuffer(stream)
		if err != nil {
			pc.logger.Error().Err(err).Msgf("fail to get the message in send message to peer")
			return "", err
		}
		resp = string(data)
		return resp, nil
	}

	return "", nil
}

func (pc *PartyCoordinator) sendMsgToPeer(msgBuf []byte, msgID string, remotePeer peer.ID, protoc protocol.ID, needResponse bool) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*4)
	defer cancel()
	var stream network.Stream
	var streamError error
	streamGetChan := make(chan struct{})
	var err error
	go func() {
		defer close(streamGetChan)
		pc.logger.Debug().Msgf("try to open stream to (%s) ", remotePeer)
		ctx := network.WithUseTransient(ctx, "join_party")
		stream, err = pc.host.NewStream(ctx, remotePeer, protoc)
		if err != nil {
			streamError = fmt.Errorf("fail to create stream to peer(%s):%w", remotePeer, err)
			return
		}

		if err := stream.Scope().ReserveMemory(JOINPARTYSIZE, network.ReservationPriorityAlways); err != nil {
			pc.logger.Error().Err(err).Msgf("fail to reserve the memory")
			stream.Close()
			streamError = err
		}
	}()
	select {
	case <-streamGetChan:
		if streamError != nil {
			pc.logger.Error().Err(streamError).Msg("fail to open stream")
			return "", streamError
		}
	case <-ctx.Done():
		pc.logger.Error().Err(ctx.Err()).Msg("fail to open stream with context timeout")
		return "", ctx.Err()
	}

	defer pc.streamMgr.AddStream(msgID, stream)
	pc.logger.Debug().Msgf("open stream to (%s) successfully", remotePeer)
	err = WriteStreamWithBuffer(msgBuf, stream)
	if err != nil {
		return "", fmt.Errorf("fail to write message to stream:%w", err)
	}

	var resp string
	if needResponse {
		data, err := ReadStreamWithBuffer(stream)
		if err != nil {
			conn := stream.Conn()
			pc.logger.Info().Msgf(">>> we have %v streams and transit is %v", conn.Stat().NumStreams, conn.Stat().Transient)
			errclose := conn.Close()
			if errclose != nil {
				pc.logger.Error().Err(err).Msgf("fail to close this conn")
			}
			pc.logger.Error().Err(err).Msgf("fail to get the message")
			return "", err
		}
		resp = string(data)
		// if it is not the correct response we do not handle the following listen logic
		if resp != "request received" {
			return resp, nil
		}

		go func() {
			// once the stream is close , though we are pending here, it should be terminated.
			payload, err := ReadStreamWithBufferNoDeadline(stream)
			if err != nil {
				pc.logger.Error().Err(err).Msgf("fail to get the message for leader response")
				return
			}

			var msg messages.JoinPartyLeaderComm
			err = proto.Unmarshal(payload, &msg)
			if err != nil {
				pc.logger.Err(err).Msg("fail to unmarshal party data")
				pc.streamMgr.AddStream("UNKNOWN", stream)
				return
			}
			pc.processRespMsg(&msg, stream)
			err = WriteStreamWithBuffer([]byte("copy_done"), stream)
			if err != nil {
				pc.logger.Error().Err(err).Msgf("fail to send response to leader")
			}
		}()
		return resp, nil
	}

	return "", nil
}

func (pc *PartyCoordinator) joinPartyMember(msgID string, leader string, threshold int, sigChan chan string) ([]peer.ID, error) {
	peerGroup, err := pc.createJoinPartyGroups(msgID, leader, []string{leader}, threshold)
	if err != nil {
		return nil, fmt.Errorf("fail to create join party:%w", err)
	}

	leaderPeerID, err := peer.Decode(leader)
	if err != nil {
		return nil, fmt.Errorf("fail to decode peer id(%s):%w", leader, err)
	}
	peerGroup.leaderSetLock.Lock()
	peerGroup.leader = leader
	peerGroup.leaderSetLock.Unlock()
	msg := messages.JoinPartyLeaderComm{
		ID: msgID,
	}

	rand.Seed(time.Now().UnixNano())
	min := 300
	max := 2000
	randDelay := rand.Intn(max-min+1) + min
	time.Sleep(time.Millisecond * time.Duration(randDelay))
	var wg sync.WaitGroup
	done := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
				cont, err := pc.sendRequestToLeader(&msg, leaderPeerID)
				if err != nil {
					pc.logger.Debug().Msg("the leader fail to receive our request")
				}
				// we do not continue, as we have received the response
				if !cont {
					return
				}
			}
			time.Sleep(time.Millisecond * 500)
		}
	}()
	// this is the total time TSS will wait for the party to form
	var sigNotify string
	wg.Add(1)
	go func() {
		defer wg.Done()
		// now we wait for the leader to notify us who we do the keygen/keysign with
		select {
		case <-peerGroup.notify:
			pc.logger.Debug().Msg("we have receive the response from the leader")
			close(done)
			return

			// the members should have a little bit delay to get the msg from the leader
		case <-time.After(pc.timeout + time.Second*5):
			// timeout
			close(done)
			pc.logger.Error().Msg("the leader has not reply us")
			return
		case result := <-sigChan:
			sigNotify = result
			close(done)
			return
		}
	}()
	wg.Wait()

	if sigNotify == "signature received" {
		return nil, ErrSignReceived
	}

	leaderResp := peerGroup.getLeaderResponse()
	pc.RemoveJoinPartyGroups(msgID)
	if leaderResp == nil {
		pc.logger.Error().Msgf("leader(%s) is not reachable", leaderPeerID)
		return nil, ErrLeaderNotReady
	}

	onlineNodes := leaderResp.PeerIDs
	// we trust the returned nodes returned by the leader, if tss fail, the leader
	// also will get blamed.
	pIDs, err := pc.getPeerIDs(onlineNodes)
	if err != nil {
		pc.logger.Error().Err(err).Msg("fail to parse peer id")
		return nil, err
	}
	if len(pIDs) < threshold {
		return pIDs, errors.New("not enough peer")
	}

	if leaderResp.Type == messages.JoinPartyLeaderComm_Success {
		return pIDs, nil
	}
	pc.logger.Error().Msg("leader response with join party timeout")
	return pIDs, ErrJoinPartyTimeout
}

func (pc *PartyCoordinator) joinPartyLeader(msgID string, peers []string, threshold int, sigChan chan string) ([]peer.ID, error) {
	peerGroup, err := pc.createJoinPartyGroups(msgID, pc.host.ID().String(), peers, threshold)
	if err != nil {
		pc.logger.Error().Err(err).Msg("fail to create the join party group")
		return nil, err
	}
	peerGroup.peerStatusLock.Lock()
	peerGroup.leader = pc.host.ID().String()
	peerGroup.peerStatusLock.Unlock()

	var sigNotify string
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-peerGroup.notify:
				pc.logger.Debug().Msg("we have enough participants")
				return

			case <-time.After(pc.timeout):
				// timeout
				pc.logger.Error().Msg("leader waits for peers timeout")
				return
			case result := <-sigChan:
				sigNotify = result
			}
		}
	}()
	wg.Wait()
	defer pc.RemoveJoinPartyGroups(msgID)
	if sigNotify == "signature received" {
		return nil, ErrSignReceived
	}
	onlinePeers, _ := peerGroup.getPeersStatus()
	onlinePeers = append(onlinePeers, pc.host.ID())

	tssNodes := make([]string, len(onlinePeers))
	for i, el := range onlinePeers {
		tssNodes[i] = el.String()
	}

	msg := messages.JoinPartyLeaderComm{
		ID:      msgID,
		Type:    messages.JoinPartyLeaderComm_Success,
		PeerIDs: tssNodes,
	}
	// we put ourselves(leader) in the online list, so need threshold +1
	if len(onlinePeers) < threshold+1 {
		// we notify the failure of the join party to everyone
		msg.Type = messages.JoinPartyLeaderComm_Timeout
		pc.sendResponseToAll(&msg, nil, peerGroup.streams)
		return onlinePeers, ErrJoinPartyTimeout
	}
	// we notify all the peers who to run keygen/keysign
	// if a nodes is not in the list, it means he is not selected by the leader to run the tss
	pc.sendResponseToAll(&msg, nil, peerGroup.streams)
	return onlinePeers, nil
}

func (pc *PartyCoordinator) JoinPartyWithLeader(msgID string, blockHeight int64, peers []string, threshold int, signChan chan string) ([]peer.ID, string, error) {
	leader, err := LeaderNode(msgID, blockHeight, peers)
	if err != nil {
		return nil, "", err
	}
	if pc.host.ID().String() == leader {
		onlines, err := pc.joinPartyLeader(msgID, peers, threshold, signChan)
		return onlines, leader, err
	}
	// now we are just the normal peer
	onlines, err := pc.joinPartyMember(msgID, leader, threshold, signChan)
	return onlines, leader, err
}

// JoinPartyWithRetry this method provide the functionality to join party with retry and back off
func (pc *PartyCoordinator) JoinPartyWithRetry(msgID string, peers []string) ([]peer.ID, error) {
	msg := messages.JoinPartyRequest{
		ID: msgID,
	}
	msgSend, err := proto.Marshal(&msg)
	if err != nil {
		pc.logger.Error().Msg("fail to marshal the message")
		return nil, err
	}

	peerGroup, err := pc.createJoinPartyGroups(msg.ID, "NONE", peers, 1)
	if err != nil {
		pc.logger.Error().Err(err).Msg("fail to create the join party group")
		return nil, err
	}
	defer pc.RemoveJoinPartyGroups(msg.ID)
	_, offline := peerGroup.getPeersStatus()
	var wg sync.WaitGroup
	done := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
				pc.sendRequestToAll(msgID, msgSend, offline)
			}
			time.Sleep(time.Second)
		}
	}()
	// this is the total time TSS will wait for the party to form
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-peerGroup.notify:
				pc.logger.Debug().Msg("we have found the new peer")
				if peerGroup.getCoordinationStatus() {
					close(done)
					return
				}
			case <-time.After(pc.timeout):
				// timeout
				close(done)
				return
			}
		}
	}()

	wg.Wait()
	onlinePeers, _ := peerGroup.getPeersStatus()
	pc.sendRequestToAll(msgID, msgSend, onlinePeers)
	// we always set ourselves as online
	onlinePeers = append(onlinePeers, pc.host.ID())
	if len(onlinePeers) == len(peers) {
		return onlinePeers, nil
	}
	return onlinePeers, ErrJoinPartyTimeout
}

func (pc *PartyCoordinator) ReleaseStream(msgID string) {
	pc.streamMgr.ReleaseStream(msgID)
}
