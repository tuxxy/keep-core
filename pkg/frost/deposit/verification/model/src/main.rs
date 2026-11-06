//! V-07 deposits, sweeps and independent refunds (KC-VER rev 02, section 3).
//!
//! A finite Stateright model of the K-06 host (planner, view/authority, main
//! purpose lock, grant guard, sweeper and executor stages, resolution
//! attester), the SPV maintainer, the depositor, an abstract C-05/C-04 chain
//! and an abstract Bitcoin. It never imports production code (R-01). Guards
//! appear only where the code has them (R-02); `SCOPE.md` maps each one.
//! Safety is `always`, progress is `sometimes` (R-03).

use serde::Serialize;
use stateright::{Checker, Model, Property};
use std::{
    collections::{BTreeMap, BTreeSet},
    fs,
    time::{Duration, Instant},
};

const AUTH: &str = "P-K06-SWEEP-AUTHORIZED-ONLY";
const MAIN: &str = "P-K06-ONE-MAIN-PURPOSE";
const REFUND: &str = "P-K06-REFUND-RACE";
const CREDIT: &str = "P-K06-CREDIT-ONCE";
const TERMINAL: &str = "P-K06-TERMINAL-SWEEP-ATTEMPT";
/// Proposed, not in KC-VER: the D13 refinement (DECISIONS.md, 30 October 2026).
const D13: &str = "P-K06-D13-NO-SIDE-SWEEP";
const KCVER: [&str; 5] = [AUTH, MAIN, REFUND, CREDIT, TERMINAL];

// Bounds (KC-VER V-07): refund locktime at tick 2, ticks 0..=2, margin one
// tick (D-09 test value, OPEN), two deposits, one reservation, two attempt
// slots, rounds 0..=1.
const LOCKTIME: u8 = 2;
const MARGIN: u8 = 1;
const MAX_TICK: u8 = 2;
const MAX_ROUND: u8 = 1;
const DEPOSITS: usize = 2;
const SLOTS: usize = 2;
/// Main output ids: 1 is the initial main M0, 2 the output that spends it
/// (M1), 3 the next one (M2). 0 is "no main".
const M0: u8 = 1;
const MAINS: usize = 4;

// Reveal states.
const ABSENT: u8 = 0;
const UNFINAL: u8 = 1;
const FINAL: u8 = 2;
const ORPHANED: u8 = 3;
// Reservation states (C-04, abstract).
const R_NONE: u8 = 0;
const R_UNFINAL: u8 = 1;
const R_RESERVED: u8 = 2;
const R_PAID: u8 = 3;
const R_SWEPT: u8 = 4;
// Bitcoin spender codes.
const UNSPENT: u8 = 0;
const BY_REFUND: u8 = 1;
const BY_PAYMENT: u8 = 2;
const BY_SWEEP: u8 = 16; // + plan id
// Lock purposes (VD-07).
const FREE: u8 = 0;
const REDEMPTION: u8 = 1;
const RESOLUTION: u8 = 2;
const SWEEP: u8 = 3;
const CLOSED: u8 = 4;
// Attempt stages (the executor and sweeper boundaries, in order).
const PLANNED: u8 = 0; // before-sweep-plan .. transaction plan built
const LOCKED: u8 = 1; // main-lock-acquired / after-resolution-attest
const ATTEMPT: u8 = 2; // sweep-attempt ledger entry (before-sign)
const INTENT: u8 = 3; // before-intent-record: authority + permit passed, intent retained
const CLAIMED: u8 = 4; // store attempt claim (engine)
const GRANTED: u8 = 5; // before-worker-start: authority + Guard.Grant passed
const RETAINED: u8 = 6; // after-signature: done record retained
const RESULT: u8 = 7; // before-done-accept passed, result record durable
const SIGNED: u8 = 8; // sweep-signed marker

#[derive(Clone, Copy, Debug, Default, Hash, PartialEq, Eq, Serialize, PartialOrd, Ord)]
struct Plan {
    deps: u8,
    main: u8,
    res: bool,
    over: bool,
}
impl Plan {
    fn id(&self) -> u8 {
        self.deps | (self.main << 2) | ((self.res as u8) << 4) | ((self.over as u8) << 5)
    }
    fn has(&self, d: usize) -> bool {
        self.deps & (1 << d) != 0
    }
}

#[derive(Clone, Copy, Debug, Default, Hash, PartialEq, Eq, Serialize)]
struct Deposit {
    reveal: u8,
    swept: bool,
    credits: u8,
    spender: u8,
}

#[derive(Clone, Copy, Debug, Default, Hash, PartialEq, Eq, Serialize)]
struct Lock {
    purpose: u8,
    plans: u64,
    until: u8,
}

#[derive(Clone, Copy, Debug, Default, Hash, PartialEq, Eq, Serialize)]
struct Attempt {
    used: bool,
    plan: Plan,
    round: u8,
    stage: u8,
    terminal: bool,
    failed: bool,
    ledger: bool,
    claimed: bool,
    granted: bool,
    retained: bool,
    accepted: bool,
    late: bool,
    marker: bool,
    broadcast: bool,
    resubmit: bool,
}

