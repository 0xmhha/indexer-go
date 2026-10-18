package model

import (
	"bytes"
	"encoding/json"
)

type CallTraceDefinition struct {
	Provider string `json:"provider"`
	Format   string `json:"format"`
	Profile  string `json:"profile"`
}

func (d CallTraceDefinition) Validate() error {
	if d.Format != "geth.callTracer.v1" {
		return ErrCaptureInvalid
	}
	for _, label := range []string{d.Provider, d.Profile} {
		if len(label) == 0 || len(label) > 64 {
			return ErrCaptureInvalid
		}
		for _, c := range label {
			if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-' {
				continue
			}
			return ErrCaptureInvalid
		}
	}
	return nil
}
func CallTraceDefinitionFor(d SupplementStreamDescriptor) (CallTraceDefinition, error) {
	var v CallTraceDefinition
	if d.Validate() != nil || d.Codec != CallTraceCodec || d.ParentCheckpoint || d.Required {
		return v, ErrCaptureInvalid
	}
	dec := json.NewDecoder(bytes.NewReader(d.Selection))
	dec.DisallowUnknownFields()
	if e := dec.Decode(&v); e != nil || v.Validate() != nil {
		return v, ErrCaptureInvalid
	}
	canonical, e := json.Marshal(v)
	if e != nil || !bytes.Equal(canonical, d.Selection) {
		return v, ErrCaptureInvalid
	}
	return v, nil
}

const CallTraceCodec = "evm.call-traces.geth.v1"

func (d ProjectionDescriptor) ValidateCallTraces() error {
	if d.CallTraces == nil {
		return nil
	}
	v := d.CallTraces
	if v.Validate() != nil || v.Codec != CallTraceCodec || v.Required || v.ParentCheckpoint || v.FromHeight <= d.Anchor.Number {
		return ErrCaptureInvalid
	}
	if _, e := CallTraceDefinitionFor(*v); e != nil {
		return e
	}
	if d.StateProofs != nil && d.StateProofs.Name == v.Name {
		return ErrCaptureInvalid
	}
	return nil
}
