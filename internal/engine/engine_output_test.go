// SPDX-License-Identifier: Apache-2.0

package engine

import "testing"

func TestPackedStateCountGuard(t *testing.T) {
	requirePackableStateCount(int(hasOutputBit) - 1)

	defer func() {
		if recover() == nil {
			t.Fatal("requirePackableStateCount did not panic at the packing limit")
		}
	}()
	requirePackableStateCount(int(hasOutputBit))
}
