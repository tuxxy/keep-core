//! Finite host model of the keep-core K-02 store and fencing protocol.
//!
//! This model is deliberately independent of the Go source. It represents the
//! store protocol in `pkg/frost/store/store.go` at the granularity of the
//! named I/O boundaries: write the temporary file, sync the file, install the
//! journal by hard link, sync the directory, advance the independent fence,
//! acknowledge. Crashes, host crashes, stale restores, clones, fence resets,
//! I/O errors and fence updates with a lost response are environment actions.
//!
//! The transition-to-source map lives in `../TRANSITION-MAP.md`. The scope,
//! bounds and assumptions live in `../SCOPE.md`. Nothing here is a proof of the
//! Go source; it is a bounded check of a separate model, connected to the
//! implementation by trace replay.
//!
//! Profiles. `Profile::Store` is the tranche 1 model (records, locks, claims).
//! `Profile::Dkg` adds the K-03 store-side state: the completed key per domain,
//! the wallet record per domain, the in-memory live DKG marker, the
//! `SaveKey` late-key guard, `LoadKey`, `DKGStatus` and `SaveWallet`. The
//! store profile's state space is unchanged by the additions (new fields stay
//! at their defaults and no new actions are offered), which the re-pin run
//! checks by reproducing the tranche 1 unique-state count.
//!
//! Design rules followed here:
//! - No target invariant is encoded as an action precondition. Guards exist in
//!   the model only where `store.go` has the same guard.
//! - A `Weakening` removes one implementation guard. The unweakened model is
//!   the claim. Each weakening must produce a counterexample for its declared
//!   property; these are model-level negative controls, not Go mutants.
//! - Safety is expressed as `always` properties. Progress is expressed only as
//!   `sometimes` witnesses (reachable success), never as liveness.

use serde::{Deserialize, Serialize};
use stateright::{Checker, Model, Property};
use std::collections::BTreeMap;
use std::time::{Duration, Instant};

pub const PROCS: usize = 2;
pub const IMAGES: usize = 2;
pub const ATTEMPTS: u8 = 2;
pub const DOMAINS: usize = 2;
pub const SLOTS: usize = 2;
pub const VALUES: u8 = 2;
pub const DEFAULT_MAX_COMMITS: u8 = 4;

/// `store.Point`. `uid` stands for the SHA-256 digest; the random seal nonce
/// makes real digests unique per commit, which a unique id models exactly.
/// `uid == 0` with `seq == 0` is the provisioned zero head.
#[derive(Clone, Copy, Debug, Default, Eq, Hash, PartialEq, Ord, PartialOrd, Serialize, Deserialize)]
pub struct Point {
    pub seq: u8,
    pub uid: u8,
}
impl Point {
    pub const ZERO: Point = Point { seq: 0, uid: 0 };
}

/// The logical content of one journal snapshot. `claims` is a bitmask over
/// attempt identities and is global to the storage identity, as in `store.go`
/// (`Claims[hex(id)]`). `dkg[d]` is the per-domain DKG reservation
/// (`DKGs[prefix]`). `slots[0]` is an immutable record slot keyed by record ID;
/// `slots[1]` is a lock slot keyed by `(seat, attempt, kind)`.
/// `dkg.WalletRecord` as the store sees it: lifecycle rank (1 Candidate,
/// 2 RegisteredPendingReady, 3 ReadyUnfunded, 4 Closed), the quarantine flag,
/// the immutable identity (descriptor and Q), and the approval receipt that is
/// immutable once set.
#[derive(Clone, Copy, Debug, Default, Eq, Hash, PartialEq, Serialize, Deserialize)]
pub struct Wallet {
    pub rank: u8,
    pub quarantined: bool,
    pub identity: u8,
    pub approval: Option<u8>,
}

#[derive(Clone, Copy, Debug, Default, Eq, Hash, PartialEq, Serialize, Deserialize)]
pub struct Snapshot {
    pub claims: u8,
    pub dkg: [bool; DOMAINS],
    pub slots: [Option<u8>; SLOTS],
    /// Dkg profile: the completed key per domain. The value is the identity it
    /// carries (descriptor and output key); both values match the request.
    pub keys: [Option<u8>; DOMAINS],
    /// Dkg profile: the wallet record per domain.
    pub wallet: [Option<Wallet>; DOMAINS],
}

/// One installed journal file. `content_durable` is set by the file sync,
/// `name_durable` by the directory sync. A host crash drops names that are not
/// durable and corrupts content that is not durable.
#[derive(Clone, Copy, Debug, Eq, Hash, PartialEq, Serialize, Deserialize)]
pub struct Journal {
    pub point: Point,
    pub snapshot: Snapshot,
    pub content_durable: bool,
    pub name_durable: bool,
    pub corrupt: bool,
}

/// One keystore directory (`<keystore>/snowfall`). Image 1 exists only after
/// `Clone` and is a copy of image 0's durable state at that time.
#[derive(Clone, Debug, Default, Eq, Hash, PartialEq)]
pub struct Image {
    pub exists: bool,
    pub identity: bool,
    pub journals: Vec<Journal>,
    /// The process holding `owner.lock` (flock) on this image.
    pub local_lock: Option<u8>,
}

#[derive(Clone, Copy, Debug, Eq, Hash, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Purpose {
    Sign,
    Dkg,
}

/// Key values written by `SaveKey`: `Some(v)` is a key that matches the
/// retained request and carries identity `v`; `None` is a key whose roster
/// does not match the request (`keyMatchesRequest` fails).
#[derive(Clone, Copy, Debug, Eq, Hash, PartialEq)]
pub enum Op {
    Claim { attempt: u8, purpose: Purpose, domain: u8 },
    Put { slot: u8, value: u8 },
    SaveKey { domain: u8, key: Option<u8> },
    SaveWallet { domain: u8, wallet: Wallet },
}

/// Where an in-progress commit stands. Each stage is the state after the named
/// step; the Go boundary where a replaying subprocess waits in that stage is
/// given in the transition map.
#[derive(Clone, Copy, Debug, Eq, Hash, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Stage {
    BeforeWrite,
    Written,
    FileSynced,
    Installed,
    DirSynced,
    Fenced,
    /// Only under `Weakening::AckBeforeFence`: acknowledged, fence not advanced.
    AckedUnfenced,
}

#[derive(Clone, Copy, Debug, Eq, Hash, PartialEq)]
pub struct Commit {
    pub op: Op,
    pub next: Snapshot,
    pub point: Point,
    pub stage: Stage,
    /// The authority accepted the advance to `point` (with or without a response).
    pub fenced: bool,
}

/// Observable result of the last completed action of a process. This is the
/// vocabulary compared by the Go replay harness.
#[derive(Clone, Copy, Debug, Eq, Hash, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Outcome {
    Opened,
    Closed,
    ClaimOk,
    Durable,
    Identical,
    Conflict,
    Busy,
    Quarantined,
    Claimed,
    DkgPending,
    Crashed,
    /// The fence update committed and its response was lost. The Go store
    /// reports this as `ErrQuarantined`.
    LostResponse,
    // Dkg profile outcomes.
    KeySaved,
    KeyIdentical,
    KeyConflict,
    KeyMismatch,
    DkgLost,
    KeyLoaded,
    Missing,
    StatusKeyStored,
    StatusInProgress,
    StatusSeatsLost,
    WalletSaved,
    WalletIdentical,
    WalletConflict,
    WalletInvalid,
}

/// The outcome of the most recent action, with the acting process and the
/// image it addressed. Only one such record exists per state.
#[derive(Clone, Copy, Debug, Eq, Hash, PartialEq)]
pub struct Last {
    pub proc: u8,
    pub image: u8,
    pub outcome: Outcome,
}

/// Volatile process memory: the `*store.Store` handle. A crash resets it.
#[derive(Clone, Debug, Default, Eq, Hash, PartialEq)]
pub struct Proc {
    pub open: bool,
    pub image: u8,
    /// `Store.poisoned`: every later operation returns `ErrQuarantined`.
    pub poisoned: bool,
    /// The last fence update of this handle committed but returned an error.
    pub uncertain: bool,
    pub point: Point,
    pub state: Snapshot,
    /// `Store.active` for signing claims: live leases that block `Close`.
    pub live_claims: u8,
    /// Ground truth: this incarnation claimed the domain's DKG and has not
    /// released it. Used by the observer only.
    pub holds_dkg: [bool; DOMAINS],
    /// `Store.liveDKGs`: the implementation's in-memory live marker.
    pub live_dkg_marker: [bool; DOMAINS],
    pub commit: Option<Commit>,
}

/// The independent fencing authority (`store.Fence` / `store.FenceLease`).
#[derive(Clone, Debug, Default, Eq, Hash, PartialEq)]
pub struct Fence {
    pub head: Point,
    pub lease: Option<u8>,
    pub reset_used: bool,
}

/// Observer history. Violation flags are monotone and stay false in the
/// unweakened model. Witness flags record that a path reached a situation.
/// `acked_*` are the facts acknowledged to callers so far.
#[derive(Clone, Debug, Default, Eq, Hash, PartialEq)]
pub struct History {
    pub acked_claims: u8,
    pub acked_slots: [Option<u8>; SLOTS],
    pub acked_keys: [Option<u8>; DOMAINS],
    pub acked_wallet: [Option<Wallet>; DOMAINS],
    /// A status report said "seats lost" for the domain (terminal).
    pub seats_lost_reported: [bool; DOMAINS],
    // Dkg profile violation flags (P-K03-*).
    pub late_key: bool,
    pub seats_lost_regressed: bool,
    pub status_mismatch: bool,
    pub load_not_durable: bool,
    pub wallet_regressed: bool,
    pub ready_without_key: bool,
    // Violation flags (P-K02-TERMINAL, -IMMUTABLE, -DURABLE, -UNCERTAIN).
    pub claim_reused: bool,
    pub conflicting_ack: bool,
    pub ack_not_durable: bool,
    pub stale_exposed: bool,
    pub ack_after_uncertain: bool,
    /// Assumption A-05: the authority was reset to zero and the image used to
    /// open has no journal. Safety flags are not raised after this point.
    pub both_lost: bool,
    /// Some open loaded committed state from a journal (witness support).
    pub reopened: bool,
}

#[derive(Clone, Debug, Default, Eq, Hash, PartialEq)]
pub struct State {
    pub procs: [Proc; PROCS],
    pub images: [Image; IMAGES],
    pub fence: Fence,
    pub commits_used: u8,
    pub cloned: bool,
    pub history: History,
    pub last: Option<Last>,
}

#[derive(Clone, Copy, Debug, Eq, Hash, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Step {
    Write,
    FileSync,
    Install,
    DirSync,
    /// Compare-and-advance. Succeeds when the head equals the handle's point;
    /// otherwise it is rejected and the handle is poisoned.
    Fence,
    /// The authority commits the advance but the response is lost.
    FenceLost,
    Ack,
}

