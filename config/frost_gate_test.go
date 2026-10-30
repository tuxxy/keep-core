package config

// Refs tuxxy/keep-core#42.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/spf13/viper"
)

// Invariant (KC-SF-V3 line 362, SC-01; D5: no production FROST wiring): a
// configuration that enables FROST is refused by the FROST node gate itself,
// before Ethereum credentials, contracts, peers or Electrum are resolved. The
// refusal is the gate's own error, whatever its wording; with FROST off the
// gate has no effect. The configuration here has no Ethereum key file, so a
// later refusal would be a different error.
func TestFrostEnabledRefusedByTheNodeGate(t *testing.T) {
	gate := frost.Config{Enabled: true}.ValidateNode()
	if gate == nil {
		t.Fatal("#42: the FROST node gate accepts an enabled configuration")
	}
	viper.Reset()
	defer viper.Reset()
	path := filepath.Join(t.TempDir(), "frost.toml")
	if err := os.WriteFile(path, []byte("[Frost]\nEnabled = true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var c Config
	err := c.ReadConfig(path, nil, StartCmdCategories...)
	if err == nil || err.Error() != gate.Error() {
		t.Fatalf("#42: an enabled FROST configuration was not refused by the node gate first: %v", err)
	}
	if err = (frost.Config{}).ValidateNode(); err != nil {
		t.Fatalf("#42: the FROST node gate refuses an inactive configuration: %v", err)
	}
}
