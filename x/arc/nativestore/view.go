package nativestore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ReadView supplies the persistent OFD inherited by a managed read-only child.
// Its owner joins that child before releasing an interrupted operation's lease.
type ReadView struct {
	dir       string
	file, run *os.File
	ctx       context.Context
	cancel    context.CancelFunc
	gate      chan struct{}
	once      sync.Once
	closeErr  error
}
type ViewLease struct {
	v      *ReadView
	mu     sync.Mutex
	closed bool
	err    error
}

func readViewClean(dir string) error {
	if err := readViewIdentity(dir); err != nil {
		return err
	}
	for _, name := range []string{".arc-native-import.pending", ".arc-native-bootstrap.pending"} {
		if _, err := os.Lstat(filepath.Join(dir, name)); !errors.Is(err, os.ErrNotExist) {
			return ErrPending
		}
	}
	return nil
}
func readViewIdentity(dir string) error {
	if err := CheckStart(dir); err != nil {
		return err
	}
	return PrivateFile(filepath.Join(dir, ".arc-native-import.identity.json"))
}
func OpenReadView(ctx context.Context, dir string) (*ReadView, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(dir) || filepath.Clean(dir) == string(os.PathSeparator) {
		return nil, ErrUnsafe
	}
	for _, path := range []string{dir, filepath.Dir(dir)} {
		if err := viewDirectory(path); err != nil {
			return nil, err
		}
	}
	if err := readViewIdentity(dir); err != nil {
		return nil, err
	}
	run, err := AcquireRun(dir)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(filepath.Dir(dir), "."+filepath.Base(dir)+".arc-native-import-view")
	f, err := openViewFile(path)
	if err != nil {
		_ = run.Close()
		return nil, err
	}
	life, cancel := context.WithCancel(context.Background())
	v := &ReadView{dir: dir, file: f, run: run, ctx: life, cancel: cancel, gate: make(chan struct{}, 1)}
	v.gate <- struct{}{}
	if err = ctx.Err(); err != nil {
		_ = v.Close()
		return nil, err
	}
	return v, nil
}
func (v *ReadView) File() *os.File { return v.file }
func (v *ReadView) checkFiles() error {
	for _, file := range []*os.File{v.file, v.run} {
		if err := PrivateFile(file.Name()); err != nil {
			return err
		}
		f, err := file.Stat()
		if err != nil {
			return err
		}
		p, err := os.Lstat(file.Name())
		if err != nil {
			return err
		}
		if !os.SameFile(f, p) {
			return ErrUnsafe
		}
	}
	return readViewIdentity(v.dir)
}
func (v *ReadView) check() error {
	if err := v.checkFiles(); err != nil {
		return err
	}
	return readViewClean(v.dir)
}
func (v *ReadView) lock(ctx context.Context, f *os.File, exclusive bool) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if v.ctx.Err() != nil {
			return os.ErrClosed
		}
		err := tryViewLock(f, exclusive)
		if err == nil {
			return nil
		}
		if !viewWouldBlock(err) {
			return err
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-v.ctx.Done():
			timer.Stop()
			return os.ErrClosed
		case <-timer.C:
		}
	}
}
func (v *ReadView) Acquire(ctx context.Context) (*ViewLease, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-v.ctx.Done():
		return nil, os.ErrClosed
	case <-v.gate:
	}
	transferred := false
	defer func() {
		if !transferred {
			v.gate <- struct{}{}
		}
	}()
	if v.ctx.Err() != nil {
		return nil, os.ErrClosed
	}
	// Import intent is meaningful only after SH: a live writer holds EX while
	// its normal commit is pending. A leftover intent after EX remains HOLD.
	if err := v.checkFiles(); err != nil {
		return nil, err
	}
	turn, err := openViewFile(filepath.Join(filepath.Dir(v.dir), "."+filepath.Base(v.dir)+".arc-native-import-turn"))
	if err != nil {
		return nil, err
	}
	defer func() { _ = turn.Close() }()
	if err = v.lock(ctx, turn, true); err != nil {
		return nil, err
	}
	if err = v.lock(ctx, v.file, false); err != nil {
		return nil, err
	}
	if err = v.check(); err != nil {
		_ = unlockView(v.file)
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		_ = unlockView(v.file)
		return nil, err
	}
	transferred = true
	return &ViewLease{v: v}, nil
}

// Validate is used before accepting decoded bytes, while the lease still owns SH.
func (l *ViewLease) Validate() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return os.ErrClosed
	}
	return l.v.check()
}
func (l *ViewLease) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.closed {
		l.err = unlockView(l.v.file)
		l.closed = true
		l.v.gate <- struct{}{}
	}
	return l.err
}
func (v *ReadView) Close() error {
	v.once.Do(func() {
		v.cancel()
		<-v.gate
		v.closeErr = errors.Join(v.file.Close(), v.run.Close())
		v.gate <- struct{}{}
	})
	return v.closeErr
}
