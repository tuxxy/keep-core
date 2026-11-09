// Package remediationb17a holds the keep-core tests of remediation batch 17a
// (the K-09 P3 runner and evidence findings). Unit tests: no chain, no
// external program, no process; each case runs in well under a second.
package remediationb17a

import (
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/keep-network/keep-core/cmd"
	"github.com/keep-network/keep-core/config"
)

// hookName matches field names of fault-injection or pause hooks: the closed
// K-09's node.Config.VerificationPause and signing.Config.AfterGrant (review
// round 6, K09-43), and the verification boundary callbacks that the FROST
// packages keep on their own types (never in configuration).
var hookName = regexp.MustCompile(`(?i)pause|fault|inject|hook|boundary|grant|crash`)

// allowedInterfaces are the interface-typed configuration fields that are
// production seams, not hooks. store.Config.Fence is the FROST store's fence
// storage (KC-SF-V3 4.3), present at the batch 17a base 1e3e6ea6.
var allowedInterfaces = map[string]bool{
	"Config.Tbtc.Frost.Store.Fence": true,
}

// configHooks walks a configuration type and returns every field through
// which a caller could inject behaviour: func, chan and unsafe pointer
// fields anywhere, interface fields outside allowedInterfaces, and fields
// named like a hook.
func configHooks(root string, typ reflect.Type) []string {
	var found []string
	seen := map[reflect.Type]bool{}
	var walk func(path string, ty reflect.Type)
	walk = func(path string, ty reflect.Type) {
		for {
			switch ty.Kind() {
			case reflect.Ptr, reflect.Slice, reflect.Array:
				ty = ty.Elem()
				continue
			case reflect.Map:
				walk(path+"[key]", ty.Key())
				ty = ty.Elem()
				continue
			}
			break
		}
		switch ty.Kind() {
		case reflect.Func, reflect.Chan, reflect.UnsafePointer:
			found = append(found, path+" ("+ty.Kind().String()+" "+ty.String()+")")
		case reflect.Interface:
			if !allowedInterfaces[path] {
				found = append(found, path+" (interface "+ty.String()+")")
			}
		case reflect.Struct:
			if seen[ty] {
				return
			}
			seen[ty] = true
			for i := 0; i < ty.NumField(); i++ {
				f := ty.Field(i)
				p := path + "." + f.Name
				if hookName.MatchString(f.Name) {
					found = append(found, p+" (named like a fault/pause hook)")
				}
				walk(p, f.Type)
			}
			delete(seen, ty)
		}
	}
	walk(root, typ)
	sort.Strings(found)
	return found
}

// TestB17aNoFaultHooksInProductionConfiguration (guard, unit).
//
// Invariant: no fault-injection or pause hook is reachable from keep-client's
// configuration. KC-SF-V3 K-09 says "Implement fault injection as test
// adapters only"; the closed K-09 put VerificationPause and AfterGrant into
// node and signing configuration (review round 6). The configuration root is
// config.Config, which holds tbtc.Config and, through it, the FROST
// configuration (tbtc.FrostConfig: registry, worker, store).
//
// This is a remediation constraint, not a defect at the base: it passes at
// 1e3e6ea6 and fails when a hook is added (controls in TESTS-BATCH17A.md).
//
// Refs tuxxy/keep-core#58
func TestB17aNoFaultHooksInProductionConfiguration(t *testing.T) {
	hooks := configHooks("Config", reflect.TypeOf(config.Config{}))
	if len(hooks) != 0 {
		t.Fatalf("K09-43: keep-client configuration carries fault-injection or pause hooks (test adapters only): %s",
			strings.Join(hooks, "; "))
	}
}

// baseCommands are keep-client's top-level commands at base keep-core
// (origin/main cmd/cmd.go): FROST adds none (D5: keep-client start refuses a
// FROST configuration until a testnet decision).
var baseCommands = []string{"ethereum", "maintainer", "maintainer-cli", "ping", "start"}

// TestB17aNoLocalNodeCommand (guard, unit).
//
// Invariant: keep-client registers no FROST local-node or fault command. The
// closed K-09 registered FrostLocalNodeCommand in the root command (K09-43,
// cmd/cmd.go:36), and that command exited 0 when its time bound expired
// (K09-38). The new K-09 starts nodes through the tbtc.Initialize seam in a
// test process (D5 revised), so neither may return to the production binary.
// Production FROST maintainer commands (maintainer-cli frost-redemptions)
// are not hooks and are not matched.
//
// Passes at 1e3e6ea6 by design (constraint); fails when such a command is
// registered.
//
// Refs tuxxy/keep-core#58
// Refs tuxxy/keep-core#53
func TestB17aNoLocalNodeCommand(t *testing.T) {
	root := cmd.Initialize("b17a", "b17a")
	var top []string
	for _, c := range root.Commands() {
		if c.Name() == "help" || c.Name() == "completion" {
			continue
		}
		top = append(top, c.Name())
	}
	sort.Strings(top)
	if !reflect.DeepEqual(top, baseCommands) {
		t.Errorf("K09-43: keep-client's top-level commands are %v; base keep-core has %v (no FROST local-node command in the production binary)", top, baseCommands)
	}
	word := regexp.MustCompile(`(?i)\blocal-?node\b|\bfault\b|\binject`)
	var visit func(c *cobra.Command, path string)
	visit = func(c *cobra.Command, path string) {
		for _, sub := range c.Commands() {
			p := path + " " + sub.Name()
			if word.MatchString(sub.Name()) || word.MatchString(sub.Use) {
				t.Errorf("K09-43: keep-client registers the command %q (FROST local-node and fault commands are test adapters only)", strings.TrimSpace(p))
			}
			visit(sub, p)
		}
	}
	visit(root, "")
}