#[derive(Clone, Debug, Default, Hash, PartialEq, Eq, Serialize)]
struct State {
    tick: u8,
    round: u8,
    up: bool,
    deposits: [Deposit; DEPOSITS],
    res: u8,
    cert_host: bool,
    cert_foreign: bool,
    cert_until: u8,
    attested: bool,
    custody_main: u8,
    custody_spent: [bool; MAINS],
    extra: u8,
    main_exists: [bool; MAINS],
    main_spender: [u8; MAINS],
    locks: [Lock; MAINS],
    pay_granted: bool,
    pay_mined: bool,
    mempool: u64,
    mined: u64,
    sweep_granted_main: [bool; MAINS],
    attempts: [Attempt; SLOTS],
    bad_auth: bool,
    bad_main: bool,
    bad_refund: bool,
    bad_terminal: bool,
    bad_d13: bool,
    w_new_main: bool,
    w_refund_down: bool,
    w_lost: bool,
    w_after_locktime: bool,
}

#[derive(Clone, Copy, Debug, Hash, PartialEq, Eq, Serialize)]
enum Action {
    Reveal(u8),
    FinalizeReveal(u8),
    OrphanReveal(u8),
    Tick,
    NextRound,
    ForeignCertificate,
    Admit,
    FinalizeAdmission,
    OrphanAdmission,
    Attest,
    ReleaseUnadmitted,
    PayGrant,
    MinePayment,
    ProvePayment,
    Close(u8),
    Plan(u8, bool),
    PlanResolution(u8),
    Retry(u8),
    Step(u8),
    WorkerFail(u8),
    AcceptReadFail(u8),
    MarkerFail(u8, bool),
    ResultFail(u8, bool),
    Broadcast(u8),
    MineSweep(u8),
    ProveSweep(u8, bool),
    Resubmit(u8),
    Refund(u8),
    Crash,
    Restart,
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Control {
    None,
    SignWithoutRevealFinality,
    PlanTimeMarginOnly,
    AcceptFeeAboveCap,
    DropMainLock,
    LockFromUnfinalizedReservation,
    NoRefundMargin,
    AcceptLateSweepSignature,
    ForgetConsumptionOnRetry,
    ReuseSweepAttempt,
    TrustLedgerMarkerOnly,
}
impl Control {
    const ALL: [Control; 11] = [
        Control::None,
        Control::SignWithoutRevealFinality,
        Control::PlanTimeMarginOnly,
        Control::AcceptFeeAboveCap,
        Control::DropMainLock,
        Control::LockFromUnfinalizedReservation,
        Control::NoRefundMargin,
        Control::AcceptLateSweepSignature,
        Control::ForgetConsumptionOnRetry,
        Control::ReuseSweepAttempt,
        Control::TrustLedgerMarkerOnly,
    ];
    fn name(self) -> &'static str {
        match self {
            Control::None => "None",
            Control::SignWithoutRevealFinality => "SignWithoutRevealFinality",
            Control::PlanTimeMarginOnly => "PlanTimeMarginOnly",
            Control::AcceptFeeAboveCap => "AcceptFeeAboveCap",
            Control::DropMainLock => "DropMainLock",
            Control::LockFromUnfinalizedReservation => "LockFromUnfinalizedReservation",
            Control::NoRefundMargin => "NoRefundMargin",
            Control::AcceptLateSweepSignature => "AcceptLateSweepSignature",
            Control::ForgetConsumptionOnRetry => "ForgetConsumptionOnRetry",
            Control::ReuseSweepAttempt => "ReuseSweepAttempt",
            Control::TrustLedgerMarkerOnly => "TrustLedgerMarkerOnly",
        }
    }
    fn target(self) -> &'static str {
        match self {
            Control::None => "",
            Control::SignWithoutRevealFinality | Control::PlanTimeMarginOnly | Control::AcceptFeeAboveCap => AUTH,
            Control::DropMainLock | Control::LockFromUnfinalizedReservation => MAIN,
            Control::NoRefundMargin | Control::AcceptLateSweepSignature => REFUND,
            Control::ForgetConsumptionOnRetry => CREDIT,
            Control::ReuseSweepAttempt | Control::TrustLedgerMarkerOnly => TERMINAL,
        }
    }
}

struct DepositModel {
    control: Control,
}

impl DepositModel {
    fn c(&self, k: Control) -> bool {
        self.control == k
    }
    /// The true refund margin at MTP `tick` (the observers always use it).
    fn before_margin(tick: u8) -> bool {
        tick + MARGIN < LOCKTIME
    }
    /// The margin the host code applies (NoRefundMargin sets it to zero).
    fn host_margin(&self) -> u8 {
        if self.c(Control::NoRefundMargin) {
            0
        } else {
            MARGIN
        }
    }
    fn res_active(s: &State) -> bool {
        s.res == R_UNFINAL || s.res == R_RESERVED
    }
    /// The host's own attestation can still be admitted or is admitted and
    /// unresolved (VD-07 reading 1).
    fn attestation_live(s: &State) -> bool {
        s.attested && (Self::res_active(s) || (s.res == R_NONE && s.tick <= s.cert_until))
    }
    fn lock_used(&self) -> bool {
        !self.c(Control::DropMainLock)
    }

