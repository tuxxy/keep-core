package frost

import "testing"

func TestInactiveFrostHasNoEffect(t *testing.T) {
	if err := (Config{}).ValidateNode(); err != nil {
		t.Fatal(err)
	}
	if err := (Config{Enabled: true}).ValidateNode(); err == nil {
		t.Fatal("node activation must remain unavailable")
	}
}
