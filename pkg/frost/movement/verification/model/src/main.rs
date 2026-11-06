//! V-08 fund movement and wallet closure model (KC-VER rev 02, section 3 V-08).
//!
//! Separate from the implementation (R-01): no production code is imported.
//! Guards follow `SCOPE-2026-11-06.md`, which cites the code for each one.
//! Bound: source S, committed target T and alternative target A; two moved
//! outputs (o1 from the movement, o2 from the ECDSA-to-FROST drain); three
//! ticks; one host process with process crash, host crash, stale restore.
use serde::Serialize;
use stateright::{Checker, HasDiscoveries, Model, Property};
use std::{
    collections::{BTreeMap, BTreeSet},
    fs,
    time::{Duration, Instant},
};

const TARGET_ONLY: &str = "P-K07-COMMITTED-TARGET-ONLY";
const ONCE: &str = "P-K07-INCOMING-ONCE";
const STRANDED: &str = "P-K07-NO-STRANDED-MOVEMENT";
const CLEAR: &str = "P-K07-CLOSE-ONLY-WHEN-CLEAR";
const RETENTION: &str = "P-K07-SIGNER-RETENTION";
const SAFETY: [&str; 5] = [TARGET_ONLY, ONCE, STRANDED, CLEAR, RETENTION];
/// Proposed witness, not in KC-VER (SCOPE-2026-11-06.md section 4).
const PROPOSED: &str = "W-escalated-only-recovered";
const DEADLINE: u64 = 840;

// Wallet indexes and states (FrostWallets.State, reduced).
const S: usize = 0;
const T: usize = 1;
const A: usize = 2;
const LIVE: u8 = 1;
const MF: u8 = 2;
const CLOSING: u8 = 3;
const CLOSED: u8 = 4;
// Commitment: 0 none, 1 recorded (not final), 2 final, 3 proven.
// Incoming: 0 unknown, 1 pending, 2 escalated, 3 swept.
const PENDING: u8 = 1;
const ESCALATED: u8 = 2;
const SWEPT: u8 = 3;
// C-09 obligation rows modeled (FrostLifecycle.obligations).
const ROW_CUSTODY: u8 = 1;
const ROW_INCOMING: u8 = 2;
const ROW_INBOUND: u8 = 4;
const ROW_UNPROVEN: u8 = 8;
const ROW_CHALLENGE: u8 = 128;

#[derive(Clone, Copy, Debug, Default, Hash, PartialEq, Eq, Serialize)]
struct Sweep {
    /// Bit 0: o1, bit 1: o2. Zero is an empty slot.
    mask: u8,
    wallet: u8,
    main: bool,
    all_escalated: bool,
    mined: bool,
    proven: bool,
}

#[derive(Clone, Debug, Default, Hash, PartialEq, Eq, Serialize)]
struct State {
    tick: u8,
    ws: [u8; 3],
    closing_tick: [u8; 3],
    /// Custody main recorded by the contract.
    value: [bool; 3],
    /// The recorded main is spent by a mined, unproven transaction.
    main_spent: [bool; 3],
    commit: u8,
    ctarget: u8,
    escalated: bool,
    proven_ever: bool,
    orphaned: bool,
    target_changed: bool,
    inc: [u8; 2],
    itarget: [u8; 2],
    ifinal: [bool; 2],
    /// Which output was recorded first (log order): 0 none, 1 o1, 2 o2.
    first: u8,
    credits: [u8; 2],
    ispent: [bool; 2],
    movement_tx: u8,
    movement_mined: bool,
    sweeps: [Sweep; 2],
    lost_reply: bool,
    released: [bool; 3],
    challenge: [u8; 3],
    late_used: bool,
    // Host durable state.
    key: [bool; 3],
    quarantined: bool,
    /// The current round's attempt is claimed (one attempt per round; only
    /// the current round can be claimed, so past rounds are quotiented away).
    mv_round_used: bool,
    sw_round_used: bool,
    mv_signed: u8,
    sw_signed: Sweep,
    archived: [bool; 3],
    // Host volatile state.
    mv_phase: u8,
    mv_plan: u8,
    sw_phase: u8,
    sw_plan: Sweep,
    cl_phase: u8,
    cl_wallet: u8,
    cl_clean: bool,
    // Observers.
    bad_target: bool,
    bad_close: bool,
    w_escalated_recovered: bool,
    w_escalated_only: bool,
    w_late_reopen: bool,
    w_defense_closing: bool,
}

