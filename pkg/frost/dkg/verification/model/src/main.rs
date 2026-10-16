//! V-03 finite host/chain model; invariants are derived from independently
//! recorded identity, read, certificate, terminal and deadline histories.
use serde::Serialize;
use stateright::{Checker, Model, Property};
use std::{
    collections::BTreeSet,
    fs,
    time::{Duration, Instant},
};
const P: [&str; 7] = [
    "P-K03-ONE-DESCRIPTOR",
    "P-K03-NO-EARLY-FUNDING",
    "P-K03-READY-ONLY-RELOADED",
    "P-K03-INCONSISTENCY-QUARANTINE",
    "P-K03-EXPIRY-TOMBSTONE",
    "P-K03-FRESH-EPOCH-DISJOINT",
    "P-K03-SUBMITTED-NOT-TERMINAL",
];
#[derive(Clone, Debug, Default, Hash, PartialEq, Eq, Serialize)]
struct S {
    pre_tick: u8,
    result_closed: bool,
    old_result_closed: bool,
    epoch: u8,
    tick: u8,
    chain: u8,
    submitted: u8,
    changed: bool,
    phase: [u8; 2],
    seen: u8,
    watched: u8,
    worker_ready: u8,
    key: u8,
    reloaded: u8,
    attest: u8,
    ready: u8,
    recognized: u8,
    certified: bool,
    closed: bool,
    retry: bool,
    foreign: bool,
    old: bool,
    old_closed: bool,
    old_quarantine: bool,
    identities: [u8; 4],
    old_ids: [u8; 4],
    // independent public/durable histories; no artificial violation bits
    recognized_desc: [u8; 2],
    recognized_view: [u8; 2],
    recognized_tick: [u8; 2],
    key_desc: [u8; 2],
    wallet_desc: [u8; 2],
    wallet_state: [u8; 2],
    receipt: u8,
    collect: [u8; 2],
    cert_desc: u8,
    cert_seats: u8,
    cert_count: u8,
    cert_tick: u8,
    deadline: u8,
    anchor: u8,
    inconsistency: u8,
    quarantine: u8,
    termination: [u8; 2],
    tombstones: [u8; 4],
    old_tombstones: [u8; 4],
    funded: bool,
    monitor_pending: u8,
    monitor_used: u8,
    monitor_overlap: u8,
}
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize)]
enum A {
    Candidate(u8),
    BeforePublish,
    Publish,
    AfterPublish,
    Foreign,
    Challenge,
    ResultTick,
    ExpireDKG,
    Approve,
    Tick,
    Remove,
    Replace,
    Observe(u8),
    BeforeFinality(u8),
    Reread(u8),
    WorkerReady(u8),
    Key(u8),
    PostKeyRead(u8),
    Reload(u8),
    BeginReadiness(u8),
    Attest(u8),
    CollectionRead(u8),
    BeforeCertificate { proc: u8, seats: u8 },
    Certify(u8),
    Postcondition(u8),
    PersistReady(u8),
    Complete(u8),
    Watch(u8),
    BeginWatch(u8),
    FinishWatch(u8),
    Duplicate,
    Retry,
    Expire,
    Fresh,
    Fund,
    ProcessCrash(u8),
    HostCrash(u8),
}
#[derive(Clone)]
struct M {
    weak: u8,
}
fn initial() -> S {
    S {
        identities: [1; 4],
        tombstones: [1, 0, 0, 0],
        deadline: 3,
        anchor: 3,
        ..S::default()
    }
}
fn active(p: u8) -> bool {
    matches!(p,0..=7|11|12|14|16..=21)
}
fn seats(n: u8) -> u8 {
    3 << (n * 2)
}
fn valid(s: &S) -> bool {
    s.chain == 1 && !s.closed && (s.tick < s.deadline || s.certified)
}
fn read_descriptor(s: &S) -> u8 {
    match s.chain {
        1 => s.identities[1],
        3 => 3 - s.identities[1],
        _ => 0,
    }
}
fn recognize(s: &mut S, n: u8) {
    s.recognized |= 1 << n;
    s.recognized_desc[n as usize] = s.identities[1];
    s.recognized_view[n as usize] = read_descriptor(s);
    s.recognized_tick[n as usize] = s.tick;
}
fn quarantine(s: &mut S, n: u8) {
    s.phase[n as usize] = 10;
    if s.receipt & (1 << n) != 0 && s.wallet_state[n as usize] < 2 {
        s.wallet_state[n as usize] = 2;
    }
    s.quarantine |= 1 << n;
    s.termination[n as usize] = 2;
}
fn expire_local(s: &mut S, n: u8) {
    s.phase[n as usize] = 13;
    if s.wallet_state[n as usize] == 0 {
        s.wallet_state[n as usize] = 1;
        s.wallet_desc[n as usize] = s.identities[1];
    }
    s.termination[n as usize] = 3;
    if s.receipt & (1 << n) != 0 {
        s.wallet_state[n as usize] = 4;
    }
}
fn check_read(s: &mut S, n: u8) -> bool {
    if s.chain != 1 {
        s.inconsistency |= 1 << n;
        quarantine(s, n);
        false
    } else if !valid(s) {
        s.closed = true;
        expire_local(s, n);
        false
    } else {
        true
    }
}
impl Model for M {
    type State = S;
    type Action = A;
    fn init_states(&self) -> Vec<S> {
        vec![initial()]
    }
    fn actions(&self, s: &S, a: &mut Vec<A>) {
        for n in 0..2 {
            match s.phase[n as usize] {
                0 => a.push(A::Candidate(n)),
                1 if s.chain == 1 || s.chain == 3 => a.push(A::Observe(n)),
                2 if s.tick >= 2 => a.push(A::BeforeFinality(n)),
                3 => a.push(A::Reread(n)),
                4 => a.push(A::WorkerReady(n)),
                21 if s.worker_ready == 15 => a.push(A::Key(n)),
                5 => {
                    a.push(A::PostKeyRead(n));
                    if self.weak == 3 {
                        a.push(A::Attest(n));
                    }
                }
                16 => a.push(A::Reload(n)),
                17 => a.push(A::BeginReadiness(n)),
                6 => a.push(A::Attest(n)),
                7 => {
                    a.push(A::CollectionRead(n));
                    if s.attest.count_ones() >= 3 {
                        for remote in 0..4 {
                            let mask = seats(n) | (1 << remote);
                            if mask.count_ones() == 3 && mask & !s.attest == 0 {
                                a.push(A::BeforeCertificate {
                                    proc: n,
                                    seats: mask,
                                });
                            }
                        }
                    }
                }
                14 => a.push(A::Certify(n)),
                18 => a.push(A::Postcondition(n)),
                19 => a.push(A::PersistReady(n)),
                20 => a.push(A::Complete(n)),
                _ => {}
            };
            if active(s.phase[n as usize]) && s.monitor_pending & (1 << n) == 0 {
                if s.phase[n as usize] == 1 && s.chain == 0 && s.submitted == 1 && s.pre_tick < 3 && s.monitor_used & (1 << n) == 0 { a.push(A::BeginWatch(n)); }
                a.extend([A::Watch(n), A::ProcessCrash(n), A::HostCrash(n)]);
            }
        }
        for n in 0..2 { if active(s.phase[n as usize]) && s.monitor_pending & (1 << n) != 0 { a.push(A::FinishWatch(n)); } }
        if s.phase[0] == 1 && s.wallet_desc[1] != 0 && s.submitted == 0 && s.chain == 0 && !s.closed
        {
            a.push(A::BeforePublish);
        }
        if s.phase[0] == 11 && s.submitted == 0 && !s.result_closed && s.pre_tick + 1 < 3 {
            a.push(A::Publish);
        }
        if s.phase[0] == 12 {
            a.push(A::AfterPublish);
        }
        if s.submitted == 0 && s.chain == 0 && !s.foreign && !s.closed {
            a.push(A::Foreign);
        }
        if s.submitted == 2 {
            a.push(A::Challenge);
        }
        if s.submitted == 1
            && s.chain == 0
            && !s.changed
            && !s.closed
            && !s.result_closed
            && s.pre_tick < 3
        {
            a.push(A::Approve);
        }
        if s.chain == 0 && s.submitted != 1 && s.phase.contains(&9) && s.pre_tick < 3 {
            a.push(A::ResultTick);
        }
        if s.chain == 0 && s.pre_tick == 3 && !s.result_closed {
            a.push(A::ExpireDKG);
        }
        if s.chain != 0 && s.tick < 3 {
            a.push(A::Tick);
        }
        if s.chain == 1 && !s.changed {
            a.extend([A::Remove, A::Replace]);
        }
        if s.chain == 1 && !s.certified && !s.closed {
            a.push(A::Fund);
            if !s.retry {
                a.push(A::Retry);
            }
            if s.tick >= s.deadline {
                a.push(A::Expire);
            }
        }
        if self.weak == 4
            && s.attest != 0
            && !s.certified
            && s.chain == 1
            && !s.closed
            && s.tick < s.deadline
        {
            a.push(A::Duplicate);
        }
        if s.epoch == 0 && (s.closed || s.result_closed || s.phase.contains(&10)) {
            a.push(A::Fresh);
        }
    }
    fn next_state(&self, s: &S, a: A) -> Option<S> {
        let mut t = s.clone();
        match a {
            A::Candidate(n) => {
                t.phase[n as usize] = 1;
                t.wallet_desc[n as usize] = t.identities[1];
                t.wallet_state[n as usize] = 1;
            }
            A::BeforePublish => t.phase[0] = 11,
            A::Publish => {
                t.submitted = 1;
                t.phase[0] = 12;
            }
            A::AfterPublish => t.phase[0] = 1,
            A::Foreign => {
                t.foreign = true;
                t.submitted = 2;
                if self.weak == 9 {
                    t.phase = [9; 2];
                    t.termination = [5; 2];
                }
            }
            A::Challenge => t.submitted = 0,
            A::ResultTick => t.pre_tick += 1,
            A::ExpireDKG => {
                t.result_closed = true;
                t.submitted = 0;
            }
            A::Approve => {
                t.chain = 1;
                t.submitted = 0;
                t.tombstones = t.identities;
            }
            A::Tick => t.tick += 1,
            A::Remove => {
                t.chain = 2;
                t.changed = true;
            }
            A::Replace => {
                t.chain = 3;
                t.changed = true;
            }
            A::Observe(n) => {
                if t.chain != 1 {
                    t.inconsistency |= 1 << n;
                    quarantine(&mut t, n);
                } else {
                    t.phase[n as usize] = 2;
                    t.seen |= 1 << n;
                    if self.weak == 1 {
                        t.phase[n as usize] = 4;
                        recognize(&mut t, n);
                    }
                }
            }
            A::BeforeFinality(n) => {
                t.phase[n as usize] = 3;
                t.receipt |= 1 << n;
            }
            A::Reread(n) => {
                if self.weak == 8 {
                    t.phase[n as usize] = 4;
                    recognize(&mut t, n);
                } else if check_read(&mut t, n) {
                    t.phase[n as usize] = 4;
                    recognize(&mut t, n);
                }
            }
            // Receipt-to-key path has no invented read. Store I/O stages joined to V02.
            A::WorkerReady(n) => {
                t.phase[n as usize] = 21;
                t.worker_ready |= seats(n);
                t.wallet_state[n as usize] = 2;
            }
            A::Key(n) => {
                t.phase[n as usize] = 5;
                t.key |= 1 << n;
                t.key_desc[n as usize] = t.identities[1];
            }
            A::PostKeyRead(n) => {
                if check_read(&mut t, n) {
                    t.phase[n as usize] = 16;
                }
            }
            A::Reload(n) => {
                t.phase[n as usize] = 17;
                t.reloaded |= seats(n);
            }
            A::BeginReadiness(n) => t.phase[n as usize] = 6,
            A::Attest(n) => {
                t.phase[n as usize] = 7;
                t.attest |= seats(n);
            }
            A::CollectionRead(n) => {
                check_read(&mut t, n);
            }
            A::BeforeCertificate {
                proc: n,
                seats: mask,
            } => {
                t.phase[n as usize] = 14;
                t.collect[n as usize] = mask;
            }
            A::Certify(n) => {
                if t.chain != 1 {
                    t.inconsistency |= 1 << n;
                    quarantine(&mut t, n);
                } else if !valid(s) {
                    if t.closed {
                        expire_local(&mut t, n);
                    } else {
                        quarantine(&mut t, n);
                    }
                } else {
                    if !t.certified {
                        t.certified = true;
                        t.cert_desc = t.identities[1];
                        t.cert_seats = t.collect[n as usize];
                        t.cert_count = t.cert_seats.count_ones() as u8;
                        t.cert_tick = t.tick;
                    }
                    t.phase[n as usize] = 18;
                }
            }
            A::Postcondition(n) => {
                if check_read(&mut t, n) {
                    t.phase[n as usize] = 19;
                }
            }
            A::PersistReady(n) => {
                t.phase[n as usize] = 20;
                t.wallet_state[n as usize] = 3;
                t.ready |= 1 << n;
            }
            A::Complete(n) => t.phase[n as usize] = 8,
            A::BeginWatch(n) => { t.monitor_pending |= 1 << n; t.monitor_used |= 1 << n; }
            A::FinishWatch(n) => {
                t.monitor_pending &= !(1 << n);
                if (t.seen | t.watched) & (1 << n) != 0 {
                    t = self.next_state(&t, A::Watch(n)).unwrap();
                    t.monitor_overlap |= 1 << n;
                }
            }
            A::Watch(n) => {
                if t.chain == 0 && (t.pre_tick == 3 || t.result_closed) {
                    t.result_closed = true;
                    t.submitted = 0;
                    expire_local(&mut t, n);
                } else {
                    let previous_watch = t.watched;
                    if t.chain == 1 {
                        t.watched |= 1 << n;
                    }
                    if ((t.seen | previous_watch) & (1 << n) != 0 && t.chain != 1)
                        || (t.chain == 3 && t.phase[n as usize] != 0)
                    {
                        t.inconsistency |= 1 << n;
                        if !(self.weak == 5 && t.chain == 2) {
                            quarantine(&mut t, n);
                        }
                    } else if t.chain == 1 && !valid(s) {
                        t.closed = true;
                        expire_local(&mut t, n);
                    }
                }
            }
            A::Duplicate => {
                t.certified = true;
                t.cert_desc = t.identities[1];
                t.cert_seats = t.attest;
                t.cert_count = 4;
                t.cert_tick = t.tick;
            }
            A::Retry => {
                t.retry = true;
                if self.weak == 6 {
                    t.deadline += 1;
                }
            }
            A::Expire => t.closed = true,
            A::Fresh => {
                t = initial();
                t.epoch = 1;
                t.old = true;
                t.old_closed = s.closed;
                t.old_result_closed = s.result_closed;
                t.old_quarantine = s.phase.contains(&10);
                t.old_ids = s.identities;
                t.old_tombstones = s.tombstones;
                t.identities = [2; 4];
                if self.weak == 7 {
                    t.identities[0] = s.identities[0];
                }
            }
            A::Fund => {
                if self.weak == 2 {
                    t.funded = true;
                }
            }
            A::ProcessCrash(n) | A::HostCrash(n) => {
                t.phase[n as usize] = 9;
                t.termination[n as usize] = 4;
            }
        };
        if &t == s {
            None
        } else {
            Some(t)
        }
    }
    fn properties(&self) -> Vec<Property<Self>> {
        let mut p = vec![
            Property::<Self>::always(P[0], |_, s| {
                (0..2).all(|n| {
                    let d = s.identities[1];
                    (s.recognized_desc[n] == 0
                        || (s.recognized_desc[n] == d
                            && s.recognized_view[n] == d
                            && s.recognized_tick[n] >= 2))
                        && (s.key_desc[n] == 0 || s.key_desc[n] == d)
                        && (s.wallet_desc[n] == 0 || s.wallet_desc[n] == d)
                }) && (s.cert_desc == 0 || s.cert_desc == s.identities[1])
            }),
            Property::<Self>::always(P[1], |_, s| !s.funded || s.certified),
            Property::<Self>::always(P[2], |_, s| {
                s.attest & !s.reloaded == 0
                    && (!s.certified
                        || (s.cert_seats & !s.reloaded == 0
                            && s.cert_count == s.cert_seats.count_ones() as u8
                            && s.cert_count >= 3))
            }),
            Property::<Self>::always(P[3], |_, s| s.inconsistency & !s.quarantine == 0),
            Property::<Self>::always(P[4], |_, s| {
                s.deadline == s.anchor
                    && (!s.certified || s.cert_tick < s.anchor)
                    && (!s.closed || s.tombstones == s.identities)
            }),
            Property::<Self>::always(P[5], |_, s| {
                !s.old
                    || (s
                        .identities
                        .iter()
                        .zip(s.old_ids.iter())
                        .all(|(a, b)| a != b)
                        && s.old_tombstones
                            .iter()
                            .zip(s.old_ids.iter())
                            .all(|(a, b)| *a == 0 || a == b)
                        && s.old_tombstones[0] == s.old_ids[0])
            }),
            Property::<Self>::always(P[6], |_, s| s.termination.iter().all(|c| *c != 5)),
        ];
        if self.weak == 0 {
            p.extend([
                Property::<Self>::sometimes("W-CANDIDATE-LOSS-FRESH-READY", |_, s| {
                    s.old_result_closed && s.phase == [8, 8]
                }),
                Property::<Self>::sometimes("W-DELAYED-PREAPPROVAL-RECONCILED", |_, s| s.monitor_overlap != 0 && s.phase == [8, 8]),
                Property::<Self>::sometimes("W-READY-UNFUNDED", |_, s| s.phase == [8, 8]),
                Property::<Self>::sometimes("W-LOST-AFTER-STATEMENT", |_, s| {
                    s.certified && s.phase.contains(&9)
                }),
                Property::<Self>::sometimes("W-QUARANTINE-FRESH-READY", |_, s| {
                    s.old_quarantine && s.phase == [8, 8]
                }),
                Property::<Self>::sometimes("W-EXPIRY-FRESH-READY", |_, s| {
                    s.old_closed && s.phase == [8, 8]
                }),
                Property::<Self>::sometimes("W-FOREIGN-SUBMITTED-ACCEPTANCE", |_, s| {
                    s.foreign && s.phase == [8, 8]
                }),
                Property::<Self>::sometimes("W-KEY-AFTER-REMOVAL-QUARANTINED", |_, s| {
                    s.changed && s.key != 0 && s.phase.contains(&10)
                }),
                Property::<Self>::sometimes("W-LOST-BEFORE-READINESS-EXPIRED", |_, s| {
                    s.closed && s.phase.iter().enumerate().any(|(n,p)| *p == 9 && s.key & (1 << n) == 0) && !s.certified
                }),
                Property::<Self>::sometimes("W-CERTIFICATE-BEFORE-DURABLE-READY", |_, s| {
                    s.certified && s.ready == 0
                }),
            ]);
        }
        p
    }
}
fn boundary(p: u8, n: u8) -> Option<&'static str> {
    match p {
        0 => Some("candidate-ready"),
        2 => Some("approval-seen"),
        3 => Some("before-finality-reread"),
        4 => Some("after-finality-reread"),
        5 => Some("after-key-ready"),
        6 => Some("before-readiness-collect"),
        14 => Some("before-certificate-submit"),
        16 => Some("after-post-key-read"),
        17 => Some("after-key-reload"),
        18 => Some("after-certificate-submit"),
        19 => Some("after-readiness-postcondition"),
        20 => Some("after-wallet-ready-save"),
        21 => Some("before-key-ready-save"),
        11 if n == 0 => Some("before-publish"),
        12 if n == 0 => Some("after-publish"),
        _ => None,
    }
}
fn trace(out: &str, id: &str, kind: &str, property: &str, m: &M, actions: &[A]) {
    let mut s = initial();
    let mut claim = initial();
    let mut steps = vec![];
    let mut diverged = false;
    let mut claim_blocked = false;
    for (i, a) in actions.iter().enumerate() {
        let before = s.clone();
        let mut available = vec![];
        m.actions(&s, &mut available);
        assert!(available.contains(a), "trace {id} step {} offers no {:?}", i + 1, a);
        s = m.next_state(&s, *a).unwrap();
        let mut offered = vec![];
        M { weak: 0 }.actions(&claim, &mut offered);
        let mut claim_available = false;
        if !claim_blocked && offered.contains(a) {
            if let Some(next) = (M { weak: 0 }).next_state(&claim, *a) {
                claim = next;
                claim_available = true;
                if s != claim {
                    diverged = true;
                }
            } else {
                claim_blocked = true;
                diverged = true;
            }
        } else {
            claim_blocked = true;
            diverged = true;
        }
        let proc = match a {
            A::Candidate(n)
            | A::Observe(n)
            | A::BeforeFinality(n)
            | A::Reread(n)
            | A::WorkerReady(n)
            | A::Key(n)
            | A::PostKeyRead(n)
            | A::Reload(n)
            | A::BeginReadiness(n)
            | A::CollectionRead(n)
            | A::Postcondition(n)
            | A::PersistReady(n)
            | A::Complete(n)
            | A::Attest(n)
            | A::BeforeCertificate { proc: n, .. }
            | A::Certify(n)
            | A::BeginWatch(n)
            | A::FinishWatch(n)
            | A::Watch(n)
            | A::ProcessCrash(n)
            | A::HostCrash(n) => Some(*n),
            A::BeforePublish | A::Publish | A::AfterPublish => Some(0),
            _ => None,
        };
        let expect = if let Some(n) = proc {
            match s.phase[n as usize] {
                8 => "ready-unfunded",
                9 => "dead",
                10 => "quarantined",
                13 => "expired",
                _ => "paused",
            }
        } else {
            "environment"
        };
        steps.push(serde_json::json!({"n":i+1,"action":a,"proc":proc,"pause_before":proc.and_then(|n|boundary(before.phase[n as usize],n)),"pause":proc.and_then(|n|boundary(s.phase[n as usize],n)),"expect":expect,"expect_state":s,"claim_state":claim,"diverges":diverged,"claim_available":claim_available}));
    }
    let host = actions.iter().any(|a| matches!(a, A::HostCrash(_)));
    fs::write(format!("{out}/traces/{id}.json"),serde_json::to_vec_pretty(&serde_json::json!({"id":id,"kind":kind,"property":property,"weakening":m.weak,"max_commits":0,"replay":if host{"partial"}else{"full"},"replay_note":if host{"Acknowledged boundary kill; unsynced OS-cache loss delegated to source-pinned V02; partial host-loss execution"}else{"Real executor subprocess hooks, controlled providers and real durable store; cryptographic engine contract joined to V02 plus real fixture"},"steps":steps,"claim_model_steps":if m.weak>0{steps.iter().map(|s|serde_json::json!({"n":s["n"],"action":s["action"],"proc":s["proc"],"pause_before":s["pause_before"],"pause":s["pause"],"expect_state":s["claim_state"],"diverges":s["diverges"],"claim_available":s["claim_available"]})).collect::<Vec<_>>()}else{vec![]}})).unwrap()).unwrap();
}
fn main() {
    let out = std::env::args().nth(1).expect("out");
    fs::create_dir_all(format!("{out}/traces")).unwrap();
    let mut runs = vec![];
    let mut ok = true;
    for weak in 0..=9 {
        let m = M { weak };
        let target = match weak {
            1 | 8 => 0,
            2 => 1,
            3 | 4 => 2,
            5 => 3,
            6 => 4,
            7 => 5,
            9 => 6,
            _ => 0,
        };
        let start = Instant::now();
        let mut b = m
            .clone()
            .checker()
            .threads(1)
            .timeout(Duration::from_secs(120));
        if weak > 0 {
            b = b.finish_when(stateright::HasDiscoveries::AnyOf(BTreeSet::from([
                P[target]
            ])));
        }
        let c = b.spawn_bfs().join();
        let completed = c.is_done() && start.elapsed() < Duration::from_secs(120);
        let mut found = vec![];
        for p in m.properties() {
            if let Some(path) = c.discovery(p.name) {
                let actions = path.into_actions();
                trace(
                    &out,
                    &format!("{}-control-{weak}", p.name),
                    if weak == 0 {
                        "witness"
                    } else {
                        "counterexample"
                    },
                    p.name,
                    &m,
                    &actions,
                );
                found.push(p.name);
            }
        }
        let pass = completed
            && if weak == 0 {
                found.len() == 10 && found.iter().all(|x| x.starts_with("W-"))
            } else {
                found == vec![P[target]]
            };
        ok &= pass;
        runs.push(serde_json::json!({"weakening":weak,"target":if weak==0{"all"}else{P[target]},"completed_before_deadline":completed,"unique":c.unique_state_count(),"generated":c.state_count(),"seconds":start.elapsed().as_secs_f64(),"discoveries":found,"check_pass":pass}));
        eprintln!("{}", runs.last().unwrap());
    }
    // Representative killed process at every actual named hook, plus host-loss limitation.
    let path = vec![
        A::Candidate(0),
        A::Candidate(1),
        A::BeforePublish,
        A::Publish,
        A::AfterPublish,
        A::Approve,
        A::Observe(0),
        A::Observe(1),
        A::Tick,
        A::Tick,
        A::BeforeFinality(0),
        A::BeforeFinality(1),
        A::Reread(0),
        A::Reread(1),
        A::WorkerReady(0),
        A::WorkerReady(1),
        A::Key(0),
        A::Key(1),
        A::PostKeyRead(0),
        A::Reload(0),
        A::BeginReadiness(0),
        A::PostKeyRead(1),
        A::Reload(1),
        A::BeginReadiness(1),
        A::Attest(0),
        A::Attest(1),
        A::BeforeCertificate { proc: 0, seats: 7 },
        A::BeforeCertificate { proc: 1, seats: 13 },
        A::Certify(0),
        A::Postcondition(0),
        A::PersistReady(0),
        A::Complete(0),
        A::Certify(1),
        A::Postcondition(1),
        A::PersistReady(1),
        A::Complete(1),
    ];
    trace(
        &out,
        "B-ALL-NINE-HOOKS",
        "boundary",
        P[0],
        &M { weak: 0 },
        &path,
    );
    let regression = vec![
        A::Candidate(0),
        A::Candidate(1),
        A::BeforePublish,
        A::Publish,
        A::AfterPublish,
        A::Approve,
        A::Observe(0),
        A::Observe(1),
        A::Tick,
        A::Tick,
        A::BeforeFinality(0),
        A::BeforeFinality(1),
        A::Reread(0),
        A::Reread(1),
        A::WorkerReady(0),
        A::WorkerReady(1),
        A::Key(0),
        A::Key(1),
        A::PostKeyRead(0),
        A::Reload(0),
        A::BeginReadiness(0),
        A::PostKeyRead(1),
        A::Reload(1),
        A::BeginReadiness(1),
        A::Attest(0),
        A::Attest(1),
        A::BeforeCertificate { proc: 0, seats: 7 },
        A::Certify(0),
        A::Postcondition(0),
        A::PersistReady(0),
        A::Remove,
        A::Watch(0),
    ];
    trace(
        &out,
        "B-READY-RETURN-MONITOR-QUARANTINE",
        "boundary",
        P[3],
        &M { weak: 0 },
        &regression,
    );
    for n in 0..2 {
        let mut s = initial();
        let mut prefix = vec![];
        let mut done = BTreeSet::new();
        for a in &path {
            if let Some(h) = boundary(s.phase[n as usize], n) {
                if done.insert(h) {
                    for host in [false, true] {
                        let mut x = prefix.clone();
                        x.push(if host {
                            A::HostCrash(n)
                        } else {
                            A::ProcessCrash(n)
                        });
                        trace(
                            &out,
                            &format!("B-{}-{n}-{h}", if host { "HOST" } else { "PROCESS" }),
                            "boundary",
                            P[2],
                            &M { weak: 0 },
                            &x,
                        );
                    }
                }
            }
            prefix.push(*a);
            s = M { weak: 0 }.next_state(&s, *a).unwrap();
        }
    }
    fs::write(format!("{out}/results.json"),serde_json::to_vec_pretty(&serde_json::json!({"tool":"Stateright 0.31.0","bounds":"EXIT-SCOPE.md","timeout_seconds_per_run":120,"runs":runs,"checks_pass":ok})).unwrap()).unwrap();
    if !ok {
        std::process::exit(1);
    }
}