/// What a release returns: live ownership of a signing claim, or of the
/// domain's DKG claim.
#[derive(Clone, Copy, Debug, Eq, Hash, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum ReleaseKind {
    Sign,
    Dkg,
}

#[derive(Clone, Copy, Debug, Eq, Hash, PartialEq)]
pub enum Action {
    Open { proc: u8, image: u8 },
    Close { proc: u8 },
    Release { proc: u8, kind: ReleaseKind, domain: u8 },
    /// Dkg profile reads: `LoadKey` and `DKGStatus` for a domain.
    LoadKey { proc: u8, domain: u8 },
    DkgStatus { proc: u8, domain: u8 },
    /// Start an operation. `fail_sync` is only offered when the operation would
    /// be an identical write; it injects an error into the identical resync.
    Begin { proc: u8, op: Op, fail_sync: bool },
    Step { proc: u8, step: Step },
    /// The current commit step returns an I/O error.
    IoError { proc: u8 },
    ProcessCrash { proc: u8 },
    HostCrash,
    FenceReset,
    RestoreStale { image: u8 },
    Clone,
}

/// A removed implementation guard. `None` is the claim.
#[derive(Clone, Copy, Debug, Eq, Hash, PartialEq, Serialize, Deserialize)]
pub enum Weakening {
    None,
    /// The historical defect: a zero head initializes an empty store even when
    /// journals exist (fixed in keep-core `18e63b3`).
    ZeroHeadIgnoresJournals,
    /// Acknowledge and update local state before the fence advances.
    AckBeforeFence,
    /// Never sync the directory after installing the journal.
    NoDirSync,
    /// Treat a rejected or lost fence update as success.
    IgnoreAdvanceError,
    /// Open proceeds when the fence lease is held by another process.
    LocalLockOnly,
    /// Releasing live ownership also forgets the claim.
    ReleaseClearsClaim,
    /// A write to an occupied slot with different contents commits instead of
    /// returning `Conflict` (the compare guard of `Put` is removed).
    PutOverwrites,
    // Dkg profile weakenings.
    /// `SaveKey` does not require the live DKG claim (the `ErrDKGLost` guard
    /// is removed).
    NoLiveOwnerCheck,
    /// Releasing the DKG claim keeps the in-memory live marker.
    ReleaseKeepsLiveDkg,
    /// `DKGStatus` ignores the saved key and reports from the marker alone.
    StatusIgnoresKey,
    /// `SaveWallet` skips the lifecycle checks (rank, closed, ready-to-closed).
    WalletRankUnchecked,
    /// `SaveWallet` accepts ReadyUnfunded without a matching durable key.
    ReadyWithoutKey,
    /// `LoadKey` returns the handle's local key without the fence check.
    LoadKeyIgnoresFence,
}

#[derive(Clone, Copy, Debug, Eq, Hash, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Profile {
    Store,
    Dkg,
}

impl Weakening {
    pub const ALL: [Weakening; 7] = [
        Weakening::ZeroHeadIgnoresJournals,
        Weakening::AckBeforeFence,
        Weakening::NoDirSync,
        Weakening::IgnoreAdvanceError,
        Weakening::LocalLockOnly,
        Weakening::ReleaseClearsClaim,
        Weakening::PutOverwrites,
    ];
    pub const DKG: [Weakening; 6] = [
        Weakening::NoLiveOwnerCheck,
        Weakening::ReleaseKeepsLiveDkg,
        Weakening::StatusIgnoresKey,
        Weakening::WalletRankUnchecked,
        Weakening::ReadyWithoutKey,
        Weakening::LoadKeyIgnoresFence,
    ];
    pub fn for_profile(p: Profile) -> &'static [Weakening] {
        match p {
            Profile::Store => &Self::ALL,
            Profile::Dkg => &Self::DKG,
        }
    }
    /// The properties each weakening is declared to break (scope record §6 and
    /// §11.5). A weakened run stops only when every declared property has a
    /// counterexample.
    pub fn declared_failures(self) -> &'static [&'static str] {
        match self {
            Weakening::None => &[],
            Weakening::ZeroHeadIgnoresJournals => &["terminal_claims"],
            Weakening::AckBeforeFence => &["durable_acks"],
            Weakening::NoDirSync => &["durable_acks"],
            Weakening::IgnoreAdvanceError => &["uncertain_fail_closed"],
            Weakening::LocalLockOnly => &["owner_exclusive"],
            Weakening::ReleaseClearsClaim => &["terminal_claims"],
            Weakening::PutOverwrites => &["immutable_slots"],
            // A late key after a lost report also regresses the terminal status.
            Weakening::NoLiveOwnerCheck => &["no_late_key", "seatslost_terminal"],
            Weakening::ReleaseKeepsLiveDkg => &["no_late_key"],
            Weakening::StatusIgnoresKey => &["status_durable"],
            Weakening::WalletRankUnchecked => &["wallet_monotone"],
            Weakening::ReadyWithoutKey => &["wallet_monotone"],
            Weakening::LoadKeyIgnoresFence => &["reload_only_durable"],
        }
    }
    pub fn declared_failure(self) -> &'static str {
        self.declared_failures().first().copied().unwrap_or("")
    }
}

#[derive(Clone, Copy, Debug)]
pub struct HostModel {
    pub weakening: Weakening,
    pub max_commits: u8,
    pub profile: Profile,
}

pub const SAFETY: [&str; 5] = [
    "owner_exclusive",
    "terminal_claims",
    "immutable_slots",
    "durable_acks",
    "uncertain_fail_closed",
];

/// Dkg profile safety properties (P-K03-NO-LATE-KEY, -SEATSLOST-TERMINAL,
/// -STATUS-DURABLE, -RELOAD-ONLY-DURABLE, -WALLET-MONOTONE).
pub const DKG_SAFETY: [&str; 5] = [
    "no_late_key",
    "seatslost_terminal",
    "status_durable",
    "reload_only_durable",
    "wallet_monotone",
];

pub fn safety_for(p: Profile) -> Vec<&'static str> {
    let mut v: Vec<&'static str> = SAFETY.to_vec();
    if p == Profile::Dkg {
        v.extend(DKG_SAFETY);
    }
    v
}

/// Witness names, the predicate IDs they support, and a short description.
pub const WITNESSES: [(&str, &str, &str); 16] = [
    ("w_fresh_claim", "P-K02-TERMINAL", "a claim on a fresh store was acknowledged"),
    ("w_identical", "P-K02-IMMUTABLE", "an identical write returned Identical"),
    ("w_conflict", "P-K02-IMMUTABLE", "a conflicting write returned Conflict"),
    ("w_claimed", "P-K02-TERMINAL", "a repeated claim was refused with ErrClaimed"),
    ("w_claimed_after_restart", "P-K02-TERMINAL", "a repeated claim was refused after a reopen from a journal"),
    ("w_dkg_pending", "P-K02-TERMINAL", "a second DKG claim in a reserved domain was refused"),
    ("w_busy", "P-K02-OWNER", "a second open was refused with ErrBusy"),
    ("w_clone_busy", "P-K02-OWNER", "an open on a cloned image was refused while the primary owner lived"),
    ("w_reopen", "P-K02-DURABLE", "a reopen loaded committed state from a journal"),
    ("w_progress_after_reopen", "P-K02-DURABLE", "a fresh claim was acknowledged after a reopen"),
    ("w_progress_after_orphan", "P-K02-TERMINAL", "a fresh claim was acknowledged while an orphan journal from an interrupted commit exists"),
    ("w_quarantine_on_open", "P-K02-DURABLE", "an open quarantined"),
    ("w_orphan_quarantine", "P-K02-DURABLE", "a zero head with an unfenced installed journal quarantined"),
    ("w_reset_quarantine", "P-K02-TERMINAL", "a reset authority with existing journals quarantined"),
    ("w_stale_restore_quarantine", "P-K02-DURABLE", "a stale disk restore quarantined"),
    ("w_fence_lost_then_quarantined", "P-K02-UNCERTAIN", "after a lost fence response the handle refused the next operation"),
];

/// Dkg profile witnesses.
pub const DKG_WITNESSES: [(&str, &str, &str); 10] = [
    ("w_key_saved_live", "P-K03-NO-LATE-KEY", "a completed key was saved while the DKG claim was live"),
    ("w_key_identical", "P-K03-RELOAD-ONLY-DURABLE", "an identical key save was acknowledged"),
    ("w_late_key_refused", "P-K03-NO-LATE-KEY", "a key save after release or restart was refused with ErrDKGLost"),
    ("w_key_loaded_after_reopen", "P-K03-RELOAD-ONLY-DURABLE", "a saved key loaded after a reopen from a journal"),
    ("w_status_key_stored_after_reopen", "P-K03-STATUS-DURABLE", "status reported KeyStored after a reopen"),
    ("w_status_seats_lost", "P-K03-SEATSLOST-TERMINAL", "status reported SeatsLost for a claim without a live owner"),
    ("w_status_in_progress", "P-K03-STATUS-DURABLE", "status reported InProgress while the claim was live"),
    ("w_wallet_ready", "P-K03-WALLET-MONOTONE", "a wallet reached ReadyUnfunded with a matching durable key"),
    ("w_wallet_closed_after_pending", "P-K03-WALLET-MONOTONE", "a pending wallet closed unfunded"),
    ("w_wallet_conflict", "P-K03-WALLET-MONOTONE", "a lifecycle or identity conflict was refused"),
];

pub fn witnesses_for(p: Profile) -> Vec<(&'static str, &'static str, &'static str)> {
    let mut v: Vec<_> = WITNESSES.to_vec();
    if p == Profile::Dkg {
        // Not reachable in the reduced dkg profile (no slots, no clone, no restore).
        v.retain(|(n, _, _)| !matches!(*n, "w_identical" | "w_conflict" | "w_clone_busy" | "w_stale_restore_quarantine"));
        v.extend(DKG_WITNESSES);
    }
    v
}

/// Reachable boundary of assumption A-05. Not a safety witness.
pub const BOUNDARY_WITNESS: &str = "w_both_lost";

fn bit(attempt: u8) -> u8 {
    1 << attempt
}

fn outcome_is(s: &State, outcome: Outcome) -> bool {
    s.last.is_some_and(|l| l.outcome == outcome)
}

fn set_last(s: &mut State, p: usize, img: usize, outcome: Outcome) {
    s.last = Some(Last { proc: p as u8, image: img as u8, outcome });
}

/// A journal that is not on the committed chain: an interrupted commit left it
/// installed. Chain journals have distinct sequence numbers up to the head.
pub fn has_orphan(image: &Image, head: Point) -> bool {
    image.journals.iter().any(|j| {
        j.point.seq > head.seq || image.journals.iter().any(|k| k.point.seq == j.point.seq && k.point != j.point)
    })
}

impl HostModel {
    fn fail_open(s: &mut State, p: usize, img: usize) {
        s.images[img].local_lock = None;
        if s.fence.lease == Some(p as u8) {
            s.fence.lease = None;
        }
        set_last(s, p, img, Outcome::Quarantined);
    }

