package node

import (
	"context"
	"errors"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path/filepath"
	"strings"

	"github.com/prometheus/prometheus/util/flock"
	log "github.com/sirupsen/logrus"
	cmn "github.com/tendermint/tmlibs/common"
	dbm "github.com/tendermint/tmlibs/db"
	browser "github.com/toqueteos/webbrowser"

	"github.com/bytom-spv/accesstoken"
	"github.com/bytom-spv/account"
	"github.com/bytom-spv/api"
	"github.com/bytom-spv/asset"
	"github.com/bytom-spv/blockchain/pseudohsm"
	"github.com/bytom-spv/blockchain/txfeed"
	cfg "github.com/bytom-spv/config"
	"github.com/bytom-spv/consensus"
	"github.com/bytom-spv/database/leveldb"
	"github.com/bytom-spv/env"
	"github.com/bytom-spv/mining/tensority"
	"github.com/bytom-spv/netsync"
	"github.com/bytom-spv/protocol"
	"github.com/bytom-spv/protocol/bc"
	w "github.com/bytom-spv/wallet"
)

const (
	webHost           = "http://127.0.0.1"
	maxNewBlockChSize = 1024
)

type Node struct {
	cmn.BaseService

	// config
	config *cfg.Config

	syncManager *netsync.SyncManager

	wallet       *w.Wallet
	accessTokens *accesstoken.CredentialStore
	api          *api.API
	chain        *protocol.Chain
	txfeed       *txfeed.Tracker
	miningEnable bool
}

func NewNode(config *cfg.Config) *Node {
	ctx := context.Background()
	if err := lockDataDirectory(config); err != nil {
		cmn.Exit("Error: " + err.Error())
	}
	initLogFile(config)
	initActiveNetParams(config)
	// Get store
	coreDB := dbm.NewDB("core", config.DBBackend, config.DBDir())
	store := leveldb.NewStore(coreDB)

	tokenDB := dbm.NewDB("accesstoken", config.DBBackend, config.DBDir())
	accessTokens := accesstoken.NewStore(tokenDB)

	chain, err := protocol.NewChain(store)
	if err != nil {
		cmn.Exit(cmn.Fmt("Failed to create chain structure: %v", err))
	}

	var accounts *account.Manager = nil
	var assets *asset.Registry = nil
	var wallet *w.Wallet = nil
	var txFeed *txfeed.Tracker = nil

	txFeedDB := dbm.NewDB("txfeeds", config.DBBackend, config.DBDir())
	txFeed = txfeed.NewTracker(txFeedDB, chain)

	if err = txFeed.Prepare(ctx); err != nil {
		log.WithField("error", err).Error("start txfeed")
		return nil
	}

	hsm, err := pseudohsm.New(config.KeysDir())
	if err != nil {
		cmn.Exit(cmn.Fmt("initialize HSM failed: %v", err))
	}

	walletDB := dbm.NewDB("wallet", config.DBBackend, config.DBDir())
	accounts = account.NewManager(walletDB, chain)
	assets = asset.NewRegistry(walletDB, chain)
	wallet, err = w.NewWallet(walletDB, accounts, assets, hsm, chain)
	if err != nil {
		log.WithField("error", err).Error("init NewWallet")
	}

	// trigger rescan wallet
	if config.Wallet.Rescan {
		wallet.RescanBlocks()
	}

	newBlockCh := make(chan *bc.Hash, maxNewBlockChSize)

	syncManager, _ := netsync.NewSyncManager(config, chain, newBlockCh, wallet)

	// run the profile server
	profileHost := config.ProfListenAddress
	if profileHost != "" {
		// Profiling bytomd programs.see (https://blog.golang.org/profiling-go-programs)
		// go tool pprof http://profileHose/debug/pprof/heap
		go func() {
			http.ListenAndServe(profileHost, nil)
		}()
	}

	node := &Node{
		config:       config,
		syncManager:  syncManager,
		accessTokens: accessTokens,
		wallet:       wallet,
		chain:        chain,
		txfeed:       txFeed,
		miningEnable: config.Mining,
	}

	node.BaseService = *cmn.NewBaseService(nil, "Node", node)

	if config.Simd.Enable {
		tensority.UseSIMD = true
	}

	return node
}

// Lock data directory after daemonization
func lockDataDirectory(config *cfg.Config) error {
	_, _, err := flock.New(filepath.Join(config.RootDir, "LOCK"))
	if err != nil {
		return errors.New("datadir already used by another process")
	}
	return nil
}

func initActiveNetParams(config *cfg.Config) {
	var exist bool
	consensus.ActiveNetParams, exist = consensus.NetParams[config.ChainID]
	if !exist {
		cmn.Exit(cmn.Fmt("chain_id[%v] don't exist", config.ChainID))
	}
}

func initLogFile(config *cfg.Config) {
	if config.LogFile == "" {
		return
	}
	cmn.EnsureDir(filepath.Dir(config.LogFile), 0700)
	file, err := os.OpenFile(config.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err == nil {
		log.SetOutput(file)
	} else {
		log.WithField("err", err).Info("using default")
	}

}

// Lanch web broser or not
func launchWebBrowser(port string) {
	webAddress := webHost + ":" + port
	log.Info("Launching System Browser with :", webAddress)
	if err := browser.Open(webAddress); err != nil {
		log.Error(err.Error())
		return
	}
}

func (n *Node) initAndstartApiServer() {
	n.api = api.NewAPI(n.syncManager, n.wallet, n.txfeed, n.chain, n.config, n.accessTokens)

	listenAddr := env.String("LISTEN", n.config.ApiAddress)
	env.Parse()
	n.api.StartServer(*listenAddr)
}

func (n *Node) OnStart() error {
	if !n.config.VaultMode {
		n.syncManager.Start()
	}
	n.initAndstartApiServer()
	if !n.config.Web.Closed {
		s := strings.Split(n.config.ApiAddress, ":")
		if len(s) != 2 {
			log.Error("Invalid api address")
		}
		launchWebBrowser(s[1])
	}
	return nil
}

func (n *Node) OnStop() {
	n.BaseService.OnStop()
	if !n.config.VaultMode {
		n.syncManager.Stop()
	}
}

func (n *Node) RunForever() {
	// Sleep forever and then...
	cmn.TrapSignal(func() {
		n.Stop()
	})
}

func (n *Node) SyncManager() *netsync.SyncManager {
	return n.syncManager
}
