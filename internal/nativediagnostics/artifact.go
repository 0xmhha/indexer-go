package nativediagnostics

import (
	"encoding/json"
	"reflect"
	"strings"
)

// completeArtifact requires the nonoptional fields of an actual production
// export. Missing scalar fields must never become observed false/zero values.
// Strict decoding has already bounded nesting and rejected duplicate/unknown keys.
func completeArtifact(data []byte, typ reflect.Type) bool {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return true
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(data, &object) != nil || object == nil {
		return false
	}
	fields := map[string]json.RawMessage{}
	for name, value := range object {
		fields[strings.ToLower(name)] = value
	}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.PkgPath != "" {
			continue
		}
		tag := strings.Split(field.Tag.Get("json"), ",")
		if tag[0] == "-" {
			continue
		}
		if field.Anonymous && tag[0] == "" {
			if !completeArtifact(data, field.Type) {
				return false
			}
			continue
		}
		if field.Type.Kind() == reflect.Pointer {
			continue
		}
		name := tag[0]
		if name == "" {
			name = field.Name
		}
		value, ok := fields[strings.ToLower(name)]
		if !ok || !completeArtifact(value, field.Type) {
			return false
		}
	}
	return true
}
