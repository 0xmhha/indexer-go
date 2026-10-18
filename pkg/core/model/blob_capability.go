package model

import (
	"encoding/hex"
	"net/url"
	"strings"
	"unicode/utf8"
)

const BlobUnknown = "unknown"
const BlobUnsupported = "unsupported"

// BlobCapability is an audited operational policy, not captured coverage,
// a fork schedule, a historical absence proof or a live RPC capability probe.
// Unknown permits a caller-supplied library provider; unsupported forbids it.
type BlobCapability struct {
	Version   uint32 `json:"version"`
	Profile   string `json:"profile"`
	Status    string `json:"status"`
	Reason    string `json:"reason"`
	Basis     string `json:"basis,omitempty"`
	Revision  string `json:"revision,omitempty"`
	SourceURL string `json:"sourceUrl,omitempty"`
}

func blobLabel(s string) bool {
	if len(s) == 0 || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '.' || c == '-' {
			continue
		}
		return false
	}
	return true
}
func (c BlobCapability) Validate() error {
	// Profile identities retain the existing custom-profile naming contract.
	if c.Version != 1 || len(c.Profile) == 0 || len(c.Profile) > 256 || !utf8.ValidString(c.Profile) || strings.ContainsRune(c.Profile, 0) || !blobLabel(c.Reason) {
		return ErrCaptureInvalid
	}
	switch c.Status {
	case BlobUnknown:
		if c.Reason != "not_audited" || c.Basis != "" || c.Revision != "" || c.SourceURL != "" {
			return ErrCaptureInvalid
		}
	case BlobUnsupported:
		if c.Basis != "node_transaction_admission" || len(c.Revision) != 40 || strings.ToLower(c.Revision) != c.Revision || len(c.SourceURL) > 512 {
			return ErrCaptureInvalid
		}
		if _, e := hex.DecodeString(c.Revision); e != nil {
			return ErrCaptureInvalid
		}
		u, e := url.Parse(c.SourceURL)
		if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !strings.Contains(u.Path, "/"+c.Revision+"/") {
			return ErrCaptureInvalid
		}
	default:
		return ErrCaptureInvalid
	}
	return nil
}
func (d ProjectionDescriptor) ValidateBlobCapability() error {
	if d.BlobCapability == nil {
		return nil
	}
	if e := d.BlobCapability.Validate(); e != nil {
		return e
	}
	if d.BlobCapability.Status == BlobUnsupported {
		for _, r := range d.Plan.Required {
			if r.Kind == DatasetBlobs {
				return ErrCaptureInvalid
			}
		}
	}
	if d.CallTraces != nil {
		trace, e := CallTraceDefinitionFor(*d.CallTraces)
		if e != nil || trace.Profile != d.BlobCapability.Profile {
			return ErrCaptureInvalid
		}
	}
	return nil
}
