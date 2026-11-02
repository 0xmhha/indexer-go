package nativealerts

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"time"

	"github.com/0xmhha/indexer-go/execwire/configfile"
)

// Limit collection width before decoding into large structs. Raw per-record
// parsing is bounded by the total file limit; strict/depth checks follow.
func boundedEnvelope(data []byte) bool {
	d := json.NewDecoder(bytes.NewReader(data))
	tok, err := d.Token()
	if err != nil || tok != json.Delim('{') {
		return false
	}
	seen := map[string]bool{}
	for d.More() {
		tok, err = d.Token()
		key, ok := tok.(string)
		key = strings.ToLower(key)
		if err != nil || !ok || seen[key] {
			return false
		}
		seen[key] = true
		if key == "version" {
			var version int
			if d.Decode(&version) != nil || version != 1 {
				return false
			}
			continue
		}
		limit := MaxRules
		if key == "deliveries" {
			limit = MaxQueued
		} else if key != "records" {
			return false
		}
		tok, err = d.Token()
		if err != nil {
			return false
		}
		if tok == nil {
			continue
		}
		if tok != json.Delim('[') {
			return false
		}
		count := 0
		for d.More() {
			count++
			if count > limit {
				return false
			}
			var raw json.RawMessage
			if d.Decode(&raw) != nil {
				return false
			}
		}
		if tok, err = d.Token(); err != nil || tok != json.Delim(']') {
			return false
		}
	}
	return len(seen) == 3
}

// Every field produced by this v1 format is required, including explicit null
// optionals. encoding/json alone would default missing levels to normal and
// silently pad/truncate fixed identity arrays.
func requiredSchema(raw any, t reflect.Type) bool {
	if t.Kind() == reflect.Pointer {
		if raw == nil {
			return true
		}
		return requiredSchema(raw, t.Elem())
	}
	if t == reflect.TypeFor[time.Time]() {
		_, ok := raw.(string)
		return ok
	}
	switch t.Kind() {
	case reflect.Struct:
		object, ok := raw.(map[string]any)
		if !ok {
			return false
		}
		for n := 0; n < t.NumField(); n++ {
			field := t.Field(n)
			found := false
			for key, value := range object {
				if strings.EqualFold(key, field.Name) {
					found = true
					if !requiredSchema(value, field.Type) {
						return false
					}
					break
				}
			}
			if !found {
				return false
			}
		}
	case reflect.Array, reflect.Slice:
		if raw == nil {
			return t.Kind() == reflect.Slice
		}
		array, ok := raw.([]any)
		if !ok || t.Kind() == reflect.Array && len(array) != t.Len() {
			return false
		}
		for _, value := range array {
			if !requiredSchema(value, t.Elem()) {
				return false
			}
		}
	}
	return true
}

func decodeSnapshot(data []byte) (Snapshot, error) {
	if len(data) == 0 || len(data) > MaxCheckpointBytes || !boundedEnvelope(data) {
		return Snapshot{}, ErrCheckpoint
	}
	var result Snapshot
	if configfile.JSON(data, &result) != nil {
		return Snapshot{}, ErrCheckpoint
	}
	var raw any
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if d.Decode(&raw) != nil || !requiredSchema(raw, reflect.TypeFor[Snapshot]()) || validateSnapshot(result) != nil {
		return Snapshot{}, ErrCheckpoint
	}
	return result, nil
}
