//! Finite model of the K-08 host enforcement predicates (KC-VER rev 02, V-09).
//!
//! Separate from the Go code (R-01): nothing here imports or links keep-core.
//! The scope, the correspondence and the bounds are in `../SCOPE.md`; the
//! transition map is in `../TRANSITION-MAP.md`. Results are bounded model
//! checking of this model, connected to the code by trace replay. They are not
//! a proof of the Go source.
//!
//! Three profiles share the V-09 shape (SCOPE.md section 4):
//! - `hb`: heartbeat signer and transaction executor over one durable,
//!   exclusive claim set; chain ticks move the current heartbeat nonce.
//! - `enf`: one wallet, released signature A (this host) and U (unauthorized),
//!   two challenges, deadline at tick 3, crash/restart/restore, the defender.
//! - `claim`: three seats, threshold 2, heartbeat outcomes and C-09 acceptance.
//!
//! Store writes reuse the K-02 kernel's outcome vocabulary by copy: a write is
//! issued, then acknowledged; a process crash keeps an issued write (OS
//! caches), a host crash drops it; a stale restore makes the store refuse to
//! open (quarantined). Those are S-K02-HOST-4 guarantees (A-V09-STORE).
use serde::Serialize;
use stateright::{Checker, HasDiscoveries, Model, Property};
use std::{
    collections::{BTreeMap, BTreeSet, HashSet, VecDeque},
    fmt::Debug,
    fs,
    hash::Hash,
    time::{Duration, Instant},
};

const HB_DOMAIN: &str = "P-K08-HEARTBEAT-DOMAIN";
const EVIDENCE: &str = "P-K08-EVIDENCE-RETAINED";
const DEFENSE: &str = "P-K08-DEFENSE-BEFORE-DEADLINE";
const SUSPECT: &str = "P-K08-NO-SUSPECT-SANCTION";

// ---------------------------------------------------------------- hb profile

/// Message of a grant: 1..=3 is HB(n), the C-08 heartbeat digest of nonce n;
/// TX is the transaction digest of the one plan.
const TX: u8 = 9;
/// Attempt ids: 1..=3 heartbeat attempt of nonce n (heartbeat tag); 4 the
/// transaction attempt; 5..=7 the heartbeat attempt of nonce n-4 under the
/// transaction tag (SharedAttemptDomain only).
const TX_ATTEMPT: u8 = 4;

#[derive(Clone, Copy, Debug, Default, Hash, PartialEq, Eq, Serialize)]
struct Proc {
    /// 0 none, 1 constructed (nonce seen), 2 authorized, 3 intent recorded,
    /// 4 defense recorded, 5 claimed, 6 granted, 7 finished, 8 refused,
    /// 9 crashed.
    phase: u8,
    /// Heartbeat nonce fixed at construction (the runner's round anchor).
    n: u8,
    attempt: u8,
    /// Message the engine request carries for the claimed attempt.
    msg: u8,
}

#[derive(Clone, Debug, Default, Hash, PartialEq, Eq, Serialize)]
struct HbState {
    tick: u8,
    /// [heartbeat signer, transaction executor]
    procs: [Proc; 2],
    starts: [u8; 2],
    crashes: u8,
    misroutes: u8,
    /// Durable claim set (bit per attempt id), global to the store.
    claims: u8,
    /// Message granted per attempt id (0 none).
    grants: [u8; 8],
    /// The last crashed process's claimed attempt (ReuseAttempt only reads it).
    crashed_attempt: u8,
    /// Nonce of a heartbeat that crashed after its durable claim (0 none).
    hb_crashed_n: u8,
    bad_nonce: bool,
    bad_cross: bool,
    bad_reuse: bool,
    w_granted_current: bool,
    w_after_crash: bool,
    w_tx_granted: bool,
}

#[derive(Clone, Copy, Debug, Hash, PartialEq, Eq, Serialize)]
enum HbAction {
    Tick,
    Start(u8),
    Step(u8),
    Crash(u8),
    Misroute(u8),
}

#[derive(Clone, Copy, Debug)]
struct HbModel {
    control: &'static str,
}

