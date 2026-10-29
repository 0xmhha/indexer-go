package ingress

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const testModule = "test.execution.ingress"

var prepared atomic.Int32

type program struct {
	deps    Dependencies
	stopped atomic.Bool
}

func (*program) Check(context.Context) error { return nil }
func (*program) Run(context.Context) error   { return nil }
func (p *program) Stop()                     { p.stopped.Store(true) }
func init() {
	Register(testModule, Module{Profile: "test", Validate: func(Config) error { return nil }, Prepare: func(_ context.Context, _ Config, d Dependencies) (Program, error) {
		prepared.Add(1)
		return &program{deps: d}, nil
	}})
	Register(testModule+".delayed", Module{Profile: "test", Validate: func(Config) error { return nil }, Prepare: func(_ context.Context, _ Config, d Dependencies) (Program, error) {
		w := d.Store.(*waitingStore)
		close(w.started)
		<-w.release
		return w.result, nil
	}})
}

type waitingStore struct {
	status
	started, release chan struct{}
	result           *program
}

type status struct{ control port.ExecutionControl }

func (s *status) ExecutionStatus(context.Context) (port.ExecutionControl, error) {
	return s.control, nil
}

type canonical struct{ calls atomic.Int32 }

func (c *canonical) HashAt(context.Context, uint64) (common.Hash, error) {
	c.calls.Add(1)
	return common.Hash{}, nil
}
func configFixture() Config {
	return Config{Module: testModule, Window: "raw", Native: NativeConfig{Executable: "/app/test-import", DataDir: "/data/native"}}
}
func depsFixture() Dependencies {
	c := port.ExecutionControl{ExecutionConfig: port.ExecutionConfig{Chain: execwire.Chain{ID: "1", Genesis: execwire.Hash{1}}, Anchor: execwire.Ref{Generation: 1, Hash: execwire.Hash{1}}, AnchorRoot: execwire.Hash{2}}, Generation: 1, Visible: execwire.Ref{Generation: 1, Hash: execwire.Hash{1}}, Root: execwire.Hash{2}, BaselineReady: true}
	return Dependencies{Store: &status{c}, Canonical: &canonical{}, Profile: evm.New("test"), Control: c, Socket: "/ipc/feed.sock"}
}
func TestIngressConfigPureValidationAndBorrowedDependencies(t *testing.T) {
	c := configFixture()
	before := prepared.Load()
	require.NoError(t, c.Validate(), "registered explicit ingress must validate without opening an executable, volume, RPC or Store")
	require.Equal(t, before, prepared.Load())
	d := depsFixture()
	p, err := Prepare(t.Context(), c, d)
	require.NoError(t, err)
	defer p.Stop()
	require.Same(t, d.Store, p.(*program).deps.Store)
	require.Same(t, d.Canonical, p.(*program).deps.Canonical)
	require.Zero(t, d.Canonical.(*canonical).calls.Load())
	n := c.Normalize()
	require.Equal(t, 172800, n.MaxRawBlocks)
	require.Equal(t, 8, n.MaxActiveBlocks)
	require.Equal(t, int64(256<<20), n.MaxActiveBytes)
	require.Equal(t, time.Minute, n.Timeout)
}
func TestIngressConfigRejectsUnknownUnsafeOrUnboundedInputs(t *testing.T) {
	for _, change := range []func(*Config){
		func(c *Config) { c.Module = "unknown" }, func(c *Config) { c.Window = "" }, func(c *Config) { c.Native.Executable = "relative" }, func(c *Config) { c.Native.DataDir = "relative" }, func(c *Config) { c.Native.DataDir = "/" }, func(c *Config) { c.Native.Executable = "/data/native/binary" }, func(c *Config) { c.MaxRawBlocks = -1 }, func(c *Config) { c.MaxRawBlocks = 1<<20 + 1 }, func(c *Config) { c.MaxActiveBlocks = 65 }, func(c *Config) { c.MaxActiveBytes = 2<<30 + 1 }, func(c *Config) { c.Timeout = -time.Second }, func(c *Config) { c.Timeout = 2 * time.Hour },
	} {
		c := configFixture()
		change(&c)
		require.Error(t, c.Validate())
	}
}
func TestIngressConfigStrictNestedYAMLAndImmutablePeers(t *testing.T) {
	var c Config
	require.Error(t, yaml.Unmarshal([]byte("module: test.execution.ingress\nnative:\n  executable: /app/native\n  data_dirr: /native/node\n"), &c))
	require.Error(t, yaml.Unmarshal([]byte("peer:\n  key_fille: /private/key\n"), &c))
	c = configFixture()
	c.Peer.Peers = []string{"first"}
	n := c.Normalize()
	c.Peer.Peers[0] = "changed"
	require.Equal(t, "first", n.Peer.Peers[0])
}
func TestIngressPrepareRejectsStaleOrUnpreparedStoreAndWrongProfile(t *testing.T) {
	for _, change := range []func(*Dependencies){
		func(d *Dependencies) { d.Store = nil }, func(d *Dependencies) { d.Canonical = nil }, func(d *Dependencies) { d.Profile = evm.New("foreign") }, func(d *Dependencies) { d.Control.BaselineReady = false; d.Store = &status{d.Control} }, func(d *Dependencies) { d.Control.Maintenance = true; d.Store = &status{d.Control} }, func(d *Dependencies) { d.Control.Generation = 2 },
	} {
		d := depsFixture()
		change(&d)
		_, err := Prepare(t.Context(), configFixture(), d)
		require.Error(t, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := Prepare(ctx, configFixture(), depsFixture())
	require.ErrorIs(t, err, context.Canceled)
}

func TestIngressPrepareCancellationClosesReturnedResources(t *testing.T) {
	d := depsFixture()
	w := &waitingStore{status: status{d.Control}, started: make(chan struct{}), release: make(chan struct{}), result: &program{}}
	d.Store = w
	c := configFixture()
	c.Module += ".delayed"
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		p, err := Prepare(ctx, c, d)
		if p != nil {
			defer p.Stop()
		}
		done <- err
	}()
	<-w.started
	cancel()
	close(w.release)
	require.ErrorIs(t, <-done, context.Canceled, "cancellation during preparation must not return an active prepared owner")
	require.True(t, w.result.stopped.Load())
}
