package main

import (
	"encoding/json"
	"math"

	"github.com/dedis/paper_17_sosp_omniledger/byzcoin_lib/protocol"
	"github.com/dedis/paper_17_sosp_omniledger/byzcoin_lib/protocol/blockchain"
	"github.com/dedis/paper_17_sosp_omniledger/byzcoin_lib/protocol/blockchain/blkparser"
	"github.com/dedis/paper_17_sosp_omniledger/crypto"
	"gopkg.in/dedis/onet.v1"
	"gopkg.in/dedis/onet.v1/log"
)

// Ntree is a basic implementation of a byzcoin consensus protocol using a tree
// and each verifiers will have independent signatures. The messages are then
// bigger and the verification time is also longer.
type Ntree struct {
	*onet.TreeNodeInstance
	// the block to sign
	block *blockchain.TrBlock
	// channel to notify the end of the verification of a block
	verifyBlockChan chan bool

	// channel to notify the end of the verification of a signature request
	verifySignatureRequestChan chan bool

	// the temps signature you receive in the first phase
	tempBlockSig         *NaiveBlockSignature
	tempBlockSigReceived int

	// the temps signature you receive in the second phase
	tempSignatureResponse         *RoundSignatureResponse
	tempSignatureResponseReceived int

	announceChan chan struct {
		*onet.TreeNode
		BlockAnnounce
	}

	blockSignatureChan chan struct {
		*onet.TreeNode
		NaiveBlockSignature
	}

	roundSignatureRequestChan chan struct {
		*onet.TreeNode
		RoundSignatureRequest
	}

	roundSignatureResponseChan chan struct {
		*onet.TreeNode
		RoundSignatureResponse
	}

	onDoneCallback func(*NtreeSignature)
}

// NewNtreeProtocol returns the NtreeProtocol  initialized
func NewNtreeProtocol(node *onet.TreeNodeInstance) (*Ntree, error) {
	nt := &Ntree{
		TreeNodeInstance:           node,
		verifyBlockChan:            make(chan bool),
		verifySignatureRequestChan: make(chan bool),
		tempBlockSig:               new(NaiveBlockSignature),
		tempSignatureResponse:      &RoundSignatureResponse{new(NaiveBlockSignature)},
	}

	if err := node.RegisterChannel(&nt.announceChan); err != nil {
		return nt, err
	}
	if err := node.RegisterChannel(&nt.blockSignatureChan); err != nil {
		return nt, err
	}
	if err := node.RegisterChannel(&nt.roundSignatureRequestChan); err != nil {
		return nt, err
	}
	if err := node.RegisterChannel(&nt.roundSignatureResponseChan); err != nil {
		return nt, err
	}

	go nt.listen()
	return nt, nil
}

// NewNTreeRootProtocol returns a NtreeProtocol with a set of transactions to
// sign for this round.
func NewNTreeRootProtocol(node *onet.TreeNodeInstance, transactions []blkparser.Tx) (*Ntree, error) {
	nt, _ := NewNtreeProtocol(node)
	var err error
	nt.block, err = byzcoin.GetBlock(transactions, "", "")
	return nt, err
}

// Start announces the new block to sign
func (nt *Ntree) Start() error {
	log.Lvl3(nt.Name(), "Start()")
	go byzcoin.VerifyBlock(nt.block, "", "", nt.verifyBlockChan)
	for _, tn := range nt.Children() {
		if err := nt.SendTo(tn, &BlockAnnounce{nt.block}); err != nil {
			return err
		}
	}
	return nil
}

// Dispatch do nothing yet since we use an implicit listen function in a go
// routine
func (nt *Ntree) Dispatch() error {
	// do nothing
	return nil
}

