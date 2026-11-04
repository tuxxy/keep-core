package config

// Refs tuxxy/keep-core#26 (fix review B1), #42.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/spf13/viper"
)

// Invariant (D5 revised): the node's FROST settings ([tbtc.frost],
// tbtc.Config.Frost) are a FROST activation too. A configuration file that
// sets any of them is refused by the FROST node gate itself, before Ethereum
// credentials, contracts, peers or Electrum are resolved, and the setting
// never reaches the node configuration.
func TestTbtcFrostSectionRefusedByTheNodeGate(t *testing.T) {
	gate := frost.Config{Enabled: true}.ValidateNode()
	viper.Reset()
	defer viper.Reset()
	path := filepath.Join(t.TempDir(), "tbtc-frost.toml")
	section := "[tbtc.frost]\nregistry = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20]\n"
	if err := os.WriteFile(path, []byte(section), 0600); err != nil {
		t.Fatal(err)
	}
	var c Config
	err := c.ReadConfig(path, nil, StartCmdCategories...)
	if err == nil || err.Error() != gate.Error() {
		t.Fatalf("B1: a [tbtc.frost] section was not refused by the FROST node gate first: %v", err)
	}
	if c.Tbtc.Frost.Configured() {
		t.Fatal("B1: a [tbtc.frost] section configured the node's FROST settings")
	}
}
