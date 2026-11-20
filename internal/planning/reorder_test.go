package planning

import "testing"

// A table-driven test: each case is a row in a slice of anonymous structs,
// and one loop runs them all as named subtests. It is the idiomatic way to
// test pure functions in Go, similar to [Theory] + [InlineData] in xUnit.
func TestReorderQuantity(t *testing.T) {
	tests := []struct {
		name   string
		stock  int
		demand int
		want   int
	}{
		{name: "no demand and no stock", stock: 0, demand: 0, want: 0},
		{name: "empty shelf", stock: 0, demand: 100, want: 150},
		{name: "below target", stock: 120, demand: 100, want: 30},
		{name: "exactly at target", stock: 150, demand: 100, want: 0},
		{name: "overstocked", stock: 500, demand: 100, want: 0},
		{name: "target rounds up", stock: 0, demand: 3, want: 5},
	}

	for _, tt := range tests {
		// t.Run gives each row its own name in the output, and a failure
		// in one row does not stop the others.
		t.Run(tt.name, func(t *testing.T) {
			got := ReorderQuantity(tt.stock, tt.demand)
			if got != tt.want {
				t.Errorf("ReorderQuantity(%d, %d) = %d, want %d",
					tt.stock, tt.demand, got, tt.want)
			}
		})
	}
}
