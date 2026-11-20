// Package planning holds the replenishment rules. It is pure logic: no I/O,
// no dependencies, so it is trivial to test and to reason about.
package planning

import "math"

// SafetyFactor scales forecast demand into a stock target, keeping a buffer
// for forecast error. 1.5 means "keep 50% more than the expected demand".
// It is a placeholder policy; a real system would tune it per SKU.
const SafetyFactor = 1.5

// ReorderQuantity returns how many units to order so stock reaches the
// target level for the forecast demand. It never returns a negative number.
func ReorderQuantity(stock, demand int) int {
	// Round the target up: with demand 3, a target of 4.5 units means 5.
	target := int(math.Ceil(float64(demand) * SafetyFactor))

	// max is a built-in since Go 1.21.
	return max(target-stock, 0)
}
