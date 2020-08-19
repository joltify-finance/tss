package keysign

import (
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/big"

	bc "github.com/binance-chain/tss-lib/common"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/decred/dcrd/dcrec/edwards/v2"
	"github.com/tendermint/btcd/btcec"
	"github.com/tendermint/tendermint/crypto/ed25519"
	"github.com/tendermint/tendermint/crypto/secp256k1"
)

// Notifier is design to receive keysign signature, success or failure
type Notifier struct {
	MessageID  string
	message    []byte // the message
	poolPubKey string
	resp       chan *bc.SignatureData
}

// NewNotifier create a new instance of Notifier
func NewNotifier(messageID string, message []byte, poolPubKey string) (*Notifier, error) {
	if len(messageID) == 0 {
		return nil, errors.New("messageID is empty")
	}
	if len(message) == 0 {
		return nil, errors.New("message is nil")
	}
	if len(poolPubKey) == 0 {
		return nil, errors.New("pool pubkey is empty")
	}
	return &Notifier{
		MessageID:  messageID,
		message:    message,
		poolPubKey: poolPubKey,
		resp:       make(chan *bc.SignatureData, 1),
	}, nil
}

// verifySignature is a method to verify the signature against the message it signed , if the signature can be verified successfully
// There is a method call VerifyBytes in crypto.PubKey, but we can't use that method to verify the signature, because it always hash the message
// first and then verify the hash of the message against the signature , which is not the case in tss
// go-tss respect the payload it receives , assume the payload had been hashed already by whoever send it in.
func (n *Notifier) verifySignature(data *bc.SignatureData) (bool, error) {
	// we should be able to use any of the pubkeys to verify the signature
	pubKey, err := sdk.GetPubKeyFromBech32(sdk.Bech32PubKeyTypeAccPub, n.poolPubKey)
	if err != nil {
		return false, fmt.Errorf("fail to get pubkey from bech32 pubkey string(%s):%w", n.poolPubKey, err)
	}

	switch pubKey.(type) {
	case secp256k1.PubKeySecp256k1:
		pk := pubKey.(secp256k1.PubKeySecp256k1)
		pub, err := btcec.ParsePubKey(pk[:], btcec.S256())
		if err != nil {
			return false, err
		}
		return ecdsa.Verify(pub.ToECDSA(), n.message, new(big.Int).SetBytes(data.R), new(big.Int).SetBytes(data.S)), nil

	case ed25519.PubKeyEd25519:
		rawPk := pubKey.(ed25519.PubKeyEd25519)
		bPk, err := edwards.ParsePubKey(rawPk[:])
		if err != nil {
			return false, fmt.Errorf("inval ed25519 key with error %w", err)
		}

		newSig, err := edwards.ParseSignature(data.Signature)
		if err != nil {
			println("new sig error, ", err.Error())
		}

		return edwards.Verify(bPk, n.message, newSig.R, newSig.S), nil
	default:
		return false, errors.New("invalid pubkey type")
	}
}

// ProcessSignature is to verify whether the signature is valid
// return value bool , true indicated we already gather all the signature from keysign party, and they are all match
// false means we are still waiting for more signature from keysign party
func (n *Notifier) ProcessSignature(data *bc.SignatureData) (bool, error) {
	// only need to verify the signature when data is not nil
	// when data is nil , which means keysign  failed, there is no signature to be verified in that case
	if data != nil {
		verify, err := n.verifySignature(data)
		if err != nil {
			return false, fmt.Errorf("fail to verify signature: %w", err)
		}
		if !verify {
			return false, nil
		}

	}
	// it is ok to push nil to the resp channel , the receiver will check it
	n.resp <- data
	return true, nil
}

// GetResponseChannel the final signature gathered from keysign party will be returned from the channel
func (n *Notifier) GetResponseChannel() <-chan *bc.SignatureData {
	return n.resp
}