    fn open(&self, s: &mut State, p: usize, img: usize) {
        // store.go: Open takes owner.lock (flock) first.
        if s.images[img].local_lock.is_some() {
            set_last(s, p, img, Outcome::Busy);
            return;
        }
        // store.go: then Fence.Acquire; ErrBusy releases the local lock.
        let lease_busy = s.fence.lease.is_some();
        if lease_busy && self.weakening != Weakening::LocalLockOnly {
            set_last(s, p, img, Outcome::Busy);
            return;
        }
        s.images[img].local_lock = Some(p as u8);
        if !lease_busy {
            s.fence.lease = Some(p as u8);
        }
        // store.go: bindIdentity. Creating identity requires no journal.
        if !s.images[img].identity {
            if !s.images[img].journals.is_empty() {
                Self::fail_open(s, p, img);
                return;
            }
            s.images[img].identity = true;
        }
        let head = s.fence.head;
        if head.seq == 0 {
            if head.uid != 0 {
                Self::fail_open(s, p, img);
                return;
            }
            let journals_present = !s.images[img].journals.is_empty();
            if journals_present && self.weakening != Weakening::ZeroHeadIgnoresJournals {
                Self::fail_open(s, p, img);
                return;
            }
            let facts = s.history.acked_claims != 0
                || s.history.acked_slots.iter().any(|x| x.is_some())
                || s.history.acked_keys.iter().any(|x| x.is_some())
                || s.history.acked_wallet.iter().any(|x| x.is_some());
            if facts && !journals_present {
                s.history.both_lost = true;
            } else if facts && !s.history.both_lost {
                // Only reachable under ZeroHeadIgnoresJournals: an empty state
                // is exposed although the disk still holds the committed facts.
                s.history.stale_exposed = true;
            }
            s.procs[p] = Proc { open: true, image: img as u8, ..Proc::default() };
            set_last(s, p, img, Outcome::Opened);
            return;
        }
        match s.images[img].journals.iter().find(|j| j.point == head) {
            Some(j) if !j.corrupt => {
                let snap = j.snapshot;
                if !s.history.both_lost {
                    if s.history.acked_claims & !snap.claims != 0 {
                        s.history.stale_exposed = true;
                    }
                    for slot in 0..SLOTS {
                        if let Some(v) = s.history.acked_slots[slot] {
                            if snap.slots[slot] != Some(v) {
                                s.history.stale_exposed = true;
                            }
                        }
                    }
                    for d in 0..DOMAINS {
                        if let Some(v) = s.history.acked_keys[d] {
                            if snap.keys[d] != Some(v) {
                                s.history.stale_exposed = true;
                            }
                        }
                        if let Some(w) = s.history.acked_wallet[d] {
                            // The wallet record is monotone, and a commit that the
                            // authority accepted but that was never acknowledged
                            // (crash between fence and ack) legitimately advances
                            // it. A reopen must therefore preserve the acknowledged
                            // facts, not reproduce the acknowledged record exactly.
                            match snap.wallet[d] {
                                Some(now)
                                    if now.identity == w.identity
                                        && now.rank >= w.rank
                                        && (!w.quarantined || now.quarantined)
                                        && (w.approval.is_none() || now.approval == w.approval) => {}
                                _ => s.history.stale_exposed = true,
                            }
                        }
                    }
                }
                s.history.reopened = true;
                s.procs[p] = Proc { open: true, image: img as u8, point: head, state: snap, ..Proc::default() };
                set_last(s, p, img, Outcome::Opened);
            }
            _ => Self::fail_open(s, p, img),
        }
    }

    fn poison(s: &mut State, p: usize) {
        s.procs[p].poisoned = true;
        s.procs[p].commit = None;
        let img = s.procs[p].image as usize;
        set_last(s, p, img, Outcome::Quarantined);
    }

    /// Returns false when the action is blocked by the commit budget.
    fn begin(&self, s: &mut State, p: usize, op: Op, fail_sync: bool) -> bool {
        // store.go: check() — closed/poisoned, then fence head must equal the
        // handle's point.
        let img = s.procs[p].image as usize;
        if s.procs[p].poisoned {
            set_last(s, p, img, Outcome::Quarantined);
            return true;
        }
        if s.fence.head != s.procs[p].point {
            Self::poison(s, p);
            return true;
        }
        let st = s.procs[p].state;
        match op {
            Op::Claim { attempt, purpose, domain } => {
                if st.claims & bit(attempt) != 0 {
                    set_last(s, p, img, Outcome::Claimed);
                    return true;
                }
                if purpose == Purpose::Dkg && st.dkg[domain as usize] {
                    set_last(s, p, img, Outcome::DkgPending);
                    return true;
                }
                let mut next = st;
                next.claims |= bit(attempt);
                if purpose == Purpose::Dkg {
                    next.dkg[domain as usize] = true;
                }
                self.start_commit(s, p, op, next)
            }
            Op::SaveKey { domain, key } => self.save_key(s, p, domain as usize, key),
            Op::SaveWallet { domain, wallet } => self.save_wallet(s, p, domain as usize, wallet),
            Op::Put { slot, value } => match st.slots[slot as usize] {
                Some(w) if w == value => {
                    // store.go: syncCurrent re-reads and re-syncs the committed
                    // snapshot before returning Identical.
                    if fail_sync {
                        Self::poison(s, p);
                        return true;
                    }
                    let point = s.procs[p].point;
                    if point.seq > 0 {
                        match s.images[img].journals.iter_mut().find(|j| j.point == point) {
                            Some(j) if !j.corrupt => {
                                j.content_durable = true;
                                j.name_durable = true;
                            }
                            _ => {
                                Self::poison(s, p);
                                return true;
                            }
                        }
                    }
                    self.record_ack(s, p, point, true, Op::Put { slot, value }, Outcome::Identical);
                    true
                }
                Some(_) if self.weakening != Weakening::PutOverwrites => {
                    set_last(s, p, img, Outcome::Conflict);
                    true
                }
                _ => {
                    let mut next = st;
                    next.slots[slot as usize] = Some(value);
                    self.start_commit(s, p, op, next)
                }
            },
        }
    }

    /// The snapshot the fencing authority currently names on the process's
    /// image, or the empty snapshot for the zero head. This is the durable
    /// truth the observer compares reports against.
    fn head_snapshot(s: &State, p: usize) -> Option<Snapshot> {
        if s.fence.head == Point::ZERO {
            return Some(Snapshot::default());
        }
        let img = s.procs[p].image as usize;
        s.images[img].journals.iter().find(|j| j.point == s.fence.head && !j.corrupt).map(|j| j.snapshot)
    }

    /// `store.go` SaveKey: top-level validation (always passes for the modeled
    /// keys), check, identical or conflicting existing key, live-claim guard,
    /// request match, commit.
    fn save_key(&self, s: &mut State, p: usize, d: usize, key: Option<u8>) -> bool {
        let img = s.procs[p].image as usize;
        let st = s.procs[p].state;
        if let Some(existing) = st.keys[d] {
            if key == Some(existing) {
                // syncCurrent, then nil: identical.
                let point = s.procs[p].point;
                if point.seq > 0 {
                    match s.images[img].journals.iter_mut().find(|j| j.point == point) {
                        Some(j) if !j.corrupt => {
                            j.content_durable = true;
                            j.name_durable = true;
                        }
                        _ => {
                            Self::poison(s, p);
                            return true;
                        }
                    }
                }
                set_last(s, p, img, Outcome::KeyIdentical);
                return true;
            }
            set_last(s, p, img, Outcome::KeyConflict);
            return true;
        }
        let live = s.procs[p].live_dkg_marker[d];
        if !st.dkg[d] || (!live && self.weakening != Weakening::NoLiveOwnerCheck) {
            set_last(s, p, img, Outcome::DkgLost);
            return true;
        }
        let Some(v) = key else {
            set_last(s, p, img, Outcome::KeyMismatch);
            return true;
        };
        // Observer: a key is being installed without live ownership, or after
        // the seats were already reported lost.
        if !s.procs[p].holds_dkg[d] || s.history.seats_lost_reported[d] {
            s.history.late_key = true;
        }
        let mut next = st;
        next.keys[d] = Some(v);
        self.start_commit(s, p, Op::SaveKey { domain: d as u8, key }, next)
    }

    /// `store.go` SaveWallet: structural validation, check, ready requires the
    /// matching durable key, identical or conflicting existing record, commit.
    fn save_wallet(&self, s: &mut State, p: usize, d: usize, w: Wallet) -> bool {
        let img = s.procs[p].image as usize;
        let st = s.procs[p].state;
        if w.rank == 0 || w.rank > 4 || (w.rank >= 2 && w.approval.is_none()) {
            set_last(s, p, img, Outcome::WalletInvalid);
            return true;
        }
        if w.rank == 3 && self.weakening != Weakening::ReadyWithoutKey && st.keys[d] != Some(w.identity) {
            set_last(s, p, img, Outcome::WalletInvalid);
            return true;
        }
        if let Some(old) = st.wallet[d] {
            if old == w {
                let point = s.procs[p].point;
                if point.seq > 0 {
                    match s.images[img].journals.iter_mut().find(|j| j.point == point) {
                        Some(j) if !j.corrupt => {
                            j.content_durable = true;
                            j.name_durable = true;
                        }
                        _ => {
                            Self::poison(s, p);
                            return true;
                        }
                    }
                }
                set_last(s, p, img, Outcome::WalletIdentical);
                return true;
            }
            let identity_conflict = old.identity != w.identity;
            let lifecycle_conflict = w.rank < old.rank
                || (old.quarantined && !w.quarantined)
                || (old.rank == 4 && w.rank != 4)
                || (old.rank == 3 && w.rank == 4);
            let approval_conflict = old.approval.is_some() && old.approval != w.approval;
            let conflict = identity_conflict
                || approval_conflict
                || (lifecycle_conflict && self.weakening != Weakening::WalletRankUnchecked);
            if conflict {
                set_last(s, p, img, Outcome::WalletConflict);
                return true;
            }
        }
        let mut next = st;
        next.wallet[d] = Some(w);
        self.start_commit(s, p, Op::SaveWallet { domain: d as u8, wallet: w }, next)
    }

    fn load_key(&self, s: &mut State, p: usize, d: usize) {
        let img = s.procs[p].image as usize;
        if s.procs[p].poisoned {
            set_last(s, p, img, Outcome::Quarantined);
            return;
        }
        if s.fence.head != s.procs[p].point && self.weakening != Weakening::LoadKeyIgnoresFence {
            Self::poison(s, p);
            return;
        }
        match s.procs[p].state.keys[d] {
            Some(v) => {
                let durable = Self::head_snapshot(s, p).map(|h| h.keys[d] == Some(v)).unwrap_or(false);
                if !durable {
                    s.history.load_not_durable = true;
                }
                set_last(s, p, img, Outcome::KeyLoaded);
            }
            None => set_last(s, p, img, Outcome::Missing),
        }
    }

