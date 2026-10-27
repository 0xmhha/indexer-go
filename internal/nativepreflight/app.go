package nativepreflight

import (
	"github.com/0xmhha/indexer-go/internal/config"
)

// appCopy normalizes only selected unresolved identity/path fields in a detached
// YAML tree. The same strict production decoder still checks every key and type.
func appCopy(b []byte, r *Report) (*config.Config, Unknown, error) {
	c := config.NewConfig()
	// DecodeStrict first catches duplicate/unknown syntax without normalization.
	// Identity HOLD strings are legal YAML strings and only production validation
	// (below) needs a neutral detached copy.
	if err := config.DecodeStrict(b, c); err != nil {
		return c, nil, err
	}
	u := Unknown{}
	e := c.Source.Execution
	if e != nil {
		for _, f := range []struct {
			key     string
			value   *string
			neutral string
		}{{"hash", &e.AnchorHash, NeutralHash}, {"root", &e.AnchorRoot, NeutralHash}, {"socket", &e.Socket, "/unresolved/execution.sock"}} {
			if Unresolved(*f.value) {
				u[f.key] = true
				r.Add("app.source.execution."+f.key, Hold, "operator_input_missing")
				*f.value = f.neutral
			}
		}
	}
	return c, u, nil
}