impl HbModel {
    fn current(s: &HbState) -> u8 {
        s.tick + 1
    }
    fn step(&self, s: &mut HbState, p: usize) {
        let cur = Self::current(s);
        let mut q = s.procs[p];
        match q.phase {
            // Heartbeat: Runner.AuthorizeHeartbeat before the intent record.
            1 => {
                q.phase = if self.control == "AnyNonce" || q.n == cur { 2 } else { 8 }
            }
            2 => q.phase = 3, // RetainSigningRecord(intent)
            3 => q.phase = 4, // RetainSigningRecord(defense)
            4 => {
                // Guarded engine: store.Claim of the attempt, exclusive.
                let fresh = if p == 1 {
                    TX_ATTEMPT
                } else if self.control == "SharedAttemptDomain" {
                    4 + q.n
                } else {
                    q.n
                };
                let resume = self.control == "ReuseAttempt" && s.crashed_attempt != 0;
                let id = if resume { s.crashed_attempt } else { fresh };
                if !resume && s.claims & (1 << id) != 0 {
                    q.phase = 8; // ErrClaimed
                } else {
                    if resume {
                        s.crashed_attempt = 0;
                    }
                    s.claims |= 1 << id;
                    q.attempt = id;
                    q.msg = if p == 1 { TX } else { q.n };
                    q.phase = 5;
                }
            }
            5 => {
                // Final read after the durable claim (before-worker-start).
                let ok = if p == 0 {
                    let bound = self.control == "AcceptRawDigest" || q.msg == q.n;
                    let fresh = self.control == "AnyNonce" || q.n == cur;
                    bound && fresh
                } else {
                    q.msg == TX // finalAuthorization: request equals the expected one
                };
                if !ok {
                    q.phase = 8;
                } else {
                    let a = q.attempt as usize;
                    if s.grants[a] != 0 && s.grants[a] != q.msg {
                        s.bad_reuse = true;
                    }
                    s.grants[a] = q.msg;
                    let hb_attempt = q.attempt != TX_ATTEMPT;
                    if (hb_attempt && q.msg == TX) || (!hb_attempt && q.msg != TX) {
                        s.bad_cross = true;
                    }
                    if p == 0 && q.msg != TX && q.msg != cur {
                        s.bad_nonce = true;
                    }
                    if p == 0 && q.msg == cur && q.msg == q.n {
                        s.w_granted_current = true;
                        if s.hb_crashed_n != 0 && q.n > s.hb_crashed_n {
                            s.w_after_crash = true;
                        }
                    }
                    if p == 1 && q.msg == TX {
                        s.w_tx_granted = true;
                    }
                    q.phase = 6;
                }
            }
            6 => q.phase = 7,
            _ => {}
        }
        s.procs[p] = q;
    }
}

impl Model for HbModel {
    type State = HbState;
    type Action = HbAction;
    fn init_states(&self) -> Vec<HbState> {
        vec![HbState::default()]
    }
    fn actions(&self, s: &HbState, o: &mut Vec<HbAction>) {
        if s.tick < 2 {
            o.push(HbAction::Tick);
        }
        for p in 0..2u8 {
            let q = s.procs[p as usize];
            if matches!(q.phase, 0 | 7 | 8 | 9) && s.starts[p as usize] < 2 {
                o.push(HbAction::Start(p));
            }
            if (1..=6).contains(&q.phase) {
                o.push(HbAction::Step(p));
                if s.crashes < 2 {
                    o.push(HbAction::Crash(p));
                }
            }
            if q.phase == 5 && s.misroutes < 1 {
                o.push(HbAction::Misroute(p));
            }
        }
    }
    fn next_state(&self, s: &HbState, a: HbAction) -> Option<HbState> {
        let mut n = s.clone();
        match a {
            HbAction::Tick => n.tick += 1,
            HbAction::Start(p) => {
                let p = p as usize;
                n.starts[p] += 1;
                // The transaction executor's reservation and plan checks are V-04's.
                n.procs[p] = Proc { phase: if p == 0 { 1 } else { 2 }, n: Self::current(s), attempt: 0, msg: 0 };
            }
            HbAction::Step(p) => self.step(&mut n, p as usize),
            HbAction::Crash(p) => {
                let q = &mut n.procs[p as usize];
                if q.phase >= 5 {
                    n.crashed_attempt = q.attempt;
                    if p == 0 {
                        n.hb_crashed_n = q.n;
                    }
                }
                q.phase = 9;
                n.crashes += 1;
            }
            HbAction::Misroute(p) => {
                // Some host path hands the engine a request for this attempt
                // with the other purpose's message.
                let q = &mut n.procs[p as usize];
                q.msg = if p == 0 { TX } else { Self::current(s) };
                n.misroutes += 1;
            }
        }
        Some(n)
    }
    fn properties(&self) -> Vec<Property<Self>> {
        vec![
            Property::<Self>::always(HB_DOMAIN, |_, s| !s.bad_nonce && !s.bad_cross && !s.bad_reuse),
            Property::<Self>::sometimes("W-heartbeat-granted-current-nonce", |_, s| s.w_granted_current),
            Property::<Self>::sometimes("W-heartbeat-granted-after-crash-retry", |_, s| s.w_after_crash),
            Property::<Self>::sometimes("W-transaction-granted", |_, s| s.w_tx_granted),
        ]
    }
}

// --------------------------------------------------------------- enf profile

#[derive(Clone, Copy, Debug, Default, Hash, PartialEq, Eq, Serialize)]
struct Chal {
    admitted: bool,
    order: u8,
    /// 0 open, 1 defended, 2 timed out
    status: u8,
}

