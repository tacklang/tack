package types_test

import (
	"testing"

	"github.com/tacklang/tack/types"
)

// TestSizeOverflowSaturates checks an array whose size does not fit a
// uint64 is reported as overflowing, not wrapped to a small size, however
// deep the overflow is.
func TestSizeOverflowSaturates(t *testing.T) {
	table := types.NewTable()
	inner := table.Array(types.Int64, 1<<40)
	outer := table.Array(inner, 1<<40)
	if s := table.Size(inner); s.Overflow || s.Bytes != 1<<43 || !s.TooLarge() {
		t.Errorf("[2^40]int64: %+v, want 2^43 bytes and too large", s)
	}
	if s := table.Size(outer); !s.Overflow || !s.TooLarge() {
		t.Errorf("[2^40][2^40]int64: %+v, want an overflow", s)
	}
	if s := table.Size(table.Array(outer, 2)); !s.Overflow {
		t.Errorf("[2][2^40][2^40]int64: %+v, want the overflow kept", s)
	}
	limit := table.Array(types.Uint8, 1<<31)
	if s := table.Size(limit); s.TooLarge() || s.Bytes != 1<<31 {
		t.Errorf("[2^31]uint8: %+v, want exactly 2^31 bytes, not too large", s)
	}
	if s := table.Size(table.Array(types.Uint8, 1<<31+1)); !s.TooLarge() {
		t.Errorf("[2^31+1]uint8: %+v, want too large", s)
	}
}
