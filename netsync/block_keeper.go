package netsync

import (
	"container/list"
	"encoding/json"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/bytom/consensus"
	"github.com/bytom/errors"
	"github.com/bytom/mining/tensority"
	"github.com/bytom/protocol/bc"
	"github.com/bytom/protocol/bc/types"
)

const (
	syncCycle                 = 5 * time.Second
	blockProcessChSize        = 1024
	blocksProcessChSize       = 128
	headersProcessChSize      = 1024
	merkleBlocksProcessChSize = 128
)

var (
	maxBlockPerMsg        = uint64(128)
	maxBlockHeadersPerMsg = uint64(2048)
	syncTimeout           = 30 * time.Second

	errAppendHeaders  = errors.New("fail to append list due to order dismatch")
	errRequestTimeout = errors.New("request timeout")
	errPeerDropped    = errors.New("Peer dropped")
	errPeerMisbehave  = errors.New("peer is misbehave")
)

type blockMsg struct {
	block  *types.Block
	peerID string
}

type merkleBlockMsg struct {
	merkleBlock *types.MerkleBlock
	peerID      string
}

type blocksMsg struct {
	blocks []*types.Block
	peerID string
}

type headersMsg struct {
	headers []*types.BlockHeader
	peerID  string
}

type blockKeeper struct {
	chain Chain
	peers *peerSet

	syncPeer             *peer
	blockProcessCh       chan *blockMsg
	merkleBlockProcessCh chan *merkleBlockMsg

	blocksProcessCh  chan *blocksMsg
	headersProcessCh chan *headersMsg

	headerList *list.List
}

func newBlockKeeper(chain Chain, peers *peerSet) *blockKeeper {
	bk := &blockKeeper{
		chain:                chain,
		peers:                peers,
		blockProcessCh:       make(chan *blockMsg, blockProcessChSize),
		blocksProcessCh:      make(chan *blocksMsg, blocksProcessChSize),
		headersProcessCh:     make(chan *headersMsg, headersProcessChSize),
		merkleBlockProcessCh: make(chan *merkleBlockMsg, merkleBlocksProcessChSize),

		headerList: list.New(),
	}
	bk.resetHeaderState()
	go bk.syncWorker()
	return bk
}

func (bk *blockKeeper) appendHeaderList(headers []*types.BlockHeader) error {
	for _, header := range headers {
		prevHeader := bk.headerList.Back().Value.(*types.BlockHeader)
		if prevHeader.Hash() != header.PreviousBlockHash {
			return errAppendHeaders
		}
		bk.headerList.PushBack(header)
	}
	return nil
}

func (bk *blockKeeper) blockLocator() []*bc.Hash {
	header := bk.chain.BestBlockHeader()
	locator := []*bc.Hash{}

	step := uint64(1)
	for header != nil {
		headerHash := header.Hash()
		locator = append(locator, &headerHash)
		if header.Height == 0 {
			break
		}

		var err error
		if header.Height < step {
			header, err = bk.chain.GetHeaderByHeight(0)
		} else {
			header, err = bk.chain.GetHeaderByHeight(header.Height - step)
		}
		if err != nil {
			log.WithField("err", err).Error("blockKeeper fail on get blockLocator")
			break
		}

		if len(locator) >= 9 {
			step *= 2
		}
	}
	return locator
}

func (bk *blockKeeper) merkleBlockToBlock(merkleBlock *types.MerkleBlock) (*types.Block, error) {
	block := &types.Block{
		BlockHeader:  merkleBlock.BlockHeader,
		Transactions: merkleBlock.Transactions,
	}
	return block, nil
}

func (bk *blockKeeper) ProcessMerkleBlock(merkleBlock *types.MerkleBlock, txStatus *bc.TransactionStatus) (bool, error) {
	block, _ := bk.merkleBlockToBlock(merkleBlock)
	return bk.chain.ProcessBlock(block, txStatus)
}