    /// `Planner.Build` (with `Pending`, `eligible`, `Synced`, `main`) and
    /// `signing.NewSweepPlan`'s cap check.
    fn plan(&self, s: &State, over: bool, resolution: bool) -> Option<Plan> {
        // Synced: a reserved, unswept deposit spent on Bitcoin before its
        // locktime is an unproven wallet sweep.
        for d in &s.deposits {
            if (d.reveal == UNFINAL || d.reveal == FINAL) && !d.swept && d.spender >= BY_SWEEP {
                return None;
            }
        }
        let mut deps = 0;
        for (i, d) in s.deposits.iter().enumerate() {
            // Over-approximation: the planner's finality is relative to its
            // anchor, which may be unfinal (SCOPE.md).
            let present = d.reveal == UNFINAL || d.reveal == FINAL;
            if present && !d.swept && d.spender == UNSPENT && s.tick + self.host_margin() < LOCKTIME {
                deps |= 1 << i;
            }
        }
        if deps == 0 {
            return None;
        }
        let cm = s.custody_main;
        let main = if resolution {
            if !Self::res_active(s) || cm == 0 {
                return None;
            }
            cm
        } else if Self::res_active(s) || cm == 0 {
            0 // deposit-only: a reserved main stays with its reservation
        } else {
            if self.lock_used() {
                let p = s.locks[cm as usize].purpose;
                if p != FREE && p != SWEEP {
                    return None;
                }
            }
            cm
        };
        if main != 0 && (!s.main_exists[main as usize] || s.main_spender[main as usize] != UNSPENT || s.custody_spent[main as usize]) {
            return None;
        }
        if over && !self.c(Control::AcceptFeeAboveCap) {
            return None;
        }
        Some(Plan { deps, main, res: resolution, over })
    }

    /// `Authority.AuthorizeSweep` (View.Read, Sweepable, BeforeMargin,
    /// mainRule, resolutionState).
    fn authorize(&self, s: &State, p: Plan) -> bool {
        for (i, d) in s.deposits.iter().enumerate() {
            if !p.has(i) {
                continue;
            }
            let final_ok = if self.c(Control::SignWithoutRevealFinality) {
                d.reveal == UNFINAL || d.reveal == FINAL
            } else {
                d.reveal == FINAL
            };
            if !final_ok || d.swept {
                return false;
            }
            if !self.c(Control::PlanTimeMarginOnly) && s.tick + self.host_margin() >= LOCKTIME {
                return false;
            }
        }
        let cm = s.custody_main;
        let active = Self::res_active(s);
        if p.main == 0 {
            if cm != 0 && !active {
                return false;
            }
        } else if p.main != cm {
            return false;
        } else if p.res {
            if !active {
                return false;
            }
        } else if active {
            return false;
        }
        true
    }

    /// `Executor.bitcoin`: every prevout exists and is unspent.
    fn prevouts(s: &State, p: Plan) -> bool {
        for (i, d) in s.deposits.iter().enumerate() {
            if p.has(i) && d.spender != UNSPENT {
                return false;
            }
        }
        p.main == 0 || (s.main_exists[p.main as usize] && s.main_spender[p.main as usize] == UNSPENT)
    }

    /// `Guard.Permit` / `permitted`.
    fn permitted(&self, s: &State, p: Plan) -> bool {
        if !self.lock_used() || p.main == 0 {
            return true;
        }
        let l = s.locks[p.main as usize];
        let bit = 1u64 << p.id();
        if p.res {
            l.purpose == RESOLUTION && l.plans & bit != 0
        } else {
            l.purpose == SWEEP && l.plans & bit != 0
        }
    }

    /// `Sweeper.signedRound`: a durable accepted result for the plan, from the
    /// marker or reconciled from an attempt entry and its result record.
    fn signed_round(&self, s: &State, p: Plan) -> Option<usize> {
        for (i, a) in s.attempts.iter().enumerate() {
            if a.used && a.plan == p && a.marker {
                return Some(i);
            }
        }
        if self.c(Control::TrustLedgerMarkerOnly) {
            return None;
        }
        for (i, a) in s.attempts.iter().enumerate() {
            if a.used && a.plan == p && a.ledger && a.accepted {
                return Some(i);
            }
        }
        None
    }
    fn late(s: &State, p: Plan) -> bool {
        s.attempts.iter().any(|a| a.used && a.plan == p && a.late)
    }

    fn end(a: &mut Attempt, failed: bool) {
        a.terminal = true;
        a.failed = failed;
    }

    /// The grant observers, evaluated on the state the grant reads.
    fn observe_grant(&self, s: &mut State, slot: usize) {
        let a = s.attempts[slot];
        let p = a.plan;
        let cm = s.custody_main;
        let active = Self::res_active(s);
        for (i, d) in s.deposits.iter().enumerate() {
            if !p.has(i) {
                continue;
            }
            if d.reveal != FINAL || d.swept || d.spender != UNSPENT || !Self::before_margin(s.tick) {
                s.bad_auth = true;
            }
            if !Self::before_margin(s.tick) {
                s.bad_refund = true;
            }
        }
        if p.over || (p.main == 0 && cm != 0 && !active) {
            s.bad_auth = true;
        }
        if p.main != 0 && !p.res {
            if p.main == M0 && (active || Self::attestation_live(s)) {
                s.bad_main = true;
            }
        }
        if p.res && !active {
            s.bad_main = true;
        }
        if p.main != 0 {
            s.sweep_granted_main[p.main as usize] = true;
        }
        if p.main == 0 && cm != 0 && active {
            s.bad_d13 = true;
        }
        for (j, b) in s.attempts.iter().enumerate() {
            if j == slot || !b.used || b.plan != p {
                continue;
            }
            if b.round == a.round && (b.claimed || b.granted) {
                s.bad_terminal = true;
            }
            if b.accepted {
                s.bad_terminal = true;
            }
        }
    }