#[derive(Clone, Copy, Debug, Hash, PartialEq, Eq, Serialize)]
enum Action {
    Tick,
    // Environment (chain, Bitcoin, keepers, maintainers).
    FinalizeCommit,
    OrphanCommit,
    TargetChange(u8),
    MovementTimeout,
    MineMovement,
    ProveMovement,
    DrainRecorded,
    FinalizeIncoming(u8),
    IncomingTimeout(u8),
    MineSweep(u8),
    ProveSweep(u8, bool),
    KeeperFinalize(u8),
    LateCredit(u8),
    OpenChallenge(u8),
    FinalizeChallenge(u8),
    // Host.
    Commit,
    MvAtomic,
    SwAtomic(u8),
    MvPlan,
    MvClaim,
    MvGrant,
    MvRefuse(bool),
    MvSign,
    MvBroadcast,
    SwPlan(u8),
    SwClaim,
    SwGrant,
    SwRefuse(bool),
    SwSign,
    SwBroadcast,
    SwAbandon,
    Defend(u8),
    ClRead(u8),
    ClRecheck,
    ClRecheckRefuse,
    ClNotify(bool),
    ClArchive,
    DeleteKey(u8),
    Crash(bool),
    RestoreStale,
    Reconcile,
}

#[derive(Clone, Copy, Debug)]
struct V08 {
    control: &'static str,
    /// "movement": movement and incoming internals (predicates 1-3);
    /// "closure": closure, challenges and retention with the movement and
    /// sweep signing as one host step each (predicates 4-5). All five
    /// properties are checked in both.
    profile: &'static str,
    max_tick: u8,
}

fn bit(o: usize) -> u8 {
    1 << o
}

impl V08 {
    fn target(&self) -> &'static str {
        match self.control {
            "TargetsFromCurrentEligibility" | "PlanTimeCommitmentOnly" => TARGET_ONLY,
            "ForgetIncomingKey" => ONCE,
            "CloseWithIncomingPending" | "SweepOnlyWhenLive" => STRANDED,
            "MainFieldOnly" | "NoRecheckBeforeNotify" => CLEAR,
            "DeleteKeyAtClosing" => RETENTION,
            _ => "",
        }
    }
    fn eligible(s: &State, w: usize) -> bool {
        w != S && s.ws[w] == LIVE
    }
    fn lowest_eligible(s: &State) -> u8 {
        if Self::eligible(s, T) {
            T as u8
        } else if Self::eligible(s, A) {
            A as u8
        } else {
            0
        }
    }
    /// The full C-09 ledger of the modeled rows. `finalized` drops rows whose
    /// record is not final yet (a late arrival).
    fn ledger(s: &State, w: usize, finalized: bool) -> u8 {
        let mut open = 0;
        if s.value[w] {
            open |= ROW_CUSTODY;
        }
        for o in 0..2 {
            if s.itarget[o] as usize == w
                && (s.inc[o] == PENDING || s.inc[o] == ESCALATED)
                && (!finalized || s.ifinal[o])
            {
                open |= ROW_INCOMING;
            }
        }
        let unproven = s.commit == 1 || s.commit == 2;
        if unproven && (!finalized || s.commit == 2) {
            if s.ctarget as usize == w {
                open |= ROW_INBOUND;
            }
            if w == S {
                open |= ROW_UNPROVEN;
            }
        }
        if (s.challenge[w] == 1 && !finalized) || s.challenge[w] == 2 {
            open |= ROW_CHALLENGE;
        }
        open
    }
    /// The contract's obligations function (C-09); `CloseWithIncomingPending`
    /// drops row 2 from it.
    fn contract_ledger(&self, s: &State, w: usize) -> u8 {
        let open = Self::ledger(s, w, false);
        if self.control == "CloseWithIncomingPending" {
            open & !ROW_INCOMING
        } else {
            open
        }
    }
    /// The closure library's reading of the ledger.
    fn host_view(&self, s: &State, w: usize, finalized: bool) -> u8 {
        let open = Self::ledger(s, w, finalized);
        if self.control == "MainFieldOnly" {
            open & ROW_CUSTODY
        } else {
            open
        }
    }
    /// Target states in which the host proposes and authorizes an incoming
    /// sweep (`validateFrostMovedFundsSweepProposal`, `Authority.incoming`).
    fn host_sweeps(&self, state: u8) -> bool {
        if self.control == "SweepOnlyWhenLive" {
            state == LIVE
        } else {
            matches!(state, LIVE | MF | CLOSING)
        }
    }
    /// States in which C-06 `sweep` accepts a proof.
    fn contract_sweeps(state: u8) -> bool {
        matches!(state, LIVE | MF | CLOSING)
    }
    /// The movement authority (`Authority.movement`) for a plan paying `plan`.
    fn movement_authorized(&self, s: &State, plan: u8) -> bool {
        if self.control == "PlanTimeCommitmentOnly" {
            return true;
        }
        let committed = if self.control == "TargetsFromCurrentEligibility" {
            Self::lowest_eligible(s)
        } else {
            s.ctarget
        };
        s.commit == 2 && committed == plan && s.ws[S] == MF && s.value[S]
    }
    /// The executor's prevout check for the movement (mempool-inclusive).
    fn movement_unspent(s: &State) -> bool {
        !s.main_spent[S] && s.movement_tx == 0
    }
    fn in_mempool(s: &State, o: usize) -> bool {
        s.sweeps
            .iter()
            .any(|x| x.mask & bit(o) != 0 && !x.mined)
    }
    /// The incoming authority (`Authority.incoming`) for a planned sweep.
    fn sweep_authorized(&self, s: &State, p: Sweep) -> bool {
        let w = p.wallet as usize;
        if !self.host_sweeps(s.ws[w]) || p.main != s.value[w] {
            return false;
        }
        (0..2).filter(|o| p.mask & bit(*o) != 0).all(|o| {
            s.itarget[o] as usize == w
                && (s.inc[o] == PENDING || s.inc[o] == ESCALATED)
                && s.ifinal[o]
        })
    }
    /// The executor's prevout check for an incoming sweep (mempool-inclusive).
    fn sweep_unspent(s: &State, p: Sweep) -> bool {
        let w = p.wallet as usize;
        let inputs = (0..2)
            .filter(|o| p.mask & bit(*o) != 0)
            .all(|o| !s.ispent[o] && !Self::in_mempool(s, o));
        let main = !p.main
            || (!s.main_spent[w]
                && !s
                    .sweeps
                    .iter()
                    .any(|x| x.mask != 0 && x.main && x.wallet == p.wallet && !x.mined));
        inputs && main
    }
    /// The incoming plan of wallet w, or None (proposal trigger, planner and
    /// the proposal-spends-output check).
    fn sweep_plan(&self, s: &State, w: usize) -> Option<Sweep> {
        if !s.key[w] || !self.host_sweeps(s.ws[w]) {
            return None;
        }
        // Proposal (findFrostIncomingOutput / IsFrostIncomingPending): the
        // first output in log order whose record is Pending. The probe
        // ProposeEscalated also accepts an Escalated one.
        let order: [usize; 2] = if s.first == 2 { [1, 0] } else { [0, 1] };
        let proposed = order.into_iter().find(|o| {
            s.itarget[*o] as usize == w
                && (s.inc[*o] == PENDING
                    || (self.control == "ProposeEscalated" && s.inc[*o] == ESCALATED))
        })?;
        // Planner.IncomingSweep: every final pending or escalated output.
        let mut p = Sweep {
            wallet: w as u8,
            all_escalated: true,
            ..Sweep::default()
        };
        for o in 0..2 {
            if s.itarget[o] as usize == w
                && (s.inc[o] == PENDING || s.inc[o] == ESCALATED)
                && s.ifinal[o]
            {
                p.mask |= bit(o);
                p.all_escalated &= s.inc[o] == ESCALATED;
            }
        }
        if p.mask & bit(proposed) == 0 {
            return None;
        }
        // The planner finds the main by script; a spent main is no custody.
        if s.value[w] && s.main_spent[w] {
            return None;
        }
        p.main = s.value[w];
        Some(p)
    }
    fn reopen(s: &mut State, w: usize) {
        if s.ws[w] == CLOSING || s.ws[w] == CLOSED {
            s.ws[w] = MF;
            s.closing_tick[w] = 0;
        }
    }
    fn trace_wallet(a: &Action) -> Option<u8> {
        match a {
            Action::TargetChange(w)
            | Action::KeeperFinalize(w)
            | Action::LateCredit(w)
            | Action::OpenChallenge(w)
            | Action::FinalizeChallenge(w)
            | Action::SwPlan(w)
            | Action::SwAtomic(w)
            | Action::Defend(w)
            | Action::ClRead(w)
            | Action::DeleteKey(w) => Some(*w),
            _ => None,
        }
    }
}

