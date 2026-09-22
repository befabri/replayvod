package repository

import (
	"math"
	"testing"
)

func TestBatchPage(t *testing.T) {
	for _, tc := range []struct {
		name  string
		after int64
		limit int
		valid bool
	}{
		{"first page", 0, 1, true},
		{"largest page", 42, 1000, true},
		{"last ID", math.MaxInt64, 1, true},
		{"negative cursor", -1, 64, false},
		{"zero limit", 0, 0, false},
		{"negative limit", 0, -1, false},
		{"oversized page", 0, 1001, false},
		{"overflowing SQL limit", 0, math.MaxInt, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page, err := NewBatchPage(tc.after, tc.limit)
			if (err == nil) != tc.valid {
				t.Fatalf("NewBatchPage(%d, %d) = %v; valid = %v", tc.after, tc.limit, err, tc.valid)
			}
			if !tc.valid {
				if page != (BatchPage{}) || page.Validate() == nil {
					t.Fatal("failed construction returned a usable page")
				}
				return
			}
			if page.AfterID() != tc.after || page.Limit() != tc.limit || page.Validate() != nil {
				t.Fatalf("page changed its bounds: %+v", page)
			}
			copy, err := NewBatchPage(tc.after, tc.limit)
			if err != nil || copy != page {
				t.Fatal("identical bounds must be equal values")
			}
		})
	}
}

func TestBatchSize(t *testing.T) {
	for _, limit := range []int{-1, 0, 1, MaxBatchSize, MaxBatchSize + 1, math.MaxInt} {
		size, err := NewBatchSize(limit)
		valid := limit >= 1 && limit <= MaxBatchSize
		if (err == nil) != valid {
			t.Fatalf("NewBatchSize(%d) = %v", limit, err)
		}
		if valid {
			if size.Limit() != limit || size.Validate() != nil {
				t.Fatalf("invalid size: %+v", size)
			}
		} else if size != (BatchSize{}) || size.Validate() == nil {
			t.Fatal("failed construction returned a usable size")
		}
	}
}
