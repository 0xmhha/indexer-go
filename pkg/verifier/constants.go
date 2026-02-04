package verifier

const (
	// MinBytecodeSimilarityThreshold is the minimum similarity threshold for bytecode matching
	// When metadata variance is allowed, bytecode must match at least 93% to be considered verified
	// Note: immutable variables are embedded in bytecode at deployment, causing slight differences
	MinBytecodeSimilarityThreshold = 0.93
)