    /// `store/dkg_status.go`: KeyStored if a key is saved, InProgress if the
    /// live marker is set, else SeatsLost; Missing without a DKG claim.
    fn dkg_status(&self, s: &mut State, p: usize, d: usize) {
        let img = s.procs[p].image as usize;
        if s.procs[p].poisoned {
            set_last(s, p, img, Outcome::Quarantined);
            return;
        }
        if s.fence.head != s.procs[p].point {
            Self::poison(s, p);
            return;
        }
        let st = s.procs[p].state;
        if !st.dkg[d] {
            set_last(s, p, img, Outcome::Missing);
            return;
        }
        let marker = s.procs[p].live_dkg_marker[d];
        let reported = if st.keys[d].is_some() && self.weakening != Weakening::StatusIgnoresKey {
            Outcome::StatusKeyStored
        } else if marker {
            Outcome::StatusInProgress
        } else {
            Outcome::StatusSeatsLost
        };
        // Observer: the expected status from the durable truth and the ground
        // truth of live ownership.
        let head = Self::head_snapshot(s, p);
        let expected = match head {
            Some(h) if !h.dkg[d] => Outcome::Missing,
            Some(h) if h.keys[d].is_some() => Outcome::StatusKeyStored,
            Some(_) if s.procs[p].holds_dkg[d] => Outcome::StatusInProgress,
            Some(_) => Outcome::StatusSeatsLost,
            None => Outcome::Quarantined,
        };
        if reported != expected {
            s.history.status_mismatch = true;
        }
        if s.history.seats_lost_reported[d] && reported != Outcome::StatusSeatsLost {
            s.history.seats_lost_regressed = true;
        }
        if reported == Outcome::StatusSeatsLost {
            s.history.seats_lost_reported[d] = true;
        }
        set_last(s, p, img, reported);
    }

    fn start_commit(&self, s: &mut State, p: usize, op: Op, next: Snapshot) -> bool {
        if s.commits_used >= self.max_commits {
            return false; // bound, not a guard: the model simply stops here
        }
        s.commits_used += 1;
        let point = Point { seq: s.procs[p].point.seq + 1, uid: s.commits_used };
        s.procs[p].commit = Some(Commit { op, next, point, stage: Stage::BeforeWrite, fenced: false });
        true
    }

    /// Record an acknowledgement of `op` at `point` and check what the caller
    /// is told against the durable truth: the authority accepted the advance to
    /// `point` and the journal named by `point` is durable on disk. A later
    /// environment change of the head (a reset) is not the store's acknowledgement
    /// being wrong; it is handled by the next operation or open.
    fn record_ack(&self, s: &mut State, p: usize, point: Point, fenced: bool, op: Op, outcome: Outcome) {
        let img = s.procs[p].image as usize;
        let durable = fenced
            && (point.seq == 0
                || s.images[img]
                    .journals
                    .iter()
                    .any(|j| j.point == point && j.content_durable && j.name_durable && !j.corrupt));
        if !durable {
            s.history.ack_not_durable = true;
        }
        if s.procs[p].uncertain || s.procs[p].poisoned {
            s.history.ack_after_uncertain = true;
        }
        match op {
            Op::Claim { attempt, .. } => {
                if s.history.acked_claims & bit(attempt) != 0 && !s.history.both_lost {
                    s.history.claim_reused = true;
                }
                s.history.acked_claims |= bit(attempt);
            }
            Op::Put { slot, value } => match s.history.acked_slots[slot as usize] {
                Some(w) => {
                    if w != value && !s.history.both_lost {
                        s.history.conflicting_ack = true;
                    }
                }
                None => s.history.acked_slots[slot as usize] = Some(value),
            },
            Op::SaveKey { domain, key } => {
                let d = domain as usize;
                if let Some(v) = key {
                    match s.history.acked_keys[d] {
                        Some(w) if w != v && !s.history.both_lost => s.history.conflicting_ack = true,
                        None => s.history.acked_keys[d] = Some(v),
                        _ => {}
                    }
                }
            }
            Op::SaveWallet { domain, wallet } => {
                let d = domain as usize;
                if let Some(old) = s.history.acked_wallet[d] {
                    if !s.history.both_lost
                        && (old.identity != wallet.identity
                            || wallet.rank < old.rank
                            || (old.quarantined && !wallet.quarantined)
                            || (old.rank == 4 && wallet.rank != 4)
                            || (old.rank == 3 && wallet.rank == 4)
                            || (old.approval.is_some() && old.approval != wallet.approval))
                    {
                        s.history.wallet_regressed = true;
                    }
                }
                if wallet.rank == 3 {
                    let ok = Self::head_snapshot(s, p).map(|h| h.keys[d] == Some(wallet.identity)).unwrap_or(false)
                        || s.procs[p].commit.map(|c| c.next.keys[d] == Some(wallet.identity)).unwrap_or(false);
                    if !ok {
                        s.history.ready_without_key = true;
                    }
                }
                s.history.acked_wallet[d] = Some(wallet);
            }
        }
        set_last(s, p, img, outcome);
    }

    fn ack(&self, s: &mut State, p: usize) {
        let c = s.procs[p].commit.expect("ack needs a commit");
        let outcome = match c.op {
            Op::Claim { .. } => Outcome::ClaimOk,
            Op::Put { .. } => Outcome::Durable,
            Op::SaveKey { .. } => Outcome::KeySaved,
            Op::SaveWallet { .. } => Outcome::WalletSaved,
        };
        self.record_ack(s, p, c.point, c.fenced, c.op, outcome);
        if let Op::Claim { purpose, domain, .. } = c.op {
            match purpose {
                Purpose::Sign => s.procs[p].live_claims += 1,
                Purpose::Dkg => {
                    s.procs[p].holds_dkg[domain as usize] = true;
                    s.procs[p].live_dkg_marker[domain as usize] = true;
                }
            }
        }
        s.procs[p].point = c.point;
        s.procs[p].state = c.next;
    }

    fn cas_ok(s: &State, p: usize) -> bool {
        s.fence.head == s.procs[p].point
    }

    /// Compare-and-advance with a response. Returns true when the handle may
    /// continue to the next stage.
    fn fence(&self, s: &mut State, p: usize) -> bool {
        let c = s.procs[p].commit.expect("fence needs a commit");
        if Self::cas_ok(s, p) {
            s.fence.head = c.point;
            if let Some(c) = s.procs[p].commit.as_mut() {
                c.fenced = true;
            }
            return true;
        }
        if self.weakening == Weakening::IgnoreAdvanceError {
            return true;
        }
        Self::poison(s, p);
        false
    }

    /// The authority commits the advance; the response is lost.
    fn fence_lost(&self, s: &mut State, p: usize) -> bool {
        let c = s.procs[p].commit.expect("fence needs a commit");
        s.fence.head = c.point;
        s.procs[p].uncertain = true;
        if let Some(c) = s.procs[p].commit.as_mut() {
            c.fenced = true;
        }
        if self.weakening == Weakening::IgnoreAdvanceError {
            return true;
        }
        Self::poison(s, p);
        let img = s.procs[p].image as usize;
        set_last(s, p, img, Outcome::LostResponse);
        false
    }

    fn set_stage(s: &mut State, p: usize, stage: Stage) {
        if let Some(c) = s.procs[p].commit.as_mut() {
            c.stage = stage;
        }
    }

    fn step(&self, s: &mut State, p: usize, step: Step) {
        let c = s.procs[p].commit.expect("step needs a commit");
        let img = s.procs[p].image as usize;
        match (c.stage, step) {
            (Stage::BeforeWrite, Step::Write) => Self::set_stage(s, p, Stage::Written),
            (Stage::Written, Step::FileSync) => Self::set_stage(s, p, Stage::FileSynced),
            (Stage::FileSynced, Step::Install) => {
                s.images[img].journals.push(Journal {
                    point: c.point,
                    snapshot: c.next,
                    content_durable: true,
                    name_durable: false,
                    corrupt: false,
                });
                Self::set_stage(s, p, Stage::Installed);
            }
            (Stage::Installed, Step::DirSync) => {
                if let Some(j) = s.images[img].journals.iter_mut().find(|j| j.point == c.point) {
                    j.name_durable = true;
                }
                Self::set_stage(s, p, Stage::DirSynced);
            }
            (Stage::Installed, Step::Fence) | (Stage::DirSynced, Step::Fence) => {
                if self.fence(s, p) {
                    Self::set_stage(s, p, Stage::Fenced);
                }
            }
            (Stage::Installed, Step::FenceLost) | (Stage::DirSynced, Step::FenceLost) => {
                if self.fence_lost(s, p) {
                    Self::set_stage(s, p, Stage::Fenced);
                }
            }
            (Stage::DirSynced, Step::Ack) => {
                // Weakening::AckBeforeFence only.
                self.ack(s, p);
                Self::set_stage(s, p, Stage::AckedUnfenced);
            }
            (Stage::AckedUnfenced, Step::Fence) => {
                if self.fence(s, p) {
                    s.procs[p].commit = None;
                }
            }
            (Stage::AckedUnfenced, Step::FenceLost) => {
                if self.fence_lost(s, p) {
                    s.procs[p].commit = None;
                }
            }
            (Stage::Fenced, Step::Ack) => {
                self.ack(s, p);
                s.procs[p].commit = None;
            }
            _ => unreachable!("step {:?} not offered at {:?}", step, c.stage),
        }
    }

    fn crash_proc(s: &mut State, p: usize) {
        let img = s.procs[p].image as usize;
        if s.images[img].local_lock == Some(p as u8) {
            s.images[img].local_lock = None;
        }
        if s.fence.lease == Some(p as u8) {
            s.fence.lease = None;
        }
        s.procs[p] = Proc::default();
        set_last(s, p, img, Outcome::Crashed);
    }

