package consensus

// ServiceFlag use uint64 to indicate what kind of server this node can provide.
// one uint64 can represent 64 type of service flag
type ServiceFlag uint64

const (
	// SFFullNode is a flag used to indicate a peer is a full node.
	SFFullNode ServiceFlag = 1 << iota
	// SFFastSync indicate peer support header first mode
	SFFastSync
	//SFSpvProof indicate peer support spv proof
	SFSpvProof
	// DefaultServices is the server that this node support
	DefaultServices = SFFastSync
)

// IsEnable check does the flag support the input flag function
func (f ServiceFlag) IsEnable(checkFlag ServiceFlag) bool {
	return f&checkFlag == checkFlag
}
