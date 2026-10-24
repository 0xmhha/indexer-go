package chains

// ExecutionProfile explicitly declares a profile with a matching verified
// journal producer. A name alone never grants execution ingest capability.
type ExecutionProfile interface {
	BinaryProfile
	ExecutionJournals() bool
}

func SupportsExecution(p Profile) bool {
	v, ok := p.(ExecutionProfile)
	return ok && v.ExecutionJournals()
}