    fn step(&self, s: &State, n: &mut State, slot: usize) -> bool {
        let a = s.attempts[slot];
        if !s.up || !a.used || a.terminal {
            return false;
        }
        let p = a.plan;
        match a.stage {
            PLANNED => {
                if p.res {
                    // ResolutionAttester.Attest: authority, then the lock moves
                    // to resolution(r, tx) before the certificate is signed.
                    if !Self::res_active(s) || p.main != s.custody_main || !self.authorize(s, p) {
                        Self::end(&mut n.attempts[slot], true);
                        return true;
                    }
                    if self.lock_used() {
                        let l = &mut n.locks[p.main as usize];
                        if l.purpose != REDEMPTION && l.purpose != RESOLUTION {
                            Self::end(&mut n.attempts[slot], true);
                            return true;
                        }
                        if l.purpose == REDEMPTION {
                            l.plans = 0;
                        }
                        l.purpose = RESOLUTION;
                        l.plans |= 1 << p.id();
                    }
                } else if p.main != 0 && self.lock_used() {
                    // AcquireSweep.
                    let l = &mut n.locks[p.main as usize];
                    match l.purpose {
                        FREE => {
                            l.purpose = SWEEP;
                            l.plans = 1 << p.id();
                        }
                        SWEEP => l.plans |= 1 << p.id(),
                        _ => {
                            Self::end(&mut n.attempts[slot], true);
                            return true;
                        }
                    }
                }
                n.attempts[slot].stage = LOCKED;
            }
            LOCKED => {
                n.attempts[slot].ledger = true;
                n.attempts[slot].stage = ATTEMPT;
            }
            ATTEMPT => {
                if !self.authorize(s, p) || !Self::prevouts(s, p) || !self.permitted(s, p) {
                    Self::end(&mut n.attempts[slot], true);
                    return true;
                }
                n.attempts[slot].stage = INTENT;
            }
            INTENT => {
                let reuse = self.c(Control::ReuseSweepAttempt);
                if !reuse
                    && s.attempts.iter().enumerate().any(|(j, b)| j != slot && b.used && b.plan == p && b.round == a.round && b.claimed)
                {
                    Self::end(&mut n.attempts[slot], true);
                    return true;
                }
                n.attempts[slot].claimed = true;
                n.attempts[slot].stage = CLAIMED;
            }
            CLAIMED => {
                if !self.authorize(s, p) || !Self::prevouts(s, p) || !self.permitted(s, p) {
                    Self::end(&mut n.attempts[slot], true);
                    return true;
                }
                self.observe_grant(n, slot);
                n.attempts[slot].granted = true;
                n.attempts[slot].stage = GRANTED;
            }
            GRANTED => {
                n.attempts[slot].retained = true;
                n.attempts[slot].stage = RETAINED;
            }
            RETAINED => {
                if self.c(Control::AcceptLateSweepSignature) || self.authorize(s, p) {
                    if !Self::before_margin(s.tick) {
                        n.bad_refund = true;
                    }
                    n.attempts[slot].accepted = true;
                    n.attempts[slot].stage = RESULT;
                } else {
                    // Every modeled refusal is terminal(): quarantine.
                    n.attempts[slot].late = true;
                    Self::end(&mut n.attempts[slot], true);
                }
            }
            RESULT => {
                n.attempts[slot].marker = true;
                n.attempts[slot].stage = SIGNED;
                Self::end(&mut n.attempts[slot], false);
            }
            _ => return false,
        }
        true
    }

    fn start(&self, n: &mut State, slot: usize, p: Plan) {
        n.attempts[slot] = Attempt { used: true, plan: p, round: n.round, stage: PLANNED, ..Default::default() };
    }

