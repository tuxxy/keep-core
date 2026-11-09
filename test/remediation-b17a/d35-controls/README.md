# Remediation batch 17a D35 controls (#37)

These are patches for `pkg/net/libp2p` `TestB17aListenAddressesBindLoopbackOnly` and `TestB17aEmptyListenAddressesKeepBaseListenSet` (review R4: empty keeps base's every-interface set, checked without a socket), and `pkg/tbtc` `TestB17aWorldNodeNetworkIsLoopbackOnly` (tuxxy/keep-core#37, decision D35 (a)). Every control opens loopback listeners only. A wrong fix that listens on every interface despite the field is not run, because it would open LAN listeners.

```
test/remediation-b17a/d35-controls/run-controls.sh <scratch keep-core checkout> <log dir> [patch ...]
```

| Patch | Implementation | Expected |
| --- | --- | --- |
| `d35-fix.patch` | Optional `libp2p.Config.ListenAddresses []string`. When it is set, the provider listens on exactly those addresses; empty keeps base's every-interface list. | all pass |
| `d35-wrong-port-zero.patch` | The listen IP is honoured, but the port is replaced by 0. | libp2p test: `K09-20: ... listens on /ip4/127.0.0.1/tcp/<other>, not on port <port>` |
| `d35-wrong-field-type.patch` | `ListenAddresses string` (not a list). | both: `K09-20: libp2p.Config has no ... ListenAddresses []string field` |
| `d35-wrong-world-reverted.patch` | The correct fix, with the b6 world's call reverted to the base configuration. | world test: `K09-20: the b6 world calls libp2p.Connect 1 times, 0 of them with the loopback configuration` |

| `d35-wrong-helper-loopback.patch` | the fix, but base's `getListenAddrs` itself returns loopback only | `K09-20: getListenAddrs(3919) = [/ip4/127.0.0.1/tcp/3919]; base listens on every interface address ...` |
| `r-d35-wrong-always-loopback.patch` (reviewer) | the field is honoured, but an empty list falls back to loopback, so production nodes become unreachable | `K09-20: discoverAndListen assigns the listen set addrs ... on a path where no listen address is configured` |

Results: `K-09-restart/TESTS-BATCH17A.md`.