impl Model for V08 {
    type State = State;
    type Action = Action;

    fn init_states(&self) -> Vec<State> {
        let mut s = State {
            ws: [MF, LIVE, LIVE],
            value: [true, false, false],
            key: [true, true, true],
            ..State::default()
        };
        s.closing_tick = [0; 3];
        vec![s]
    }

    fn actions(&self, s: &State, o: &mut Vec<Action>) {
        let closure = self.profile == "closure";
        if s.tick < self.max_tick {
            o.push(Action::Tick);
        }
        // Environment.
        if s.commit == 1 {
            o.push(Action::FinalizeCommit);
            o.push(Action::OrphanCommit);
        }
        for w in [T, A] {
            if s.ws[w] == LIVE {
                o.push(Action::TargetChange(w as u8));
            }
        }
        // Escalation is left out of the closure profile: it changes no C-09
        // row (rows 2 and 8 count escalated records alike) and only removes
        // host sweep proposals, so it adds no state the closure predicates
        // distinguish (RESULTS-2026-11-06.md, "Profiles and reductions").
        if !closure && s.tick >= 2 && (s.commit == 1 || s.commit == 2) && !s.escalated {
            o.push(Action::MovementTimeout);
        }
        if s.movement_tx != 0 && !s.movement_mined && !s.main_spent[S] {
            o.push(Action::MineMovement);
        }
        if s.movement_mined
            && (s.commit == 1 || s.commit == 2)
            && s.movement_tx == s.ctarget
            && s.value[S]
        {
            o.push(Action::ProveMovement);
        }
        if s.inc[1] == 0 && s.ws[T] == LIVE {
            o.push(Action::DrainRecorded);
        }
        for i in 0..2u8 {
            let k = i as usize;
            if s.inc[k] != 0 && !s.ifinal[k] {
                o.push(Action::FinalizeIncoming(i));
            }
            if !closure && s.tick >= 2 && s.inc[k] == PENDING {
                o.push(Action::IncomingTimeout(i));
            }
            let x = s.sweeps[k];
            if x.mask != 0 && !x.mined {
                let w = x.wallet as usize;
                let inputs = (0..2).filter(|q| x.mask & bit(*q) != 0).all(|q| !s.ispent[q]);
                if inputs && (!x.main || (s.value[w] && !s.main_spent[w])) {
                    o.push(Action::MineSweep(i));
                }
            }
            if x.mined && (!x.proven || self.control == "ForgetIncomingKey") {
                let w = x.wallet as usize;
                // C-06 sweep: state, and the consumption key of every input.
                // The main-input set rule is not modeled (over-approximation).
                let keyed = (0..2).filter(|q| x.mask & bit(*q) != 0).all(|q| {
                    s.itarget[q] as usize == w
                        && (s.inc[q] == PENDING
                            || s.inc[q] == ESCALATED
                            || self.control == "ForgetIncomingKey")
                });
                if Self::contract_sweeps(s.ws[w]) && keyed {
                    o.push(Action::ProveSweep(i, false));
                    if !closure {
                        o.push(Action::ProveSweep(i, true));
                    }
                }
            }
        }
        for w in 0..3usize {
            if s.ws[w] == CLOSING && s.tick > s.closing_tick[w] && self.contract_ledger(s, w) == 0 {
                o.push(Action::KeeperFinalize(w as u8));
            }
            if !closure {
                continue;
            }
            if (s.ws[w] == CLOSING || s.ws[w] == CLOSED) && !s.value[w] && !s.late_used {
                o.push(Action::LateCredit(w as u8));
            }
            if s.released[w] && s.challenge[w] == 0 && s.tick < 3 {
                o.push(Action::OpenChallenge(w as u8));
            }
            if s.challenge[w] == 1 {
                o.push(Action::FinalizeChallenge(w as u8));
            }
        }
        // Host.
        if s.commit == 0
            && !s.proven_ever
            && s.ws[S] == MF
            && s.value[S]
            && Self::lowest_eligible(s) != 0
        {
            o.push(Action::Commit);
        }
        if !s.quarantined {
            let live = s.mv_phase != 0 || s.sw_phase != 0 || s.cl_phase != 0;
            if live {
                o.push(Action::Crash(false));
                o.push(Action::Crash(true));
            }
            o.push(Action::RestoreStale);
            if closure {
                if s.key[S]
                    && s.movement_tx == 0
                    && self.movement_authorized(s, s.ctarget)
                    && Self::movement_unspent(s)
                {
                    o.push(Action::MvAtomic);
                }
                if s.sweeps.iter().any(|x| x.mask == 0) {
                    for w in [T, A] {
                        if let Some(p) = self.sweep_plan(s, w) {
                            if self.sweep_authorized(s, p) && Self::sweep_unspent(s, p) {
                                o.push(Action::SwAtomic(w as u8));
                            }
                        }
                    }
                }
            }
            // Movement attempt.
            if !closure && s.mv_phase == 0
                && s.key[S]
                && (s.commit == 1 || s.commit == 2)
                && s.value[S]
                && !s.main_spent[S]
            {
                let plan = if self.control == "TargetsFromCurrentEligibility" {
                    Self::lowest_eligible(s)
                } else {
                    s.ctarget
                };
                if plan != 0 && s.mv_signed == 0 {
                    o.push(Action::MvPlan);
                }
            }
            if s.mv_phase == 1 && !s.mv_round_used {
                o.push(Action::MvClaim);
            }
            if s.mv_phase == 2 {
                let auth = self.movement_authorized(s, s.mv_plan);
                if auth && Self::movement_unspent(s) {
                    o.push(Action::MvGrant);
                } else {
                    o.push(Action::MvRefuse(auth));
                }
            }
            if s.mv_phase == 3 {
                o.push(Action::MvSign);
            }
            if s.mv_signed != 0
                && s.movement_tx == 0
                && (s.mv_phase == 4 || s.mv_phase == 0)
                && self.movement_authorized(s, s.mv_signed)
            {
                o.push(Action::MvBroadcast);
            }
            // Incoming sweep attempt.
            if !closure && s.sw_phase == 0 && s.sw_signed.mask == 0 {
                for w in [T, A] {
                    if self.sweep_plan(s, w).is_some() {
                        o.push(Action::SwPlan(w as u8));
                    }
                }
            }
            if s.sw_phase == 1 && !s.sw_round_used {
                o.push(Action::SwClaim);
            }
            if s.sw_phase == 2 {
                let auth = self.sweep_authorized(s, s.sw_plan);
                if auth && Self::sweep_unspent(s, s.sw_plan) {
                    o.push(Action::SwGrant);
                } else {
                    o.push(Action::SwRefuse(auth));
                }
            }
            if s.sw_phase == 3 {
                o.push(Action::SwSign);
            }
            if s.sw_signed.mask != 0 && (s.sw_phase == 4 || s.sw_phase == 0) {
                if self.sweep_authorized(s, s.sw_signed) {
                    if s.sweeps.iter().any(|x| x.mask == 0) {
                        o.push(Action::SwBroadcast);
                    }
                } else {
                    o.push(Action::SwAbandon);
                }
            }
            for w in 0..3usize {
                if closure && (s.challenge[w] == 1 || s.challenge[w] == 2) && s.key[w] {
                    o.push(Action::Defend(w as u8));
                }
            }
            if s.cl_phase == 3 {
                o.push(Action::ClArchive);
            }
            if self.control == "DeleteKeyAtClosing" {
                for w in 0..3usize {
                    if s.ws[w] == CLOSING && s.key[w] {
                        o.push(Action::DeleteKey(w as u8));
                    }
                }
            }
        } else {
            o.push(Action::Reconcile);
        }
        // The closure library reads the chain and notifies without the store;
        // only its archive write needs the store.
        if closure && s.cl_phase == 0 {
            for w in 0..3usize {
                if s.ws[w] == CLOSING || s.ws[w] == CLOSED {
                    // A refused read changes nothing and is not a transition.
                    if self.host_view(s, w, true) == 0 {
                        o.push(Action::ClRead(w as u8));
                    }
                }
            }
        }
        if s.cl_phase == 1 {
            let w = s.cl_wallet as usize;
            if self.control == "NoRecheckBeforeNotify" {
                o.push(Action::ClNotify(self.finalize_ok(s, w)));
            } else if self.host_view(s, w, false) == 0 {
                o.push(Action::ClRecheck);
            } else {
                o.push(Action::ClRecheckRefuse);
            }
        }
        if s.cl_phase == 2 {
            o.push(Action::ClNotify(self.finalize_ok(s, s.cl_wallet as usize)));
        }
    }