func (bk *blockKeeper) fastBlockSync(checkPoint *consensus.Checkpoint) error {
	bk.resetHeaderState()
	lastHeader := bk.headerList.Back().Value.(*types.BlockHeader)
	for ; lastHeader.Hash() != checkPoint.Hash; lastHeader = bk.headerList.Back().Value.(*types.BlockHeader) {
		if lastHeader.Height >= checkPoint.Height {
			return errors.Wrap(errPeerMisbehave, "peer is not in the checkpoint branch")
		}

		lastHash := lastHeader.Hash()
		headers, err := bk.requireHeaders([]*bc.Hash{&lastHash}, &checkPoint.Hash)
		if err != nil {
			return err
		}

		if len(headers) == 0 {
			return errors.Wrap(errPeerMisbehave, "requireHeaders return empty list")
		}

		if err := bk.appendHeaderList(headers); err != nil {
			return err
		}
	}

	fastHeader := bk.headerList.Front()
	for bk.chain.BestBlockHeight() <= checkPoint.Height {
		if bk.chain.BestBlockHeight() == 0 {
			fastHeader = fastHeader.Next()
		}
		hash := fastHeader.Value.(*types.BlockHeader).Hash()
		merkleBlock, err := bk.requireMerkleBlock(fastHeader.Value.(*types.BlockHeader).Height, &hash)
		if err != nil {
			return err
		}
		if err := bk.VerifyBlockHeader(fastHeader.Value.(*types.BlockHeader), merkleBlock); err != nil {
			return err
		}
		txStatus, err := bk.VerifyMerkleBlock(merkleBlock)
		if err != nil {
			return err
		}
		blockHash := merkleBlock.Hash()
		if blockHash != fastHeader.Value.(*types.BlockHeader).Hash() {
			return errPeerMisbehave
		}
		seed, err := bk.chain.CalcNextSeed(&merkleBlock.PreviousBlockHash)
		if err != nil {
			return errors.Wrap(err, "fail on fastBlockSync calculate seed")
		}
		tensority.AIHash.AddCache(&blockHash, seed, &bc.Hash{})
		_, err = bk.ProcessMerkleBlock(merkleBlock, txStatus)
		tensority.AIHash.RemoveCache(&blockHash, seed)
		if err != nil {
			return errors.Wrap(err, "fail on fastBlockSync process block")
		}
		if fastHeader = fastHeader.Next(); fastHeader == nil {
			return nil
		}
	}
	return nil
}

func (bk *blockKeeper) locateBlocks(locator []*bc.Hash, stopHash *bc.Hash) ([]*types.Block, error) {
	headers, err := bk.locateHeaders(locator, stopHash)
	if err != nil {
		return nil, err
	}

	blocks := []*types.Block{}
	for i, header := range headers {
		if uint64(i) >= maxBlockPerMsg {
			break
		}

		headerHash := header.Hash()
		block, err := bk.chain.GetBlockByHash(&headerHash)
		if err != nil {
			return nil, err
		}

		blocks = append(blocks, block)
	}
	return blocks, nil
}

func (bk *blockKeeper) locateHeaders(locator []*bc.Hash, stopHash *bc.Hash) ([]*types.BlockHeader, error) {
	stopHeader, err := bk.chain.GetHeaderByHash(stopHash)
	if err != nil {
		return nil, err
	}

	startHeader, err := bk.chain.GetHeaderByHeight(0)
	if err != nil {
		return nil, err
	}

	for _, hash := range locator {
		header, err := bk.chain.GetHeaderByHash(hash)
		if err == nil && bk.chain.InMainChain(header.Hash()) {
			startHeader = header
			break
		}
	}

	totalHeaders := stopHeader.Height - startHeader.Height
	if totalHeaders > maxBlockHeadersPerMsg {
		totalHeaders = maxBlockHeadersPerMsg
	}

	headers := []*types.BlockHeader{}
	for i := uint64(1); i <= totalHeaders; i++ {
		header, err := bk.chain.GetHeaderByHeight(startHeader.Height + i)
		if err != nil {
			return nil, err
		}

		headers = append(headers, header)
	}
	return headers, nil
}

func (bk *blockKeeper) nextCheckpoint() *consensus.Checkpoint {
	height := bk.chain.BestBlockHeader().Height
	checkpoints := consensus.ActiveNetParams.Checkpoints
	if len(checkpoints) == 0 || height >= checkpoints[len(checkpoints)-1].Height {
		return nil
	}

	nextCheckpoint := &checkpoints[len(checkpoints)-1]
	for i := len(checkpoints) - 2; i >= 0; i-- {
		if height >= checkpoints[i].Height {
			break
		}
		nextCheckpoint = &checkpoints[i]
	}
	return nextCheckpoint
}

