package execwire

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"time"
)

// Split copies the payload once. The resulting frames share that immutable copy.
func Split(chain Chain, ref Ref, p []byte, frameBytes int) ([]Frame, error) {
	if !validChain(chain) || !validRef(ref) || ref.Height == 0 && ref.Hash != chain.Genesis {
		return nil, ErrProtocol
	}
	if len(p) == 0 || len(p) > MaxBlockBytes || frameBytes <= MaxHeaderBytes || frameBytes > MaxFrameBytes {
		return nil, ErrLimit
	}
	chunk := frameBytes - MaxHeaderBytes
	count := (len(p) + chunk - 1) / chunk
	if count > MaxParts {
		return nil, ErrLimit
	}
	p = bytes.Clone(p)
	digest := Hash(sha256.Sum256(p))
	out := make([]Frame, 0, count)
	for i, start := 0, 0; start < len(p); i, start = i+1, start+chunk {
		data := p[start:min(start+chunk, len(p))]
		out = append(out, Frame{Kind: ResultPart, Header: Header{Version: Version, Chain: chain, Ref: ref, Digest: digest, PartDigest: Hash(sha256.Sum256(data)), Index: uint32(i), Count: uint32(count), Total: uint64(len(p))}, Payload: data})
	}
	return out, nil
}

type Limits struct {
	Blocks int
	Bytes  uint64
	TTL    time.Duration
}
type assemblyKey struct {
	Chain Chain
	Ref   Ref
}
type assembly struct {
	header   Header
	until    time.Time
	parts    [][]byte
	received int
	bytes    uint64
}

// Assembler has one owner. Reservations include a block's entire declared size,
// including missing parts. Completed payload construction transiently needs a
// second copy of that block; it releases the reservation before returning.
type Assembler struct {
	limits  Limits
	pending map[assemblyKey]*assembly
	bytes   uint64
}

func NewAssembler(limits Limits) (*Assembler, error) {
	if limits.Blocks <= 0 || limits.Blocks > 64 || limits.Bytes == 0 || limits.Bytes > 256<<20 || limits.TTL <= 0 || limits.TTL > time.Hour {
		return nil, ErrLimit
	}
	return &Assembler{limits: limits, pending: make(map[assemblyKey]*assembly)}, nil
}

func (a *Assembler) Add(now time.Time, f Frame) ([]byte, bool, error) {
	if f.Kind != ResultPart {
		return nil, false, ErrProtocol
	}
	if err := validateFrame(f); err != nil {
		return nil, false, err
	}
	a.Expire(now)
	key := assemblyKey{f.Header.Chain, f.Header.Ref}
	item := a.pending[key]
	if item == nil {
		if len(a.pending) >= a.limits.Blocks || f.Header.Total > a.limits.Bytes-a.bytes {
			return nil, false, ErrLimit
		}
		item = &assembly{header: f.Header, until: now.Add(a.limits.TTL), parts: make([][]byte, int(f.Header.Count))}
		a.pending[key] = item
		a.bytes += f.Header.Total
	} else if item.header.Digest != f.Header.Digest || item.header.Total != f.Header.Total || item.header.Count != f.Header.Count {
		return nil, false, fmt.Errorf("%w: conflicting assembly", ErrProtocol)
	}
	index := int(f.Header.Index)
	if prev := item.parts[index]; prev != nil {
		if !bytes.Equal(prev, f.Payload) {
			return nil, false, fmt.Errorf("%w: conflicting duplicate", ErrProtocol)
		}
		return nil, false, nil
	}
	if uint64(len(f.Payload)) > item.header.Total-item.bytes {
		return nil, false, ErrLimit
	}
	item.parts[index] = bytes.Clone(f.Payload)
	item.received++
	item.bytes += uint64(len(f.Payload))
	if item.received != len(item.parts) {
		return nil, false, nil
	}
	delete(a.pending, key)
	a.bytes -= item.header.Total
	if item.bytes != item.header.Total {
		return nil, false, ErrIncomplete
	}
	p := make([]byte, 0, int(item.bytes))
	for _, part := range item.parts {
		p = append(p, part...)
	}
	if Hash(sha256.Sum256(p)) != item.header.Digest {
		return nil, false, fmt.Errorf("%w: result digest", ErrProtocol)
	}
	return p, true, nil
}

func (a *Assembler) Expire(now time.Time) int {
	n := 0
	for key, item := range a.pending {
		if !now.Before(item.until) {
			delete(a.pending, key)
			a.bytes -= item.header.Total
			n++
		}
	}
	return n
}
func (a *Assembler) Usage() (int, uint64) { return len(a.pending), a.bytes }
func (a *Assembler) Reset()               { clear(a.pending); a.bytes = 0 }
