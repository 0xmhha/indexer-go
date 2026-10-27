package nativepreflight

import (
	"context"
	"testing"
)

func TestMissingExplicitInputsFail(t *testing.T) {
	r := Check(context.Background(), Inputs{}, Product{Profile: "arc-native"})
	if r.Status != "FAIL" {
		t.Fatalf("missing explicit file inputs must FAIL, got %s", r.Status)
	}
}
