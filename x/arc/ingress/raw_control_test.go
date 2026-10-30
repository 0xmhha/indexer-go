package arcingress

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestNativeRawControlV1MigrationRequiresCompleteInitialReadyAndSQLBinding(t *testing.T) {
	for _, bad := range []bool{false, true} {
		t.Run(map[bool]string{false: "valid", true: "stale"}[bad], func(t *testing.T) {
			p := rawProgram(t)
			p.bootstrap = p.identity.BootstrapDigest
			ready := nativeReady{Version: 1, Identity: p.identity, Head: p.head}
			if bad {
				s := p.deps.Store.(*mutableStatus)
				s.mu.Lock()
				s.control.Generation++
				s.mu.Unlock()
			}
			err := p.migrateControl(t.Context(), ready)
			owner, readErr := p.raw.ReadOwner(t.Context())
			require.NoError(t, readErr)
			var control rawControl
			require.NoError(t, strictJSON(owner.State, &control))
			if bad {
				require.Error(t, err)
				require.Equal(t, uint8(1), control.Version)
			} else {
				require.NoError(t, err)
				require.Equal(t, uint8(2), control.Version, "N3 owner must be explicitly upgraded only after complete current proof")
				require.Equal(t, uint64(1), control.NativeEpoch)
				require.Equal(t, uint64(1), control.Generation)
			}
		})
	}
}
func TestNativeRawControlV2BindsIndependentNativeReceiverAndRawEpochs(t *testing.T) {
	for _, change := range []string{"none", "native", "generation", "raw", "version"} {
		t.Run(change, func(t *testing.T) {
			p := rawProgram(t)
			before, err := json.Marshal(p.control)
			require.NoError(t, err)
			c := p.control
			c.Version = 2
			c.NativeEpoch = p.head.ImportEpoch
			c.Generation = p.deps.Control.Generation
			c.RawEpoch = 9
			switch change {
			case "native":
				c.NativeEpoch++
			case "generation":
				c.Generation++
			case "raw":
				c.RawEpoch = 0
			case "version":
				c.Version = 3
			}
			next, err := json.Marshal(c)
			require.NoError(t, err)
			require.NoError(t, p.raw.CompareOwner(t.Context(), before, next))
			err = p.reconcile(t.Context())
			if change == "none" {
				require.NoError(t, err, "raw-only rotations do not assign E1 or native generations")
			} else {
				require.Error(t, err)
			}
		})
	}
}