// listen will select on the differents channels
func (nt *Ntree) listen() {
	for {
		select {
		// Dispatch the block through the whole tree
		case msg := <-nt.announceChan:
			log.Lvl3(nt.Name(), "Received Block announcement")
			nt.block = msg.BlockAnnounce.Block
			// verify the block
			go byzcoin.VerifyBlock(nt.block, "", "", nt.verifyBlockChan)
			if nt.IsLeaf() {
				nt.startBlockSignature()
				continue
			}
			for _, tn := range nt.Children() {
				err := nt.SendTo(tn, &msg.BlockAnnounce)
				if err != nil {
					log.Error(nt.Name(),
						"couldn't send to", tn.Name(),
						err)
				}
			}
			// generate your own signature / exception and pass that up to the
			// root
		case msg := <-nt.blockSignatureChan:
			nt.handleBlockSignature(&msg.NaiveBlockSignature)
			// Dispatch the signature + expcetion made before through the whole
			// tree
		case msg := <-nt.roundSignatureRequestChan:
			log.Lvl3(nt.Name(), " Signature Request Received")
			go nt.verifySignatureRequest(&msg.RoundSignatureRequest)

			if nt.IsLeaf() {
				nt.startSignatureResponse()
				continue
			}

			for _, tn := range nt.Children() {
				err := nt.SendTo(tn, &msg.RoundSignatureRequest)
				if err != nil {
					log.Error(nt.Name(), "couldn't sent to",
						tn.Name(), err)
				}
			}
			// Decide if we want to sign this or not
		case msg := <-nt.roundSignatureResponseChan:
			nt.handleRoundSignatureResponse(&msg.RoundSignatureResponse)
		}
	}
}

// startBlockSignature will  send the first signature up the tree.
func (nt *Ntree) startBlockSignature() {
	log.Lvl3(nt.Name(), "Starting Block Signature Phase")
	nt.computeBlockSignature()
	if err := nt.SendTo(nt.Parent(), nt.tempBlockSig); err != nil {
		log.Error(err)
	}

}

// computeBlockSignature compute the signature out of the block.
func (nt *Ntree) computeBlockSignature() {
	// wait the end of verification of the block
	ok := <-nt.verifyBlockChan
	//marshal the blck
	marshalled, err := json.Marshal(nt.block)
	if err != nil {
		log.Error(err)
		return
	}

	// if stg is wrong, we put exceptions
	if !ok {
		nt.tempBlockSig.Exceptions = append(nt.tempBlockSig.Exceptions, Exception{nt.TreeNode().ID})
	} else { // we put signature
		schnorr, _ := crypto.SignSchnorr(nt.Suite(), nt.Private(), marshalled)
		nt.tempBlockSig.Sigs = append(nt.tempBlockSig.Sigs, schnorr)
	}
	log.Lvl3(nt.Name(), "Block Signature Computed")
}

// handleBlockSignature will look if the block is valid. If it is, we sign it.
// if it is not, we don't sign it and we put up an exception.
func (nt *Ntree) handleBlockSignature(msg *NaiveBlockSignature) {
	nt.tempBlockSig.Sigs = append(nt.tempBlockSig.Sigs, msg.Sigs...)
	nt.tempBlockSig.Exceptions = append(nt.tempBlockSig.Exceptions, msg.Exceptions...)
	nt.tempBlockSigReceived++
	// not enough signatures for the moment
	log.Lvl3(nt.Name(), "Handle Block Signature(", nt.tempBlockSigReceived, "/", len(nt.Children()), ")")
	if nt.tempBlockSigReceived < len(nt.Children()) {
		return
	}
	nt.computeBlockSignature()
	// if we are root => going further in the protocol
	if nt.IsRoot() {
		nt.startSignatureRequest(msg)
		return
	}
	// send msg up the tree
	if err := nt.SendTo(nt.Parent(), nt.tempBlockSig); err != nil {
		log.Error(err)
	}

	log.Lvl3(nt.Name(), "Handle Block Signature => Sent UP")
}

// startSignatureRequest is the root starting the new phase. It will broadcast
// the signature of everyone amongst the tree.
func (nt *Ntree) startSignatureRequest(msg *NaiveBlockSignature) {
	log.Lvl3(nt.Name(), "Start Signature Request")
	sigRequest := &RoundSignatureRequest{msg}
	go nt.verifySignatureRequest(sigRequest)
	for _, tn := range nt.Children() {
		if err := nt.SendTo(tn, sigRequest); err != nil {
			log.Error(nt.Name(), "couldn't send to", tn.Name(), err)
		}
	}
}

