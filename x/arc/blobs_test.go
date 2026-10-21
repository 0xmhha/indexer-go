package arc_test

import (
	"sync"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/x/arc"
	"github.com/stretchr/testify/require"
)

type projectProfile struct{ *evm.Profile }

func (p projectProfile) ID() string { return "project-arc" }

type plainProfile struct{ chains.Profile }

func TestBlobCapabilityAuditAndProfileInheritance(t *testing.T) {
	p := arc.New()
	cap, e := chains.BlobCapabilityOf(p)
	require.NoError(t, e)
	require.Equal(t, arc.BlobCapability(), cap)
	require.Equal(t, model.BlobUnsupported, cap.Status)
	outer, e := chains.BlobCapabilityOf(projectProfile{Profile: p})
	require.NoError(t, e)
	require.Equal(t, "project-arc", outer.Profile)
	require.Equal(t, cap.Revision, outer.Revision)
	unknown, e := chains.BlobCapabilityOf(plainProfile{Profile: evm.New("plain")})
	require.NoError(t, e)
	require.Equal(t, model.BlobUnknown, unknown.Status)
	require.Empty(t, unknown.SourceURL)
	_, e = chains.BlobCapabilityOf(nil)
	require.Error(t, e)
}
func TestBlobCapabilityConcurrentCopies(t *testing.T) {
	p := arc.New()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 8; j++ {
				cap, e := chains.BlobCapabilityOf(p)
				if e != nil || cap.Status != model.BlobUnsupported {
					t.Errorf("policy: %v %v", cap, e)
					return
				}
				cap.Status = "modified"
			}
		}()
	}
	wg.Wait()
	cap, e := chains.BlobCapabilityOf(p)
	require.NoError(t, e)
	require.Equal(t, model.BlobUnsupported, cap.Status)
}
