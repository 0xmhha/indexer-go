package port

// QueryStore is the storage the APIs serve from: the chain data and the
// indexes every storage provides, and the contract ABIs and verifications
// the APIs accept. Optional indexes (address, token holder, module, model,
// orphan) are taken by type assertion.
type QueryStore interface {
	Reader
	LogReader
	ABIReader
	ABIWriter
	SearchReader
	ContractVerificationReader
	ContractVerificationWriter
	HistoricalReader
	TokenMetadataReader
	SetCodeIndexReader
	UserOpIndexReader
}