    fn next(&self, s: &State, action: Action) -> Option<State> {
        let mut n = s.clone();
        match action {
            Action::Reveal(d) => {
                let d = d as usize;
                if s.deposits[d].reveal != ABSENT {
                    return None;
                }
                n.deposits[d].reveal = UNFINAL;
            }
            Action::FinalizeReveal(d) => {
                if s.deposits[d as usize].reveal != UNFINAL {
                    return None;
                }
                n.deposits[d as usize].reveal = FINAL;
            }
            Action::OrphanReveal(d) => {
                if s.deposits[d as usize].reveal != UNFINAL {
                    return None;
                }
                n.deposits[d as usize].reveal = ORPHANED;
            }
            Action::Tick => {
                if s.tick >= MAX_TICK {
                    return None;
                }
                n.tick += 1;
            }
            Action::NextRound => {
                if s.round >= MAX_ROUND {
                    return None;
                }
                n.round += 1;
            }
            Action::ForeignCertificate => {
                if s.cert_foreign || s.res != R_NONE {
                    return None;
                }
                n.cert_foreign = true;
                n.cert_until = n.cert_until.max(s.tick + 1);
            }
            Action::Admit => {
                if !(s.cert_host || s.cert_foreign) || s.tick > s.cert_until || s.res != R_NONE || s.custody_main != M0 || s.custody_spent[M0 as usize] {
                    return None;
                }
                n.res = R_UNFINAL;
            }
            Action::FinalizeAdmission => {
                if s.res != R_UNFINAL {
                    return None;
                }
                n.res = R_RESERVED;
            }
            Action::OrphanAdmission => {
                if s.res != R_UNFINAL {
                    return None;
                }
                n.res = R_NONE;
            }
            Action::Attest => {
                if !s.up || s.attested || s.res != R_NONE || s.custody_main != M0 || s.custody_spent[M0 as usize] {
                    return None;
                }
                if self.lock_used() {
                    let l = &mut n.locks[M0 as usize];
                    if l.purpose != FREE {
                        return None;
                    }
                    *l = Lock { purpose: REDEMPTION, plans: 0, until: s.tick + 1 };
                }
                n.attested = true;
                n.cert_host = true;
                n.cert_until = n.cert_until.max(s.tick + 1);
            }
            Action::ReleaseUnadmitted => {
                if !s.up || !self.lock_used() {
                    return None;
                }
                let l = s.locks[M0 as usize];
                if l.purpose != REDEMPTION || s.res != R_NONE {
                    return None;
                }
                if !self.c(Control::LockFromUnfinalizedReservation) && s.tick <= l.until {
                    return None;
                }
                n.locks[M0 as usize] = Lock { purpose: FREE, plans: 0, until: 0 };
            }
            Action::PayGrant => {
                if !s.up || s.pay_granted || s.res != R_RESERVED || s.main_spender[M0 as usize] != UNSPENT {
                    return None;
                }
                if self.lock_used() && s.locks[M0 as usize].purpose != REDEMPTION {
                    return None;
                }
                if s.sweep_granted_main[M0 as usize] {
                    n.bad_main = true;
                }
                n.pay_granted = true;
            }
            Action::MinePayment => {
                if !s.pay_granted || s.pay_mined || s.main_spender[M0 as usize] != UNSPENT {
                    return None;
                }
                n.pay_mined = true;
                n.main_spender[M0 as usize] = BY_PAYMENT;
                n.main_exists[2] = true;
            }
            Action::ProvePayment => {
                if !s.pay_mined || s.res != R_RESERVED {
                    return None;
                }
                n.res = R_PAID;
                n.custody_spent[M0 as usize] = true;
                n.custody_main = 2;
            }
            Action::Close(m) => {
                let m = m as usize;
                if !s.up || !self.lock_used() || !s.custody_spent[m] || s.locks[m].purpose == CLOSED {
                    return None;
                }
                n.locks[m] = Lock { purpose: CLOSED, plans: 0, until: 0 };
            }
            Action::Plan(slot, over) => {
                let slot = slot as usize;
                if !s.up || s.attempts[slot].used || (slot > 0 && !s.attempts[slot - 1].used) {
                    return None;
                }
                let p = self.plan(s, over, false)?;
                // The same plan again is a retry and goes through Resolve.
                if s.attempts.iter().any(|a| a.used && a.plan == p) {
                    return None;
                }
                self.start(&mut n, slot, p);
            }
            Action::PlanResolution(slot) => {
                let slot = slot as usize;
                if !s.up || s.attempts[slot].used || (slot > 0 && !s.attempts[slot - 1].used) {
                    return None;
                }
                let p = self.plan(s, false, true)?;
                if s.attempts.iter().any(|a| a.used && a.plan == p) {
                    return None;
                }
                self.start(&mut n, slot, p);
            }
            Action::Retry(slot) => {
                // Sweeper.Resolve for the plan of the previous slot.
                let slot = slot as usize;
                if slot == 0 || !s.up || s.attempts[slot].used {
                    return None;
                }
                let prev = s.attempts[slot - 1];
                if !prev.used || !prev.terminal {
                    return None;
                }
                let p = prev.plan;
                if Self::late(s, p) || self.signed_round(s, p).is_some() || !self.authorize(s, p) {
                    return None;
                }
                if !self.c(Control::ReuseSweepAttempt)
                    && s.attempts.iter().any(|a| a.used && a.plan == p && a.ledger && a.round == s.round)
                {
                    return None;
                }
                self.start(&mut n, slot, p);
                // Resolve calls Sign directly: the plan is already built.
            }
            Action::Step(slot) => {
                if !self.step(s, &mut n, slot as usize) {
                    return None;
                }
            }
            Action::WorkerFail(slot) => {
                let a = s.attempts[slot as usize];
                if !s.up || !a.used || a.terminal || a.stage != GRANTED {
                    return None;
                }
                Self::end(&mut n.attempts[slot as usize], true);
            }
            Action::AcceptReadFail(slot) => {
                // The acceptance read fails: retained, not accepted, not late.
                let a = s.attempts[slot as usize];
                if !s.up || !a.used || a.terminal || a.stage != RETAINED {
                    return None;
                }
                Self::end(&mut n.attempts[slot as usize], true);
            }
            Action::ResultFail(slot, persisted) => {
                // The result record write errors; it may have persisted.
                let a = s.attempts[slot as usize];
                if !s.up || !a.used || a.terminal || a.stage != RETAINED || !self.authorize(s, a.plan) {
                    return None;
                }
                n.attempts[slot as usize].accepted = persisted;
                Self::end(&mut n.attempts[slot as usize], true);
            }
            Action::MarkerFail(slot, persisted) => {
                let a = s.attempts[slot as usize];
                if !s.up || !a.used || a.terminal || a.stage != RESULT {
                    return None;
                }
                n.attempts[slot as usize].marker = persisted;
                Self::end(&mut n.attempts[slot as usize], true);
            }
            Action::Broadcast(slot) => {
                // Sweeper.Broadcast: signedRound, then the executor rechecks.
                let slot = slot as usize;
                let a = s.attempts[slot];
                if !s.up || !a.used || a.broadcast || s.mempool & (1 << a.plan.id()) != 0 {
                    return None;
                }
                let i = self.signed_round(s, a.plan)?;
                if i != slot || !self.authorize(s, a.plan) {
                    return None;
                }
                if Self::late(s, a.plan) {
                    n.bad_refund = true;
                }
                n.attempts[slot].broadcast = true;
                n.mempool |= 1 << a.plan.id();
            }
            Action::MineSweep(slot) => {
                let p = s.attempts[slot as usize].plan;
                if !s.attempts[slot as usize].used || s.mempool & (1 << p.id()) == 0 || s.mined & (1 << p.id()) != 0 || !Self::prevouts(s, p) {
                    return None;
                }
                for i in 0..DEPOSITS {
                    if p.has(i) {
                        n.deposits[i].spender = BY_SWEEP + p.id();
                    }
                }
                if p.main != 0 {
                    n.main_spender[p.main as usize] = BY_SWEEP + p.id();
                    if (p.main as usize) + 1 < MAINS {
                        n.main_exists[p.main as usize + 1] = true;
                    }
                }
                n.mined |= 1 << p.id();
            }
            Action::ProveSweep(slot, lost) => {
                let slot = slot as usize;
                let p = s.attempts[slot].plan;
                if !s.attempts[slot].used || s.mined & (1 << p.id()) == 0 || s.attempts[slot].resubmit {
                    return None;
                }
                // FrostSweepMaintainer.credited: nothing to do when all swept.
                if (0..DEPOSITS).filter(|&i| p.has(i)).all(|i| s.deposits[i].swept) {
                    return None;
                }
                if !self.contract_credit(s, &mut n, p) {
                    return None;
                }
                if lost {
                    n.w_lost = true;
                    if self.c(Control::ForgetConsumptionOnRetry) {
                        n.attempts[slot].resubmit = true;
                    }
                }
            }
            Action::Resubmit(slot) => {
                let slot = slot as usize;
                if !s.attempts[slot].resubmit {
                    return None;
                }
                let p = s.attempts[slot].plan;
                n.attempts[slot].resubmit = false;
                if !self.contract_credit(s, &mut n, p) {
                    return None;
                }
            }
            Action::Refund(d) => {
                let d = d as usize;
                if s.tick < LOCKTIME || s.deposits[d].spender != UNSPENT {
                    return None;
                }
                n.deposits[d].spender = BY_REFUND;
                if !s.up {
                    n.w_refund_down = true;
                }
            }
            Action::Crash => {
                if !s.up {
                    return None;
                }
                n.up = false;
                for a in &mut n.attempts {
                    if a.used && !a.terminal {
                        Self::end(a, true);
                    }
                }
            }
            Action::Restart => {
                if s.up {
                    return None;
                }
                n.up = true;
            }
        }
        // Quotient: a terminal attempt's stage is never consulted again.
        for a in &mut n.attempts {
            if a.terminal {
                a.stage = SIGNED;
            }
        }
        if n == *s {
            None
        } else {
            Some(n)
        }
    }