    fn next_state(&self, s: &State, act: Action) -> Option<State> {
        let mut n = s.clone();
        match act {
            Action::Tick => {
                n.tick += 1;
                n.mv_round_used = false;
                n.sw_round_used = false;
            }
            Action::FinalizeCommit => n.commit = 2,
            Action::OrphanCommit => {
                n.commit = 0;
                n.ctarget = 0;
                n.escalated = false;
                n.orphaned = true;
                n.target_changed = false;
            }
            Action::TargetChange(w) => {
                let w = w as usize;
                // C-09 moveFunds: Closing only with no value and no obligation.
                n.ws[w] = if !s.value[w] && self.contract_ledger(s, w) == 0 {
                    n.closing_tick[w] = s.tick;
                    CLOSING
                } else {
                    MF
                };
                if s.ctarget as usize == w && (s.commit == 1 || s.commit == 2) {
                    n.target_changed = true;
                }
            }
            Action::MovementTimeout => n.escalated = true,
            Action::MineMovement => {
                n.movement_mined = true;
                n.main_spent[S] = true;
            }
            Action::ProveMovement => {
                n.commit = 3;
                n.proven_ever = true;
                let t = s.ctarget as usize;
                n.inc[0] = PENDING;
                n.itarget[0] = t as u8;
                if n.first == 0 {
                    n.first = 1;
                }
                n.value[S] = false;
                n.main_spent[S] = false;
                if s.ws[S] == MF {
                    n.ws[S] = CLOSING;
                    n.closing_tick[S] = s.tick;
                }
            }
            Action::DrainRecorded => {
                n.inc[1] = PENDING;
                n.itarget[1] = T as u8;
                if n.first == 0 {
                    n.first = 2;
                }
            }
            Action::FinalizeIncoming(i) => n.ifinal[i as usize] = true,
            Action::IncomingTimeout(i) => n.inc[i as usize] = ESCALATED,
            Action::MineSweep(i) => {
                let x = s.sweeps[i as usize];
                n.sweeps[i as usize].mined = true;
                for q in 0..2 {
                    if x.mask & bit(q) != 0 {
                        n.ispent[q] = true;
                    }
                }
                if x.main {
                    n.main_spent[x.wallet as usize] = true;
                }
            }
            Action::ProveSweep(i, lost) => {
                let x = s.sweeps[i as usize];
                let w = x.wallet as usize;
                for q in 0..2 {
                    if x.mask & bit(q) != 0 {
                        n.credits[q] = (n.credits[q] + 1).min(2);
                        if s.inc[q] == ESCALATED {
                            n.w_escalated_recovered = true;
                        }
                        n.inc[q] = SWEPT;
                    }
                }
                if x.all_escalated {
                    n.w_escalated_only = true;
                }
                n.value[w] = true;
                n.main_spent[w] = false;
                Self::reopen(&mut n, w);
                n.sweeps[i as usize].proven = true;
                if lost {
                    n.lost_reply = true;
                }
            }
            Action::KeeperFinalize(w) => n.ws[w as usize] = CLOSED,
            Action::LateCredit(w) => {
                let w = w as usize;
                n.value[w] = true;
                n.late_used = true;
                Self::reopen(&mut n, w);
                n.w_late_reopen = true;
            }
            Action::OpenChallenge(w) => n.challenge[w as usize] = 1,
            Action::FinalizeChallenge(w) => n.challenge[w as usize] = 2,
            Action::Commit => {
                n.commit = 1;
                n.ctarget = Self::lowest_eligible(s);
                n.target_changed = false;
            }
            Action::MvAtomic => {
                n.released[S] = true;
                n.movement_tx = s.ctarget;
            }
            Action::SwAtomic(w) => {
                let p = self.sweep_plan(s, w as usize).unwrap();
                n.released[w as usize] = true;
                let slot = s.sweeps.iter().position(|x| x.mask == 0).unwrap();
                n.sweeps[slot] = p;
            }
            Action::MvPlan => {
                n.mv_plan = if self.control == "TargetsFromCurrentEligibility" {
                    Self::lowest_eligible(s)
                } else {
                    s.ctarget
                };
                n.mv_phase = 1;
            }
            Action::MvClaim => {
                n.mv_round_used = true;
                n.mv_phase = 2;
            }
            Action::MvGrant => {
                if !(s.commit == 2 && s.ctarget == s.mv_plan && s.ws[S] == MF) {
                    n.bad_target = true;
                }
                n.mv_phase = 3;
            }
            Action::MvRefuse(_) => {
                n.mv_phase = 0;
                n.mv_plan = 0;
            }
            Action::MvSign => {
                n.mv_signed = s.mv_plan;
                n.mv_phase = 4;
            }
            Action::MvBroadcast => {
                n.movement_tx = s.mv_signed;
                n.mv_phase = 0;
                n.mv_plan = 0;
            }
            Action::SwPlan(w) => {
                n.sw_plan = self.sweep_plan(s, w as usize).unwrap();
                n.sw_phase = 1;
            }
            Action::SwClaim => {
                n.sw_round_used = true;
                n.sw_phase = 2;
            }
            Action::SwGrant => {
                n.sw_phase = 3;
            }
            Action::SwRefuse(_) => {
                n.sw_phase = 0;
                n.sw_plan = Sweep::default();
            }
            Action::SwSign => {
                n.sw_signed = s.sw_plan;
                n.sw_phase = 4;
            }
            Action::SwBroadcast => {
                let slot = s.sweeps.iter().position(|x| x.mask == 0).unwrap();
                n.sweeps[slot] = s.sw_signed;
                n.sw_signed = Sweep::default();
                n.sw_phase = 0;
                n.sw_plan = Sweep::default();
            }
            Action::SwAbandon => {
                n.sw_signed = Sweep::default();
                n.sw_phase = 0;
                n.sw_plan = Sweep::default();
            }
            Action::Defend(w) => {
                let w = w as usize;
                n.challenge[w] = 3;
                if s.ws[w] == CLOSING || s.ws[w] == CLOSED {
                    n.w_defense_closing = true;
                }
            }
            Action::ClRead(w) => {
                n.cl_phase = 1;
                n.cl_wallet = w;
                n.cl_clean = false;
            }
            Action::ClRecheck => {
                n.cl_phase = 2;
                n.cl_clean = Self::ledger(s, s.cl_wallet as usize, false) == 0;
            }
            Action::ClRecheckRefuse => {
                n.cl_phase = 0;
                n.cl_clean = false;
            }
            Action::ClNotify(closed) => {
                let w = s.cl_wallet as usize;
                if Self::ledger(s, w, false) != 0 && !s.cl_clean {
                    n.bad_close = true;
                }
                if closed {
                    n.ws[w] = CLOSED;
                }
                // A revert is reread: a Closed wallet continues (lost reply).
                n.cl_phase = if n.ws[w] == CLOSED { 3 } else { 0 };
                n.cl_clean = false;
            }
            Action::ClArchive => {
                n.archived[s.cl_wallet as usize] = true;
                n.cl_phase = 0;
            }
            Action::DeleteKey(w) => n.key[w as usize] = false,
            Action::Crash(_) => {
                n.mv_phase = 0;
                n.mv_plan = 0;
                n.sw_phase = 0;
                n.sw_plan = Sweep::default();
                n.cl_phase = 0;
                n.cl_clean = false;
            }
            Action::RestoreStale => {
                n.quarantined = true;
                n.mv_phase = 0;
                n.mv_plan = 0;
                n.sw_phase = 0;
                n.sw_plan = Sweep::default();
                n.cl_phase = 0;
                n.cl_clean = false;
            }
            Action::Reconcile => n.quarantined = false,
        }
        // Quotient: released flags only enable challenges, which open before
        // tick 3 only.
        if n.tick >= 3 {
            n.released = [false; 3];
        }
        if &n == s {
            None
        } else {
            Some(n)
        }
    }

