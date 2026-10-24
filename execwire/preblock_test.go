package execwire

import (
	"reflect"
	"testing"
)

func TestPreBlockSystemJournalOrderAndAttribution(t *testing.T) {
	_, _, r := fixture()
	// Numeric value fixes the additive wire contract before its implementation.
	pre := Step{Cause: Cause(4), TxIndex: -1, Accounts: r.Steps[0].Accounts}
	r.Steps = append([]Step{pre}, r.Steps...)
	for i := range r.Steps {
		r.Steps[i].Ordinal = uint32(i)
	}
	p, err := MarshalResult(r)
	if err != nil {
		t.Fatalf("pre-block system transitions must be accepted before transactions: %v", err)
	}
	got, err := UnmarshalResult(p)
	if err != nil || !reflect.DeepEqual(got, r) {
		t.Fatalf("pre-block round trip: %v", err)
	}
	for name, mutate := range map[string]func(*Result){
		"after transaction": func(r *Result) { r.Steps[0], r.Steps[1] = r.Steps[1], r.Steps[0] },
		"duplicate":         func(r *Result) { r.Steps = append([]Step{r.Steps[0]}, r.Steps...) },
		"transaction index": func(r *Result) { r.Steps[0].TxIndex = 0 },
		"transaction hash":  func(r *Result) { r.Steps[0].TxHash = Hash{9} },
	} {
		t.Run(name, func(t *testing.T) {
			bad, err := UnmarshalResult(p)
			if err != nil {
				t.Fatal(err)
			}
			mutate(&bad)
			for i := range bad.Steps {
				bad.Steps[i].Ordinal = uint32(i)
			}
			if _, err := MarshalResult(bad); err == nil {
				t.Fatal("accepted invalid pre-block attribution/order")
			}
		})
	}
	r.TxCount = 0
	r.Steps = []Step{r.Steps[0], {Ordinal: 1, Cause: Finalize, TxIndex: -1}}
	if _, err := MarshalResult(r); err != nil {
		t.Fatalf("empty block still has system transitions: %v", err)
	}
}