func (bk *blockKeeper) processBlock(peerID string, block *types.Block) {
	bk.blockProcessCh <- &blockMsg{block: block, peerID: peerID}
}

func (bk *blockKeeper) processMerkleBlock(peerID string, block *types.MerkleBlock) {
	bk.merkleBlockProcessCh <- &merkleBlockMsg{merkleBlock: block, peerID: peerID}
}

func (bk *blockKeeper) processBlocks(peerID string, blocks []*types.Block) {
	bk.blocksProcessCh <- &blocksMsg{blocks: blocks, peerID: peerID}
}

func (bk *blockKeeper) processHeaders(peerID string, headers []*types.BlockHeader) {
	bk.headersProcessCh <- &headersMsg{headers: headers, peerID: peerID}
}

func (bk *blockKeeper) regularBlockSync(wantHeight uint64) error {
	i := bk.chain.BestBlockHeight() + 1
	for i <= wantHeight {
		merkleBlock, err := bk.requireMerkleBlock(i, nil)
		if err != nil {
			return err
		}
		txStatus, err := bk.VerifyMerkleBlock(merkleBlock)
		if err != nil {
			return err
		}
		isOrphan, err := bk.ProcessMerkleBlock(merkleBlock, txStatus)
		if err != nil {
			return err
		}

		if isOrphan {
			i--
			continue
		}
		i = bk.chain.BestBlockHeight() + 1
	}
	return nil
}

func (bk *blockKeeper) requireBlock(height uint64) (*types.Block, error) {
	if ok := bk.syncPeer.getBlockByHeight(height); !ok {
		return nil, errPeerDropped
	}

	waitTicker := time.NewTimer(syncTimeout)
	for {
		select {
		case msg := <-bk.blockProcessCh:
			if msg.peerID != bk.syncPeer.ID() {
				continue
			}
			if msg.block.Height != height {
				continue
			}
			return msg.block, nil
		case <-waitTicker.C:
			return nil, errors.Wrap(errRequestTimeout, "requireBlock")
		}
	}
}

func (bk *blockKeeper) requireMerkleBlock(height uint64, hash *bc.Hash) (*types.MerkleBlock, error) {
	var blockHash [32]byte
	if hash != nil {
		blockHash = hash.Byte32()
	}
	if ok := bk.syncPeer.getMerkleBlock(height, blockHash); !ok {
		return nil, errPeerDropped
	}

	waitTicker := time.NewTimer(syncTimeout)
	for {
		select {
		case msg := <-bk.merkleBlockProcessCh:
			if msg.peerID != bk.syncPeer.ID() {
				continue
			}
			return msg.merkleBlock, nil
		case <-waitTicker.C:
			return nil, errors.Wrap(errRequestTimeout, "requireBlocks")
		}
	}
}

func (bk *blockKeeper) VerifyBlockHeader(header *types.BlockHeader, merkleBlock *types.MerkleBlock) error {
	if header.Hash() != merkleBlock.BlockHeader.Hash() {
		return errors.New("BlockHeader mismatch")
	}
	return nil
}

func (bk *blockKeeper) VerifyMerkleBlock(merkleBlock *types.MerkleBlock) (*bc.TransactionStatus, error) {
	if len(merkleBlock.Transactions) == 0 {
		return nil, nil
	}

	var txProofHashes []*bc.Hash
	for _, v := range merkleBlock.TxHashes {
		hash := bc.NewHash(v)
		txProofHashes = append(txProofHashes, &hash)
	}

	flags := merkleBlock.Flags

	var relatedTxHashes []*bc.Hash
	for _, v := range merkleBlock.Transactions {
		relatedTxHashes = append(relatedTxHashes, &v.ID)
	}
	if !types.ValidateTxMerkleTreeProof(txProofHashes, flags, relatedTxHashes, merkleBlock.BlockHeader.TransactionsMerkleRoot) {
		return nil, errors.New("tx merkle proof check error")
	}

	var txStatusProofHashes []*bc.Hash
	for _, v := range merkleBlock.StatusHashes {
		hash := bc.NewHash(v)
		txStatusProofHashes = append(txStatusProofHashes, &hash)
	}

	var relatedStatus []*bc.TxVerifyResult
	txStatus := &bc.TransactionStatus{}

	for _, v := range merkleBlock.RawTxStatuses {
		status := &bc.TxVerifyResult{}
		if err := json.Unmarshal(v, status); err != nil {
			return nil, errors.New("tx status unmarshal error")
		}
		relatedStatus = append(relatedStatus, status)
		txStatus.VerifyStatus = append(txStatus.VerifyStatus, status)
	}
	if !types.ValidateStatusMerkleTreeProof(txStatusProofHashes, flags, relatedStatus, merkleBlock.BlockHeader.TransactionStatusHash) {
		return nil, errors.New("tx status merkle proof check error")
	}
	return txStatus, nil
}

