package config

import (
	"bytes"
	"github.com/0xmhha/indexer-go/execwire/configfile"
	"gopkg.in/yaml.v3"
	"io"
	"reflect"
	"strings"
)

// YAML is strict against the actual Config schema; aliases/merge keys are refused
// to keep expansion bounded and prevent duplicate-key ambiguity.
func DecodeStrict(b []byte, c *Config) error {
	if len(b) == 0 || len(b) > configfile.MaxBytes {
		return configfile.ErrInput
	}
	var root yaml.Node
	if err := yaml.Unmarshal(b, &root); err != nil {
		return configfile.ErrInput
	}
	var walk func(*yaml.Node, int) bool
	walk = func(n *yaml.Node, depth int) bool {
		if depth > 32 || n.Kind == yaml.AliasNode {
			return false
		}
		if n.Kind == yaml.MappingNode {
			seen := map[string]bool{}
			for i := 0; i < len(n.Content); i += 2 {
				k := n.Content[i]
				if k.Kind != yaml.ScalarNode || k.Value == "<<" || seen[k.Value] {
					return false
				}
				seen[k.Value] = true
			}
		}
		for _, child := range n.Content {
			if !walk(child, depth+1) {
				return false
			}
		}
		return true
	}
	if !walk(&root, 0) || !validYAMLNulls(&root, reflect.TypeOf(Config{})) {
		return configfile.ErrInput
	}
	d := yaml.NewDecoder(bytes.NewReader(b))
	d.KnownFields(true)
	if err := d.Decode(c); err != nil {
		return configfile.ErrInput
	}
	var tail any
	if d.Decode(&tail) != io.EOF {
		return configfile.ErrInput
	}
	return nil
}

func validYAMLNulls(n *yaml.Node, t reflect.Type) bool {
	if n.Tag == "!!null" {
		switch t.Kind() {
		case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface:
			return true
		}
		return false
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if n.Kind == yaml.DocumentNode {
		for _, c := range n.Content {
			if !validYAMLNulls(c, t) {
				return false
			}
		}
	}
	if n.Kind == yaml.MappingNode {
		for i := 0; i < len(n.Content); i += 2 {
			key, child := n.Content[i].Value, n.Content[i+1]
			if t.Kind() == reflect.Map {
				if !validYAMLNulls(child, t.Elem()) {
					return false
				}
			} else if t.Kind() == reflect.Struct {
				for j := 0; j < t.NumField(); j++ {
					f := t.Field(j)
					name := strings.Split(f.Tag.Get("yaml"), ",")[0]
					if name == "" {
						name = strings.ToLower(f.Name)
					}
					if name == key && !validYAMLNulls(child, f.Type) {
						return false
					}
				}
			}
		}
	}
	if n.Kind == yaml.SequenceNode && (t.Kind() == reflect.Slice || t.Kind() == reflect.Array) {
		for _, c := range n.Content {
			if !validYAMLNulls(c, t.Elem()) {
				return false
			}
		}
	}
	return true
}
