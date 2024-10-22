package test

import (
	"context"
	"time"

	dbm "github.com/tendermint/tmlibs/db"

	"github.com/bytom-spv/account"
	"github.com/bytom-spv/blockchain/pseudohsm"
	"github.com/bytom-spv/blockchain/txbuilder"
	"github.com/bytom-spv/consensus"
	"github.com/bytom-spv/crypto/ed25519/chainkd"
	"github.com/bytom-spv/database/leveldb"
	"github.com/bytom-spv/protocol"
	"github.com/bytom-spv/protocol/bc"
	"github.com/bytom-spv/protocol/bc/types"
	"github.com/bytom-spv/protocol/vm"
)

const (
	vmVersion    = 1
	assetVersion = 1
)

// MockChain mock chain with genesis block
func MockChain(testDB dbm.DB) (*protocol.Chain, *leveldb.Store, *protocol.TxPool, error) {
	store := leveldb.NewStore(testDB)
	txPool := protocol.NewTxPool(store)
	chain, err := protocol.NewChain(store, txPool)
	return chain, store, txPool, err
}

// MockUTXO mock a utxo
func MockUTXO(controlProg *account.CtrlProgram) *account.UTXO {
	utxo := &account.UTXO{}
	utxo.OutputID = bc.Hash{V0: 1}
	utxo.SourceID = bc.Hash{V0: 2}
	utxo.AssetID = *consensus.BTMAssetID
	utxo.Amount = 1000000000
	utxo.SourcePos = 0
	utxo.ControlProgram = controlProg.ControlProgram
	utxo.AccountID = controlProg.AccountID
	utxo.Address = controlProg.Address
	utxo.ControlProgramIndex = controlProg.KeyIndex
	return utxo
}

// MockTx mock a tx
func MockTx(utxo *account.UTXO, testAccount *account.Account) (*txbuilder.Template, *types.TxData, error) {
	txInput, sigInst, err := account.UtxoToInputs(testAccount.Signer, utxo)
	if err != nil {
		return nil, nil, err
	}

	b := txbuilder.NewBuilder(time.Now())
	b.AddInput(txInput, sigInst)
	out := types.NewTxOutput(*consensus.BTMAssetID, 100, []byte{byte(vm.OP_FAIL)})
	b.AddOutput(out)
	return b.Build()
}

// MockSign sign a tx
func MockSign(tpl *txbuilder.Template, hsm *pseudohsm.HSM, password string) (bool, error) {
	err := txbuilder.Sign(nil, tpl, password, func(_ context.Context, xpub chainkd.XPub, path [][]byte, data [32]byte, password string) ([]byte, error) {
		return hsm.XSign(xpub, path, data[:], password)
	})
	if err != nil {
		return false, err
	}
	return txbuilder.SignProgress(tpl), nil
}

// MockBlock mock a block
func MockBlock() *bc.Block {
	return &bc.Block{
		BlockHeader: &bc.BlockHeader{Height: 1},
	}
}
