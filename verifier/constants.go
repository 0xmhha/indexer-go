package verifier

const (
	// MinBytecodeSimilarityThreshold is the minimum similarity threshold for bytecode matching
	// When metadata variance is allowed, bytecode must match at least 95% to be considered verified
	MinBytecodeSimilarityThreshold = 0.95
)
