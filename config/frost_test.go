package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestInactiveFrostHasNoEffect(t *testing.T) {
	var c Config
	if err := c.Frost.ValidateNode(); err != nil {
		t.Fatal(err)
	}
}

func TestFrostActivationRejectedBeforeCredentials(t *testing.T) {
	viper.Reset()
	defer viper.Reset()
	path := filepath.Join(t.TempDir(), "frost.toml")
	if err := os.WriteFile(path, []byte("[Frost]\nEnabled = true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var c Config
	err := c.ReadConfig(path, nil, StartCmdCategories...)
	if err == nil || !strings.Contains(err.Error(), "only the local K-01 harness") {
		t.Fatalf("activation did not fail before Ethereum validation: %v", err)
	}
}
