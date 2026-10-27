//go:build !frosttest

package node

import (
	"reflect"
	"strings"
	"testing"
)

func TestProductionRejectsFaultConfiguration(t *testing.T) {
	if _, ok := reflect.TypeOf(Config{}).FieldByName("VerificationPause"); ok {
		t.Fatal("production exposes pause field")
	}
	if _, err := ReadConfig(strings.NewReader(`{"VerificationPause":{"Boundary":"after-incoming-retain","Marker":"/tmp/forbidden"}}`)); err == nil {
		t.Fatal("production accepted fault configuration")
	}
}
