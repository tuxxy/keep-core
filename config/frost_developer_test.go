package config

// Chaosnet M2 acceptance (tux, 2026-11-13): keep-core starts as a FROST node
// from TOML on the --developer network only. Acceptance items 1-4 of the M2
// brief. Mainnet and testnet keep the D5 refusal (DECISIONS.md D5 revised,
// 2026-11-03) unchanged: same error, same point, before chain configuration
// or credentials. The frozen config/frost_tbtc_gate_test.go and
// config/frost_gate_test.go stay as they are.
//
// Fixture and pinned settings: frost_developer_fixture_test.go.

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/keep-network/keep-core/pkg/frost"
)

// Acceptance 1. Invariant: on the developer network a valid [tbtc.frost]
// section (worker path and SHA-256, keystore, key file, fence file) with the
// FROST registry from [developer] loads, and the node's FROST configuration
// holds exactly those values, the store key read from the key file. The key
// file may or may not end with one newline.
func TestDeveloperFrostSectionConfiguresTheNode(t *testing.T) {
	for _, keyFile := range []struct{ name, content string }{
		{"newline", hex.EncodeToString(devStoreKey[:]) + "\n"},
		{"no newline", hex.EncodeToString(devStoreKey[:])},
	} {
		t.Run(keyFile.name, func(t *testing.T) {
			d := newDevFrost(t)
			if err := os.WriteFile(d.keyFile, []byte(keyFile.content), 0600); err != nil {
				t.Fatal(err)
			}
			c, err := readDev(t, d, d.toml(true, nil), "--developer")
			if err != nil {
				t.Fatalf("M2-1: a valid developer [tbtc.frost] configuration was refused: %v", err)
			}
			f := c.Tbtc.Frost
			if !f.Configured() {
				t.Fatal("M2-1: the developer FROST configuration did not configure the node")
			}
			if f.Registry != common.HexToAddress(devRegistry) {
				t.Errorf("M2-1: FROST registry is %x, want [developer] FrostWalletRegistryAddress %s", f.Registry, devRegistry)
			}
			if f.Worker.Path != d.worker {
				t.Errorf("M2-1: worker path is %q, want %q", f.Worker.Path, d.worker)
			}
			if f.Worker.SHA256 != d.workerSH {
				t.Errorf("M2-1: worker SHA-256 is %x, want %x", f.Worker.SHA256, d.workerSH)
			}
			if f.Store.Keystore != d.keystore {
				t.Errorf("M2-1: keystore is %q, want %q", f.Store.Keystore, d.keystore)
			}
			if f.Store.Key != devStoreKey {
				t.Error("M2-1: the store key is not the 32 bytes encoded in the key file (values withheld)")
			}
			if f.Store.Fence == nil {
				t.Error("M2-1: no fence was configured from FenceFile")
			}
		})
	}
}

// Acceptance 2. Invariant (D5 unchanged off developer): the same valid file,
// on mainnet (no network flag, or --mainnet) and on --testnet, is refused
// with exactly the FROST node gate's error, before any other check: also
// when its key file is missing or a FROST path is relative, so no FROST
// setting is read before the gate. Nothing reaches the configuration.
// PASSES TODAY by design: it guards against widening the developer opening.
func TestFrostSectionStillRefusedOffDeveloper(t *testing.T) {
	gate := frost.Config{Enabled: true}.ValidateNode()
	if gate == nil {
		t.Fatal("M2-2: the FROST node gate accepts an enabled configuration")
	}
	for _, network := range []struct {
		name string
		args []string
	}{
		{"mainnet by default", nil},
		{"mainnet", []string{"--mainnet"}},
		{"testnet", []string{"--testnet"}},
	} {
		for _, variant := range []string{"valid", "missing key file", "relative keystore"} {
			t.Run(network.name+"/"+variant, func(t *testing.T) {
				d := newDevFrost(t)
				content := d.toml(true, nil)
				switch variant {
				case "missing key file":
					if err := os.Remove(d.keyFile); err != nil {
						t.Fatal(err)
					}
				case "relative keystore":
					content = d.toml(true, map[string]string{"Keystore": `"frost-keystore"`})
				}
				c, err := readDev(t, d, content, network.args...)
				if err == nil || err.Error() != gate.Error() {
					t.Fatalf("M2-2: %s FROST configuration was not refused by the D5 gate first: %v", network.name, err)
				}
				if !reflect.ValueOf(c.Tbtc.Frost).IsZero() {
					t.Fatalf("M2-2: a refused %s FROST configuration reached the node configuration", network.name)
				}
			})
		}
	}
}