    fn properties(&self) -> Vec<Property<Self>> {
        vec![
            Property::<Self>::always(TARGET_ONLY, |_, s| {
                !s.bad_target && (0..2).all(|o| s.inc[o] != SWEPT || s.ispent[o])
            }),
            Property::<Self>::always(ONCE, |_, s| s.credits.iter().all(|c| *c <= 1)),
            Property::<Self>::always(STRANDED, |m, s| {
                let outputs = (0..2).all(|o| {
                    let w = s.itarget[o] as usize;
                    match s.inc[o] {
                        PENDING => {
                            V08::contract_sweeps(s.ws[w]) && m.host_sweeps(s.ws[w]) && s.key[w]
                        }
                        ESCALATED => V08::contract_sweeps(s.ws[w]),
                        _ => true,
                    }
                });
                let movement = !(s.commit == 2 && !s.escalated) || (s.ws[S] == MF && s.key[S]);
                outputs && movement
            }),
            Property::<Self>::always(CLEAR, |_, s| !s.bad_close),
            Property::<Self>::always(RETENTION, |_, s| {
                (0..3).all(|w| {
                    s.key[w]
                        || (s.ws[w] == CLOSED
                            && V08::ledger(s, w, false) == 0
                            && s.tick == 3
                            && s.challenge[w] != 1
                            && s.challenge[w] != 2)
                })
            }),
            Property::<Self>::sometimes("W-movement-completes-target-ineligible", |_, s| {
                s.commit == 3 && s.target_changed
            }),
            Property::<Self>::sometimes("W-sweep-after-lost-reply-credits-once", |_, s| {
                s.lost_reply && s.credits.iter().any(|c| *c == 1) && s.credits.iter().all(|c| *c <= 1)
            }),
            Property::<Self>::sometimes("W-escalated-recovered", |_, s| s.w_escalated_recovered),
            Property::<Self>::sometimes("W-late-arrival-reopens", |_, s| s.w_late_reopen),
            Property::<Self>::sometimes("W-defense-after-closure-begins", |_, s| {
                s.w_defense_closing
            }),
            Property::<Self>::sometimes("W-drain-swept", |_, s| s.inc[1] == SWEPT),
            Property::<Self>::sometimes("W-closed-and-archived", |_, s| {
                (0..3).any(|w| s.archived[w] && s.ws[w] == CLOSED)
            }),
            Property::<Self>::sometimes("W-recommit-after-orphan", |_, s| {
                s.commit == 3 && s.orphaned
            }),
            Property::<Self>::sometimes(PROPOSED, |_, s| s.w_escalated_only),
        ]
    }
}

