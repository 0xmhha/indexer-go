package nativealerts

import (
	"context"
	"slices"
)

type DisabledSink struct{}

func (DisabledSink) Enabled() bool                                { return false }
func (DisabledSink) Send(context.Context, Event) (Receipt, error) { return Receipt{}, ErrDisabled }

// FakeSink is caller-owned and bounded; it has no network implementation.
type FakeSink struct {
	limit              int
	events             []Event
	Fail, WrongReceipt bool
}

func NewFakeSink(limit int) (*FakeSink, error) {
	if limit < 1 || limit > MaxDeliveries {
		return nil, ErrRule
	}
	return &FakeSink{limit: limit}, nil
}
func (f *FakeSink) Enabled() bool   { return f != nil }
func (f *FakeSink) Events() []Event { return slices.Clone(f.events) }
func (f *FakeSink) Send(ctx context.Context, event Event) (Receipt, error) {
	if ctx.Err() != nil {
		return Receipt{}, ErrDelivery
	}
	if len(f.events) >= f.limit {
		return Receipt{}, ErrBackpressure
	}
	f.events = append(f.events, event)
	if f.Fail {
		return Receipt{}, ErrDelivery
	}
	if f.WrongReceipt {
		return Receipt{ID: ID{1}}, nil
	}
	return Receipt{ID: event.ID}, nil
}