#[derive(Clone, Debug, Default, Hash, PartialEq, Eq, Serialize)]
struct EnfState {
    tick: u8,
    /// Kind of A: 1 heartbeat, 2 losing replacement sweep (never mined).
    kind: u8,
    /// Executor recovery is blocked (for example a stuck C-04 read).
    blocked: bool,
    /// Executor program counter: 0 not started, 1..=9 next stage, 10 finished, 20 dead.
    exec: u8,
    released: bool,
    settled: bool,
    /// Durable records of A: [intent, defense, done]; 0 absent, 1 issued, 2 acknowledged.
    rec: [u8; 3],
    up: bool,
    recovered: bool,
    quarantined: bool,
    crashes: u8,
    restores: u8,
    lost: u8,
    next_order: u8,
    /// [A, U]
    ch: [Chal; 2],
    /// Defender: 0 idle, 1 scanning, 2 before-defense.
    def_phase: u8,
    def_list: [u8; 2],
    /// `Resolved` as read by the pass's challenge snapshot (Defender.open).
    def_resolved: [bool; 2],
    def_len: u8,
    def_idx: u8,
    terminated: bool,
    restarted: bool,
    w_after_settle: bool,
    w_after_restart: bool,
    w_second: bool,
    w_lost_reply: bool,
}

#[derive(Clone, Copy, Debug, Hash, PartialEq, Eq, Serialize)]
enum Stage {
    IntentIssue,
    IntentAck,
    DefenseIssue,
    DefenseAck,
    Claim,
    Grant,
    Return,
    DoneIssue,
    DoneAck,
}

const NORMAL: [Stage; 9] = [
    Stage::IntentIssue,
    Stage::IntentAck,
    Stage::DefenseIssue,
    Stage::DefenseAck,
    Stage::Claim,
    Stage::Grant,
    Stage::Return,
    Stage::DoneIssue,
    Stage::DoneAck,
];
const AFTER_GRANT: [Stage; 9] = [
    Stage::IntentIssue,
    Stage::IntentAck,
    Stage::Claim,
    Stage::Grant,
    Stage::DefenseIssue,
    Stage::DefenseAck,
    Stage::Return,
    Stage::DoneIssue,
    Stage::DoneAck,
];

#[derive(Clone, Copy, Debug, Hash, PartialEq, Eq, Serialize)]
enum EnfAction {
    Tick,
    ExecStart,
    ExecStep(Stage),
    /// host = false: process crash (issued writes survive); true: host crash.
    Crash(bool),
    Restart,
    Recover,
    Restore,
    Settle,
    Challenge(u8),
    Timeout(u8),
    DefStart,
    DefNext,
    /// The defense transaction lands; `lost` loses its reply.
    DefSubmit(bool),
}

#[derive(Clone, Copy, Debug)]
struct EnfModel {
    control: &'static str,
}

impl EnfModel {
    fn stages(&self) -> &'static [Stage; 9] {
        if self.control == "RecordAfterGrant" {
            &AFTER_GRANT
        } else {
            &NORMAL
        }
    }
    fn abort_on_failure(&self) -> bool {
        self.control == "AbortOnFirstFailure"
    }
    fn host_only(a: &EnfAction) -> bool {
        matches!(
            a,
            EnfAction::Restart | EnfAction::Recover | EnfAction::DefStart | EnfAction::DefNext | EnfAction::DefSubmit(false)
        )
    }
    /// SC-08 conditional reachability, evaluated in one state: a defense of A
    /// is reachable by host-only actions (no tick, no fault, no other party).
    fn defense_reachable(&self, s: &EnfState) -> bool {
        let mut seen = HashSet::new();
        let mut queue = VecDeque::from([s.clone()]);
        while let Some(x) = queue.pop_front() {
            if x.ch[0].status == 1 {
                return true;
            }
            if !seen.insert(x.clone()) {
                continue;
            }
            let mut acts = vec![];
            self.actions(&x, &mut acts);
            for a in acts.into_iter().filter(Self::host_only) {
                if let Some(y) = self.next_state(&x, a) {
                    queue.push_back(y);
                }
            }
        }
        false
    }
    fn defense_due(s: &EnfState) -> bool {
        s.tick < 3 && !s.quarantined && s.ch[0].admitted && s.ch[0].status == 0 && s.rec[1] == 2
    }
    fn evidence_ok(s: &EnfState) -> bool {
        if s.quarantined || !matches!(s.exec, 10 | 20) {
            return true;
        }
        (!s.released || (s.rec[0] == 2 && s.rec[1] == 2)) && (s.exec != 10 || s.rec[2] == 2)
    }
    fn next_challenge(&self, n: &mut EnfState) {
        n.def_idx += 1;
        n.def_phase = 1;
    }
    fn fail_challenge(&self, n: &mut EnfState) {
        if self.abort_on_failure() {
            n.def_phase = 0;
        } else {
            self.next_challenge(n);
        }
    }
}

