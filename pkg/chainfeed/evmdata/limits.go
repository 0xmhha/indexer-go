package evmdata

// DecodeLimits bounds encoded datasets and model cardinality before allocation.
// Decode retains the existing capture limits; execution uses bounded wire limits.
type DecodeLimits struct{ PartBytes, Items, Logs int }
