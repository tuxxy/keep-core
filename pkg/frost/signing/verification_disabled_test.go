//go:build !frosttest

package signing

import (
	"reflect"
	"testing"
)

func TestProductionOmitsGrantFaultHook(t *testing.T) {
	if _, ok := reflect.TypeOf(Config{}).FieldByName("AfterGrant"); ok {
		t.Fatal("production exposes grant fault hook")
	}
}
