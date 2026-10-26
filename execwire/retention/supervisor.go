package retention

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"sync"

	"github.com/0xmhha/indexer-go/execwire"
)

type Supervision struct {
	Version                 uint16
	PermitDigest, Challenge execwire.Hash
	BackupDigest            execwire.Hash
}
type SupervisedReply struct {
	Version   uint16
	Challenge execwire.Hash
	Receipt   Receipt
}

// Supervise requires an inherited pipe from the DB lease owner. Ordinary
// artifact files/TTYs cannot act as a live supervisor. EOF/unexpected input
// cancels filesystem work, and stop closes/joins every owned reader.
func Supervise(parent context.Context, f *os.File, p Permit) (context.Context, Supervision, func(), error) {
	if f == nil {
		return nil, Supervision{}, nil, ErrUnsafe
	}
	if e := parent.Err(); e != nil {
		_ = f.Close()
		return nil, Supervision{}, nil, e
	}
	transferred := false
	defer func() {
		if !transferred && f != nil {
			_ = f.Close()
		}
	}()
	if p.Validate() != nil {
		return nil, Supervision{}, nil, ErrUnsafe
	}
	info, e := f.Stat()
	if e != nil {
		return nil, Supervision{}, nil, e
	}
	if info.Mode()&os.ModeNamedPipe == 0 {
		return nil, Supervision{}, nil, ErrUnsafe
	}
	f, e = supervisorPipe(f)
	if e != nil {
		return nil, Supervision{}, nil, e
	}
	digest, e := Digest(p)
	if e != nil {
		return nil, Supervision{}, nil, e
	}
	ctx, cancel := context.WithCancel(parent)
	type initialized struct {
		hello Supervision
		err   error
	}
	ready := make(chan initialized, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer cancel()
		reader := bufio.NewReaderSize(f, 4096)
		line, e := reader.ReadSlice('\n')
		var hello Supervision
		if e == nil {
			dec := json.NewDecoder(bytes.NewReader(line))
			dec.DisallowUnknownFields()
			e = dec.Decode(&hello)
			var extra any
			if e == nil && dec.Decode(&extra) != io.EOF {
				e = ErrUnsafe
			}
		}
		if e == nil && (hello.Version != 1 || hello.PermitDigest != digest || hello.Challenge == (execwire.Hash{})) {
			e = ErrUnsafe
		}
		ready <- initialized{hello: hello, err: e}
		if e != nil {
			return
		}
		_, _ = reader.ReadByte() // EOF, error OR any extra input revokes the lease.
	}()
	var once sync.Once
	stop := func() { once.Do(func() { cancel(); _ = f.Close(); <-done }) }
	select {
	case <-ctx.Done():
		stop()
		return nil, Supervision{}, nil, ctx.Err()
	case value := <-ready:
		if value.err != nil {
			stop()
			return nil, Supervision{}, nil, value.err
		}
		if e = ctx.Err(); e != nil {
			stop()
			return nil, Supervision{}, nil, e
		}
		transferred = true
		return ctx, value.hello, stop, nil
	}
}