    /// The abstract C-05 sweep proof (and the C-04 resolution route).
    fn contract_credit(&self, s: &State, n: &mut State, p: Plan) -> bool {
        let consumption = !self.c(Control::ForgetConsumptionOnRetry);
        for i in 0..DEPOSITS {
            if !p.has(i) {
                continue;
            }
            let d = n.deposits[i];
            if d.reveal == ABSENT || d.reveal == ORPHANED || (consumption && d.swept) {
                return false;
            }
        }
        let active = Self::res_active(s);
        let cm = s.custody_main;
        if p.res {
            if !active || cm != p.main || s.custody_spent[cm as usize] {
                return false;
            }
        } else if p.main != 0 {
            if active || cm != p.main || s.custody_spent[cm as usize] {
                return false;
            }
        } else if !(cm == 0 || active) {
            return false;
        }
        for i in 0..DEPOSITS {
            if p.has(i) {
                n.deposits[i].swept = true;
                n.deposits[i].credits += 1;
            }
        }
        if p.main != 0 {
            n.custody_spent[p.main as usize] = true;
            n.custody_main = p.main + 1;
            if p.main == 2 && s.res == R_PAID {
                n.w_new_main = true;
            }
        } else if cm == 0 {
            n.custody_main = 0; // not reachable: M0 exists from the start
        } else {
            n.extra += 1;
        }
        if p.res {
            n.res = R_SWEPT;
        }
        if s.tick >= LOCKTIME {
            n.w_after_locktime = true;
        }
        true
    }
}

