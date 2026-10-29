package arcingress

import (
	"context"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func validNativeReady() (nativeReady, port.ExecutionControl) {
	a := nativeRef{Hash: execwire.Hash{1}, Root: execwire.Hash{2}}
	i := nativeIdentity{Version: 1, Instance: execwire.Hash{3}, ChainID: 5042002, Genesis: a.Hash, ArcRevision: ArcRevision, RethRevision: RethRevision, Layout: "reth-native-v2", BootstrapDigest: execwire.Hash{4}, Anchor: a}
	h := nativeHead{Version: 1, Instance: i.Instance, ImportEpoch: 1, ReadEpoch: 2, RequestID: 1, Tip: nativeRef{Height: 1, Hash: execwire.Hash{5}, Root: execwire.Hash{6}}}
	c := port.ExecutionControl{ExecutionConfig: port.ExecutionConfig{Chain: execwire.Chain{ID: "5042002", Genesis: a.Hash}, Anchor: execwire.Ref{Generation: 1, Hash: a.Hash}, AnchorRoot: a.Root}, Generation: 1, BaselineReady: true, Visible: execwire.Ref{Generation: 1, Height: 1, Hash: h.Tip.Hash}, Root: h.Tip.Root}
	return nativeReady{Version: 1, Identity: i, Head: h}, c
}
func TestNativeReadyRejectsSQLTipConflictAndInvalidSequence(t *testing.T) {
	r, c := validNativeReady()
	require.NoError(t, validateReady(r, c, r.Identity.BootstrapDigest))
	c.Visible.Hash = execwire.Hash{9}
	require.Error(t, validateReady(r, c, r.Identity.BootstrapDigest), "native ready cannot hide a conflicting published SQL tip")
	_, c = validNativeReady()
	c.Root = execwire.Hash{9}
	require.Error(t, validateReady(r, c, r.Identity.BootstrapDigest))
	_, c = validNativeReady()
	r.Head.ReadEpoch = 0
	require.Error(t, validateReady(r, c, r.Identity.BootstrapDigest))
}
func TestNativeChildCancelledBeforeStartOpensNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	c, err := startNative(ctx, "/absent/native-import", "/absent/native", time.Second)
	require.Nil(t, c)
	require.ErrorIs(t, err, context.Canceled, "cancellation must precede lease or child I/O")
}
