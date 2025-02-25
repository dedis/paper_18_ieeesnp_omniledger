package byzcoin

import (
	"errors"

	"github.com/dedis/paper_17_sosp_omniledger/byzcoin_lib/protocol/blockchain"
	"gopkg.in/dedis/onet.v1/log"
)

var magicNum = [4]byte{0xF9, 0xBE, 0xB4, 0xD9}

// ReadFirstNBlocks specifcy how many blocks in the the BlocksDir it must read
// (so you only have to copy the first blocks to deterLab)
const ReadFirstNBlocks = 66000

// Client is a client simulation. At the moment we do not measure the
// communication between client and server. Hence, we do not even open a real
// network connection
type Client struct {
	// holds the sever as a struct
	srv BlockServer
}

// NewClient returns a fresh new client out of a blockserver
func NewClient(s BlockServer) *Client {
	return &Client{srv: s}
}

// StartClientSimulation can be called from outside (from an simulation
// implementation) to simulate a client. Parameters:
// blocksDir is the directory where to find the transaction blocks (.dat files)
// numTxs is the number of transactions the client will create
func (c *Client) StartClientSimulation(blocksDir string, numTxs int) error {
	return c.triggerTransactions(blocksDir, numTxs)
}

func (c *Client) triggerTransactions(blocksPath string, nTxs int) error {
	log.Lvl2("ByzCoin Client will trigger up to", nTxs, "transactions")
	parser, err := blockchain.NewParser(blocksPath, magicNum)

	transactions, err := parser.Parse(0, ReadFirstNBlocks)
	if len(transactions) == 0 {
		return errors.New("Couldn't read any transactions.")
	}
	if err != nil {
		log.Error("Error: Couldn't parse blocks in", blocksPath,
			".\nPlease download bitcoin blocks as .dat files first and place them in",
			blocksPath, "Either run a bitcoin node (recommended) or using a torrent.")
		return err
	}
	if len(transactions) < nTxs {
		log.Errorf("Read only %v but caller wanted %v", len(transactions), nTxs)
	}
	consumed := 0
	if len(transactions) < nTxs {
		consumed = len(transactions)
	} else {
		consumed = nTxs
	}
	for consumed > 0 {
		for _, tr := range transactions {
			// "send" transaction to server (we skip tcp connection on purpose here)
			c.srv.AddTransaction(tr)
		}
		consumed--
	}
	return nil
}
