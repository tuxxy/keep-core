package tbtc

import "github.com/keep-network/keep-core/pkg/net"

// Batch-6 test exports (tuxxy/keep-core#26 and the FROST leader rule). They
// expose base coordination constants and decode base coordination messages
// for the tests' network recorder. They add no behaviour.

// B6CoordinationFrequencyBlocks is base's coordination window frequency.
const B6CoordinationFrequencyBlocks = uint64(coordinationFrequencyBlocks)

// B6CoordinationActivePhaseBlocks is base's active phase length.
const B6CoordinationActivePhaseBlocks = uint64(coordinationActivePhaseDurationBlocks)

// B6CoordinationSafeBlockShift is base's seed block shift.
const B6CoordinationSafeBlockShift = uint64(coordinationSafeBlockShift)

// B6CoordinationMessage decodes a base coordination message: its window, its
// wallet's 20-byte key and its proposal's action. ok is false for any other
// message.
func B6CoordinationMessage(m net.TaggedMarshaler) (window uint64, wallet [20]byte, action string, ok bool) {
	message, ok := m.(*coordinationMessage)
	if !ok {
		return 0, [20]byte{}, "", false
	}
	action = "unknown"
	func() {
		defer func() { _ = recover() }()
		action = message.proposal.ActionType().String()
	}()
	return message.coordinationBlock, message.walletPublicKeyHash, action, true
}