// Acceptance 3. Invariant (rule 5): on the developer network a configuration
// without FROST settings loads exactly as before: FROST unconfigured, the
// node gate open. PASSES TODAY by design (regression guard).
func TestDeveloperWithoutFrostUnchanged(t *testing.T) {
	d := newDevFrost(t)
	c, err := readDev(t, d, d.toml(false, nil), "--developer")
	if err != nil {
		t.Fatalf("M2-3: a developer configuration without FROST was refused: %v", err)
	}
	if !reflect.ValueOf(c.Tbtc.Frost).IsZero() {
		t.Fatal("M2-3: FROST is configured without any FROST setting")
	}
	if err := c.ValidateFrostNode(); err != nil {
		t.Fatalf("M2-3: the FROST node gate refuses a configuration without FROST: %v", err)
	}
}

// Acceptance 4. Invariant: on the developer network every malformed FROST
// setting is refused at configuration with an error that names the setting,
// which is not the D5 gate's error, and nothing reaches the node
// configuration. Cases: each setting missing, each path relative, the
// worker SHA-256 not 64 hex digits or zero, the registry zero, the key file
// missing, unreadable or a directory, and its contents not exactly one
// encoding of a nonzero 32-byte key.
func TestDeveloperFrostBadSettingsRefused(t *testing.T) {
	gate := frost.Config{Enabled: true}.ValidateNode()
	type bad struct {
		name    string
		setting string
		prepare func(t *testing.T, d *devFrost) string
	}
	var cases []bad
	for _, s := range []string{"FrostWalletRegistryAddress", "WorkerPath", "WorkerSHA256", "Keystore", "KeyFile", "FenceFile"} {
		s := s
		cases = append(cases, bad{"missing " + s, s, func(_ *testing.T, d *devFrost) string { return d.toml(true, nil, s) }})
	}
	for s, relative := range map[string]string{
		"WorkerPath": `"bin/snowfall-worker"`, "Keystore": `"frost-keystore"`,
		"KeyFile": `"frost-store.key"`, "FenceFile": `"frost.fence"`,
	} {
		s, relative := s, relative
		cases = append(cases, bad{"relative " + s, s, func(_ *testing.T, d *devFrost) string {
			return d.toml(true, map[string]string{s: relative})
		}})
	}
	digest := strings.Repeat("ab", 32)
	for name, value := range map[string]string{
		"63 digits": digest[:63], "65 digits": digest + "a", "non-hex": strings.Repeat("zz", 32),
		"0x prefix": "0x" + digest, "zero": strings.Repeat("0", 64), "empty": "",
	} {
		value := value
		cases = append(cases, bad{"WorkerSHA256 " + name, "WorkerSHA256", func(_ *testing.T, d *devFrost) string {
			return d.toml(true, map[string]string{"WorkerSHA256": fmt.Sprintf("%q", value)})
		}})
	}
	cases = append(cases, bad{"zero registry", "FrostWalletRegistryAddress", func(_ *testing.T, d *devFrost) string {
		return d.toml(true, map[string]string{"FrostWalletRegistryAddress": `"0x0000000000000000000000000000000000000000"`})
	}})
	cases = append(cases,
		bad{"key file missing", "KeyFile", func(t *testing.T, d *devFrost) string {
			if err := os.Remove(d.keyFile); err != nil {
				t.Fatal(err)
			}
			return d.toml(true, nil)
		}},
		bad{"key file unreadable", "KeyFile", func(t *testing.T, d *devFrost) string {
			if err := os.Chmod(d.keyFile, 0); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(d.keyFile, 0600) })
			return d.toml(true, nil)
		}},
		bad{"key file is a directory", "KeyFile", func(t *testing.T, d *devFrost) string {
			if err := os.Remove(d.keyFile); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(d.keyFile, 0700); err != nil {
				t.Fatal(err)
			}
			return d.toml(true, nil)
		}},
	)
	keyHex := hex.EncodeToString(devStoreKey[:])
	for name, content := range badKeyContents(keyHex) {
		content := content
		cases = append(cases, bad{"key file " + name, "KeyFile", func(t *testing.T, d *devFrost) string {
			if err := os.WriteFile(d.keyFile, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			return d.toml(true, nil)
		}})
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			d := newDevFrost(t)
			content := tc.prepare(t, d)
			contents, _ := os.ReadFile(d.keyFile)
			stop := captureLogs(t)
			c, err := readDev(t, d, content, "--developer")
			logs := stop()
			if err == nil {
				t.Fatalf("M2-4: %s was accepted on the developer network", tc.name)
			}
			if gate != nil && err.Error() == gate.Error() {
				t.Fatalf("M2-4: %s was refused by the D5 gate, not by FROST settings validation", tc.name)
			}
			if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.setting)) {
				t.Errorf("M2-4: the refusal of %s does not name the setting %s: %v", tc.name, tc.setting, err)
			}
			if !reflect.ValueOf(c.Tbtc.Frost).IsZero() {
				t.Errorf("M2-4: a refused FROST setting (%s) reached the node configuration", tc.name)
			}
			for _, secret := range [][]byte{devStoreKey[:], bytes.TrimSpace(contents)} {
				assertNoLeak(t, "M2-4: the refusal", err.Error(), secret)
				assertNoLeak(t, "M2-4: the log output", logs, secret)
			}
		})
	}
}