impl Model for EnfModel {
    type State = EnfState;
    type Action = EnfAction;
    fn init_states(&self) -> Vec<EnfState> {
        let mut out = vec![];
        for kind in [1, 2] {
            for blocked in [false, true] {
                out.push(EnfState { kind, blocked, up: true, def_list: [9, 9], ..Default::default() });
            }
        }
        out
    }
    fn actions(&self, s: &EnfState, o: &mut Vec<EnfAction>) {
        if s.tick < 3 {
            o.push(EnfAction::Tick);
        }
        if s.up {
            if s.exec == 0 {
                o.push(EnfAction::ExecStart);
            }
            if (1..=9).contains(&s.exec) {
                o.push(EnfAction::ExecStep(self.stages()[(s.exec - 1) as usize]));
            }
            if s.crashes < 2 {
                o.push(EnfAction::Crash(false));
                o.push(EnfAction::Crash(true));
            }
            if !s.blocked && !s.recovered {
                o.push(EnfAction::Recover);
            }
            if s.def_phase == 0
                && (s.ch[0].admitted || s.ch[1].admitted)
                && (self.control != "DefendAfterRecovery" || s.recovered)
            {
                o.push(EnfAction::DefStart);
            }
            if s.def_phase == 1 {
                o.push(EnfAction::DefNext);
            }
            if s.def_phase == 2 {
                o.push(EnfAction::DefSubmit(false));
                if s.lost < 1 {
                    o.push(EnfAction::DefSubmit(true));
                }
            }
        } else {
            o.push(EnfAction::Restart);
        }
        if s.restores < 1 {
            o.push(EnfAction::Restore);
        }
        if s.released && !s.settled {
            o.push(EnfAction::Settle);
        }
        for c in 0..2u8 {
            let ch = s.ch[c as usize];
            if s.tick < 3 && !ch.admitted && (c == 1 || s.released) {
                o.push(EnfAction::Challenge(c));
            }
            if s.tick >= 3 && ch.admitted && ch.status == 0 {
                o.push(EnfAction::Timeout(c));
            }
        }
    }
    fn next_state(&self, s: &EnfState, a: EnfAction) -> Option<EnfState> {
        let mut n = s.clone();
        match a {
            EnfAction::Tick => n.tick += 1,
            EnfAction::ExecStart => n.exec = 1,
            EnfAction::ExecStep(stage) => {
                match stage {
                    Stage::IntentIssue => n.rec[0] = 1,
                    Stage::IntentAck => n.rec[0] = 2,
                    Stage::DefenseIssue => n.rec[1] = 1,
                    Stage::DefenseAck => n.rec[1] = 2,
                    Stage::Claim | Stage::Return => {}
                    Stage::Grant => n.released = true,
                    Stage::DoneIssue => n.rec[2] = 1,
                    Stage::DoneAck => n.rec[2] = 2,
                }
                n.exec += 1;
            }
            EnfAction::Crash(host) => {
                n.up = false;
                n.recovered = false;
                n.def_phase = 0;
                n.def_list = [9, 9];
                n.def_resolved = [false, false];
                n.def_len = 0;
                n.def_idx = 0;
                if (1..=9).contains(&n.exec) {
                    n.exec = 20;
                }
                for r in &mut n.rec {
                    if *r == 1 {
                        *r = if host { 0 } else { 2 };
                    }
                }
                n.crashes += 1;
            }
            EnfAction::Restart => {
                n.up = true;
                n.restarted = true;
            }
            EnfAction::Recover => n.recovered = true,
            EnfAction::Restore => {
                n.quarantined = true;
                n.restores += 1;
            }
            EnfAction::Settle => {
                n.settled = true;
                if self.control == "DeleteAtSettlement" && n.up {
                    n.rec = [0, 0, 0];
                }
            }
            EnfAction::Challenge(c) => {
                let ch = &mut n.ch[c as usize];
                ch.admitted = true;
                ch.order = n.next_order;
                n.next_order += 1;
            }
            EnfAction::Timeout(c) => {
                n.ch[c as usize].status = 2;
                n.terminated = true;
            }
            EnfAction::DefStart => {
                let mut list: Vec<u8> = (0..2u8).filter(|c| n.ch[*c as usize].admitted).collect();
                list.sort_by_key(|c| n.ch[*c as usize].order);
                n.def_len = list.len() as u8;
                n.def_list = [9, 9];
                n.def_resolved = [false, false];
                for (i, c) in list.into_iter().enumerate() {
                    n.def_list[i] = c;
                    n.def_resolved[i] = n.ch[c as usize].status != 0;
                }
                n.def_idx = 0;
                n.def_phase = 1;
            }
            EnfAction::DefNext => {
                if n.def_idx >= n.def_len {
                    n.def_phase = 0;
                    n.def_list = [9, 9];
                    n.def_resolved = [false, false];
                    n.def_len = 0;
                    n.def_idx = 0;
                } else {
                    let c = n.def_list[n.def_idx as usize];
                    if n.def_resolved[n.def_idx as usize] {
                        self.next_challenge(&mut n); // resolved in the pass's snapshot
                    } else if n.quarantined {
                        self.fail_challenge(&mut n); // record read fails closed
                    } else if c == 0 && n.rec[1] == 2 {
                        n.def_phase = 2; // defense-record-read, before-defense
                    } else {
                        // Not ours: no defense record (U, or A without one).
                        if self.abort_on_failure() {
                            n.def_phase = 0;
                        } else {
                            self.next_challenge(&mut n);
                        }
                    }
                }
            }
            EnfAction::DefSubmit(lost) => {
                let c = n.def_list[n.def_idx as usize] as usize;
                if n.ch[c].status == 0 {
                    let reverts = self.control == "ProvenOnly" && n.kind == 2;
                    if reverts {
                        self.fail_challenge(&mut n);
                        return Some(n);
                    }
                    n.ch[c].status = 1;
                    if n.settled {
                        n.w_after_settle = true;
                    }
                    if n.restarted && n.tick < 3 {
                        n.w_after_restart = true;
                    }
                    if n.ch[1].admitted && n.ch[1].order < n.ch[0].order && n.ch[1].status == 0 {
                        n.w_second = true;
                    }
                    if lost {
                        n.lost += 1;
                        n.w_lost_reply = true;
                    }
                }
                // Reply or reread: resolved, continue with the next challenge.
                self.next_challenge(&mut n);
            }
        }
        Some(n)
    }
    fn properties(&self) -> Vec<Property<Self>> {
        vec![
            Property::<Self>::always(EVIDENCE, |_, s| EnfModel::evidence_ok(s)),
            Property::<Self>::always(DEFENSE, |m, s| !EnfModel::defense_due(s) || m.defense_reachable(s)),
            Property::<Self>::sometimes("W-heartbeat-signed-challenged-defended", |_, s| s.kind == 1 && s.ch[0].status == 1),
            Property::<Self>::sometimes("W-losing-replacement-defended-after-refund", |_, s| s.kind == 2 && s.w_after_settle),
            Property::<Self>::sometimes("W-defense-after-restart-before-deadline", |_, s| s.w_after_restart),
            Property::<Self>::sometimes("W-first-not-ours-second-defended", |_, s| s.w_second),
            Property::<Self>::sometimes("W-defense-reply-lost-settled-by-reread", |_, s| s.w_lost_reply),
            Property::<Self>::sometimes("W-unauthorized-timed-out-terminated", |_, s| s.ch[1].status == 2 && s.terminated),
        ]
    }
}