    fn apply(&self, s: &mut State, a: Action) -> bool {
        s.last = None;
        match a {
            Action::Open { proc, image } => {
                self.open(s, proc as usize, image as usize);
                true
            }
            Action::Close { proc } => {
                let p = proc as usize;
                let img = s.procs[p].image as usize;
                if s.procs[p].live_claims > 0 || s.procs[p].holds_dkg.iter().any(|h| *h) {
                    set_last(s, p, img, Outcome::Busy);
                    return true;
                }
                if s.images[img].local_lock == Some(proc) {
                    s.images[img].local_lock = None;
                }
                if s.fence.lease == Some(proc) {
                    s.fence.lease = None;
                }
                s.procs[p] = Proc::default();
                set_last(s, p, img, Outcome::Closed);
                true
            }
            Action::Release { proc, kind, domain } => {
                let p = proc as usize;
                match kind {
                    ReleaseKind::Sign => s.procs[p].live_claims -= 1,
                    ReleaseKind::Dkg => {
                        let d = domain as usize;
                        s.procs[p].holds_dkg[d] = false;
                        if self.weakening != Weakening::ReleaseKeepsLiveDkg {
                            s.procs[p].live_dkg_marker[d] = false;
                        }
                    }
                }
                if self.weakening == Weakening::ReleaseClearsClaim {
                    s.procs[p].state.claims = 0;
                }
                true
            }
            Action::LoadKey { proc, domain } => {
                self.load_key(s, proc as usize, domain as usize);
                true
            }
            Action::DkgStatus { proc, domain } => {
                self.dkg_status(s, proc as usize, domain as usize);
                true
            }
            Action::Begin { proc, op, fail_sync } => self.begin(s, proc as usize, op, fail_sync),
            Action::Step { proc, step } => {
                self.step(s, proc as usize, step);
                true
            }
            Action::IoError { proc } => {
                Self::poison(s, proc as usize);
                true
            }
            Action::ProcessCrash { proc } => {
                Self::crash_proc(s, proc as usize);
                true
            }
            Action::HostCrash => {
                for p in 0..PROCS {
                    if s.procs[p].open {
                        Self::crash_proc(s, p);
                    }
                }
                for img in s.images.iter_mut() {
                    img.journals.retain(|j| j.name_durable);
                    for j in img.journals.iter_mut() {
                        if !j.content_durable {
                            j.corrupt = true;
                        }
                    }
                    img.local_lock = None;
                }
                s.fence.lease = None;
                true
            }
            Action::FenceReset => {
                s.fence.head = Point::ZERO;
                s.fence.reset_used = true;
                true
            }
            Action::RestoreStale { image } => {
                let img = &mut s.images[image as usize];
                // A restore replaces the directory with an earlier backup: every
                // name in it is durable, and the newest committed journal is gone.
                img.journals.retain(|j| j.name_durable && j.content_durable && !j.corrupt);
                if let Some(max) = img.journals.iter().map(|j| j.point).max() {
                    img.journals.retain(|j| j.point != max);
                }
                true
            }
            Action::Clone => {
                // A clone is a file-level copy of the live directory, as an
                // operator or the replay harness makes it: every installed
                // journal name is copied, including one whose directory entry
                // is not yet synced, and the copies are durable in the clone.
                // (Revised in tranche 2, finding F-10; the first model copied
                // durable journals only.)
                let src = &s.images[0];
                let journals: Vec<Journal> = src
                    .journals
                    .iter()
                    .filter(|j| !j.corrupt)
                    .map(|j| Journal { content_durable: true, name_durable: true, ..*j })
                    .collect();
                let identity = src.identity;
                s.images[1] = Image { exists: true, identity, journals, local_lock: None };
                s.cloned = true;
                true
            }
        }
    }
}

impl Model for HostModel {
    type State = State;
    type Action = Action;

    fn init_states(&self) -> Vec<State> {
        let mut s = State::default();
        s.images[0].exists = true;
        vec![s]
    }

    fn actions(&self, s: &State, out: &mut Vec<Action>) {
        for p in 0..PROCS {
            let pr = &s.procs[p];
            let proc = p as u8;
            if !pr.open {
                for img in 0..IMAGES {
                    if s.images[img].exists {
                        out.push(Action::Open { proc, image: img as u8 });
                    }
                }
                continue;
            }
            if let Some(c) = pr.commit {
                match c.stage {
                    Stage::BeforeWrite => out.push(Action::Step { proc, step: Step::Write }),
                    Stage::Written => out.push(Action::Step { proc, step: Step::FileSync }),
                    Stage::FileSynced => out.push(Action::Step { proc, step: Step::Install }),
                    Stage::Installed => {
                        if self.weakening == Weakening::NoDirSync {
                            out.push(Action::Step { proc, step: Step::Fence });
                            if Self::cas_ok(s, p) {
                                out.push(Action::Step { proc, step: Step::FenceLost });
                            }
                        } else {
                            out.push(Action::Step { proc, step: Step::DirSync });
                        }
                    }
                    Stage::DirSynced => {
                        if self.weakening == Weakening::AckBeforeFence {
                            out.push(Action::Step { proc, step: Step::Ack });
                        } else {
                            out.push(Action::Step { proc, step: Step::Fence });
                            if Self::cas_ok(s, p) {
                                out.push(Action::Step { proc, step: Step::FenceLost });
                            }
                        }
                    }
                    Stage::AckedUnfenced => {
                        out.push(Action::Step { proc, step: Step::Fence });
                        if Self::cas_ok(s, p) {
                            out.push(Action::Step { proc, step: Step::FenceLost });
                        }
                    }
                    Stage::Fenced => out.push(Action::Step { proc, step: Step::Ack }),
                }
                if c.stage != Stage::AckedUnfenced {
                    out.push(Action::IoError { proc });
                }
                out.push(Action::ProcessCrash { proc });
                continue;
            }
            out.push(Action::Close { proc });
            if pr.live_claims > 0 {
                out.push(Action::Release { proc, kind: ReleaseKind::Sign, domain: 0 });
            }
            for domain in 0..DOMAINS as u8 {
                if pr.holds_dkg[domain as usize] {
                    out.push(Action::Release { proc, kind: ReleaseKind::Dkg, domain });
                }
            }
            out.push(Action::ProcessCrash { proc });
            for attempt in 0..ATTEMPTS {
                // Signing claims use only domain 0: the claim key is global and
                // the domain does not enter a signing claim's state. DKG claims
                // use both domains because the reservation is per domain. The
                // dkg profile keeps only DKG claims in domain 0: the scope and
                // signing-claim behavior is checked by the store profile on
                // the same kernel.
                if self.profile == Profile::Store {
                    out.push(Action::Begin { proc, op: Op::Claim { attempt, purpose: Purpose::Sign, domain: 0 }, fail_sync: false });
                    for domain in 0..DOMAINS as u8 {
                        out.push(Action::Begin { proc, op: Op::Claim { attempt, purpose: Purpose::Dkg, domain }, fail_sync: false });
                    }
                } else {
                    out.push(Action::Begin { proc, op: Op::Claim { attempt, purpose: Purpose::Dkg, domain: 0 }, fail_sync: false });
                }
            }
            match self.profile {
                Profile::Store => {
                    for slot in 0..SLOTS as u8 {
                        for value in 0..VALUES {
                            out.push(Action::Begin { proc, op: Op::Put { slot, value }, fail_sync: false });
                            if pr.state.slots[slot as usize] == Some(value) {
                                out.push(Action::Begin { proc, op: Op::Put { slot, value }, fail_sync: true });
                            }
                        }
                    }
                }
                Profile::Dkg => {
                    // Keys and the wallet record live in domain 0; domain 1 keeps
                    // only its DKG reservation for the scope checks.
                    for key in [Some(0u8), Some(1u8), None] {
                        out.push(Action::Begin { proc, op: Op::SaveKey { domain: 0, key }, fail_sync: false });
                    }
                    for rank in 1..=4u8 {
                        for identity in 0..2u8 {
                            for quarantined in [false, true] {
                                let approvals: &[Option<u8>] = if rank == 1 { &[None] } else { &[Some(0), Some(1)] };
                                for approval in approvals {
                                    out.push(Action::Begin {
                                        proc,
                                        op: Op::SaveWallet { domain: 0, wallet: Wallet { rank, quarantined, identity, approval: *approval } },
                                        fail_sync: false,
                                    });
                                }
                            }
                        }
                    }
                    out.push(Action::LoadKey { proc, domain: 0 });
                    out.push(Action::DkgStatus { proc, domain: 0 });
                }
            }
        }
        if !s.fence.reset_used {
            out.push(Action::FenceReset);
        }
        // Clone, stale restore and host crash are kernel faults whose effect on
        // the store is checked by the store profile. The dkg profile omits them
        // to keep its state space within the run limits (SCOPE.md §11.3).
        if self.profile == Profile::Store {
            if !s.cloned {
                out.push(Action::Clone);
            }
            for img in 0..IMAGES {
                let image = &s.images[img];
                if image.exists && image.local_lock.is_none() && image.journals.iter().any(|j| j.name_durable) {
                    out.push(Action::RestoreStale { image: img as u8 });
                }
            }
            let volatile = s.images.iter().any(|i| i.journals.iter().any(|j| !j.name_durable || !j.content_durable));
            if s.procs.iter().any(|p| p.open) || volatile {
                out.push(Action::HostCrash);
            }
        }
    }

    fn next_state(&self, last: &State, action: Action) -> Option<State> {
        let mut s = last.clone();
        if self.apply(&mut s, action) {
            Some(s)
        } else {
            None
        }
    }

