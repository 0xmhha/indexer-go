package execwire

const FullBlobFormat = "eip4844.full-blob-proof.v1"
const MaxBlobResponseBytes = 32*(131072+128) + 1024

// BlobBundle retains a complete relay envelope before any spool admission/ACK.
// Nil means not collected. Syntax/bounds are checked here; the consumer must
// bind canonical transactions/chain/profile and verify every KZG proof.
type BlobBundle struct {
	Version  uint32
	Format   string
	Profile  string
	Provider string
	Response []byte
}

func ValidateBlobBundle(b *BlobBundle) error {
	if b == nil {
		return nil
	}
	if b.Version != 1 || b.Format != FullBlobFormat || !blobLabel(b.Profile) || !blobLabel(b.Provider) || len(b.Response) == 0 {
		return ErrProtocol
	}
	if len(b.Response) > MaxBlobResponseBytes {
		return ErrLimit
	}
	return nil
}
func blobLabel(s string) bool {
	if len(s) == 0 || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}
