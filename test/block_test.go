// +build functional

package test

import (
	"os"
	"testing"
	"time"

	dbm "github.com/tendermint/tmlibs/db"

	"github.com/bytom/consensus"
	"github.com/bytom/protocol/bc"
	"github.com/bytom/protocol/bc/types"
	"github.com/bytom/protocol/vm"
	"github.com/bytom/protocol"
	"github.com/bytom/protocol/validation"
	"fmt"
)

func TestBlockHeader(t *testing.T) {
	db := dbm.NewDB("block_test_db", "leveldb", "block_test_db")
	defer os.RemoveAll("block_test_db")
	chain, _, _, _ := MockChain(db)
	genesisHeader := chain.BestBlockHeader()
	if err := AppendBlocks(chain, 1); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		desc       string
		version    func() uint64
		prevHeight func() uint64
		timestamp  func() uint64
		prevHash   func() *bc.Hash
		bits       func() uint64
		solve      bool
		valid      bool
	}{
		{
			desc:       "block version is 0",
			version:    func() uint64 { return 0 },
			prevHeight: chain.BestBlockHeight,
			timestamp:  func() uint64 { return chain.BestBlockHeader().Timestamp + 1 },
			prevHash:   chain.BestBlockHash,
			bits:       func() uint64 { return chain.BestBlockHeader().Bits },
			solve:      true,
			valid:      false,
		},
		{
			desc:       "block version grater than prevBlock.Version",
			version:    func() uint64 { return chain.BestBlockHeader().Version + 10 },
			prevHeight: chain.BestBlockHeight,
			timestamp:  func() uint64 { return chain.BestBlockHeader().Timestamp + 1 },
			prevHash:   chain.BestBlockHash,
			bits:       func() uint64 { return chain.BestBlockHeader().Bits },
			solve:      true,
			valid:      true,
		},
		{
			desc:       "invalid block, misorder block height",
			version:    func() uint64 { return chain.BestBlockHeader().Version },
			prevHeight: func() uint64 { return chain.BestBlockHeight() + 1 },
			timestamp:  func() uint64 { return chain.BestBlockHeader().Timestamp + 1 },
			prevHash:   chain.BestBlockHash,
			bits:       func() uint64 { return chain.BestBlockHeader().Bits },
			solve:      true,
			valid:      false,
		},
		{
			desc:       "invalid prev hash, prev hash dismatch",
			version:    func() uint64 { return chain.BestBlockHeader().Version },
			prevHeight: chain.BestBlockHeight,
			timestamp:  func() uint64 { return chain.BestBlockHeader().Timestamp + 1 },
			prevHash:   func() *bc.Hash { hash := genesisHeader.Hash(); return &hash },
			bits:       func() uint64 { return chain.BestBlockHeader().Bits },
			solve:      true,
			valid:      false,
		},
		{
			desc:       "invalid bits",
			version:    func() uint64 { return chain.BestBlockHeader().Version },
			prevHeight: chain.BestBlockHeight,
			timestamp:  func() uint64 { return chain.BestBlockHeader().Timestamp + 1 },
			prevHash:   chain.BestBlockHash,
			bits:       func() uint64 { return chain.BestBlockHeader().Bits + 100 },
			solve:      true,
			valid:      false,
		},
		{
			desc:       "invalid timestamp, greater than MaxTimeOffsetSeconds from system time",
			version:    func() uint64 { return chain.BestBlockHeader().Version },
			prevHeight: chain.BestBlockHeight,
			timestamp:  func() uint64 { return uint64(time.Now().Unix()) + consensus.MaxTimeOffsetSeconds + 60 },
			prevHash:   chain.BestBlockHash,
			bits:       func() uint64 { return chain.BestBlockHeader().Bits },
			solve:      true,
			valid:      false,
		},
		{
			desc:       "valid timestamp, greater than last block",
			version:    func() uint64 { return chain.BestBlockHeader().Version },
			prevHeight: chain.BestBlockHeight,
			timestamp:  func() uint64 { return chain.BestBlockHeader().Timestamp + 3 },
			prevHash:   chain.BestBlockHash,
			bits:       func() uint64 { return chain.BestBlockHeader().Bits },
			solve:      true,
			valid:      true,
		},
		{
			desc:       "valid timestamp, less than last block, but greater than median",
			version:    func() uint64 { return chain.BestBlockHeader().Version },
			prevHeight: chain.BestBlockHeight,
			timestamp:  func() uint64 { return chain.BestBlockHeader().Timestamp - 1 },
			prevHash:   chain.BestBlockHash,
			bits:       func() uint64 { return chain.BestBlockHeader().Bits },
			solve:      true,
			valid:      true,
		},
		{
			desc:       "invalid timestamp, less than median",
			version:    func() uint64 { return chain.BestBlockHeader().Version },
			prevHeight: chain.BestBlockHeight,
			timestamp:  func() uint64 { return genesisHeader.Timestamp },
			prevHash:   chain.BestBlockHash,
			bits:       func() uint64 { return chain.BestBlockHeader().Bits },
			solve:      true,
			valid:      false,
		},
	}

	for _, c := range cases {
		block, err := NewBlock(chain, nil, []byte{byte(vm.OP_TRUE)})
		if err != nil {
			t.Fatal(err)
		}

		block.Version = c.version()
		block.Height = c.prevHeight() + 1
		block.Timestamp = c.timestamp()
		block.PreviousBlockHash = *c.prevHash()
		block.Bits = c.bits()
		seed, err := chain.CalcNextSeed(&block.PreviousBlockHash)
		if err != nil && c.valid {
			t.Fatal(err)
		}

		if c.solve {
			Solve(seed, block)
		}
		_, err = chain.ProcessBlock(block)
		result := err == nil
		if result != c.valid {
			t.Fatalf("%s test failed, expected: %t, have: %t, err: %s", c.desc, c.valid, result, err)
		}
	}
}