impl V08 {
    /// C-09 finalize: Closing, closing period elapsed, empty obligations.
    fn finalize_ok(&self, s: &State, w: usize) -> bool {
        s.ws[w] == CLOSING && s.tick > s.closing_tick[w] && self.contract_ledger(s, w) == 0
    }
}

fn action_json(a: &Action) -> (String, serde_json::Value) {
    let v = serde_json::to_value(a).unwrap();
    if let Some(obj) = v.as_object() {
        let (k, f) = obj.iter().next().unwrap();
        (k.clone(), f.clone())
    } else {
        (v.as_str().unwrap().to_owned(), serde_json::Value::Null)
    }
}

fn write_trace(out: &str, m: &V08, property: &str, acts: &[Action]) {
    let claim = V08 { control: "None", ..*m };
    let mut state = m.init_states().remove(0);
    let mut prefix = state.clone();
    let mut diverged = false;
    let mut steps = vec![];
    let mut claim_steps = vec![];
    for (k, a) in acts.iter().enumerate() {
        state = m.next_state(&state, *a).unwrap();
        let next = if diverged {
            None
        } else {
            let mut offered = vec![];
            claim.actions(&prefix, &mut offered);
            if offered.contains(a) {
                claim.next_state(&prefix, *a)
            } else {
                None
            }
        };
        let first = !diverged && next.as_ref() != Some(&state);
        if let Some(v) = next {
            prefix = v;
        }
        if first {
            diverged = true;
        }
        let (name, fields) = action_json(a);
        let wallet = V08::trace_wallet(a);
        steps.push(serde_json::json!({"n":k+1,"action":name,"fields":fields,"wallet":wallet,"expect":"transition","expect_state":state,"diverges":first}));
        if !diverged || first {
            claim_steps.push(serde_json::json!({"n":k+1,"action":name,"fields":fields,"wallet":wallet,"expect":if first {"refused-or-different-state"} else {"transition"},"expect_state":prefix,"diverges":first}));
        }
    }
    let id = format!("{}-{}-{}", m.profile, m.control, property);
    let kind = if property.starts_with("W-") { "witness" } else { "counterexample" };
    let trace = serde_json::json!({"id":id,"kind":kind,"profile":m.profile,"max_tick":m.max_tick,"property":property,"weakening":m.control,"steps":steps,"claim_model_steps":claim_steps});
    fs::write(
        format!("{out}/traces/{id}.json"),
        serde_json::to_vec_pretty(&trace).unwrap(),
    )
    .unwrap();
}

