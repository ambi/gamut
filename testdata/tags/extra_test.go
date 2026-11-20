//go:build extra

package tags

import "testing"

func TestDouble(t *testing.T) {
	if Double(3) != 6 {
		t.Fatal("Double")
	}
}