// badKeyContents are key-file contents that are not exactly 64 hex digits
// (optionally one trailing newline) of a nonzero key. All are TEST values.
func badKeyContents(keyHex string) map[string]string {
	return map[string]string{
		"empty":             "",
		"63 digits":         keyHex[:63],
		"65 digits":         keyHex + "a",
		"non-hex":           strings.Repeat("zq", 32),
		"0x prefix":         "0x" + keyHex,
		"raw 32 bytes":      string(devStoreKey[:]),
		"two lines":         keyHex + "\n" + keyHex + "\n",
		"zero key":          strings.Repeat("0", 64),
		"inner space":       keyHex[:32] + " " + keyHex[32:],
		"base64 of the key": base64.StdEncoding.EncodeToString(devStoreKey[:]),
	}
}

// Acceptance 4 (secrecy). Invariant: loading a valid developer FROST
// configuration logs nothing of the store key, and no rendering of the
// loaded configuration (%v, %+v, %#v, %s of the FROST config, its store
// config, the tbtc config and the whole client config) contains it.
func TestDeveloperFrostKeyNeverRendered(t *testing.T) {
	d := newDevFrost(t)
	stop := captureLogs(t)
	c, err := readDev(t, d, d.toml(true, nil), "--developer")
	logs := stop()
	if err != nil {
		t.Fatalf("M2-4: a valid developer [tbtc.frost] configuration was refused: %v", err)
	}
	assertNoLeak(t, "M2-4: the log output of loading", logs, devStoreKey[:])
	if c.Tbtc.Frost.Store.Key != devStoreKey {
		t.Fatal("M2-4: the store key was not loaded (values withheld)")
	}
	for name, value := range map[string]interface{}{
		"tbtc.FrostConfig": c.Tbtc.Frost, "store.Config": c.Tbtc.Frost.Store,
		"tbtc.Config": c.Tbtc, "config.Config": *c,
	} {
		for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
			assertNoLeak(t, fmt.Sprintf("M2-4: %s rendered with %s", name, verb), fmt.Sprintf(verb, value), devStoreKey[:])
		}
	}
}