func (bk *blockKeeper) requireBlocks(locator []*bc.Hash, stopHash *bc.Hash) ([]*types.Block, error) {
	if ok := bk.syncPeer.getBlocks(locator, stopHash); !ok {
		return nil, errPeerDropped
	}

	waitTicker := time.NewTimer(syncTimeout)
	for {
		select {
		case msg := <-bk.blocksProcessCh:
			if msg.peerID != bk.syncPeer.ID() {
				continue
			}
			return msg.blocks, nil
		case <-waitTicker.C:
			return nil, errors.Wrap(errRequestTimeout, "requireBlocks")
		}
	}
}

func (bk *blockKeeper) requireHeaders(locator []*bc.Hash, stopHash *bc.Hash) ([]*types.BlockHeader, error) {
	if ok := bk.syncPeer.getHeaders(locator, stopHash); !ok {
		return nil, errPeerDropped
	}

	waitTicker := time.NewTimer(syncTimeout)
	for {
		select {
		case msg := <-bk.headersProcessCh:
			if msg.peerID != bk.syncPeer.ID() {
				continue
			}
			return msg.headers, nil
		case <-waitTicker.C:
			return nil, errors.Wrap(errRequestTimeout, "requireHeaders")
		}
	}
}

// resetHeaderState sets the headers-first mode state to values appropriate for
// syncing from a new peer.
func (bk *blockKeeper) resetHeaderState() {
	header := bk.chain.BestBlockHeader()
	bk.headerList.Init()
	if bk.nextCheckpoint() != nil {
		bk.headerList.PushBack(header)
	}
}

func (bk *blockKeeper) startSync() bool {
	checkPoint := bk.nextCheckpoint()
	peer := bk.peers.bestPeer(consensus.SFFastSync | consensus.SFFullNode)
	if peer != nil && checkPoint != nil && peer.Height() >= checkPoint.Height {
		bk.syncPeer = peer
		if err := bk.fastBlockSync(checkPoint); err != nil {
			log.WithField("err", err).Warning("fail on fastBlockSync")
			bk.peers.errorHandler(peer.ID(), err)
			return false
		}
		return true
	}

	blockHeight := bk.chain.BestBlockHeight()
	peer = bk.peers.bestPeer(consensus.SFFullNode)
	if peer != nil && peer.Height() > blockHeight {
		bk.syncPeer = peer
		targetHeight := blockHeight + maxBlockPerMsg
		if targetHeight > peer.Height() {
			targetHeight = peer.Height()
		}

		if err := bk.regularBlockSync(targetHeight); err != nil {
			log.WithField("err", err).Warning("fail on regularBlockSync")
			bk.peers.errorHandler(peer.ID(), err)
			return false
		}
		return true
	}
	return false
}

func (bk *blockKeeper) syncWorker() {
	genesisBlock, err := bk.chain.GetBlockByHeight(0)
	if err != nil {
		log.WithField("err", err).Error("fail on handleStatusRequestMsg get genesis")
		return
	}
	syncTicker := time.NewTicker(syncCycle)
	for {
		<-syncTicker.C
		if update := bk.startSync(); !update {
			continue
		}

		block, err := bk.chain.GetBlockByHeight(bk.chain.BestBlockHeight())
		if err != nil {
			log.WithField("err", err).Error("fail on syncWorker get best block")
		}

		if err := bk.peers.broadcastMinedBlock(block); err != nil {
			log.WithField("err", err).Error("fail on syncWorker broadcast new block")
		}

		if err = bk.peers.broadcastNewStatus(block, genesisBlock); err != nil {
			log.WithField("err", err).Error("fail on syncWorker broadcast new status")
		}
	}
}
