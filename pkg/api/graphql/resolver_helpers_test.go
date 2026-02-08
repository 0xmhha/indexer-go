package graphql

import (
	"testing"
)

func TestCalculateBlockRangeReverse(t *testing.T) {
	tests := []struct {
		name          string
		latestHeight  uint64
		offset        int
		limit         int
		wantStart     uint64
		wantEnd       uint64
		wantOk        bool
	}{
		{
			name:         "page 1 (offset=0)",
			latestHeight: 100,
			offset:       0,
			limit:        20,
			wantStart:    81,
			wantEnd:      100,
			wantOk:       true,
		},
		{
			name:         "page 2 (offset=20)",
			latestHeight: 100,
			offset:       20,
			limit:        20,
			wantStart:    61,
			wantEnd:      80,
			wantOk:       true,
		},
		{
			name:         "page 3 (offset=40)",
			latestHeight: 100,
			offset:       40,
			limit:        20,
			wantStart:    41,
			wantEnd:      60,
			wantOk:       true,
		},
		{
			name:         "page 4 (offset=60)",
			latestHeight: 100,
			offset:       60,
			limit:        20,
			wantStart:    21,
			wantEnd:      40,
			wantOk:       true,
		},
		{
			name:         "last page partial",
			latestHeight: 100,
			offset:       90,
			limit:        20,
			wantStart:    0,
			wantEnd:      10,
			wantOk:       true,
		},
		{
			name:         "offset exceeds height",
			latestHeight: 50,
			offset:       60,
			limit:        20,
			wantOk:       false,
		},
		{
			name:         "small chain",
			latestHeight: 5,
			offset:       0,
			limit:        20,
			wantStart:    0,
			wantEnd:      5,
			wantOk:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			br, ok := calculateBlockRangeReverse(tt.latestHeight, tt.offset, tt.limit)
			if ok != tt.wantOk {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOk)
			}
			if !ok {
				return
			}
			if br.StartBlock != tt.wantStart {
				t.Errorf("StartBlock = %d, want %d", br.StartBlock, tt.wantStart)
			}
			if br.EndBlock != tt.wantEnd {
				t.Errorf("EndBlock = %d, want %d", br.EndBlock, tt.wantEnd)
			}
		})
	}
}

func TestCalculateBlockRangeForward(t *testing.T) {
	tests := []struct {
		name       string
		numberFrom uint64
		numberTo   uint64
		offset     int
		limit      int
		wantStart  uint64
		wantEnd    uint64
		wantOk     bool
	}{
		{
			name:       "page 1 of filtered range",
			numberFrom: 50,
			numberTo:   150,
			offset:     0,
			limit:      20,
			wantStart:  50,
			wantEnd:    69,
			wantOk:     true,
		},
		{
			name:       "page 2 of filtered range",
			numberFrom: 50,
			numberTo:   150,
			offset:     20,
			limit:      20,
			wantStart:  70,
			wantEnd:    89,
			wantOk:     true,
		},
		{
			name:       "offset exceeds range",
			numberFrom: 50,
			numberTo:   60,
			offset:     20,
			limit:      20,
			wantOk:     false,
		},
		{
			name:       "last page partial",
			numberFrom: 50,
			numberTo:   65,
			offset:     10,
			limit:      20,
			wantStart:  60,
			wantEnd:    65,
			wantOk:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			br, ok := calculateBlockRangeForward(tt.numberFrom, tt.numberTo, tt.offset, tt.limit)
			if ok != tt.wantOk {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOk)
			}
			if !ok {
				return
			}
			if br.StartBlock != tt.wantStart {
				t.Errorf("StartBlock = %d, want %d", br.StartBlock, tt.wantStart)
			}
			if br.EndBlock != tt.wantEnd {
				t.Errorf("EndBlock = %d, want %d", br.EndBlock, tt.wantEnd)
			}
		})
	}
}