    fn properties(&self) -> Vec<Property<Self>> {
        let mut p = vec![
            // P-K02-OWNER
            Property::<Self>::always("owner_exclusive", |_, s| {
                s.procs.iter().filter(|p| p.open).count() <= 1
                    && s.procs.iter().enumerate().all(|(i, p)| !p.open || s.fence.lease == Some(i as u8))
            }),
            // P-K02-TERMINAL
            Property::<Self>::always("terminal_claims", |_, s| !s.history.claim_reused),
            // P-K02-IMMUTABLE
            Property::<Self>::always("immutable_slots", |_, s| !s.history.conflicting_ack),
            // P-K02-DURABLE
            Property::<Self>::always("durable_acks", |_, s| !s.history.ack_not_durable && !s.history.stale_exposed),
            // P-K02-UNCERTAIN
            Property::<Self>::always("uncertain_fail_closed", |_, s| !s.history.ack_after_uncertain),
        ];
        if self.profile == Profile::Dkg {
            p.extend([
                // P-K03-NO-LATE-KEY
                Property::<Self>::always("no_late_key", |_, s| !s.history.late_key),
                // P-K03-SEATSLOST-TERMINAL
                Property::<Self>::always("seatslost_terminal", |_, s| !s.history.seats_lost_regressed),
                // P-K03-STATUS-DURABLE
                Property::<Self>::always("status_durable", |_, s| !s.history.status_mismatch),
                // P-K03-RELOAD-ONLY-DURABLE
                Property::<Self>::always("reload_only_durable", |_, s| !s.history.load_not_durable),
                // P-K03-WALLET-MONOTONE
                Property::<Self>::always("wallet_monotone", |_, s| !s.history.wallet_regressed && !s.history.ready_without_key),
            ]);
        }
        if self.weakening == Weakening::None && self.profile == Profile::Dkg {
            p.extend([
                Property::<Self>::sometimes("w_key_saved_live", |_, s| outcome_is(s, Outcome::KeySaved)),
                Property::<Self>::sometimes("w_key_identical", |_, s| outcome_is(s, Outcome::KeyIdentical)),
                Property::<Self>::sometimes("w_late_key_refused", |_, s| {
                    s.last.is_some_and(|l| l.outcome == Outcome::DkgLost && s.procs[l.proc as usize].state.dkg[0])
                }),
                Property::<Self>::sometimes("w_key_loaded_after_reopen", |_, s| {
                    s.history.reopened && s.last.is_some_and(|l| l.outcome == Outcome::KeyLoaded && !s.procs[l.proc as usize].holds_dkg[0])
                }),
                Property::<Self>::sometimes("w_status_key_stored_after_reopen", |_, s| {
                    s.history.reopened && s.last.is_some_and(|l| l.outcome == Outcome::StatusKeyStored && !s.procs[l.proc as usize].holds_dkg[0])
                }),
                Property::<Self>::sometimes("w_status_seats_lost", |_, s| outcome_is(s, Outcome::StatusSeatsLost)),
                Property::<Self>::sometimes("w_status_in_progress", |_, s| outcome_is(s, Outcome::StatusInProgress)),
                Property::<Self>::sometimes("w_wallet_ready", |_, s| {
                    s.last.is_some_and(|l| l.outcome == Outcome::WalletSaved && s.procs[l.proc as usize].state.wallet[0].is_some_and(|w| w.rank == 3))
                }),
                Property::<Self>::sometimes("w_wallet_closed_after_pending", |_, s| {
                    s.last.is_some_and(|l| l.outcome == Outcome::WalletSaved && s.procs[l.proc as usize].state.wallet[0].is_some_and(|w| w.rank == 4))
                        && s.history.acked_wallet[0].is_some_and(|w| w.rank == 4)
                }),
                Property::<Self>::sometimes("w_wallet_conflict", |_, s| outcome_is(s, Outcome::WalletConflict)),
            ]);
        }
        if self.weakening == Weakening::None {
            p.extend([
                Property::<Self>::sometimes("w_fresh_claim", |_, s| outcome_is(s, Outcome::ClaimOk)),
                Property::<Self>::sometimes("w_identical", |m, s| m.profile == Profile::Dkg || outcome_is(s, Outcome::Identical)),
                Property::<Self>::sometimes("w_conflict", |m, s| m.profile == Profile::Dkg || outcome_is(s, Outcome::Conflict)),
                Property::<Self>::sometimes("w_claimed", |_, s| outcome_is(s, Outcome::Claimed)),
                Property::<Self>::sometimes("w_claimed_after_restart", |_, s| {
                    // The refusing handle holds no live claim: the tombstone came
                    // from a journal loaded by a reopen, not from this handle's
                    // own live ownership.
                    s.history.reopened
                        && s.last.is_some_and(|l| l.outcome == Outcome::Claimed && s.procs[l.proc as usize].live_claims == 0)
                }),
                Property::<Self>::sometimes("w_dkg_pending", |_, s| outcome_is(s, Outcome::DkgPending)),
                Property::<Self>::sometimes("w_busy", |_, s| {
                    s.last.is_some_and(|l| l.outcome == Outcome::Busy && !s.procs[l.proc as usize].open)
                }),
                Property::<Self>::sometimes("w_clone_busy", |m, s| {
                    m.profile == Profile::Dkg
                        || (s.last.is_some_and(|l| l.outcome == Outcome::Busy && l.image == 1 && !s.procs[l.proc as usize].open)
                            && s.procs.iter().any(|p| p.open && p.image == 0))
                }),
                Property::<Self>::sometimes("w_reopen", |_, s| {
                    s.last.is_some_and(|l| l.outcome == Outcome::Opened && s.procs[l.proc as usize].point.seq > 0)
                }),
                Property::<Self>::sometimes("w_progress_after_reopen", |_, s| {
                    s.history.reopened
                        && s.last.is_some_and(|l| l.outcome == Outcome::ClaimOk && s.procs[l.proc as usize].point.seq > 1)
                }),
                Property::<Self>::sometimes("w_progress_after_orphan", |_, s| {
                    s.last.is_some_and(|l| {
                        l.outcome == Outcome::ClaimOk && has_orphan(&s.images[l.image as usize], s.fence.head)
                    })
                }),
                Property::<Self>::sometimes("w_quarantine_on_open", |_, s| {
                    s.last.is_some_and(|l| l.outcome == Outcome::Quarantined && !s.procs[l.proc as usize].open)
                }),
                Property::<Self>::sometimes("w_orphan_quarantine", |_, s| {
                    s.last.is_some_and(|l| {
                        l.outcome == Outcome::Quarantined
                            && !s.procs[l.proc as usize].open
                            && s.fence.head == Point::ZERO
                            && !s.fence.reset_used
                            && !s.images[l.image as usize].journals.is_empty()
                    })
                }),
                Property::<Self>::sometimes("w_reset_quarantine", |_, s| {
                    s.last.is_some_and(|l| {
                        l.outcome == Outcome::Quarantined
                            && !s.procs[l.proc as usize].open
                            && s.fence.head == Point::ZERO
                            && s.fence.reset_used
                            && !s.images[l.image as usize].journals.is_empty()
                    })
                }),
                Property::<Self>::sometimes("w_stale_restore_quarantine", |m, s| {
                    m.profile == Profile::Dkg || s.last.is_some_and(|l| {
                        let image = &s.images[l.image as usize];
                        l.outcome == Outcome::Quarantined
                            && !s.procs[l.proc as usize].open
                            && s.fence.head.seq > 0
                            && image.identity
                            && !image.journals.is_empty()
                            && !image.journals.iter().any(|j| j.point == s.fence.head)
                    })
                }),
                Property::<Self>::sometimes("w_fence_lost_then_quarantined", |_, s| {
                    s.last.is_some_and(|l| {
                        let p = &s.procs[l.proc as usize];
                        l.outcome == Outcome::Quarantined && p.open && p.uncertain
                    })
                }),
                Property::<Self>::sometimes(BOUNDARY_WITNESS, |m, s| m.profile == Profile::Dkg || s.history.both_lost),
            ]);
        }
        p
    }

    fn format_action(&self, action: &Action) -> String {
        format!("{action:?}")
    }
}

// ---------------------------------------------------------------------------
// Trace export
// ---------------------------------------------------------------------------

/// One replayable step. `expect` is the acting process's outcome after the
/// step. `pause` is the Go boundary where a replaying subprocess waits after
/// the step while a commit is in progress. `expect_state` is the abstract
/// snapshot a successful open must load.
#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct TraceStep {
    pub n: usize,
    pub action: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub proc: Option<u8>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub image: Option<u8>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub attempt: Option<u8>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub purpose: Option<Purpose>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub domain: Option<u8>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub slot: Option<u8>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub value: Option<u8>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub step: Option<Step>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub fail_sync: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub kind: Option<ReleaseKind>,
    /// SaveKey: "0" or "1" for a matching key with that identity, "bad" for a
    /// key whose roster does not match the request.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub key: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub wallet: Option<Wallet>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub expect: Option<Outcome>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub pause: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub expect_state: Option<Snapshot>,
    /// Set on the first step whose outcome differs from the unweakened model
    /// when a weakened path is re-executed on the claim model.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub diverges: Option<bool>,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Trace {
    pub id: String,
    pub kind: String,
    pub property: String,
    pub weakening: Weakening,
    pub profile: Profile,
    pub max_commits: u8,
    /// `full`, `partial` (host crash is simulated by applying the modeled loss)
    /// or `none` with a reason.
    pub replay: String,
    pub replay_note: String,
    pub steps: Vec<TraceStep>,
    /// For a counterexample of a weakened model: the same actions executed on
    /// the claim model, cut at the first step that is not offered there.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub claim_model_steps: Option<Vec<TraceStep>>,
}

/// The Go boundary at which a subprocess waits in each commit stage.
pub fn pause_for(stage: Stage) -> &'static str {
    match stage {
        Stage::BeforeWrite => "before-write",
        Stage::Written => "before-file-sync",
        Stage::FileSynced => "before-install",
        Stage::Installed => "before-directory-sync",
        Stage::DirSynced => "before-fence",
        Stage::Fenced => "before-acknowledgement",
        Stage::AckedUnfenced => "before-fence",
    }
}

fn describe(n: usize, a: Action, after: &State) -> TraceStep {
    let mut t = TraceStep {
        n,
        action: String::new(),
        proc: None,
        image: None,
        attempt: None,
        purpose: None,
        domain: None,
        slot: None,
        value: None,
        step: None,
        fail_sync: None,
        kind: None,
        key: None,
        wallet: None,
        expect: None,
        pause: None,
        expect_state: None,
        diverges: None,
    };
    let proc_outcome = |p: u8| after.last.filter(|l| l.proc == p).map(|l| l.outcome);
    let proc_pause = |p: u8| after.procs[p as usize].commit.map(|c| pause_for(c.stage).to_string());
    match a {
        Action::Open { proc, image } => {
            t.action = "open".into();
            t.proc = Some(proc);
            t.image = Some(image);
            t.expect = proc_outcome(proc);
            if t.expect == Some(Outcome::Opened) {
                t.expect_state = Some(after.procs[proc as usize].state);
            }
        }
        Action::Close { proc } => {
            t.action = "close".into();
            t.proc = Some(proc);
            t.expect = proc_outcome(proc);
        }
        Action::Release { proc, kind, domain } => {
            t.action = "release".into();
            t.proc = Some(proc);
            t.kind = Some(kind);
            t.domain = Some(domain);
        }
        Action::LoadKey { proc, domain } => {
            t.action = "load_key".into();
            t.proc = Some(proc);
            t.domain = Some(domain);
            t.expect = proc_outcome(proc);
        }
        Action::DkgStatus { proc, domain } => {
            t.action = "dkg_status".into();
            t.proc = Some(proc);
            t.domain = Some(domain);
            t.expect = proc_outcome(proc);
        }
        Action::Begin { proc, op, fail_sync } => {
            t.proc = Some(proc);
            match op {
                Op::Claim { attempt, purpose, domain } => {
                    t.action = "claim".into();
                    t.attempt = Some(attempt);
                    t.purpose = Some(purpose);
                    t.domain = Some(domain);
                }
                Op::Put { slot, value } => {
                    t.action = "put".into();
                    t.slot = Some(slot);
                    t.value = Some(value);
                    if fail_sync {
                        t.fail_sync = Some(true);
                    }
                }
                Op::SaveKey { domain, key } => {
                    t.action = "save_key".into();
                    t.domain = Some(domain);
                    t.key = Some(match key {
                        Some(v) => v.to_string(),
                        None => "bad".into(),
                    });
                }
                Op::SaveWallet { domain, wallet } => {
                    t.action = "save_wallet".into();
                    t.domain = Some(domain);
                    t.wallet = Some(wallet);
                }
            }
            t.expect = proc_outcome(proc);
            t.pause = proc_pause(proc);
        }
        Action::Step { proc, step } => {
            t.action = "step".into();
            t.proc = Some(proc);
            t.step = Some(step);
            t.expect = proc_outcome(proc);
            t.pause = proc_pause(proc);
        }
        Action::IoError { proc } => {
            t.action = "io_error".into();
            t.proc = Some(proc);
            t.expect = proc_outcome(proc);
        }
        Action::ProcessCrash { proc } => {
            t.action = "process_crash".into();
            t.proc = Some(proc);
            t.expect = proc_outcome(proc);
        }
        Action::HostCrash => t.action = "host_crash".into(),
        Action::FenceReset => t.action = "fence_reset".into(),
        Action::RestoreStale { image } => {
            t.action = "restore_stale".into();
            t.image = Some(image);
        }
        Action::Clone => t.action = "clone".into(),
    }
    t
}

