package chainfeed

// PeerSettings contains explicit transport inputs, independent of capture or
// execution ownership. Audited chain modules validate and open the transport.
type PeerSettings struct {
	NetworkID  uint64   `yaml:"network_id"`
	ForkHash   string   `yaml:"fork_hash"`
	ForkNext   uint64   `yaml:"fork_next"`
	KeyFile    string   `yaml:"key_file"`
	Peers      []string `yaml:"peers"`
	ListenAddr string   `yaml:"listen_addr"`
}