impl Model for DepositModel {
    type State = State;
    type Action = Action;
    fn init_states(&self) -> Vec<State> {
        let mut s = State { up: true, custody_main: M0, ..Default::default() };
        s.main_exists[M0 as usize] = true;
        vec![s]
    }
    fn actions(&self, _s: &State, out: &mut Vec<Action>) {
        for d in 0..DEPOSITS as u8 {
            out.push(Action::Reveal(d));
            out.push(Action::FinalizeReveal(d));
            out.push(Action::OrphanReveal(d));
            out.push(Action::Refund(d));
        }
        out.extend([
            Action::Tick,
            Action::NextRound,
            Action::ForeignCertificate,
            Action::Admit,
            Action::FinalizeAdmission,
            Action::OrphanAdmission,
            Action::Attest,
            Action::ReleaseUnadmitted,
            Action::PayGrant,
            Action::MinePayment,
            Action::ProvePayment,
            Action::Crash,
            Action::Restart,
        ]);
        for m in 1..MAINS as u8 {
            out.push(Action::Close(m));
        }
        for slot in 0..SLOTS as u8 {
            out.push(Action::Plan(slot, false));
            out.push(Action::Plan(slot, true));
            out.push(Action::PlanResolution(slot));
            out.push(Action::Retry(slot));
            out.push(Action::Step(slot));
            out.push(Action::WorkerFail(slot));
            out.push(Action::AcceptReadFail(slot));
            out.push(Action::ResultFail(slot, false));
            out.push(Action::ResultFail(slot, true));
            out.push(Action::MarkerFail(slot, false));
            out.push(Action::MarkerFail(slot, true));
            out.push(Action::Broadcast(slot));
            out.push(Action::MineSweep(slot));
            out.push(Action::ProveSweep(slot, false));
            out.push(Action::ProveSweep(slot, true));
            out.push(Action::Resubmit(slot));
        }
    }
    fn next_state(&self, s: &State, a: Action) -> Option<State> {
        self.next(s, a)
    }
    fn properties(&self) -> Vec<Property<Self>> {
        vec![
            Property::<Self>::always(AUTH, |_, s| !s.bad_auth),
            Property::<Self>::always(MAIN, |_, s| !s.bad_main),
            Property::<Self>::always(REFUND, |_, s| {
                !s.bad_refund && !s.deposits.iter().any(|d| d.credits > 0 && d.spender == BY_REFUND)
            }),
            Property::<Self>::always(CREDIT, |_, s| s.deposits.iter().all(|d| d.credits <= 1)),
            Property::<Self>::always(TERMINAL, |_, s| !s.bad_terminal),
            Property::<Self>::always(D13, |_, s| !s.bad_d13),
            Property::<Self>::sometimes("W-authorized-sweep-credited", |_, s| {
                s.deposits.iter().any(|d| d.credits == 1) && !s.bad_auth
            }),
            Property::<Self>::sometimes("W-sweep-after-payment-new-main", |_, s| s.w_new_main),
            Property::<Self>::sometimes("W-resolution-sweep-resolves", |_, s| s.res == R_SWEPT),
            Property::<Self>::sometimes("W-refund-operators-stopped", |_, s| s.w_refund_down),
            Property::<Self>::sometimes("W-sweep-before-margin-credited-after-locktime", |_, s| s.w_after_locktime),
            Property::<Self>::sometimes("W-lost-reply-credits-once", |_, s| {
                s.w_lost && s.deposits.iter().any(|d| d.credits == 1) && s.deposits.iter().all(|d| d.credits <= 1)
            }),
            Property::<Self>::sometimes("W-fresh-attempt-after-failure", |_, s| {
                let (a, b) = (s.attempts[0], s.attempts[1]);
                a.used && a.failed && !a.accepted && a.claimed && b.used && b.plan == a.plan && b.round != a.round && b.accepted
            }),
        ]
    }
}

fn action_parts(a: &Action) -> (String, serde_json::Value) {
    let v = serde_json::to_value(a).unwrap();
    if let Some(o) = v.as_object() {
        let (k, f) = o.iter().next().unwrap();
        (k.clone(), f.clone())
    } else {
        (v.as_str().unwrap().to_owned(), serde_json::Value::Null)
    }
}

/// Writes one trace (Appendix C), cut at the first state that violates the
/// property (counterexample) or satisfies it (witness).
fn write_trace(out: &str, tag: &str, m: &DepositModel, property: &Property<DepositModel>, acts: &[Action]) {
    let property_name = property.name;
    let mut cut = acts.len();
    {
        let mut st = m.init_states().remove(0);
        for (k, a) in acts.iter().enumerate() {
            st = m.next_state(&st, *a).unwrap();
            let holds = (property.condition)(m, &st);
            let witness = property_name.starts_with("W-");
            if (witness && holds) || (!witness && !holds) {
                cut = k + 1;
                break;
            }
        }
    }
    let acts = &acts[..cut];
    let property = property_name;
    let claim = DepositModel { control: Control::None };
    let mut state = m.init_states().remove(0);
    let mut prefix = state.clone();
    let mut diverged = false;
    let mut steps = vec![];
    let mut claim_steps = vec![];
    for (k, a) in acts.iter().enumerate() {
        state = m.next_state(&state, *a).unwrap();
        let next = if diverged { None } else { claim.next_state(&prefix, *a) };
        let first = !diverged && next.as_ref() != Some(&state);
        if let Some(v) = next.clone() {
            if !first {
                prefix = v;
            }
        }
        if first {
            diverged = true;
        }
        let (name, fields) = action_parts(a);
        steps.push(serde_json::json!({"n": k + 1, "action": name, "fields": fields, "proc": proc_of(a), "expect": "transition", "expect_state": state, "diverges": first}));
        if !diverged || first {
            claim_steps.push(serde_json::json!({"n": k + 1, "action": name, "fields": fields, "proc": proc_of(a),
                "expect": if first { if next.is_none() { "refused" } else { "different-state" } } else { "transition" },
                "expect_state": if first { next.map(|v| serde_json::to_value(v).unwrap()).unwrap_or(serde_json::Value::Null) } else { serde_json::to_value(&prefix).unwrap() },
                "diverges": first}));
        }
    }
    let kind = if property.starts_with("W-") { "witness" } else { "counterexample" };
    let id = format!("{}{}-{}", tag, m.control.name(), property);
    let mut trace = serde_json::json!({"id": id, "kind": kind, "property": property, "weakening": m.control.name(),
        "bounds": {"deposits": DEPOSITS, "attempt_slots": SLOTS, "max_tick": MAX_TICK, "locktime": LOCKTIME, "margin": MARGIN, "max_round": MAX_ROUND},
        "replay": "pending", "replay_note": "classified by the Go replay (v07_replay_test.go)", "steps": steps});
    if m.control != Control::None {
        trace["claim_model_steps"] = serde_json::Value::Array(claim_steps);
    }
    fs::write(format!("{out}/traces/{id}.json"), serde_json::to_vec_pretty(&trace).unwrap()).unwrap();
}

