// Package observetest builds synthetic observation bindings for owner tests.
package observetest

import (
	"github.com/0xmhha/indexer-go/internal/nativeobserve"
	"testing"
)

func Bind(t testing.TB, metrics ...nativeobserve.Metric) (*nativeobserve.Inbox, *nativeobserve.Tap) {
	t.Helper()
	i, e := nativeobserve.NewInbox(4)
	if e != nil {
		t.Fatal(e)
	}
	var bindings []nativeobserve.Binding
	for _, m := range metrics {
		b := nativeobserve.Binding{Scope: nativeobserve.Scope{Kind: nativeobserve.NativePipeline, ID: nativeobserve.ResourceID{byte(m)}}, Metric: m, Unit: nativeobserve.Blocks, Source: nativeobserve.Source{Token: nativeobserve.SourceToken{1}, Incarnation: 1, Run: nativeobserve.RunID{1}}, Series: nativeobserve.Digest{1}, Policy: nativeobserve.Digest{1}, Chain: nativeobserve.Chain{ID: 5042002, Genesis: nativeobserve.Hash{1}, AnchorHash: nativeobserve.Hash{1}}}
		switch m {
		case nativeobserve.RawDurableHeight:
			b.Definition = nativeobserve.RawDurable
			b.Epochs = nativeobserve.Epochs{Receiver: 1, Raw: 1}
		case nativeobserve.NativeExecutedHeight:
			b.Definition = nativeobserve.NativeExecuted
			b.Epochs = nativeobserve.Epochs{Receiver: 1, Native: 1}
		case nativeobserve.SQLVisibleHeight:
			b.Definition = nativeobserve.SQLVisible
			b.Epochs = nativeobserve.Epochs{Receiver: 1, Native: 1, Read: 1}
		case nativeobserve.SourceACKHeight:
			b.Definition = nativeobserve.DurableSourceACK
			b.Epochs = nativeobserve.Epochs{Receiver: 1, Native: 1, Read: 1}
		case nativeobserve.CaptureReceivedHeight:
			b.Scope.Kind = nativeobserve.LegacyCapture
			b.Definition = nativeobserve.CaptureReceived
			b.Epochs = nativeobserve.Epochs{Capture: 1}
		default:
			t.Fatal("unsupported test metric")
		}
		bindings = append(bindings, b)
	}
	tap, e := i.Bind(bindings)
	if e != nil {
		t.Fatal(e)
	}
	return i, tap
}
