mod admission;
use serde::Serialize;
use stateright::{Checker, Model, Property};
use std::{
    collections::BTreeSet,
    fs,
    time::{Duration, Instant},
};

const AUTH: &str = "P-K04-AUTHORIZED-ONLY";
const TERMINAL: &str = "P-K05-TERMINAL-ATTEMPT";
const DOUBLE: &str = "P-K05-NO-DOUBLE-OUTCOME";
const ACTIVE: &str = "P-C04-ONE-ACTIVE-CUSTODY-RESERVATION";
const RECOVERY: &str = "P-K04-DURABLE-SIGNATURE-RECOVERY";
const LATE: &str = "P-K05-LATE-SIGNATURE-QUARANTINE";
#[derive(Clone, Copy, Debug, Default, Hash, PartialEq, Eq, Serialize)]
struct Request {
    // 0 absent, 1 unfinalized, 2 finalized, 3 timeout-pending; 4 paid, 5 cancelled, 6 refunded.
    reservation: u8,
    was_reserved: bool,
    certificate_until: u8,
    anchor: u8,
    receipt: u8,
    intent: bool,
    changed: bool,
    possible: bool,
    cancel: bool,
    paid: bool,
    refund: bool,
}
#[derive(Clone, Copy, Debug, Default, Hash, PartialEq, Eq, Serialize)]
struct Attempt {
    request: u8,
    phase: u8,
    claimed: bool,
    consumed: bool,
    terminal: bool,
    mode: u8,
    read_anchor: u8,
    returned: bool,
    retained: bool,
    aggregate: bool,
    recovered: bool,
    cancelled: bool,
    died: bool,
    io_failed: bool,
    released: bool,
    delivered: bool,
    quarantined: bool,
    accepted: bool,
}
#[derive(Clone, Debug, Default, Hash, PartialEq, Eq, Serialize)]
struct State {
    tick: u8,
    active_reservation: Option<u8>,
    journal_owner: Option<u8>,
    requests: [Request; 2],
    attempts: [Attempt; 2],
    bad_auth: bool,
    bad_terminal: bool,
    bad_late: bool,
    bad_recovery: bool,
}
#[derive(Clone, Copy, Debug, Hash, PartialEq, Eq, Serialize)]
enum Action {
    Reserve(u8, u8),
    Finalize(u8),
    Change(u8),
    Reanchor(u8),
    Tick,
    Start(u8, u8, u8),
    Step(u8),
    Crash(u8, bool),
    IoError(u8),
    Retry(u8),
    CancelCaller(u8),
    Recover(u8),
    Deliver(u8, bool),
    Pay(u8),
    Cancel(u8),
    Refund(u8),
}
#[derive(Clone, Copy, Debug)]
struct SigningModel {
    control: &'static str,
}
impl SigningModel {
    fn target(&self) -> &'static str {
        match self.control {
            "AllowConcurrentReservations" => ACTIVE,
            "ReuseAttempt" => TERMINAL,
            "CancelledRetention" | "AggregateOnlyRecovery" => RECOVERY,
            "RefundOnElapsedTime" | "RefundOnCertificateExpiry" => DOUBLE,
            "AcceptLateSignature" | "ForeignDoneContext" => LATE,
            _ => AUTH,
        }
    }
    fn authorized(&self, s: &State, a: Attempt) -> bool {
        let r = s.requests[a.request as usize];
        r.reservation == 2
            && !r.changed
            && r.receipt == r.anchor
            && r.receipt != 0
            && r.intent
            && a.mode == 0
    }
}
impl Model for SigningModel {
    type State = State;
    type Action = Action;
    fn init_states(&self) -> Vec<State> {
        vec![State::default()]
    }
    fn actions(&self, s: &State, o: &mut Vec<Action>) {
        if s.tick < 3 {
            o.push(Action::Tick)
        }
        for r in 0..2u8 {
            let q = s.requests[r as usize];
            if q.reservation == 0
                && s.tick < 3
                && !q.refund
                && (s.active_reservation.is_none() || self.control == "AllowConcurrentReservations")
            {
                for deadline in 1..=2 {
                    if s.tick <= deadline {
                        o.push(Action::Reserve(r, deadline))
                    }
                }
            }
            if q.reservation == 1 && s.tick < 3 {
                o.push(Action::Finalize(r))
            }
            if q.reservation > 0 && !q.changed {
                o.push(Action::Change(r))
            }
            if q.reservation > 0 && q.anchor == 1 {
                o.push(Action::Reanchor(r))
            }
            if q.possible && !q.cancel && !q.paid {
                o.push(Action::Pay(r))
            }
            if q.reservation == 3 && !q.paid && !q.cancel {
                o.push(Action::Cancel(r))
            }
            if (s.tick == 3
                || (self.control == "RefundOnCertificateExpiry"
                    && q.was_reserved
                    && s.tick > q.certificate_until))
                && !q.refund
                && !q.paid
                && (q.cancel
                    || q.reservation == 0
                    || matches!(
                        self.control,
                        "RefundOnElapsedTime" | "RefundOnCertificateExpiry"
                    ))
            {
                o.push(Action::Refund(r))
            }
        }
        for i in 0..2u8 {
            let a = s.attempts[i as usize];
            if a.phase == 0 && !a.terminal {
                for r in 0..2u8 {
                    for mode in 0..4u8 {
                        o.push(Action::Start(i, r, mode))
                    }
                }
            }
            if a.phase > 0 && a.phase < 33 && !a.terminal {
                // Store mutex serializes a commit, not an entire executor. Two
                // waiting writers do not deadlock each other before acquisition.
                if s.journal_owner.is_none() || s.journal_owner == Some(i) {
                    o.push(Action::Step(i));
                }
                o.push(Action::Crash(i, false));
                o.push(Action::Crash(i, true));
                o.push(Action::IoError(i));
            }
            if a.phase >= 20 && !a.terminal && !a.cancelled {
                o.push(Action::CancelCaller(i));
            }
            if a.terminal && a.retained && !a.recovered {
                o.push(Action::Recover(i));
            }
            if a.terminal && a.claimed {
                o.push(Action::Retry(i))
            }
            if a.released && !a.delivered {
                o.push(Action::Deliver(i, false));
                o.push(Action::Deliver(i, true));
            }
        }
    }
    fn next_state(&self, s: &State, act: Action) -> Option<State> {
        let mut n = s.clone();
        match act {
            Action::Reserve(r, deadline) => {
                n.active_reservation = Some(r);
                n.requests[r as usize].was_reserved = true;
                n.requests[r as usize].certificate_until = deadline;
                n.requests[r as usize].reservation = 1;
                n.requests[r as usize].anchor = 1;
            }
            Action::Finalize(r) => n.requests[r as usize].reservation = 2,
            Action::Change(r) => n.requests[r as usize].changed = true,
            Action::Reanchor(r) => n.requests[r as usize].anchor = 2,
            Action::Tick => {
                n.tick += 1;
                if n.tick == 3 {
                    for r in &mut n.requests {
                        if matches!(r.reservation, 1 | 2) {
                            r.reservation = 3
                        }
                    }
                }
            }
            Action::Start(i, r, mode) => {
                let q = s.requests[r as usize];
                let ok = mode == 0
                    && q.reservation == 2
                    && !q.changed
                    && (q.receipt == 0 || q.receipt == q.anchor);
                let bypass = (self.control == "ReleaseBeforeFinality"
                    && mode == 0
                    && q.reservation == 1
                    && !q.changed)
                    || (self.control == "OmitExactPlan"
                        && mode == 1
                        && q.reservation == 2
                        && !q.changed)
                    || (self.control == "EmptyAuthorizationStartsWorker"
                        && mode >= 2
                        && q.reservation == 2
                        && !q.changed);
                if !ok && !bypass {
                    return None;
                }
                n.attempts[i as usize] = Attempt {
                    request: r,
                    phase: 1,
                    mode,
                    read_anchor: q.anchor,
                    ..Attempt::default()
                };
            }
            Action::Step(i) => {
                let mut a = s.attempts[i as usize];
                let r = a.request as usize;
                if matches!(a.phase, 1 | 7 | 13 | 21 | 27) {
                    n.journal_owner = Some(i);
                }
                if matches!(a.phase, 6 | 12 | 18 | 26 | 32) {
                    n.journal_owner = None;
                }
                // Three separate six-stage commits: receipt, exact intent, attempt claim.
                // write/file-sync/install/directory-sync/fence/ack; crash is offered at every stage.
                if a.phase == 6 {
                    if n.requests[r].receipt != 0 && n.requests[r].receipt != a.read_anchor {
                        a.terminal = true
                    } else {
                        n.requests[r].receipt = a.read_anchor
                    }
                }
                if a.phase == 12 {
                    n.requests[r].intent = true;
                }
                if a.phase == 18 {
                    if a.claimed {
                        n.bad_terminal = true;
                    }
                    a.claimed = true;
                }
                if a.phase == 19 {
                    let valid = self.authorized(s, a);
                    let q = s.requests[r];
                    let exact_anchor = q.receipt != 0 && q.receipt == q.anchor;
                    let canonical = q.reservation == 2 && !q.changed && q.intent;
                    let bypass = self.control == "OmitFinalGuard"
                        || (self.control == "ReleaseBeforeFinality"
                            && q.reservation == 1
                            && !q.changed
                            && q.intent
                            && exact_anchor
                            && a.mode == 0)
                        || (self.control == "OmitExactPlan"
                            && canonical
                            && exact_anchor
                            && a.mode == 1)
                        || (self.control == "EmptyAuthorizationStartsWorker"
                            && canonical
                            && exact_anchor
                            && a.mode >= 2)
                        || (self.control == "ForgetDurableReceipt" && canonical && a.mode == 0);
                    if !valid && !bypass {
                        a.terminal = true
                    } else {
                        n.bad_auth |= !valid;
                        a.consumed = true;
                        a.released = true;
                        n.requests[r].possible = true;
                    }
                }
                if a.phase == 20 {
                    a.returned = true;
                }
                if a.phase == 21 && a.cancelled && self.control == "CancelledRetention" {
                    a.terminal = true;
                    n.bad_recovery = true;
                }
                if a.phase == 26 {
                    a.retained = true;
                }
                if a.phase == 27 && (a.cancelled || !self.authorized(s, a)) {
                    a.terminal = true;
                }
                if a.phase == 32 {
                    a.aggregate = true;
                    a.terminal = true;
                }
                a.phase += 1;
                if a.terminal && n.journal_owner == Some(i) {
                    n.journal_owner = None;
                }
                n.attempts[i as usize] = a;
            }
            Action::Crash(i, host) => {
                if n.journal_owner == Some(i) {
                    n.journal_owner = None;
                }
                let a = &mut n.attempts[i as usize];
                a.terminal = true;
                a.died = true;
                // Unacknowledged writes may survive process loss; host loss drops them.
                // They never grant authority. Durable acknowledged receipt/intent/claim survive.
                if !host && a.phase == 18 {
                    a.claimed = true;
                }
            }
            Action::IoError(i) => {
                if n.journal_owner == Some(i) {
                    n.journal_owner = None;
                }
                n.attempts[i as usize].terminal = true;
                n.attempts[i as usize].io_failed = true;
            }
            Action::CancelCaller(i) => n.attempts[i as usize].cancelled = true,
            Action::Recover(i) => {
                if self.control == "AggregateOnlyRecovery" && !n.attempts[i as usize].aggregate {
                    n.bad_recovery = true;
                } else {
                    n.attempts[i as usize].recovered = true;
                }
            }
            Action::Retry(i) => {
                if self.control != "ReuseAttempt" {
                    return None;
                }
                let a = &mut n.attempts[i as usize];
                a.terminal = false;
                a.phase = 18;
            }
            Action::Deliver(i, foreign) => {
                let mut a = n.attempts[i as usize];
                let q = n.requests[a.request as usize];
                a.delivered = true;
                let valid = self.authorized(s, a) && !foreign;
                let bypass = (self.control == "AcceptLateSignature" && !foreign)
                    || (self.control == "ForeignDoneContext" && foreign && self.authorized(s, a));
                a.accepted = valid || bypass;
                a.quarantined = !a.accepted;
                n.bad_late |= a.accepted && !valid;
                n.attempts[i as usize] = a;
                // Delayed payment is independently possible even when the honest host quarantines.
                let _ = q;
            }
            Action::Pay(r) => {
                n.requests[r as usize].paid = true;
                n.requests[r as usize].reservation = 4;
                if n.active_reservation == Some(r) {
                    n.active_reservation = None;
                }
            }
            Action::Cancel(r) => {
                n.requests[r as usize].cancel = true;
                n.requests[r as usize].reservation = 5;
                if n.active_reservation == Some(r) {
                    n.active_reservation = None;
                }
            }
            Action::Refund(r) => {
                n.requests[r as usize].refund = true;
                n.requests[r as usize].reservation = 6;
                if n.active_reservation == Some(r) {
                    n.active_reservation = None;
                }
            }
        }
        // Quotient terminal program counters: no continuation consults them.
        // Preserve all durable facts, returned/retained/result flags, the request,
        // grant mode and late-delivery context. A crash before claim/release has
        // no future request-dependent action, so normalize only that dead binding.
        for a in &mut n.attempts {
            if a.terminal {
                a.phase = 33;
                if !a.claimed && !a.released {
                    a.request = 0;
                    a.mode = 0;
                    a.read_anchor = 0;
                }
            }
        }
        if &n == s {
            None
        } else {
            Some(n)
        }
    }
    fn properties(&self) -> Vec<Property<Self>> {
        vec![
            Property::<Self>::always(ACTIVE, |_, s| {
                s.requests
                    .iter()
                    .filter(|r| matches!(r.reservation, 1 | 2 | 3))
                    .count()
                    <= 1
            }),
            Property::<Self>::always(AUTH, |_, s| !s.bad_auth),
            Property::<Self>::always(TERMINAL, |_, s| !s.bad_terminal),
            Property::<Self>::always(DOUBLE, |_, s| {
                !s.requests.iter().any(|r| r.paid && r.refund)
            }),
            Property::<Self>::always(LATE, |_, s| !s.bad_late),
            Property::<Self>::always(RECOVERY, |_, s| {
                !s.bad_recovery
                    && !s.attempts.iter().any(|a| {
                        a.terminal
                            && a.returned
                            && a.cancelled
                            && !a.died
                            && !a.io_failed
                            && !a.retained
                    })
            }),
            Property::<Self>::sometimes("W-sign-after-admission-certificate-expiry", |_, s| {
                s.attempts.iter().any(|a| {
                    a.released
                        && s.tick > s.requests[a.request as usize].certificate_until
                        && s.requests[a.request as usize].reservation == 2
                })
            }),
            Property::<Self>::sometimes("W-next-reservation-after-confirmed-outcome", |_, s| {
                s.active_reservation.is_some() && s.requests.iter().any(|r| r.paid || r.cancel)
            }),
            Property::<Self>::sometimes("W-authorized-release", |_, s| {
                s.attempts.iter().any(|a| a.released) && !s.bad_auth
            }),
            Property::<Self>::sometimes("W-fresh-after-failure", |_, s| {
                s.attempts[0].terminal
                    && s.attempts[0].claimed
                    && s.attempts[1].released
                    && s.attempts[0].request == s.attempts[1].request
            }),
            Property::<Self>::sometimes("W-payment-settles", |_, s| {
                s.requests.iter().any(|r| r.paid)
            }),
            Property::<Self>::sometimes("W-cancel-then-refund", |_, s| {
                s.requests.iter().any(|r| r.cancel && r.refund)
            }),
            Property::<Self>::sometimes("W-late-quarantine", |_, s| {
                s.attempts
                    .iter()
                    .any(|a| a.quarantined && s.requests[a.request as usize].reservation == 3)
            }),
            Property::<Self>::sometimes("W-cancelled-return-retained", |_, s| {
                s.attempts
                    .iter()
                    .any(|a| a.cancelled && a.returned && a.retained)
            }),
            Property::<Self>::sometimes("W-done-only-recovered", |_, s| {
                s.attempts
                    .iter()
                    .any(|a| a.recovered && !a.aggregate && a.retained)
            }),
            Property::<Self>::sometimes("W-unreserved-refund", |_, s| {
                s.requests.iter().any(|r| r.refund && !r.was_reserved)
            }),
        ]
    }
}
fn main() {
    let out = std::env::args().nth(1).expect("output directory");
    fs::create_dir_all(format!("{out}/traces")).unwrap();
    let mut reports = vec![];
    let mut ok = true;
    for control in [
        "None",
        "ReleaseBeforeFinality",
        "OmitExactPlan",
        "OmitFinalGuard",
        "EmptyAuthorizationStartsWorker",
        "ForgetDurableReceipt",
        "ReuseAttempt",
        "RefundOnElapsedTime",
        "RefundOnCertificateExpiry",
        "AcceptLateSignature",
        "ForeignDoneContext",
        "CancelledRetention",
        "AggregateOnlyRecovery",
        "AllowConcurrentReservations",
    ] {
        let m = SigningModel { control };
        let start = Instant::now();
        let mut b = m.checker().threads(4).timeout(Duration::from_secs(1800));
        if control != "None" {
            b = b.finish_when(stateright::HasDiscoveries::AllOf(BTreeSet::from([
                m.target()
            ])));
        }
        let c = b.spawn_bfs().join();
        let elapsed = start.elapsed().as_secs_f64();
        let complete = c.is_done() && elapsed < 1800.;
        let mut found = std::collections::BTreeMap::new();
        for p in m.properties() {
            let path = c.discovery(p.name);
            found.insert(p.name, path.is_some());
            if let Some(path) = path.filter(|_| control == "None" || p.name == m.target()) {
                let acts: Vec<Action> = path.into_actions();
                let mut state = State::default();
                let mut steps = vec![];
                let mut claim_steps = vec![];
                let mut prefix = State::default();
                let claim = SigningModel { control: "None" };
                let mut diverged = false;
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
                    let action_json = serde_json::to_value(a).unwrap();
                    let (name, fields) = if let Some(object) = action_json.as_object() {
                        let (name, fields) = object.iter().next().unwrap();
                        (name.clone(), fields.clone())
                    } else {
                        (
                            action_json.as_str().unwrap().to_owned(),
                            serde_json::Value::Null,
                        )
                    };
                    let attempt_slot = match a {
                        Action::Start(i, _, _)
                        | Action::Step(i)
                        | Action::Crash(i, _)
                        | Action::IoError(i)
                        | Action::Retry(i)
                        | Action::CancelCaller(i)
                        | Action::Recover(i)
                        | Action::Deliver(i, _) => Some(*i),
                        _ => None,
                    };
                    let proc = attempt_slot.map(|_| 0u8);
                    steps.push(serde_json::json!({"n":k+1,"attempt":attempt_slot,"action":name,"fields":fields,"proc":proc,"expect":"transition","expect_state":state,"diverges":first}));
                    if !diverged || first {
                        claim_steps.push(serde_json::json!({"n":k+1,"attempt":attempt_slot,"action":name,"fields":fields,"proc":proc,"expect":if first {"refused-or-different-state"}else{"transition"},"expect_state":prefix,"diverges":first}));
                    }
                }
                let trace = serde_json::json!({"id":format!("{control}-{}",p.name),"kind":if p.name.starts_with("W-"){"witness"}else{"counterexample"},"property":p.name,"weakening":control,"max_commits":10,"replay":"none","replay_note":"Abstract trace. Go seam replay classification is recorded separately; no C-04 contract exists.","steps":steps,"claim_model_steps":claim_steps});
                fs::write(
                    format!("{out}/traces/{control}-{}.json", p.name),
                    serde_json::to_vec_pretty(&trace).unwrap(),
                )
                .unwrap();
            }
        }
        let pass = complete
            && if control == "None" {
                [AUTH, TERMINAL, DOUBLE, LATE, RECOVERY, ACTIVE]
                    .iter()
                    .all(|p| !found[*p])
                    && found
                        .iter()
                        .filter(|(p, _)| p.starts_with("W-"))
                        .all(|(_, v)| *v)
            } else {
                found[m.target()]
            };
        ok &= pass;
        let report = serde_json::json!({"control":control,"target":m.target(),"completed_before_deadline":complete,"seconds":elapsed,"unique_states":c.unique_state_count(),"generated_states":c.state_count(),"max_depth":c.max_depth(),"discoveries":found,"pass":pass});
        eprintln!("{report}");
        reports.push(report);
    }
    fs::write(
        format!("{out}/model-results.json"),
        serde_json::to_vec_pretty(&reports).unwrap(),
    )
    .unwrap();
    ok &= admission::run(&out);
    if !ok {
        std::process::exit(1)
    }
}