fn proc_of(a: &Action) -> &'static str {
    match a {
        Action::Reveal(_) | Action::FinalizeReveal(_) | Action::OrphanReveal(_) | Action::Tick | Action::NextRound
        | Action::ForeignCertificate | Action::Admit | Action::FinalizeAdmission | Action::OrphanAdmission
        | Action::MinePayment | Action::MineSweep(_) | Action::WorkerFail(_) | Action::AcceptReadFail(_)
        | Action::ResultFail(..) | Action::MarkerFail(..) => "env",
        Action::ProvePayment | Action::ProveSweep(..) | Action::Resubmit(_) => "maintainer",
        Action::Refund(_) => "depositor",
        _ => "host",
    }
}

/// Usage: v07-deposit-model OUT DEADLINE_SECONDS THREADS [CONTROL|all] [stop|exhaustive]
/// "stop" (default) ends a weakened run at the first counterexample of its
/// declared property (R-08); "exhaustive" explores weakened models fully to
/// record which properties each weakening breaks. The claim model ("None") is
/// always exhaustive.
fn main() {
    let mut args = std::env::args().skip(1);
    let out = args.next().expect("output directory");
    let deadline: u64 = args.next().map(|v| v.parse().unwrap()).unwrap_or(840);
    let threads: usize = args.next().map(|v| v.parse().unwrap()).unwrap_or(1);
    let only = args.next().unwrap_or_else(|| "all".into());
    let mode = args.next().unwrap_or_else(|| "stop".into());
    assert!(threads >= 1 && threads <= 4, "at most four model threads");
    assert!(mode == "stop" || mode == "exhaustive");
    fs::create_dir_all(format!("{out}/traces")).unwrap();
    let tag = if mode == "exhaustive" { "X-" } else { "" };
    let mut reports = vec![];
    let mut ok = true;
    for control in Control::ALL {
        if only != "all" && only != control.name() {
            continue;
        }
        if mode == "exhaustive" && control == Control::None {
            continue;
        }
        let m = DepositModel { control };
        let start = Instant::now();
        let mut b = DepositModel { control }.checker().threads(threads).timeout(Duration::from_secs(deadline));
        if control != Control::None && mode == "stop" {
            b = b.finish_when(stateright::HasDiscoveries::AllOf(BTreeSet::from([control.target()])));
        }
        let c = b.spawn_bfs().join();
        let elapsed = start.elapsed().as_secs_f64();
        let complete = c.is_done() && elapsed < deadline as f64;
        let exhaustive = complete && (control == Control::None || mode == "exhaustive");
        let mut found = BTreeMap::new();
        for p in m.properties() {
            let path = c.discovery(p.name);
            found.insert(p.name, path.is_some());
            if let Some(path) = path {
                if (control == Control::None && mode == "stop") || p.name == control.target() {
                    write_trace(&out, tag, &m, &p, &path.into_actions());
                }
            }
        }
        let broken: Vec<&str> = KCVER.iter().copied().filter(|p| found[*p]).collect();
        let pass = if control == Control::None {
            complete
                && broken.is_empty()
                && found.iter().filter(|(p, _)| p.starts_with("W-")).all(|(_, v)| *v)
        } else if mode == "exhaustive" {
            complete && found[control.target()]
        } else {
            found[control.target()]
        };
        ok &= pass;
        let report = serde_json::json!({"control": control.name(), "target": control.target(), "mode": mode,
            "threads": threads, "deadline_seconds": deadline,
            "completed_before_deadline": complete, "exhaustive": exhaustive,
            "seconds": elapsed, "unique_states": c.unique_state_count(), "generated_states": c.state_count(),
            "max_depth": c.max_depth(), "discoveries": found, "kcver_properties_broken": broken, "pass": pass,
            "d13_proposed_counterexample": found[D13]});
        eprintln!("{report}");
        reports.push(report);
    }
    let name = format!("model-results-{only}-{mode}.json");
    fs::write(format!("{out}/{name}"), serde_json::to_vec_pretty(&reports).unwrap()).unwrap();
    if !ok {
        std::process::exit(1)
    }
}