// ------------------------------------------------------------- claim profile

#[derive(Clone, Copy, Debug, Default, Hash, PartialEq, Eq, Serialize)]
struct RoundRec {
    /// Host of seat 1 and seat 2: 0 down (no start record), 1 outcome recorded,
    /// 2 start record but no outcome (crashed in the round).
    s: [u8; 2],
    live3: bool,
    /// Ground truth: the heartbeat signature was produced.
    signed: bool,
    /// Each host's recorded `Signed`.
    sig: [bool; 2],
    /// A worker error carried `Suspect` = seat 3 to the unsigned hosts.
    suspect: bool,
}

impl RoundRec {
    fn present(&self) -> [bool; 3] {
        [self.s[0] != 0, self.s[1] != 0, self.live3]
    }
    fn full(&self) -> bool {
        self.present().iter().all(|p| *p)
    }
}

#[derive(Clone, Debug, Default, Hash, PartialEq, Eq, Serialize)]
struct ClaimState {
    rounds: u8,
    rec: [RoundRec; 4],
    accepted: bool,
    claimed: u8,
    windows: [u8; 2],
    /// Each host's derivation after the last round (claimed seat mask, 0 none).
    derived: [u8; 2],
    bad: bool,
    w_real: bool,
    w_unknown_ends: bool,
}

#[derive(Clone, Copy, Debug, Hash, PartialEq, Eq, Serialize)]
enum ClaimAction {
    Round(RoundRec),
    Claim,
}

#[derive(Clone, Copy, Debug)]
struct ClaimModel {
    control: &'static str,
    rounds: u8,
    failures: u8,
}

/// Result of one host's derivation: claimed seat mask and counted window.
struct Derived {
    claim: Option<(u8, u8)>,
    unknown_break: bool,
}

impl ClaimModel {
    /// enforcement.Claimer.Derive with MinimumActivePercent = 70 over three seats.
    fn derive(&self, s: &ClaimState, h: usize) -> Derived {
        let need = if self.control == "SingleFailureClaim" { 1 } else { self.failures };
        let mut window = 0u8;
        let mut count = 0u8;
        let mut absent = 0b111u8;
        let mut unknown_break = false;
        for r in (0..s.rounds as usize).rev() {
            if count >= need {
                break;
            }
            let rr = s.rec[r];
            match rr.s[h] {
                0 => continue,
                2 => {
                    if self.control == "SkipUnknownRound" {
                        continue;
                    }
                    unknown_break = count > 0;
                    break;
                }
                _ => {}
            }
            let mut present = rr.present();
            if self.control == "ClaimFromSuspect" && rr.suspect && !rr.sig[h] {
                present[2] = false;
            }
            let k = present.iter().filter(|p| **p).count();
            let failure = k * 100 < 70 * 3;
            if !failure && rr.sig[h] {
                break;
            }
            if failure {
                window |= 1 << r;
                count += 1;
                for (i, p) in present.iter().enumerate() {
                    if *p {
                        absent &= !(1 << i);
                    }
                }
            }
        }
        let claim = if count < need || absent == 0 { None } else { Some((absent, window)) };
        Derived { claim, unknown_break }
    }
    fn judge(&self, s: &ClaimState, claimed: u8, windows: [u8; 2]) -> bool {
        for h in 0..2 {
            let w = windows[h];
            if w.count_ones() < self.failures as u32 {
                return false;
            }
            let lo = w.trailing_zeros() as usize;
            let hi = 7 - w.leading_zeros() as usize;
            for r in lo..=hi {
                let rr = s.rec[r];
                if w & (1 << r) != 0 {
                    for i in 0..3 {
                        if claimed & (1 << i) != 0 && rr.present()[i] {
                            return false; // claimed seat was authenticated present
                        }
                    }
                }
                if rr.s[h] == 2 || (rr.s[h] == 1 && rr.full() && rr.sig[h]) {
                    return false; // window spans a possibly or surely signed heartbeat
                }
            }
        }
        true
    }
}

