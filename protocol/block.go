package protocol

import (
	log "github.com/sirupsen/logrus"

	"github.com/bytom/errors"
	"github.com/bytom/protocol/bc"
	"github.com/bytom/protocol/bc/types"
	"github.com/bytom/protocol/state"
)

var (
	// ErrBadBlock is returned when a block is invalid.
	ErrBadBlock = errors.New("invalid block")
	// ErrBadStateRoot is returned when the computed assets merkle root
	// disagrees with the one declared in a block header.
	ErrBadStateRoot = errors.New("invalid state merkle root")
)

// BlockExist check is a block in chain or orphan
func (c *Chain) BlockExist(hash *bc.Hash) bool {
	return c.index.BlockExist(hash) || c.orphanManage.BlockExist(hash)
}

// GetBlockByHash return a block by given hash
func (c *Chain) GetBlockByHash(hash *bc.Hash) (*types.Block, error) {
	return c.store.GetBlock(hash)
}

// GetBlockByHeight return a block header by given height
func (c *Chain) GetBlockByHeight(height uint64) (*types.Block, error) {
	node := c.index.NodeByHeight(height)
	if node == nil {
		return nil, errors.New("can't find block in given height")
	}
	return c.store.GetBlock(&node.Hash)
}

// GetHeaderByHash return a block header by given hash
func (c *Chain) GetHeaderByHash(hash *bc.Hash) (*types.BlockHeader, error) {
	node := c.index.GetNode(hash)
	if node == nil {
		return nil, errors.New("can't find block header in given hash")
	}
	return node.BlockHeader(), nil
}

// GetHeaderByHeight return a block header by given height
func (c *Chain) GetHeaderByHeight(height uint64) (*types.BlockHeader, error) {
	node := c.index.NodeByHeight(height)
	if node == nil {
		return nil, errors.New("can't find block header in given height")
	}
	return node.BlockHeader(), nil
}

func (c *Chain) calcReorganizeNodes(node *state.BlockNode) ([]*state.BlockNode, []*state.BlockNode) {
	var attachNodes []*state.BlockNode
	var detachNodes []*state.BlockNode

	attachNode := node
	for c.index.NodeByHeight(attachNode.Height) != attachNode {
		attachNodes = append([]*state.BlockNode{attachNode}, attachNodes...)
		attachNode = attachNode.Parent
	}

	detachNode := c.bestNode
	for detachNode != attachNode {
		detachNodes = append(detachNodes, detachNode)
		detachNode = detachNode.Parent
	}
	return attachNodes, detachNodes
}

func (c *Chain) connectBlock(block *types.Block) (err error) {
	bcBlock := types.MapBlock(block)
	node := c.index.GetNode(&bcBlock.ID)
	if err := c.setState(node); err != nil {
		return err
	}

	return nil
}

func (c *Chain) reorganizeChain(node *state.BlockNode) error {
	return c.setState(node)
}

// SaveBlock will validate and save block into storage
func (c *Chain) saveBlock(block *types.Block, txStatus *bc.TransactionStatus) error {
	bcBlock := types.MapBlock(block)
	parent := c.index.GetNode(&block.PreviousBlockHash)

	if err := c.store.SaveBlock(block, txStatus); err != nil {
		return err
	}

	c.orphanManage.Delete(&bcBlock.ID)
	node, err := state.NewBlockNode(&block.BlockHeader, parent)
	if err != nil {
		return err
	}

	c.index.AddNode(node)
	return nil
}

//后一个节点的txstatus信息在哪里？
func (c *Chain) saveSubBlock(block *types.Block, txStatus *bc.TransactionStatus) *types.Block {
	blockHash := block.Hash()
	prevOrphans, ok := c.orphanManage.GetPrevOrphans(&blockHash)
	if !ok {
		return block
	}

	bestBlock := block
	for _, prevOrphan := range prevOrphans {
		orphanBlock, ok := c.orphanManage.Get(prevOrphan)
		if !ok {
			log.WithFields(log.Fields{"hash": prevOrphan.String()}).Warning("saveSubBlock fail to get block from orphanManage")
			continue
		}
		if err := c.saveBlock(orphanBlock, txStatus); err != nil {
			log.WithFields(log.Fields{"hash": prevOrphan.String(), "height": orphanBlock.Height}).Warning("saveSubBlock fail to save block")
			continue
		}

		if subBestBlock := c.saveSubBlock(orphanBlock, txStatus); subBestBlock.Height > bestBlock.Height {
			bestBlock = subBestBlock
		}
	}
	return bestBlock
}

type processBlockResponse struct {
	isOrphan bool
	err      error
}

type processBlockMsg struct {
	block    *types.Block
	txStatus *bc.TransactionStatus
	reply    chan processBlockResponse
}

// ProcessBlock is the entry for chain update
func (c *Chain) ProcessBlock(block *types.Block, txStatus *bc.TransactionStatus) (bool, error) {
	reply := make(chan processBlockResponse, 1)
	c.processBlockCh <- &processBlockMsg{block: block, txStatus: txStatus, reply: reply}
	response := <-reply
	return response.isOrphan, response.err
}

func (c *Chain) blockProcesser() {
	for msg := range c.processBlockCh {
		isOrphan, err := c.processBlock(msg.block, msg.txStatus)
		msg.reply <- processBlockResponse{isOrphan: isOrphan, err: err}
	}
}

// ProcessBlock is the entry for handle block insert
func (c *Chain) processBlock(block *types.Block, txStatus *bc.TransactionStatus) (bool, error) {
	blockHash := block.Hash()
	if c.BlockExist(&blockHash) {
		log.WithFields(log.Fields{"hash": blockHash.String(), "height": block.Height}).Info("block has been processed")
		return c.orphanManage.BlockExist(&blockHash), nil
	}

	if parent := c.index.GetNode(&block.PreviousBlockHash); parent == nil {
		c.orphanManage.Add(block)
		return true, nil
	}

	if err := c.saveBlock(block, txStatus); err != nil {
		return false, err
	}

	bestBlock := c.saveSubBlock(block, txStatus)
	bestBlockHash := bestBlock.Hash()
	bestNode := c.index.GetNode(&bestBlockHash)

	if bestNode.Parent == c.bestNode {
		log.Debug("append block to the end of mainchain")
		return false, c.connectBlock(bestBlock)
	}

	if bestNode.Height > c.bestNode.Height && bestNode.WorkSum.Cmp(c.bestNode.WorkSum) >= 0 {
		log.Debug("start to reorganize chain")
		return false, c.reorganizeChain(bestNode)
	}
	return false, nil
}