func TestMaxBlockGas(t *testing.T) {
	chainDB := dbm.NewDB("test_block_db", "leveldb", "test_block_db")
	defer os.RemoveAll("test_block_db")
	chain, _, _, err := MockChain(chainDB)
	if err != nil {
		t.Fatal(err)
	}

	if err := AppendBlocks(chain, 7); err != nil {
		t.Fatal(err)
	}

	block, err := chain.GetBlockByHeight(1)
	if err != nil {
		t.Fatal(err)
	}

	tx, err := CreateTxFromTx(block.Transactions[0], 0, 600000000000, []byte{byte(vm.OP_TRUE)})
	if err != nil {
		t.Fatal(err)
	}

	outputAmount := uint64(600000000000)
	txs := []*types.Tx{tx}
	for i := 1; i < 50000; i++ {
		outputAmount -= 10000000
		tx, err := CreateTxFromTx(txs[i-1], 0, outputAmount, []byte{byte(vm.OP_TRUE)})
		if err != nil {
			t.Fatal(err)
		}
		txs = append(txs, tx)
	}

	block, err = NewBlock(chain, txs, []byte{byte(vm.OP_TRUE)})
	if err != nil {
		t.Fatal(err)
	}

	if err := SolveAndUpdate(chain, block); err == nil {
		t.Fatalf("test max block gas failed")
	}
}

// NewBlock create block according to the current status of chain
func NewMerkleBlock(chain *protocol.Chain, txs []*types.Tx, controlProgram []byte) (*types.Block, error) {
	gasUsed := uint64(0)
	txsFee := uint64(0)
	txEntries := []*bc.Tx{nil}
	txStatus := bc.NewTransactionStatus()
	txStatus.SetStatus(0, false)

	preBlockHeader := chain.BestBlockHeader()
	preBlockHash := preBlockHeader.Hash()
	nextBits, err := chain.CalcNextBits(&preBlockHash)
	if err != nil {
		return nil, err
	}

	b := &types.Block{
		BlockHeader: types.BlockHeader{
			Version:           1,
			Height:            preBlockHeader.Height + 1,
			PreviousBlockHash: preBlockHeader.Hash(),
			Timestamp:         preBlockHeader.Timestamp + 1,
			BlockCommitment:   types.BlockCommitment{},
			Bits:              nextBits,
		},
		Transactions: []*types.Tx{nil},
	}

	bcBlock := &bc.Block{BlockHeader: &bc.BlockHeader{Height: preBlockHeader.Height + 1}}
	for _, tx := range txs {
		gasOnlyTx := false
		gasStatus, err := validation.ValidateTx(tx.Tx, bcBlock)
		if err != nil {
			if !gasStatus.GasValid {
				continue
			}
			gasOnlyTx = true
		}

		txStatus.SetStatus(len(b.Transactions), gasOnlyTx)
		b.Transactions = append(b.Transactions, tx)
		txEntries = append(txEntries, tx.Tx)
		gasUsed += uint64(gasStatus.GasUsed)
		txsFee += txFee(tx)
	}

	coinbaseTx, err := CreateCoinbaseTx(controlProgram, preBlockHeader.Height+1, txsFee)
	if err != nil {
		return nil, err
	}

	b.Transactions[0] = coinbaseTx
	txEntries[0] = coinbaseTx.Tx
	b.TransactionsMerkleRoot, err = bc.TxMerkleRoot(txEntries)
	if err != nil {
		return nil, err
	}

	b.TransactionStatusHash, err = bc.TxStatusMerkleRoot(txStatus.VerifyStatus)
	b.Transactions = b.Transactions[1:len(b.Transactions)/2]
	return b, err
}

func TestMerkleBlock(t *testing.T) {
	chainDB := dbm.NewDB("test_block_db", "leveldb", "test_block_db")
	defer os.RemoveAll("test_block_db")
	chain, _, _, err := MockChain(chainDB)
	if err != nil {
		t.Fatal(err)
	}

	if err := AppendBlocks(chain, 7); err != nil {
		t.Fatal(err)
	}

	block, err := chain.GetBlockByHeight(1)
	if err != nil {
		t.Fatal(err)
	}

	tx, err := CreateTxFromTx(block.Transactions[0], 0, 600000000000, []byte{byte(vm.OP_TRUE)})
	if err != nil {
		t.Fatal(err)
	}

	outputAmount := uint64(600000000000)
	txs := []*types.Tx{tx}
	for i := 1; i < 50000; i++ {
		outputAmount -= 10000000
		tx, err := CreateTxFromTx(txs[i-1], 0, outputAmount, []byte{byte(vm.OP_TRUE)})
		if err != nil {
			t.Fatal(err)
		}
		txs = append(txs, tx)
	}

	block, err = NewMerkleBlock(chain, txs, []byte{byte(vm.OP_TRUE)})
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(len(block.Transactions))
	if err := SolveAndUpdate(chain, block); err == nil {
		t.Fatalf("test max block gas failed")
	}
}