impl Model for ClaimModel {
    type State = ClaimState;
    type Action = ClaimAction;
    fn init_states(&self) -> Vec<ClaimState> {
        vec![ClaimState::default()]
    }
    fn actions(&self, s: &ClaimState, o: &mut Vec<ClaimAction>) {
        if s.rounds < self.rounds {
            for s0 in 0..3u8 {
                for s1 in 0..3u8 {
                    for live3 in [false, true] {
                        let base = RoundRec { s: [s0, s1], live3, ..Default::default() };
                        let k = base.present().iter().filter(|p| **p).count();
                        for signed in [false, true] {
                            if signed && k < 2 {
                                continue;
                            }
                            for sig0 in [false, true] {
                                for sig1 in [false, true] {
                                    let sig = [sig0, sig1];
                                    if (0..2).any(|h| sig[h] && (!signed || base.s[h] != 1)) {
                                        continue;
                                    }
                                    let unsigned_up = (0..2).any(|h| base.s[h] == 1 && !sig[h]);
                                    for suspect in [false, true] {
                                        if suspect && !unsigned_up {
                                            continue;
                                        }
                                        o.push(ClaimAction::Round(RoundRec { signed, sig, suspect, ..base }));
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
        if !s.accepted && s.rounds > 0 {
            let (a, b) = (self.derive(s, 0), self.derive(s, 1));
            if let (Some((m0, _)), Some((m1, _))) = (a.claim, b.claim) {
                // C-09: threshold (2) signers outside the claimed set; only the
                // hosts of seats 1 and 2 keep records, each signs only its own derivation.
                if m0 == m1 && m0 & 0b011 == 0 {
                    o.push(ClaimAction::Claim);
                }
            }
        }
    }
    fn next_state(&self, s: &ClaimState, a: ClaimAction) -> Option<ClaimState> {
        let mut n = s.clone();
        match a {
            ClaimAction::Round(r) => {
                n.rec[n.rounds as usize] = r;
                n.rounds += 1;
                for h in 0..2 {
                    let d = self.derive(&n, h);
                    if d.unknown_break {
                        n.w_unknown_ends = true;
                    }
                    n.derived[h] = d.claim.map(|(m, _)| m).unwrap_or(0);
                }
            }
            ClaimAction::Claim => {
                let (m, w0) = self.derive(s, 0).claim.unwrap();
                let (_, w1) = self.derive(s, 1).claim.unwrap();
                n.accepted = true;
                n.claimed = m;
                n.windows = [w0, w1];
                if self.judge(s, m, [w0, w1]) {
                    n.w_real = m == 0b100;
                } else {
                    n.bad = true;
                }
            }
        }
        Some(n)
    }
    fn properties(&self) -> Vec<Property<Self>> {
        vec![
            Property::<Self>::always(SUSPECT, |_, s| !s.bad),
            Property::<Self>::sometimes("W-real-inactivity-claim-names-absent-seat", |_, s| s.w_real),
            Property::<Self>::sometimes("W-started-round-without-outcome-ends-window", |_, s| s.w_unknown_ends),
        ]
    }
}

// ------------------------------------------------------------------- runner

fn target(control: &str) -> Option<&'static str> {
    match control {
        "AcceptRawDigest" | "ReuseAttempt" | "AnyNonce" | "SharedAttemptDomain" => Some(HB_DOMAIN),
        "DeleteAtSettlement" | "RecordAfterGrant" => Some(EVIDENCE),
        "DefendAfterRecovery" | "ProvenOnly" | "AbortOnFirstFailure" => Some(DEFENSE),
        "ClaimFromSuspect" | "SingleFailureClaim" | "SkipUnknownRound" => Some(SUSPECT),
        _ => None,
    }
}

/// A weakening expected to be masked by other guards (SCOPE.md, Q3).
fn masked(control: &str) -> bool {
    control == "SharedAttemptDomain"
}

fn split(v: serde_json::Value) -> (String, serde_json::Value) {
    if let Some(o) = v.as_object() {
        let (k, f) = o.iter().next().unwrap();
        (k.clone(), f.clone())
    } else {
        (v.as_str().unwrap().to_owned(), serde_json::Value::Null)
    }
}

const SHORTEST_LIMIT: usize = 2_000_000;

/// Breadth-first shortest action path from an initial state to a state where
/// `cond` holds.
fn shortest<M>(m: &M, cond: fn(&M, &M::State) -> bool) -> Option<Vec<M::Action>>
where
    M: Model,
    M::State: Clone + Hash + Eq,
    M::Action: Clone,
{
    let mut parent: std::collections::HashMap<M::State, Option<(M::State, M::Action)>> = Default::default();
    let mut queue = VecDeque::new();
    for s in m.init_states() {
        if !parent.contains_key(&s) {
            parent.insert(s.clone(), None);
            queue.push_back(s);
        }
    }
    while let Some(s) = queue.pop_front() {
        if cond(m, &s) {
            let mut acts = vec![];
            let mut cur = s;
            while let Some(Some((prev, a))) = parent.get(&cur).cloned() {
                acts.push(a);
                cur = prev;
            }
            acts.reverse();
            return Some(acts);
        }
        let mut acts = vec![];
        m.actions(&s, &mut acts);
        for a in acts {
            if let Some(n) = m.next_state(&s, a.clone()) {
                if !parent.contains_key(&n) {
                    parent.insert(n.clone(), Some((s.clone(), a)));
                    queue.push_back(n);
                }
            }
        }
    }
    None
}

#[allow(clippy::too_many_arguments)]
fn check<M>(out: &str, profile: &str, bound: &str, m: M, claim: M, control: &'static str, safety: &[&'static str], deadline: u64) -> serde_json::Value
where
    M: Model + Clone + Send + Sync + 'static,
    M::State: Serialize + Clone + Debug + Hash + Eq + Send + Sync,
    M::Action: Serialize + Clone + Debug + PartialEq + Send + Sync,
{
    let start = Instant::now();
    let mut b = m.clone().checker().threads(4).timeout(Duration::from_secs(deadline));
    let tgt = target(control);
    if let Some(t) = tgt.filter(|_| !masked(control)) {
        b = b.finish_when(HasDiscoveries::AllOf(BTreeSet::from([t])));
    }
    let c = b.spawn_bfs().join();
    let seconds = start.elapsed().as_secs_f64();
    let complete = c.is_done() && seconds < deadline as f64;
    let mut found = BTreeMap::new();
    let mut traces = vec![];
    for p in m.properties() {
        let path = c.discovery(p.name);
        found.insert(p.name, path.is_some());
        let keep = control == "None" || Some(p.name) == tgt;
        let Some(path) = path.filter(|_| keep) else { continue };
        let mut acts: Vec<M::Action> = path.into_actions();
        // A witness trace is replaced by a shortest path when the space is
        // small enough for a single-threaded search (readable replay).
        if p.name.starts_with("W-") && c.unique_state_count() <= SHORTEST_LIMIT {
            if let Some(short) = shortest(&m, p.condition) {
                acts = short;
            }
        }
        let inits = m.init_states();
        // Re-execute from the initial state the path starts from.
        let mut state = None;
        for init in inits {
            let mut x = init.clone();
            let mut ok = true;
            for a in &acts {
                let mut offered = vec![];
                m.actions(&x, &mut offered);
                if !offered.contains(a) {
                    ok = false;
                    break;
                }
                x = m.next_state(&x, a.clone()).unwrap();
            }
            if ok {
                state = Some(init);
                break;
            }
        }
        let init = state.expect("trace start");
        // Cut the path at the first state where the discovery holds: the
        // multi-threaded search can report a later state of the same path.
        let always = matches!(p.expectation, stateright::Expectation::Always);
        let holds = |x: &M::State| if always { !(p.condition)(&m, x) } else { (p.condition)(&m, x) };
        let mut x = init.clone();
        let mut cut = acts.len();
        for (k, a) in acts.iter().enumerate() {
            x = m.next_state(&x, a.clone()).unwrap();
            if holds(&x) {
                cut = k + 1;
                break;
            }
        }
        let acts = &acts[..cut];
        let mut cur = init.clone();
        let mut prefix = init.clone();
        let mut diverged = false;
        let mut steps = vec![];
        let mut claim_steps = vec![];
        for (k, a) in acts.iter().enumerate() {
            cur = m.next_state(&cur, a.clone()).unwrap();
            let mut offered_here = false;
            let next = if diverged {
                None
            } else {
                let mut offered = vec![];
                claim.actions(&prefix, &mut offered);
                if offered.contains(a) {
                    offered_here = true;
                    claim.next_state(&prefix, a.clone())
                } else {
                    None
                }
            };
            let first = !diverged && next.as_ref() != Some(&cur);
            if let Some(v) = next {
                prefix = v;
            }
            if first {
                diverged = true;
            }
            let (name, fields) = split(serde_json::to_value(a).unwrap());
            steps.push(serde_json::json!({"n":k+1,"action":name,"fields":fields,"expect":"transition","expect_state":cur,"diverges":first}));
            if !diverged || first {
                claim_steps.push(serde_json::json!({"n":k+1,"action":name,"fields":fields,"offered":offered_here,"expect":if first {"refused-or-different-state"} else {"transition"},"expect_state":prefix,"diverges":first}));
            }
        }
        let id = format!("{profile}-{control}-{}", p.name);
        let trace = serde_json::json!({"id":id,"profile":profile,"bound":bound,"kind":if p.name.starts_with("W-"){"witness"}else{"counterexample"},"property":p.name,"weakening":control,"init":init,"replay":"pending","replay_note":"set by the Go replay report","steps":steps,"claim_model_steps":claim_steps});
        fs::write(format!("{out}/traces/{id}.json"), serde_json::to_vec_pretty(&trace).unwrap()).unwrap();
        traces.push(id);
    }
    // R-07 "exactly its predicate": explore the weakened model exhaustively
    // (no early stop) and record every other safety property it breaks.
    let mut exact = serde_json::Value::Null;
    let mut exact_ok = true;
    if let Some(t) = tgt.filter(|_| !masked(control)) {
        let s2 = Instant::now();
        let c2 = m.clone().checker().threads(4).timeout(Duration::from_secs(deadline)).spawn_bfs().join();
        let secs = s2.elapsed().as_secs_f64();
        let others: Vec<&str> = safety.iter().copied().filter(|p| *p != t && c2.discovery(p).is_some()).collect();
        let done = c2.is_done() && secs < deadline as f64;
        exact_ok = done && others.is_empty() && c2.discovery(t).is_some();
        exact = serde_json::json!({"exhaustive_completed_before_deadline":done,"seconds":secs,"unique_states":c2.unique_state_count(),"other_safety_broken":others});
    }
    let witnesses_ok = found.iter().filter(|(p, _)| p.starts_with("W-")).all(|(_, v)| *v);
    let pass = complete
        && match tgt {
            None => safety.iter().all(|p| !found[*p]) && witnesses_ok,
            Some(t) if masked(control) => !found[t],
            Some(t) => found[t] && exact_ok,
        };
    let r = serde_json::json!({"profile":profile,"bound":bound,"control":control,"target":tgt,"expected":if masked(control){"no counterexample (masked)"} else if tgt.is_some(){"counterexample"} else {"no counterexample, all witnesses"},"completed_before_deadline":complete,"deadline_seconds":deadline,"seconds":seconds,"unique_states":c.unique_state_count(),"generated_states":c.state_count(),"max_depth":c.max_depth(),"discoveries":found,"traces":traces,"exactness":exact,"pass":pass});
    eprintln!("{r}");
    r
}

fn main() {
    let args: Vec<String> = std::env::args().collect();
    let out = args.get(1).expect("output directory").clone();
    let profile = args.get(2).expect("profile: hb | enf | claim | claim-f3").clone();
    let deadline: u64 = std::env::var("V09_DEADLINE_SECONDS").ok().and_then(|v| v.parse().ok()).unwrap_or(800);
    fs::create_dir_all(format!("{out}/traces")).unwrap();
    let mut reports = vec![];
    match profile.as_str() {
        "hb" => {
            for control in ["None", "AcceptRawDigest", "ReuseAttempt", "AnyNonce", "SharedAttemptDomain"] {
                reports.push(check(&out, "hb", "nonces 1..3, 2 heartbeat starts, 2 transaction starts, 2 crashes, 1 misroute", HbModel { control }, HbModel { control: "None" }, control, &[HB_DOMAIN], deadline));
            }
        }
        "enf" => {
            for control in ["None", "DeleteAtSettlement", "RecordAfterGrant", "DefendAfterRecovery", "ProvenOnly", "AbortOnFirstFailure"] {
                reports.push(check(&out, "enf", "1 wallet, A + U, 2 challenges, deadline tick 3, 2 crashes, 1 restore, 1 lost reply", EnfModel { control }, EnfModel { control: "None" }, control, &[EVIDENCE, DEFENSE], deadline));
            }
        }
        "claim" | "claim-f3" => {
            let (rounds, failures) = if profile == "claim" { (3, 2) } else { (4, 3) };
            let bound = format!("3 seats, threshold 2, {rounds} rounds, F = {failures}");
            for control in ["None", "ClaimFromSuspect", "SingleFailureClaim", "SkipUnknownRound"] {
                reports.push(check(&out, &profile, &bound, ClaimModel { control, rounds, failures }, ClaimModel { control: "None", rounds, failures }, control, &[SUSPECT], deadline));
            }
        }
        _ => panic!("unknown profile"),
    }
    let ok = reports.iter().all(|r| r["pass"].as_bool() == Some(true));
    fs::write(format!("{out}/model-results-{profile}.json"), serde_json::to_vec_pretty(&reports).unwrap()).unwrap();
    if !ok {
        std::process::exit(1)
    }
}
