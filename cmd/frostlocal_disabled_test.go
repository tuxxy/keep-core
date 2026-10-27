//go:build !frosttest

package cmd

import "testing"

func TestProductionOmitsFaultNodeCommand(t *testing.T) {
	for _, c := range RootCmd.Commands() {
		if c.Name() == "frost-local-node" {
			t.Fatal("fault-enabled local node registered in production CLI")
		}
	}
}
