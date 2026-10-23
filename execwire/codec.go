package execwire

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
)

func (h Hash) MarshalText() ([]byte, error)     { return hexText(h[:]), nil }
func (h *Hash) UnmarshalText(p []byte) error    { return decodeHex(p, h[:]) }
func (a Address) MarshalText() ([]byte, error)  { return hexText(a[:]), nil }
func (a *Address) UnmarshalText(p []byte) error { return decodeHex(p, a[:]) }

func hexText(p []byte) []byte {
	out := make([]byte, 2+hex.EncodedLen(len(p)))
	copy(out, "0x")
	hex.Encode(out[2:], p)
	return out
}

func decodeHex(p, out []byte) error {
	if len(p) != 2+len(out)*2 || !bytes.HasPrefix(p, []byte("0x")) {
		return fmt.Errorf("%w: hex length or prefix", ErrProtocol)
	}
	for _, c := range p[2:] {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return fmt.Errorf("%w: noncanonical hex", ErrProtocol)
		}
	}
	_, err := hex.Decode(out, p[2:])
	return err
}

func validRef(r Ref) bool {
	return r.Generation > 0 && r.Generation <= math.MaxInt64 && r.Height <= math.MaxInt64 && r.Hash != (Hash{})
}

func validChain(c Chain) bool {
	if len(c.ID) == 0 || len(c.ID) > 78 || c.ID[0] == '0' || c.Genesis == (Hash{}) {
		return false
	}
	for _, ch := range c.ID {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func validateHeader(k Kind, h Header, payload int) error {
	if h.Version != Version || !validChain(h.Chain) || !validRef(h.Ref) || (h.Ref.Height == 0 && h.Ref.Hash != h.Chain.Genesis) {
		return fmt.Errorf("%w: version, chain or reference", ErrProtocol)
	}
	if k == ResultPart {
		if h.Total == 0 || h.Total > MaxBlockBytes || h.Count == 0 || h.Count > MaxParts || uint64(h.Count) > h.Total || h.Index >= h.Count {
			return fmt.Errorf("%w: part bounds", ErrLimit)
		}
		if payload == 0 || uint64(payload) > h.Total || h.Digest == (Hash{}) || h.PartDigest == (Hash{}) || h.Code != "" {
			return fmt.Errorf("%w: part metadata", ErrProtocol)
		}
		return nil
	}
	if payload != 0 || h.Total != 0 || h.Count != 0 || h.Index != 0 || h.PartDigest != (Hash{}) {
		return fmt.Errorf("%w: control frame payload", ErrProtocol)
	}
	switch k {
	case Hello, Rewind:
		if h.Digest != (Hash{}) || h.Code != "" {
			return fmt.Errorf("%w: control metadata", ErrProtocol)
		}
	case Committed:
		if h.Digest == (Hash{}) || h.Code != "" {
			return fmt.Errorf("%w: committed digest", ErrProtocol)
		}
	case Failure:
		if len(h.Code) == 0 || len(h.Code) > 64 || h.Digest != (Hash{}) {
			return fmt.Errorf("%w: failure code", ErrProtocol)
		}
		for _, ch := range h.Code {
			if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '_' {
				return fmt.Errorf("%w: failure code", ErrProtocol)
			}
		}
	default:
		return fmt.Errorf("%w: unknown kind", ErrProtocol)
	}
	return nil
}

func validateFrame(f Frame) error {
	if len(f.Payload) > MaxFrameBytes-5 {
		return ErrLimit
	}
	if err := validateHeader(f.Kind, f.Header, len(f.Payload)); err != nil {
		return err
	}
	head, err := json.Marshal(f.Header)
	if err != nil {
		return err
	}
	if len(head) > MaxHeaderBytes || len(f.Payload) > MaxFrameBytes-5-len(head) {
		return ErrLimit
	}
	if f.Kind == ResultPart && Hash(sha256.Sum256(f.Payload)) != f.Header.PartDigest {
		return fmt.Errorf("%w: part digest", ErrProtocol)
	}
	return nil
}

// Encode writes uint32be(body length), kind, uint32be(JSON-header length),
// canonical JSON header and the opaque payload. The body never exceeds 8 MiB.
func Encode(w io.Writer, f Frame) error {
	if err := validateFrame(f); err != nil {
		return err
	}
	head, err := json.Marshal(f.Header)
	if err != nil {
		return err
	}
	if len(head) > MaxHeaderBytes || len(f.Payload) > MaxFrameBytes-5-len(head) {
		return ErrLimit
	}
	var prefix [9]byte
	binary.BigEndian.PutUint32(prefix[:4], uint32(5+len(head)+len(f.Payload)))
	prefix[4] = byte(f.Kind)
	binary.BigEndian.PutUint32(prefix[5:], uint32(len(head)))
	for _, p := range [][]byte{prefix[:], head, f.Payload} {
		if len(p) == 0 {
			continue
		}
		n, err := w.Write(p)
		if err != nil {
			return err
		}
		if n != len(p) {
			return io.ErrShortWrite
		}
	}
	return nil
}

// Decode checks declared lengths before allocating header or payload buffers.
// A protocol or partial-frame failure is terminal for that byte stream.
func Decode(r io.Reader) (Frame, error) {
	var prefix [9]byte
	if _, err := io.ReadFull(r, prefix[:4]); err != nil {
		return Frame{}, err
	}
	n := binary.BigEndian.Uint32(prefix[:4])
	if n < 5 || n > MaxFrameBytes {
		return Frame{}, ErrLimit
	}
	if _, err := io.ReadFull(r, prefix[4:]); err != nil {
		return Frame{}, err
	}
	hn := binary.BigEndian.Uint32(prefix[5:])
	if hn == 0 || hn > MaxHeaderBytes || hn > n-5 {
		return Frame{}, ErrLimit
	}
	head := make([]byte, int(hn))
	if _, err := io.ReadFull(r, head); err != nil {
		return Frame{}, err
	}
	f := Frame{Kind: Kind(prefix[4])}
	if err := parseJSON(head, &f.Header); err != nil {
		return Frame{}, err
	}
	length := int(n - 5 - hn)
	if err := validateHeader(f.Kind, f.Header, length); err != nil {
		return Frame{}, err
	}
	if length > 0 {
		f.Payload = make([]byte, length)
		if _, err := io.ReadFull(r, f.Payload); err != nil {
			return Frame{}, err
		}
	}
	if err := validateFrame(f); err != nil {
		return Frame{}, err
	}
	return f, nil
}

// parseJSON bounds nesting and token counts before decoding into slices of
// structs, so a short array of empty objects cannot cause an enormous allocation.
// Re-encoding rejects duplicate keys, alternate hex and ambiguous JSON encodings.
func parseJSON(p []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(p))
	depth, tokens := 0, 0
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("%w: malformed JSON", ErrProtocol)
		}
		tokens++
		if tokens > 262144 {
			return fmt.Errorf("%w: JSON tokens", ErrLimit)
		}
		if d, ok := tok.(json.Delim); ok {
			if d == '{' || d == '[' {
				depth++
			} else {
				depth--
			}
			if depth > 16 || depth < 0 {
				return fmt.Errorf("%w: JSON depth", ErrLimit)
			}
		}
	}
	if depth != 0 {
		return fmt.Errorf("%w: JSON depth", ErrProtocol)
	}
	dec = json.NewDecoder(bytes.NewReader(p))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("%w: JSON fields", ErrProtocol)
	}
	canonical, err := json.Marshal(out)
	if err != nil || !bytes.Equal(canonical, p) {
		return fmt.Errorf("%w: noncanonical JSON", ErrProtocol)
	}
	return nil
}