// Go routine that will do the verification of the signature request in
// parrallele
func (nt *Ntree) verifySignatureRequest(msg *RoundSignatureRequest) {
	// verification if we have too much exceptions
	threshold := int(math.Ceil(float64(len(nt.Tree().List())) / 3.0))
	if len(msg.Exceptions) > threshold {
		nt.verifySignatureRequestChan <- false
	}

	// verification of all the signatures
	var goodSig int
	marshalled, _ := json.Marshal(nt.block)
	for _, sig := range msg.Sigs {
		if err := crypto.VerifySchnorr(nt.Suite(), nt.Public(), marshalled, sig); err == nil {
			goodSig++
		}
	}

	log.Lvl3(nt.Name(), "Verification of signatures =>", goodSig, "/", len(msg.Sigs), ")")
	// enough good signatures ?
	if goodSig <= 2*threshold {
		nt.verifySignatureRequestChan <- false
	}

	nt.verifySignatureRequestChan <- true
}

// Start the last phase : send up the final signature
func (nt *Ntree) startSignatureResponse() {
	log.Lvl3(nt.Name(), "Start Signature Response phase")
	nt.computeSignatureResponse()
	if err := nt.SendTo(nt.Parent(), nt.tempSignatureResponse); err != nil {
		log.Error(err)
	}
}

// computeSignatureResponse will compute the response out of the signature
// request. It's the final signature.
func (nt *Ntree) computeSignatureResponse() {
	// wait for the verification to be done
	ok := <-nt.verifySignatureRequestChan
	if !ok {
		nt.tempSignatureResponse.Exceptions = append(nt.tempSignatureResponse.Exceptions, Exception{nt.TreeNode().ID})
	} else {
		// compute the message out of the previous signature
		// marshal only the header here (so signature between the two phases are
		// garanteed to be different)
		marshalled, err := json.Marshal(nt.block.Header)
		if err != nil {
			log.Error(err)
			return
		}
		sig, err := crypto.SignSchnorr(nt.Suite(), nt.Private(), marshalled)
		if err != nil {
			return
		}
		nt.tempSignatureResponse.Sigs = append(nt.tempSignatureResponse.Sigs, sig)
	}
}

// SignatureResponse is the last phase where the final signature goes up until
// the root
func (nt *Ntree) handleRoundSignatureResponse(msg *RoundSignatureResponse) {
	// do we have received it all
	nt.tempSignatureResponse.Sigs = append(nt.tempSignatureResponse.Sigs, msg.Sigs...)
	nt.tempSignatureResponse.Exceptions = append(nt.tempSignatureResponse.Exceptions, msg.Exceptions...)
	nt.tempSignatureResponseReceived++
	log.Lvl3(nt.Name(), "Handle Round Signature Response(", nt.tempSignatureResponseReceived, "/", len(nt.Children()))
	if nt.tempSignatureResponseReceived < len(nt.Children()) {
		return
	}

	nt.computeSignatureResponse()

	// if i'm root I'm finished
	if nt.IsRoot() {
		if nt.onDoneCallback != nil {
			nt.onDoneCallback(&NtreeSignature{nt.block, nt.tempSignatureResponse})
		}
		return
	}
	if err := nt.SendTo(nt.Parent(), msg); err != nil {
		log.Error(nt.Name(), "couldn't send to", nt.Name(), err)
	}
}

// RegisterOnDone is the callback that will be executed when the final signature
// is done.
func (nt *Ntree) RegisterOnDone(fn func(*NtreeSignature)) {
	nt.onDoneCallback = fn
}

// BlockAnnounce is used to signal the block to the whole tree.
type BlockAnnounce struct {
	Block *blockchain.TrBlock
}

// NaiveBlockSignature contains the signatures of a block that goes up the tree using this message
type NaiveBlockSignature struct {
	Sigs       []crypto.SchnorrSig
	Exceptions []Exception
}

// Exception is  just representing the notion that a peers does not accept to
// sign something. It justs passes its TreeNodeId inside. No need for public key
// or whatever because each signatures is independent.
type Exception struct {
	ID onet.TreeNodeID
}

// RoundSignatureRequest basically is the the block signature broadcasting
// down the tree.
type RoundSignatureRequest struct {
	*NaiveBlockSignature
}

// RoundSignatureResponse is the final signatures
type RoundSignatureResponse struct {
	*NaiveBlockSignature
}

// NtreeSignature is the signature that we give back to the simulation or control
type NtreeSignature struct {
	Block *blockchain.TrBlock
	*RoundSignatureResponse
}
