package store

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestSigningRecordsRetainExactIntent(t *testing.T) {
	c := testConfig(t)
	s, v := openTest(t, c)
	ctx := context.Background()
	id := [32]byte{91}
	payload := []byte("public exact transaction intent")
	must(t, v.RetainSigningRecord(ctx, id, payload))
	var steps []string
	s.boundary = func(name string) error { steps = append(steps, name); return nil }
	must(t, v.RetainSigningRecord(ctx, id, payload))
	if strings.Join(steps, ",") != "before-identical-sync,after-identical-sync" {
		t.Fatal("identical intent was not durably acknowledged", steps)
	}
	s.boundary = nil
	if err := v.RetainSigningRecord(ctx, id, []byte("changed intent")); err == nil {
		t.Fatal("conflicting intent accepted")
	}
	release, err := v.Claim(ctx, id, "sign", []byte("worker claim"))
	must(t, err)
	release()
	must(t, s.Close())
	_, v = openTest(t, c)
	got, err := v.ReadSigningRecord(ctx, id)
	must(t, err)
	if !bytes.Equal(got, payload) {
		t.Fatal("reopen changed intent")
	}
	got[0] ^= 1
	again, err := v.ReadSigningRecord(ctx, id)
	must(t, err)
	if !bytes.Equal(again, payload) {
		t.Fatal("caller mutated retained intent")
	}
	if _, err = v.ReadSigningRecord(ctx, [32]byte{92}); !errors.Is(err, ErrMissing) {
		t.Fatal("missing record", err)
	}
}
func TestSigningRecordUncertaintyIsTerminal(t *testing.T) {
	for _, point := range []string{"before-fence", "after-fence", "before-acknowledgement", "before-identical-sync"} {
		t.Run(point, func(t *testing.T) {
			c := testConfig(t)
			s, v := openTest(t, c)
			ctx := context.Background()
			id := [32]byte{91}
			payload := []byte("signed transaction")
			if point == "before-identical-sync" {
				must(t, v.RetainSigningRecord(ctx, id, payload))
			}
			s.boundary = func(name string) error {
				if name == point {
					return errors.New("injected storage uncertainty")
				}
				return nil
			}
			if err := v.RetainSigningRecord(ctx, id, payload); err == nil {
				t.Fatal("uncertain write acknowledged")
			}
			if _, err := v.ReadSigningRecord(ctx, id); !errors.Is(err, ErrQuarantined) {
				t.Fatal("uncertain store stayed usable", err)
			}
			must(t, s.Close())
			recovered, err := Open(ctx, c)
			if point == "before-fence" {
				if !errors.Is(err, ErrQuarantined) {
					if recovered != nil {
						recovered.Close()
					}
					t.Fatal("unfenced journal restored", err)
				}
				return
			}
			must(t, err)
			defer recovered.Close()
			scoped, err := recovered.Scope(testDomain())
			must(t, err)
			got, err := scoped.ReadSigningRecord(ctx, id)
			must(t, err)
			if !bytes.Equal(got, payload) {
				t.Fatal("fenced result not recovered")
			}
		})
	}
}
func TestSigningRecordDomainBinding(t *testing.T) {
	s, v := openTest(t, testConfig(t))
	ctx := context.Background()
	id := [32]byte{91}
	must(t, v.RetainSigningRecord(ctx, id, []byte("intent")))
	domain := testDomain()
	domain.Epoch++
	other, err := s.Scope(domain)
	must(t, err)
	if _, err = other.ReadSigningRecord(ctx, id); err == nil {
		t.Fatal("record decrypted in another domain")
	}
}
