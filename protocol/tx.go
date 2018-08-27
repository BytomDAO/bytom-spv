package protocol

import (
	"github.com/bytom/errors"
	"github.com/bytom/protocol/bc"
)

// ErrBadTx is returned for transactions failing validation
var ErrBadTx = errors.New("invalid transaction")

// GetTransactionStatus return the transaction status of give block
func (c *Chain) GetTransactionStatus(hash *bc.Hash) (*bc.TransactionStatus, error) {
	return c.store.GetTransactionStatus(hash)
}
