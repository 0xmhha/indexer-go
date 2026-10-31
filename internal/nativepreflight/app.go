package nativepreflight

import (
	"github.com/0xmhha/indexer-go/internal/config"
	"gopkg.in/yaml.v3"
)

// appCopy normalizes only selected unresolved identity/path fields in a detached
// YAML tree. The same strict production decoder still checks every key and type.
func appCopy(b []byte, r *Report) (*config.Config, Unknown, error) {
	c := config.NewConfig()
	root, err := config.ParseStrictYAML(b)
	if err != nil {
		return c, nil, err
	}
	u := Unknown{}
	// Original duplicate/null/alias/trailing/bound checks run before rewriting.
	// Only the selected numeric operator fields accept deliberate placeholders;
	// unknown keys and all other types still pass through the production decoder.
	if execution := appYAMLField(root, "source", "execution"); execution != nil && execution.Kind == yaml.MappingNode {
		for _, f := range []struct {
			parent           *yaml.Node
			field, key, path string
		}{
			{execution, "anchor_height", "height", "app.source.execution.anchor_height"},
			{appYAMLField(root, "indexer"), "start_height", "start", "app.indexer.start_height"},
		} {
			n := appYAMLField(f.parent, f.field)
			if n == nil || n.Kind == yaml.ScalarNode && n.Tag == "!!str" && Unresolved(n.Value) {
				u[f.key] = true
				r.Add(f.path, Hold, "operator_input_missing")
				if n != nil {
					n.Tag, n.Value = "!!int", "0"
					n.Style = 0
				}
			}
		}
	}
	if ing := appYAMLField(root, "source", "execution", "ingress"); ing != nil {
		peer := appYAMLField(ing, "peer")
		n := appYAMLField(peer, "network_id")
		if n == nil || n.Kind == yaml.ScalarNode && n.Tag == "!!str" && Unresolved(n.Value) {
			u["ingress_network"] = true
			r.Add("app.ingress.peer.network_id", Hold, "operator_input_missing")
			if n != nil {
				n.Tag, n.Value, n.Style = "!!int", "0", 0
			}
		}
	}
	if len(u) > 0 {
		b, err = yaml.Marshal(root)
		if err != nil {
			return c, nil, err
		}
	}
	if err := config.DecodeStrict(b, c); err != nil {
		return c, nil, err
	}
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

func appYAMLField(n *yaml.Node, path ...string) *yaml.Node {
	if n != nil && n.Kind == yaml.DocumentNode && len(n.Content) == 1 {
		n = n.Content[0]
	}
	for _, key := range path {
		if n == nil || n.Kind != yaml.MappingNode {
			return nil
		}
		var child *yaml.Node
		for i := 0; i < len(n.Content); i += 2 {
			if n.Content[i].Value == key {
				child = n.Content[i+1]
				break
			}
		}
		n = child
	}
	return n
}
