package netsync

import (
	"github.com/bytom/account"
	log "github.com/sirupsen/logrus"
)

//spvAddressMgr
func (sm *SyncManager) spvAddressMgr() {
	for {
		select {
		case newAddr := <-sm.newAddrCh:
			//sm.spvAddresses:=append(sm.spvAddresses,newAddr)
			if err := sm.peers.broadcastAddr(newAddr.ControlProgram); err != nil {
				log.WithField("err", err).Errorf("fail on filter add address")
			}
			sm.spvAddAddress(newAddr)
		case <-sm.quitSync:
			return
		}
	}
}

func (sm *SyncManager) spvAddAddress(cp *account.CtrlProgram) {
	sm.addrMutex.Lock()
	defer sm.addrMutex.Unlock()

	sm.spvAddresses = append(sm.spvAddresses, cp)
}

func (sm *SyncManager) spvAddress() [][]byte {
	addrs := make([][]byte, 0)
	sm.addrMutex.Lock()
	defer sm.addrMutex.Unlock()
	for _, addr := range sm.spvAddresses {
		addrs = append(addrs, addr.ControlProgram)
	}
	return addrs
}