/// Execute `actions` from the initial state of `model`, describing each step.
/// Stops at the first action that is not offered or has no effect.
pub fn execute(model: &HostModel, actions: &[Action]) -> (Vec<TraceStep>, State, bool) {
    let mut state = model.init_states().remove(0);
    let mut steps = Vec::new();
    for (i, a) in actions.iter().enumerate() {
        let mut offered = Vec::new();
        model.actions(&state, &mut offered);
        if !offered.contains(a) {
            return (steps, state, false);
        }
        match model.next_state(&state, *a) {
            Some(next) => {
                steps.push(describe(i + 1, *a, &next));
                state = next;
            }
            None => return (steps, state, false),
        }
    }
    (steps, state, true)
}

pub fn replay_class(actions: &[Action]) -> (&'static str, &'static str) {
    if actions.iter().any(|a| matches!(a, Action::HostCrash)) {
        ("partial", "host_crash is simulated: the replay harness removes the files the model declares not durable; it cannot drop the OS cache")
    } else {
        ("full", "every step maps to a store call, a boundary pause or a test-only environment action")
    }
}

pub fn trace_from_path(
    id: &str,
    kind: &str,
    property: &str,
    model: &HostModel,
    actions: &[Action],
) -> Trace {
    let (steps, _, complete) = execute(model, actions);
    assert!(complete, "path actions must replay on their own model");
    let (replay, note) = replay_class(actions);
    let claim_model_steps = if model.weakening != Weakening::None {
        let claim = HostModel { weakening: Weakening::None, max_commits: model.max_commits, profile: model.profile };
        let (mut claim_steps, _, _) = execute(&claim, actions);
        for (c, w) in claim_steps.iter_mut().zip(steps.iter()) {
            if c.expect != w.expect || c.pause != w.pause || c.expect_state != w.expect_state {
                c.diverges = Some(true);
                break;
            }
        }
        Some(claim_steps)
    } else {
        None
    };
    Trace {
        id: id.into(),
        kind: kind.into(),
        property: property.into(),
        weakening: model.weakening,
        profile: model.profile,
        max_commits: model.max_commits,
        replay: replay.into(),
        replay_note: note.into(),
        steps,
        claim_model_steps,
    }
}

// ---------------------------------------------------------------------------
// Runner
// ---------------------------------------------------------------------------

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct RunReport {
    pub weakening: Weakening,
    pub profile: Profile,
    pub max_commits: u8,
    pub threads: usize,
    pub timeout_secs: u64,
    pub completed: bool,
    pub timed_out: bool,
    pub seconds: f64,
    pub unique_states: usize,
    pub generated_states: usize,
    pub max_depth: usize,
    /// property name -> counterexample (always) or witness (sometimes) found
    pub discoveries: BTreeMap<String, bool>,
    pub counterexample_actions: BTreeMap<String, Vec<String>>,
}

pub struct RunOutput {
    pub report: RunReport,
    pub traces: Vec<Trace>,
}

/// Check `model`. The claim model is explored exhaustively (it finishes only
/// when every property has a discovery or every state was visited; its safety
/// properties must have none). A weakened model stops at the first
/// counterexample of its declared property: it is a negative control, and an
/// exhaustive exploration of a broken model adds no evidence.
pub fn run(model: HostModel, threads: usize, timeout: Duration) -> RunOutput {
    let start = Instant::now();
    let mut builder = model.checker().threads(threads).timeout(timeout);
    if model.weakening != Weakening::None {
        let set: std::collections::BTreeSet<&'static str> = model.weakening.declared_failures().iter().copied().collect();
        builder = builder.finish_when(stateright::HasDiscoveries::AllOf(set));
    }
    let checker = builder.spawn_bfs().join();
    let seconds = start.elapsed().as_secs_f64();
    // Stateright reports `is_done` when its job market is closed, and the
    // market also closes when the timeout passes. A run is complete only when
    // it was done before the deadline. A run that reached the deadline is
    // blocked, whatever `is_done` says.
    let timed_out = seconds >= timeout.as_secs_f64();
    let completed = checker.is_done() && !timed_out;
    let mut discoveries = BTreeMap::new();
    let mut counterexample_actions = BTreeMap::new();
    let mut traces = Vec::new();
    for prop in model.properties() {
        let found = checker.discovery(prop.name);
        discoveries.insert(prop.name.to_string(), found.is_some());
        if let Some(path) = found {
            let actions: Vec<Action> = path.into_actions();
            // Trace IDs are namespaced by profile so both profiles' sets can
            // live in one directory.
            let prefix = match model.profile {
                Profile::Store => "",
                Profile::Dkg => "DKG-",
            };
            let (kind, id) = match prop.expectation {
                stateright::Expectation::Always => {
                    counterexample_actions.insert(prop.name.to_string(), actions.iter().map(|a| format!("{a:?}")).collect());
                    ("counterexample", format!("{prefix}CE-{}-{:?}", prop.name.to_uppercase().replace('_', "-"), model.weakening))
                }
                _ => ("witness", format!("{prefix}W-{}", prop.name.trim_start_matches("w_").to_uppercase().replace('_', "-"))),
            };
            traces.push(trace_from_path(&id, kind, prop.name, &model, &actions));
        }
    }
    RunOutput {
        report: RunReport {
            weakening: model.weakening,
            profile: model.profile,
            max_commits: model.max_commits,
            threads,
            timeout_secs: timeout.as_secs(),
            completed,
            timed_out,
            seconds,
            unique_states: checker.unique_state_count(),
            generated_states: checker.state_count(),
            max_depth: checker.max_depth(),
            discoveries,
            counterexample_actions,
        },
        traces,
    }
}

// ---------------------------------------------------------------------------
// Declared representative boundary set
// ---------------------------------------------------------------------------

fn p0() -> u8 {
    0
}
fn open0() -> Action {
    Action::Open { proc: p0(), image: 0 }
}
fn claim(attempt: u8) -> Action {
    Action::Begin { proc: p0(), op: Op::Claim { attempt, purpose: Purpose::Sign, domain: 0 }, fail_sync: false }
}
fn step(step: Step) -> Action {
    Action::Step { proc: p0(), step }
}

/// The longest prefix of `actions` that the model offers and that changes state.
pub fn executable_prefix(model: &HostModel, actions: &[Action]) -> Vec<Action> {
    let mut prefix = Vec::new();
    let mut st = model.init_states().remove(0);
    for act in actions {
        let mut offered = Vec::new();
        model.actions(&st, &mut offered);
        if !offered.contains(act) {
            break;
        }
        match model.next_state(&st, *act) {
            Some(n) => {
                prefix.push(*act);
                st = n;
            }
            None => break,
        }
    }
    prefix
}

/// Hand-declared traces that cover each commit boundary with each fault kind,
/// plus the ordinary success paths. Expectations come from the claim model.
pub fn boundary_traces(max_commits: u8) -> Vec<Trace> {
    let model = HostModel { weakening: Weakening::None, max_commits, profile: Profile::Store };
    let mut out = Vec::new();
    let full_commit = [Step::Write, Step::FileSync, Step::Install, Step::DirSync, Step::Fence, Step::Ack];
    let mut push = |id: &str, actions: Vec<Action>| {
        let (steps, _, complete) = execute(&model, &actions);
        assert!(complete, "boundary trace {id} does not execute on the claim model");
        let (replay, note) = replay_class(&actions);
        out.push(Trace {
            id: id.into(),
            kind: "boundary".into(),
            property: "declared representative set".into(),
            weakening: Weakening::None,
            profile: Profile::Store,
            max_commits,
            replay: replay.into(),
            replay_note: note.into(),
            steps,
            claim_model_steps: None,
        });
    };

    // B-FRESH: open, claim, release, close, reopen, repeat claim refused, fresh claim ok.
    let mut a = vec![open0(), claim(0)];
    a.extend(full_commit.iter().map(|s| step(*s)));
    a.extend([Action::Release { proc: 0, kind: ReleaseKind::Sign, domain: 0 }, Action::Close { proc: 0 }, open0(), claim(0), claim(1)]);
    a.extend(full_commit.iter().map(|s| step(*s)));
    push("B-FRESH-REOPEN-TERMINAL", a);

    // B-IDENTICAL-CONFLICT: record slot and lock slot.
    for slot in 0..SLOTS as u8 {
        let mut a = vec![open0(), Action::Begin { proc: 0, op: Op::Put { slot, value: 0 }, fail_sync: false }];
        a.extend(full_commit.iter().map(|s| step(*s)));
        a.push(Action::Begin { proc: 0, op: Op::Put { slot, value: 0 }, fail_sync: false });
        a.push(Action::Begin { proc: 0, op: Op::Put { slot, value: 1 }, fail_sync: false });
        a.push(Action::Begin { proc: 0, op: Op::Put { slot, value: 0 }, fail_sync: true });
        push(&format!("B-SLOT{slot}-IDENTICAL-CONFLICT-SYNCERROR"), a);
    }

    // B-DKG: reservation across domains and restart.
    let mut a = vec![open0(), Action::Begin { proc: 0, op: Op::Claim { attempt: 0, purpose: Purpose::Dkg, domain: 0 }, fail_sync: false }];
    a.extend(full_commit.iter().map(|s| step(*s)));
    a.push(Action::Begin { proc: 0, op: Op::Claim { attempt: 1, purpose: Purpose::Dkg, domain: 0 }, fail_sync: false });
    a.push(Action::Begin { proc: 0, op: Op::Claim { attempt: 0, purpose: Purpose::Dkg, domain: 1 }, fail_sync: false });
    a.push(Action::Begin { proc: 0, op: Op::Claim { attempt: 1, purpose: Purpose::Dkg, domain: 1 }, fail_sync: false });
    a.extend(full_commit.iter().map(|s| step(*s)));
    push("B-DKG-RESERVATION-SCOPES", a);

    // B-CRASH-<stage>: process crash at each stage of the first commit, reopen,
    // repeat claim, fresh claim.
    let stages: [(&str, usize); 7] = [
        ("before-write", 0),
        ("before-file-sync", 1),
        ("before-install", 2),
        ("before-directory-sync", 3),
        ("before-fence", 4),
        ("before-acknowledgement", 5),
        ("after-return", 6),
    ];
    for (name, done) in stages {
        let mut a = vec![open0(), claim(0)];
        a.extend(full_commit[..done].iter().map(|s| step(*s)));
        a.push(Action::ProcessCrash { proc: 0 });
        a.push(open0());
        // After an unfenced installed journal with a zero head the reopen
        // quarantines, so the following claims are not offered; execute()
        // truncates there and the trace records the quarantine.
        a.push(claim(0));
        a.push(claim(1));
        a.extend(full_commit.iter().map(|s| step(*s)));
        push(&format!("B-CRASH-{}", name.to_uppercase()), executable_prefix(&model, &a));
    }

    // B-IOERROR-<stage>: injected error at each stage, then the handle refuses.
    for (name, done) in &stages[..6] {
        let mut a = vec![open0(), claim(0)];
        a.extend(full_commit[..*done].iter().map(|s| step(*s)));
        a.push(Action::IoError { proc: 0 });
        a.push(claim(1));
        a.push(Action::Close { proc: 0 });
        a.push(open0());
        a.push(claim(0));
        a.push(claim(1));
        a.extend(full_commit.iter().map(|s| step(*s)));
        push(&format!("B-IOERROR-{}", name.to_uppercase()), executable_prefix(&model, &a));
    }

    // B-FENCE-LOST: lost response, handle refuses, reopen sees the commit.
    let mut a = vec![open0(), claim(0)];
    a.extend([step(Step::Write), step(Step::FileSync), step(Step::Install), step(Step::DirSync), step(Step::FenceLost)]);
    a.push(claim(1));
    a.push(Action::Close { proc: 0 });
    a.push(open0());
    a.push(claim(0));
    a.push(claim(1));
    a.extend(full_commit.iter().map(|s| step(*s)));
    push("B-FENCE-LOST-RESPONSE", a);

    // B-FENCE-RESET-LIVE: head changes while open; the next operation quarantines.
    let mut a = vec![open0(), claim(0)];
    a.extend(full_commit.iter().map(|s| step(*s)));
    a.push(Action::FenceReset);
    a.push(claim(1));
    push("B-FENCE-CHANGED-LIVE", a);

    // B-FENCE-RESET-REOPEN: the historical defect scenario on the claim model.
    let mut a = vec![open0(), claim(0)];
    a.extend(full_commit.iter().map(|s| step(*s)));
    a.extend([Action::Release { proc: 0, kind: ReleaseKind::Sign, domain: 0 }, Action::Close { proc: 0 }, Action::FenceReset, open0()]);
    push("B-FENCE-RESET-REOPEN-QUARANTINE", a);

    // B-STALE-RESTORE: newest journal removed while the authority survives.
    let mut a = vec![open0(), claim(0)];
    a.extend(full_commit.iter().map(|s| step(*s)));
    a.extend([Action::Release { proc: 0, kind: ReleaseKind::Sign, domain: 0 }, Action::Close { proc: 0 }, Action::RestoreStale { image: 0 }, open0()]);
    push("B-STALE-RESTORE-QUARANTINE", a);

    // B-SECOND-PROCESS and B-CLONE.
    let mut a = vec![open0(), Action::Open { proc: 1, image: 0 }, Action::Clone, Action::Open { proc: 1, image: 1 }];
    a.push(claim(0));
    a.extend(full_commit.iter().map(|s| step(*s)));
    a.extend([Action::Release { proc: 0, kind: ReleaseKind::Sign, domain: 0 }, Action::Close { proc: 0 }, Action::Open { proc: 1, image: 1 }]);
    push("B-SECOND-PROCESS-AND-CLONE", a);

    // B-HOST-CRASH-<stage>: partial replay (simulated loss).
    for (name, done) in &stages[..6] {
        let mut a = vec![open0(), claim(0)];
        a.extend(full_commit[..*done].iter().map(|s| step(*s)));
        a.push(Action::HostCrash);
        a.push(open0());
        a.push(claim(0));
        a.push(claim(1));
        a.extend(full_commit.iter().map(|s| step(*s)));
        push(&format!("B-HOSTCRASH-{}", name.to_uppercase()), executable_prefix(&model, &a));
    }
    out
}

