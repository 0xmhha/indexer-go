package storage

import (
	"testing"
)

func TestProposalStatus_String(t *testing.T) {
	tests := []struct {
		status   ProposalStatus
		expected string
	}{
		{ProposalStatusNone, "none"},
		{ProposalStatusVoting, "voting"},
		{ProposalStatusApproved, "approved"},
		{ProposalStatusExecuted, "executed"},
		{ProposalStatusCancelled, "cancelled"},
		{ProposalStatusExpired, "expired"},
		{ProposalStatusFailed, "failed"},
		{ProposalStatusRejected, "rejected"},
		{ProposalStatus(99), "unknown(99)"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := tt.status.String()
			if result != tt.expected {
				t.Errorf("ProposalStatus(%d).String() = %q, want %q", tt.status, result, tt.expected)
			}
		})
	}
}
