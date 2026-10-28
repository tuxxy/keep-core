package cmd

import (
	"strings"
	"testing"

	"github.com/keep-network/keep-core/pkg/frost"
)

func TestInactiveFrostHasNoEffect(t *testing.T) {
	if err := (frost.Config{}).ValidateNode(); err != nil {
		t.Fatal(err)
	}
	previous := clientConfig.Frost
	defer func() { clientConfig.Frost = previous }()
	clientConfig.Frost.Enabled = true
	// start must stop at the local gate before trying chain access or keys.
	err := start(StartCommand)
	if err == nil || !strings.Contains(err.Error(), "only the local K-01 harness") {
		t.Fatalf("activation was not refused first: %v", err)
	}
}
