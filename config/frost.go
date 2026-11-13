package config

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/hashicorp/go-multierror"
	"github.com/spf13/viper"

	commonEthereum "github.com/keep-network/keep-common/pkg/chain/ethereum"
	chainEthereum "github.com/keep-network/keep-core/pkg/chain/ethereum"
	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/store"
	"github.com/keep-network/keep-core/pkg/tbtc"
)

// frostSection is the node's FROST settings section. Its presence is the
// FROST activation; the FROST registry address alone does not activate FROST.
const frostSection = "tbtc.frost"

// ValidateFrostNode is the FROST node gate over every FROST setting of the
// client configuration: the [frost] switch and the node's FROST settings
// ([tbtc.frost], tbtc.Config.Frost). The [frost] switch is refused on every
// network. The node's FROST settings are allowed on the developer network
// only; until a testnet decision every other network refuses them (D5).
func (c *Config) ValidateFrostNode() error {
	return c.validateFrostNode(c.Ethereum.Network)
}

// validateFrostNode is the gate with the client network resolved from the
// flags; the configuration's own network must be the developer network too.
func (c *Config) validateFrostNode(clientNetwork commonEthereum.Network) error {
	if err := c.Frost.ValidateNode(); err != nil {
		return err
	}
	if viper.IsSet(frostSection) || c.Tbtc.Frost.Configured() {
		if clientNetwork != commonEthereum.Developer ||
			c.Ethereum.Network != commonEthereum.Developer {
			return frost.Config{Enabled: true}.ValidateNode()
		}
	}
	return nil
}

// readFrostNode fills the node's FROST configuration from [tbtc.frost] and
// developer.frostWalletRegistryAddress. It runs after the FROST node gate, so
// only on the developer network. Without [tbtc.frost] FROST stays
// unconfigured. On any error the node's FROST configuration is left as it
// was.
func (c *Config) readFrostNode() error {
	if !viper.IsSet(frostSection) {
		return nil
	}

	var result *multierror.Error
	var fc tbtc.FrostConfig

	registryKey := GetDeveloperContractAddressKey(
		chainEthereum.FrostWalletRegistryContractName,
	)
	registry, err := c.Ethereum.ContractAddress(
		chainEthereum.FrostWalletRegistryContractName,
	)
	switch {
	case err != nil:
		result = multierror.Append(result, fmt.Errorf(
			"invalid value for %s: [%w]", registryKey, err,
		))
	case registry == ([20]byte{}):
		result = multierror.Append(result, fmt.Errorf(
			"invalid value for %s: zero address", registryKey,
		))
	default:
		fc.Registry = registry
	}

	path := func(name string) string {
		key := frostSection + "." + name
		value := viper.GetString(key)
		if value == "" {
			result = multierror.Append(result, fmt.Errorf(
				"missing value for %s; see tbtc.frost section in configuration",
				key,
			))
			return ""
		}
		if !filepath.IsAbs(value) {
			result = multierror.Append(result, fmt.Errorf(
				"invalid value for %s: must be an absolute path", key,
			))
			return ""
		}
		return value
	}

	fc.Worker.Path = path("WorkerPath")

	workerSHA256Key := frostSection + ".WorkerSHA256"
	if digest, ok := decodeHex32([]byte(viper.GetString(workerSHA256Key))); ok {
		fc.Worker.SHA256 = digest
	} else {
		result = multierror.Append(result, fmt.Errorf(
			"invalid value for %s: must be 64 hexadecimal digits of a "+
				"nonzero SHA-256 digest",
			workerSHA256Key,
		))
	}

	fc.Store.Keystore = path("Keystore")

	if keyFile := path("KeyFile"); keyFile != "" {
		key, err := readFrostKeyFile(keyFile)
		if err != nil {
			result = multierror.Append(result, fmt.Errorf(
				"invalid value for %s.KeyFile: %w", frostSection, err,
			))
		} else {
			fc.Store.Key = key
		}
	}

	if fenceFile := path("FenceFile"); fenceFile != "" {
		fence, err := store.NewDeveloperFence(fenceFile)
		if err != nil {
			result = multierror.Append(result, fmt.Errorf(
				"invalid value for %s.FenceFile: %w", frostSection, err,
			))
		} else {
			fc.Store.Fence = fence
		}
	}

	if err := result.ErrorOrNil(); err != nil {
		clear(fc.Store.Key[:])
		return err
	}

	c.Tbtc.Frost = fc
	return nil
}

// decodeHex32 decodes exactly 64 hexadecimal digits (no 0x prefix) of a
// nonzero 32-byte value.
func decodeHex32(s []byte) (out [32]byte, ok bool) {
	if len(s) != 2*len(out) {
		return out, false
	}
	if _, err := hex.Decode(out[:], s); err != nil {
		clear(out[:])
		return out, false
	}
	if out == ([32]byte{}) {
		return out, false
	}
	return out, true
}

// errFrostKeyFile is the key file's format error. It never includes the
// file's contents.
var errFrostKeyFile = errors.New(
	"the key file must hold exactly 64 hexadecimal digits of a nonzero " +
		"key, optionally followed by one newline",
)

// readFrostKeyFile reads the FROST store key from a key file: exactly 64
// hexadecimal digits, no 0x prefix, at most one trailing newline, nonzero.
// Neither the key nor the file's contents appear in an error.
func readFrostKeyFile(path string) ([32]byte, error) {
	var key [32]byte

	file, err := os.Open(path)
	if err != nil {
		return key, fmt.Errorf("cannot open the key file: [%w]", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return key, fmt.Errorf("cannot read the key file: [%w]", err)
	}
	if !info.Mode().IsRegular() {
		return key, errors.New("the key file is not a regular file")
	}

	// One byte more than the longest valid file detects a longer one.
	buffer := make([]byte, 2*len(key)+2)
	defer clear(buffer)
	n, err := io.ReadFull(file, buffer)
	if err != nil &&
		!errors.Is(err, io.ErrUnexpectedEOF) &&
		!errors.Is(err, io.EOF) {
		return key, errors.New("cannot read the key file")
	}
	contents := bytes.TrimSuffix(buffer[:n], []byte("\n"))

	key, ok := decodeHex32(contents)
	if !ok {
		return [32]byte{}, errFrostKeyFile
	}
	return key, nil
}