const MOVEMENT_WITNESSES: [&str; 6] = [
    "W-movement-completes-target-ineligible",
    "W-sweep-after-lost-reply-credits-once",
    "W-escalated-recovered",
    "W-drain-swept",
    "W-recommit-after-orphan",
    PROPOSED,
];
const CLOSURE_WITNESSES: [&str; 3] = [
    "W-late-arrival-reopens",
    "W-defense-after-closure-begins",
    "W-closed-and-archived",
];

fn main() {
    let mut args = std::env::args().skip(1);
    let out = args.next().expect("output directory");
    // Optional filters for sizing runs: profile, control, max tick.
    let only_profile = args.next().filter(|x| x != "all");
    let only_control = args.next().filter(|x| x != "all");
    let max_tick: u8 = args.next().map(|x| x.parse().unwrap()).unwrap_or(3);
    fs::create_dir_all(format!("{out}/traces")).unwrap();
    let mut reports = vec![];
    let mut ok = true;
    let runs: [(&str, &str); 12] = [
        ("movement", "None"),
        ("movement", "TargetsFromCurrentEligibility"),
        ("movement", "PlanTimeCommitmentOnly"),
        ("movement", "ForgetIncomingKey"),
        ("movement", "CloseWithIncomingPending"),
        ("movement", "SweepOnlyWhenLive"),
        ("movement", "ProposeEscalated"),
        ("closure", "None"),
        ("closure", "MainFieldOnly"),
        ("closure", "NoRecheckBeforeNotify"),
        ("closure", "DeleteKeyAtClosing"),
        ("closure", "CloseWithIncomingPending"),
    ];
    for (profile, control) in runs {
        if only_profile.as_deref().is_some_and(|o| o != profile)
            || only_control.as_deref().is_some_and(|o| o != control)
        {
            continue;
        }
        let m = V08 { control, profile, max_tick };
        let witnesses: &[&str] = if profile == "movement" {
            &MOVEMENT_WITNESSES
        } else {
            &CLOSURE_WITNESSES
        };
        let start = Instant::now();
        let mut b = m.checker().threads(4).timeout(Duration::from_secs(DEADLINE));
        if !m.target().is_empty() {
            b = b.finish_when(HasDiscoveries::AllOf(BTreeSet::from([m.target()])));
        }
        let c = b.spawn_bfs().join();
        let elapsed = start.elapsed().as_secs_f64();
        let complete = c.is_done() && elapsed < DEADLINE as f64;
        let mut found = BTreeMap::new();
        for p in m.properties() {
            if p.name.starts_with("W-") && !witnesses.contains(&p.name) {
                continue;
            }
            let path = c.discovery(p.name);
            found.insert(p.name, path.is_some());
            let keep = match control {
                "None" => true,
                "ProposeEscalated" => p.name == PROPOSED || SAFETY.contains(&p.name),
                _ => p.name == m.target(),
            };
            if let Some(path) = path.filter(|_| keep) {
                write_trace(&out, &m, p.name, &path.into_actions());
            }
        }
        let witnesses_ok = witnesses
            .iter()
            .filter(|p| **p != PROPOSED)
            .all(|p| found[*p]);
        let safety_ok = SAFETY.iter().all(|p| !found[*p]);
        let pass = complete
            && match control {
                "None" => safety_ok && witnesses_ok,
                "ProposeEscalated" => safety_ok && found[PROPOSED],
                _ => found[m.target()],
            };
        ok &= pass;
        let other: Vec<&str> = SAFETY
            .iter()
            .copied()
            .filter(|p| found[*p] && *p != m.target())
            .collect();
        let report = serde_json::json!({"profile":profile,"control":control,"max_tick":max_tick,"target":m.target(),"completed_before_deadline":complete,"deadline_seconds":DEADLINE,"seconds":elapsed,"unique_states":c.unique_state_count(),"generated_states":c.state_count(),"max_depth":c.max_depth(),"discoveries":found,"other_safety_discoveries":other,"proposed_witness":PROPOSED,"pass":pass});
        eprintln!("{report}");
        reports.push(report);
    }
    let name = match (&only_profile, &only_control) {
        (None, None) => "model-results.json".to_owned(),
        _ => format!(
            "model-results-{}-{}-t{}.json",
            only_profile.as_deref().unwrap_or("all"),
            only_control.as_deref().unwrap_or("all"),
            max_tick
        ),
    };
    fs::write(format!("{out}/{name}"), serde_json::to_vec_pretty(&reports).unwrap()).unwrap();
    if !ok {
        std::process::exit(1)
    }
}