/// Hand-declared traces for the Dkg profile: the completed-key commit crashed
/// at each stage, the late key after release and after restart, the status
/// after each, and the wallet lifecycle. Expectations come from the claim model.
pub fn dkg_boundary_traces(max_commits: u8) -> Vec<Trace> {
    let model = HostModel { weakening: Weakening::None, max_commits, profile: Profile::Dkg };
    let full_commit = [Step::Write, Step::FileSync, Step::Install, Step::DirSync, Step::Fence, Step::Ack];
    let mut out = Vec::new();
    let mut push = |id: &str, actions: Vec<Action>| {
        let actions = executable_prefix(&model, &actions);
        let (steps, _, complete) = execute(&model, &actions);
        assert!(complete, "dkg boundary trace {id} does not execute on the claim model");
        let (replay, note) = replay_class(&actions);
        out.push(Trace {
            id: id.into(),
            kind: "boundary".into(),
            property: "declared representative set (dkg)".into(),
            weakening: Weakening::None,
            profile: Profile::Dkg,
            max_commits,
            replay: replay.into(),
            replay_note: note.into(),
            steps,
            claim_model_steps: None,
        });
    };
    let claim_dkg = |a: u8| Action::Begin { proc: 0, op: Op::Claim { attempt: a, purpose: Purpose::Dkg, domain: 0 }, fail_sync: false };
    let save_key = |k: Option<u8>| Action::Begin { proc: 0, op: Op::SaveKey { domain: 0, key: k }, fail_sync: false };
    let wallet = |rank: u8, identity: u8, approval: Option<u8>| {
        Action::Begin { proc: 0, op: Op::SaveWallet { domain: 0, wallet: Wallet { rank, quarantined: false, identity, approval } }, fail_sync: false }
    };
    let status = Action::DkgStatus { proc: 0, domain: 0 };
    let load = Action::LoadKey { proc: 0, domain: 0 };
    let release_dkg = Action::Release { proc: 0, kind: ReleaseKind::Dkg, domain: 0 };
    let steps = |n: usize| full_commit[..n].iter().map(|s| Action::Step { proc: 0, step: *s }).collect::<Vec<_>>();

    // D-KEY-LIVE: claim, status in progress, save key, status stored, load, identical save, release, reopen, status, load.
    let mut a = vec![open0(), claim_dkg(0)];
    a.extend(steps(6));
    a.push(status);
    a.push(save_key(Some(0)));
    a.extend(steps(6));
    a.push(status);
    a.push(load);
    a.push(save_key(Some(0)));
    a.push(save_key(Some(1)));
    a.push(release_dkg);
    a.push(Action::Close { proc: 0 });
    a.push(open0());
    a.push(status);
    a.push(load);
    push("D-KEY-LIVE-SAVE-RELOAD", a);

    // D-LATE-KEY-RELEASE: claim, release, save key refused, status seats lost.
    let mut a = vec![open0(), claim_dkg(0)];
    a.extend(steps(6));
    a.push(release_dkg);
    a.push(save_key(Some(0)));
    a.push(status);
    push("D-LATE-KEY-AFTER-RELEASE", a);

    // D-LATE-KEY-RESTART: claim, crash, reopen, status seats lost, save key refused, fresh dkg refused.
    let mut a = vec![open0(), claim_dkg(0)];
    a.extend(steps(6));
    a.push(Action::ProcessCrash { proc: 0 });
    a.push(open0());
    a.push(status);
    a.push(save_key(Some(0)));
    a.push(claim_dkg(1));
    push("D-LATE-KEY-AFTER-RESTART", a);

    // D-KEY-MISMATCH and D-NO-CLAIM: a bad key while live; a key without any claim.
    let mut a = vec![open0(), claim_dkg(0)];
    a.extend(steps(6));
    a.push(save_key(None));
    push("D-KEY-MISMATCH-WHILE-LIVE", a);
    push("D-KEY-WITHOUT-CLAIM", vec![open0(), save_key(Some(0)), status, load]);

    // D-KEYCRASH-<stage>: key commit interrupted at each stage, reopen, status, load.
    let names = ["before-write", "before-file-sync", "before-install", "before-directory-sync", "before-fence", "before-acknowledgement"];
    for (i, name) in names.iter().enumerate() {
        let mut a = vec![open0(), claim_dkg(0)];
        a.extend(steps(6));
        a.push(save_key(Some(0)));
        a.extend(steps(i));
        a.push(Action::ProcessCrash { proc: 0 });
        a.push(open0());
        a.push(status);
        a.push(load);
        a.push(save_key(Some(0)));
        push(&format!("D-KEYCRASH-{}", name.to_uppercase()), a);
    }

    // D-KEY-FENCE-LOST: lost response on the key commit; handle refuses; reopen decides.
    let mut a = vec![open0(), claim_dkg(0)];
    a.extend(steps(6));
    a.push(save_key(Some(0)));
    a.extend([Action::Step { proc: 0, step: Step::Write }, Action::Step { proc: 0, step: Step::FileSync }, Action::Step { proc: 0, step: Step::Install }, Action::Step { proc: 0, step: Step::DirSync }, Action::Step { proc: 0, step: Step::FenceLost }]);
    a.push(status);
    a.push(Action::Close { proc: 0 });
    a.push(open0());
    a.push(status);
    a.push(load);
    push("D-KEY-FENCE-LOST-RESPONSE", a);

    // D-WALLET-LIFECYCLE: candidate, pending, ready refused without key, key, ready, closed refused, identity change refused.
    let mut a = vec![open0(), wallet(1, 0, None)];
    a.extend(steps(6));
    a.push(wallet(2, 0, Some(0)));
    a.extend(steps(6));
    a.push(wallet(3, 0, Some(0)));
    a.push(claim_dkg(0));
    a.extend(steps(6));
    a.push(save_key(Some(0)));
    a.extend(steps(6));
    a.push(wallet(3, 0, Some(0)));
    a.extend(steps(6));
    a.push(wallet(4, 0, Some(0)));
    a.push(wallet(3, 1, Some(0)));
    a.push(wallet(3, 0, Some(1)));
    a.push(wallet(2, 0, Some(0)));
    a.push(wallet(3, 0, Some(0)));
    push("D-WALLET-LIFECYCLE", a);

    // D-WALLET-EXPIRY: pending then closed; reopen keeps closed; reopening to pending refused.
    let mut a = vec![open0(), wallet(2, 0, Some(0))];
    a.extend(steps(6));
    a.push(wallet(4, 0, Some(0)));
    a.extend(steps(6));
    a.push(Action::Close { proc: 0 });
    a.push(open0());
    a.push(wallet(2, 0, Some(0)));
    a.push(wallet(4, 0, Some(0)));
    push("D-WALLET-PENDING-EXPIRES-CLOSED", a);

    // D-WALLET-READY-KEY-MISMATCH: key identity 1, wallet identity 0 ready refused.
    let mut a = vec![open0(), claim_dkg(0)];
    a.extend(steps(6));
    a.push(save_key(Some(1)));
    a.extend(steps(6));
    a.push(wallet(3, 0, Some(0)));
    a.push(wallet(3, 1, Some(0)));
    push("D-WALLET-READY-REQUIRES-MATCHING-KEY", a);
    out
}
