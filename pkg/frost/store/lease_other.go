//go:build !unix

package store

import (
	"errors"
	"os"
)

func lockFile(_ *os.File) error {
	return errors.New("FROST process leases require a qualified Unix filesystem")
}
