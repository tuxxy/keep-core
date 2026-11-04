package tbtc

// K-09 lifecycle demonstration: base constants the harness derives the
// heartbeat checklist from, independently of the node (k09_lifecycle_test.go).

// K09HeartbeatProbability is base's per-window heartbeat probability.
const K09HeartbeatProbability = coordinationHeartbeatProbability

// K09HeartbeatFailureThreshold is base's number of consecutive heartbeat
// failures before an inactivity claim.
const K09HeartbeatFailureThreshold = heartbeatConsecutiveFailureThreshold
