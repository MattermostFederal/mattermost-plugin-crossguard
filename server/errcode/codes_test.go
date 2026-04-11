package errcode

import "testing"

// TestCodesUnique asserts that every code declared in AllCodes is distinct.
// Two log call sites must never share an identifier.
func TestCodesUnique(t *testing.T) {
	seen := make(map[int]int, len(AllCodes))
	for i, code := range AllCodes {
		if prev, ok := seen[code]; ok {
			t.Errorf("duplicate code %d at AllCodes[%d] (also at index %d)", code, i, prev)
		}
		seen[code] = i
	}
}
