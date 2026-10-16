// Package store implements encrypted, immutable FROST journal snapshots. It is
// inactive until an application supplies an independent, durable fencing service.
package store

import "context"

// Point identifies the exact committed snapshot. Zero is a newly provisioned
// store, never a request to reset an existing store.
type Point struct {
	Sequence uint64
	Digest   [32]byte
}

// Fence must be independently durable: its state MUST NOT be restored or cloned
// with the keystore. Acquire excludes every other process, including a clone on
// another host. It must never silently create a new identity after data loss.
// The lease is non-expiring until Close; do not implement this with a timeout
// that allows an old process to continue signing after a new owner takes over.
// Provisioning and recovery of the service are deployment responsibilities.
type Fence interface {
	Acquire(context.Context, [32]byte) (FenceLease, error)
}

type FenceLease interface {
	Head(context.Context) (Point, error)
	// Advance atomically compares before and durably installs after. An error may
	// mean the write committed; the caller must quarantine and reacquire to learn.
	Advance(context.Context, Point, Point) error
	Close() error
}
