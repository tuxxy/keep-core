//! C-04 consumer/host-attestation semantics. No Go/Solidity implementation
//! imports this model (R-01); the K-05 slot lock is replayed against it.
//! Certificate validity and collision-free ABI/Keccak tuple identity are
//! inputs, not cryptographic conclusions.
use serde::Serialize;
use stateright::{Checker, Model, Property};
use std::{
    collections::BTreeSet,
    fs,
    time::{Duration, Instant},
};
const REPLACE: &str = "P-HOST-REPLACEMENT-FINALIZED-ABSENCE";
const ACTIVE: &str = "P-C04-ONE-ACTIVE-CUSTODY-RESERVATION";
const GEN: &str = "P-C04-NEXT-INTENT-GENERATION";
const VERSION: &str = "P-C04-CURRENT-STATE-VERSION";
const LOCK: &str = "P-HOST-ONE-INTENT-ATTESTATION";
const EXPIRY: &str = "P-C04-NO-EXPIRED-ADMISSION";
const ID: &str = "P-C04-RESERVATION-ID-CONTEXT";
#[derive(Clone, Copy, Debug, Hash, Eq, PartialEq, Serialize)]
struct State {
    tick: u8,
    renewed_slot: Option<u8>,
    renewal_blocked_replacement: bool,
    finalized_tick: Option<u8>,
    // Custody facts at the finalized tick. The host's absence check reads
    // finalized canonical state, never the current unfinalized state.
    finalized_counter: u8,
    finalized_version: u8,
    absence_slot: Option<u8>,
    replacement_slot: Option<u8>,
    replacement_admitted: bool,
    replacement_witness_candidate: bool,
    issued_live_mask: u8,
    bad_replacement: bool,
    bad_lock: bool,
    counter: u8,
    version: u8,
    readiness_generation: u8,
    admissions: u8,
    last_admitted_version: u8,
    locks: [u8; 4],
    attestation_history: [u8; 4],
    reserved: [bool; 2],
    active: [bool; 2],
    deadlines: [u8; 2],
    crash_used: bool,

    bad_generation: bool,
    bad_version: bool,
    bad_expiry: bool,
    bad_id: bool,
    refused_lock: bool,
    refused_admitted_replacement: bool,
    refused_expiry: bool,
    refused_version: bool,
}
#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize)]
enum Action {
    Tick,
    FinalizeTick,
    ConfirmAbsence(u8, u8),
    Renew(u8, u8),
    ReadinessChange,
    StateSpend,
    PolicyChange,
    Crash,
    Attest(u8, u8, u8),
    Admit(u8, u8, u8, u8, bool),
}
#[derive(Clone, Copy)]
struct Admission {
    control: &'static str,
}
impl Admission {
    fn full_deadline(s: &State, slot: usize) -> u8 {
        if s.locks[slot] == 1 {
            if s.renewed_slot == Some(slot as u8) {
                2
            } else {
                1
            }
        } else {
            3
        }
    }
    fn observed_deadline(&self, s: &State, slot: usize) -> u8 {
        if self.control == "IgnoreOutstandingRenewal" && s.locks[slot] == 1 {
            1
        } else {
            Self::full_deadline(s, slot)
        }
    }
    /// The implemented replacement guard (Attester.lock): every prior inclusive
    /// deadline is finalized, and finalized custody shows the generation was
    /// not admitted and the state version is unchanged. `check_deadline` is
    /// false only for the ReplaceBeforeInclusiveDeadline weakening.
    fn finalized_absence(s: &State, g: u8, v: u8, deadline: u8, check_deadline: bool) -> bool {
        s.finalized_tick
            .is_some_and(|f| !check_deadline || f >= deadline)
            && s.finalized_counter < g
            && s.finalized_version == v
    }
    fn target(&self) -> &'static str {
        match self.control {
            "ReplaceBeforeInclusiveDeadline"
            | "ReplaceWithoutFinalizedAbsence"
            | "IgnoreOutstandingRenewal" => REPLACE,
            "AllowConcurrentReservations" => ACTIVE,
            "SkipNextGeneration" | "UseReadinessGeneration" => GEN,
            "AdmitOldStateVersion" => VERSION,
            "ForgetTupleLocksOnCrash" | "OmitTupleLock" => LOCK,
            "IgnoreCertificateExpiry" => EXPIRY,
            _ => ID,
        }
    }
}
impl Model for Admission {
    type State = State;
    type Action = Action;
    fn init_states(&self) -> Vec<State> {
        [true]
            .into_iter()
            .map(|_| State {
                tick: 0,
                renewed_slot: None,
                renewal_blocked_replacement: false,
                finalized_tick: None,
                finalized_counter: 0,
                finalized_version: 1,
                absence_slot: None,
                replacement_slot: None,
                replacement_admitted: false,
                replacement_witness_candidate: false,
                issued_live_mask: 0,
                bad_replacement: false,
                bad_lock: false,
                counter: 0,
                version: 1,
                readiness_generation: 1,
                admissions: 0,
                last_admitted_version: 0,
                locks: [0; 4],
                attestation_history: [0; 4],
                reserved: [false; 2],
                active: [false; 2],
                deadlines: [0; 2],
                crash_used: false,

                bad_generation: false,
                bad_version: false,
                bad_expiry: false,
                bad_id: false,
                refused_lock: false,
                refused_admitted_replacement: false,
                refused_expiry: false,
                refused_version: false,
            })
            .collect()
    }
    fn actions(&self, s: &State, o: &mut Vec<Action>) {
        if s.tick < 3 {
            o.push(Action::Tick)
        }
        if s.tick > 0 && s.finalized_tick != Some(s.tick) {
            o.push(Action::FinalizeTick);
        }
        if s.readiness_generation == 1 {
            o.push(Action::ReadinessChange)
        }
        if s.version == 1 {
            o.push(Action::StateSpend);
            o.push(Action::PolicyChange)
        }
        if !s.crash_used {
            o.push(Action::Crash)
        }
        for g in 1..=2 {
            for v in 1..=2 {
                o.push(Action::ConfirmAbsence(g, v));
                o.push(Action::Renew(g, v));
                for plan in 0..2 {
                    o.push(Action::Attest(g, v, plan));
                    if s.admissions < 2
                        && !s.reserved[(g - 1) as usize]
                        && (!s.active.contains(&true)
                            || self.control == "AllowConcurrentReservations")
                    {
                        for deadline in 1..=3 {
                            o.push(Action::Admit(g, v, plan, deadline, true));
                            o.push(Action::Admit(g, v, plan, deadline, false));
                        }
                    }
                }
            }
        }
    }
    fn next_state(&self, s: &State, a: Action) -> Option<State> {
        let mut n = *s;
        match a {
            Action::Tick => n.tick += 1,
            Action::FinalizeTick => {
                n.finalized_tick = Some(s.tick);
                n.finalized_counter = s.counter;
                n.finalized_version = s.version;
            }
            Action::Renew(g, v) => {
                let slot = ((g - 1) * 2 + (v - 1)) as usize;
                // One same-intent renewal: original deadline 1 -> deadline 2.
                // History remains durable and any older absence observation is invalid.
                if s.renewed_slot.is_some()
                    || s.replacement_slot.is_some()
                    || s.locks[slot] != 1
                    || s.tick > 2
                    || s.counter >= g
                    || s.reserved[(g - 1) as usize]
                {
                    return None;
                }
                n.renewed_slot = Some(slot as u8);
                n.absence_slot = None;
            }
            Action::ConfirmAbsence(g, v) => {
                let slot = ((g - 1) * 2 + (v - 1)) as usize;
                let old = s.locks[slot];
                // Two intents; certificates bind original/renewal/replacement deadlines 1/2/3.
                let deadline = self.observed_deadline(s, slot);
                if old == 0 || !Self::finalized_absence(s, g, v, deadline, true) {
                    return None;
                }
                // Authenticated canonical finalized absence is a supplier fact,
                // not a provider inferred solely from an elapsed deadline.
                n.absence_slot = Some(slot as u8);
            }
            Action::ReadinessChange => {
                n.readiness_generation = 2;
                if self.control == "UseReadinessGeneration" {
                    n.counter = n.readiness_generation
                }
            }
            // A finalized tick cannot change; custody changes need a later tick.
            Action::StateSpend | Action::PolicyChange
                if s.finalized_tick.is_some_and(|f| s.tick <= f) =>
            {
                return None
            }
            Action::StateSpend => {
                n.version = 2;
                n.active = [false; 2];
            }
            Action::PolicyChange => n.version = 2,
            Action::Crash => {
                n.crash_used = true;
                if self.control == "ForgetTupleLocksOnCrash" {
                    n.locks = [0; 4]
                }
            }
            Action::Attest(g, v, p) => {
                // A complete statement binds its deadline. Replacement is bounded
                // to one slot once, after expiry plus authenticated finalized absence.
                let slot = ((g - 1) * 2 + (v - 1)) as usize;
                let bit = 1 << p;
                let old = s.locks[slot];
                let deadline = self.observed_deadline(s, slot);
                // The supplier must have authenticated the finalized absence
                // read for this slot (absence_slot); the guard itself is the
                // implemented finalized-state rule.
                let authenticated = s.absence_slot == Some(slot as u8);
                let expired = s.tick > deadline;
                let full_deadline = Self::full_deadline(s, slot);
                let legal_replacement = old != 0
                    && old != bit
                    && s.replacement_slot.is_none()
                    && Self::finalized_absence(s, g, v, full_deadline, true);
                let observed_replacement = old != 0
                    && old != bit
                    && s.replacement_slot.is_none()
                    && authenticated
                    && Self::finalized_absence(s, g, v, deadline, true);
                let control_replacement = old != 0
                    && old != bit
                    && s.replacement_slot.is_none()
                    && ((self.control == "ReplaceBeforeInclusiveDeadline"
                        && Self::finalized_absence(s, g, v, deadline, false))
                        || (self.control == "ReplaceWithoutFinalizedAbsence"
                            && s.finalized_tick.is_some_and(|f| f >= deadline)));
                if observed_replacement || control_replacement {
                    n.replacement_witness_candidate = legal_replacement
                        && s.issued_live_mask & (1 << slot) != 0
                        && g == s.counter + 1
                        && v == s.version;
                    n.bad_replacement |= !legal_replacement;
                    n.replacement_slot = Some(slot as u8);
                    n.absence_slot = None;
                    n.locks[slot] = bit;
                    n.attestation_history[slot] |= bit;
                } else if old != 0 && old != bit && s.attestation_history[slot] & bit != 0 {
                    // Identical retransmission of a statement already in the
                    // durable history is idempotent and changes nothing.
                    return None;
                } else if old != 0 && old != bit && self.control != "OmitTupleLock" {
                    n.refused_lock = true;
                    n.refused_admitted_replacement |= s.reserved[(g - 1) as usize]
                        && s.tick > s.deadlines[(g - 1) as usize]
                        && expired;
                    n.renewal_blocked_replacement |= s.renewed_slot == Some(slot as u8)
                        && s.finalized_tick
                            .is_some_and(|f| f >= 1 && f < full_deadline);
                } else {
                    // A lost/omitted lock cannot erase a prior attestation.
                    n.bad_lock |=
                        s.attestation_history[slot] != 0 && s.attestation_history[slot] & bit == 0;
                    if old == 0
                        && s.tick <= if p == 0 { 1 } else { 3 }
                        && g == s.counter + 1
                        && v == s.version
                        && !s.active.contains(&true)
                        && !s.finalized_tick.is_some_and(|f| s.tick <= f)
                    {
                        n.issued_live_mask |= 1 << slot;
                    }
                    n.locks[slot] = bit;
                    n.attestation_history[slot] |= bit;
                }
            }
            Action::Admit(g, v, plan, deadline, id_matches) => {
                // A finalized block cannot gain a later admission. This matters
                // for the authenticated absence proof at the inclusive deadline.
                if s.finalized_tick.is_some_and(|f| s.tick <= f) {
                    return None;
                }
                // Authority-valid certificates are inputs. The missing real consumer must
                // validate funded state, exact intent, eligibility, policy and these guards.
                let expired = s.tick > deadline;
                if g != s.counter + 1 && self.control != "SkipNextGeneration" {
                    return None;
                }
                if v != s.version && self.control != "AdmitOldStateVersion" {
                    n.refused_version |= v < s.version;
                } else if expired && self.control != "IgnoreCertificateExpiry" {
                    n.refused_expiry |= s.tick > deadline;
                } else if !id_matches && self.control != "AcceptForeignReservationID" {
                    return None;
                } else {
                    n.bad_generation |= g != s.counter + 1;
                    n.bad_version |= v != s.version;
                    // Inclusive admission deadline; it does not revoke an existing active reservation.
                    n.bad_expiry |= s.tick > deadline;
                    n.bad_id |= !id_matches;
                    let slot = ((g - 1) * 2 + (v - 1)) as usize;
                    n.replacement_admitted |= s.replacement_slot == Some(slot as u8)
                        && s.locks[slot] == 1 << plan
                        && deadline == if plan == 0 { 1 } else { 3 };
                    n.counter += 1;
                    n.admissions += 1;
                    n.last_admitted_version = v;
                    n.reserved[(g - 1) as usize] = true;
                    n.active[(g - 1) as usize] = true;
                    n.deadlines[(g - 1) as usize] = deadline;
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
            Property::<Self>::always(ACTIVE, |_, s| s.active.iter().filter(|v| **v).count() <= 1),
            Property::<Self>::always(GEN, |_, s| !s.bad_generation && s.counter == s.admissions),
            Property::<Self>::always(VERSION, |_, s| !s.bad_version),
            Property::<Self>::always(LOCK, |_, s| !s.bad_lock),
            Property::<Self>::always(REPLACE, |_, s| !s.bad_replacement),
            Property::<Self>::always(EXPIRY, |_, s| !s.bad_expiry),
            Property::<Self>::always(ID, |_, s| !s.bad_id),
            Property::<Self>::sometimes("W-expired-unadmitted-replaced-and-admitted", |_, s| {
                s.replacement_admitted
                    && s.replacement_witness_candidate
                    && !s.bad_replacement
                    && !s.bad_lock
            }),
            Property::<Self>::sometimes("W-admitted-expiry-refuses-replacement", |_, s| {
                s.tick == 3
                    && s.refused_admitted_replacement
                    && s.admissions > 0
                    && s.replacement_slot.is_none()
                    && !s.bad_replacement
            }),
            Property::<Self>::sometimes("W-outstanding-renewal-blocks-replacement", |_, s| {
                s.renewal_blocked_replacement && !s.bad_replacement
            }),
            Property::<Self>::sometimes(
                "W-renewal-deadline-finalized-before-replacement-admission",
                |_, s| {
                    s.replacement_admitted
                        && s.replacement_witness_candidate
                        && s.replacement_slot == s.renewed_slot
                        && s.renewed_slot.is_some()
                        && !s.bad_replacement
                        && !s.bad_lock
                },
            ),
            Property::<Self>::sometimes("W-next-two-generations", |_, s| {
                s.counter == 2 && s.admissions == 2
            }),
            Property::<Self>::sometimes("W-readiness-independent", |_, s| {
                s.readiness_generation == 2 && s.counter == 0
            }),
            Property::<Self>::sometimes("W-state-version2-admission", |_, s| {
                s.version == 2 && s.last_admitted_version == 2 && !s.bad_version
            }),
            Property::<Self>::sometimes("W-conflicting-attestation-refused", |_, s| s.refused_lock),
            Property::<Self>::sometimes("W-expired-certificate-refused", |_, s| {
                s.refused_expiry && s.tick == 3
            }),
            Property::<Self>::sometimes("W-stale-version-refused", |_, s| s.refused_version),
            Property::<Self>::sometimes("W-admission-survives-certificate-deadline", |_, s| {
                s.active
                    .iter()
                    .enumerate()
                    .any(|(g, active)| *active && s.tick > s.deadlines[g])
            }),
        ]
    }
}
pub fn run(out: &str) -> bool {
    let mut reports = vec![];
    let mut ok = true;
    fs::create_dir_all(format!("{out}/admission-traces")).unwrap();
    for control in [
        "None",
        "ReplaceBeforeInclusiveDeadline",
        "ReplaceWithoutFinalizedAbsence",
        "IgnoreOutstandingRenewal",
        "SkipNextGeneration",
        "UseReadinessGeneration",
        "AdmitOldStateVersion",
        "ForgetTupleLocksOnCrash",
        "OmitTupleLock",
        "IgnoreCertificateExpiry",
        "AcceptForeignReservationID",
        "AllowConcurrentReservations",
    ] {
        let m = Admission { control };
        let start = Instant::now();
        let mut b = m.checker().threads(4).timeout(Duration::from_secs(1800));
        if control != "None" {
            b = b.finish_when(stateright::HasDiscoveries::AllOf(BTreeSet::from([
                m.target()
            ])))
        }
        let c = b.spawn_bfs().join();
        let seconds = start.elapsed().as_secs_f64();
        let completed = c.is_done() && seconds < 1800.;
        let mut discoveries = std::collections::BTreeMap::new();
        for p in m.properties() {
            let path = c.discovery(p.name);
            discoveries.insert(p.name, path.is_some());
            if let Some(path) = path.filter(|_| control == "None" || p.name == m.target()) {
                let actions: Vec<Action> = path.into_actions();
                let mut initial = m.init_states();
                // Replay from the fixed initial custody/version world.
                let mut chosen = None;
                for s in initial.drain(..) {
                    let mut state = s;
                    let mut valid = true;
                    for a in &actions {
                        if let Some(next) = m.next_state(&state, *a) {
                            state = next
                        } else {
                            valid = false;
                            break;
                        }
                    }
                    let prop = m
                        .properties()
                        .into_iter()
                        .find(|x| x.name == p.name)
                        .unwrap();
                    if valid
                        && match prop.expectation {
                            stateright::Expectation::Always => !(prop.condition)(&m, &state),
                            _ => (prop.condition)(&m, &state),
                        }
                    {
                        chosen = Some(s);
                        break;
                    }
                }
                let mut state = chosen.expect("trace initial world");
                let mut claim_state = state;
                let claim = Admission { control: "None" };
                let mut diverged = false;
                let mut claim_steps = vec![];
                let mut steps = vec![];
                for (i, a) in actions.iter().enumerate() {
                    state = m.next_state(&state, *a).unwrap();
                    let next = if diverged {
                        None
                    } else {
                        let mut offered = vec![];
                        claim.actions(&claim_state, &mut offered);
                        if offered.contains(a) {
                            claim.next_state(&claim_state, *a)
                        } else {
                            None
                        }
                    };
                    let first = !diverged && next.as_ref() != Some(&state);
                    if let Some(next) = next {
                        claim_state = next;
                    }
                    if first {
                        diverged = true;
                    }
                    steps.push(serde_json::json!({"n":i+1,"action":a,"expect_state":state,"diverges":first}));
                    if !diverged || first {
                        claim_steps.push(serde_json::json!({"n":i+1,"action":a,"expect_state":claim_state,"diverges":first}));
                    }
                }
                let t = serde_json::json!({"id":format!("{control}-{}",p.name),"kind":if p.name.starts_with("W-"){"witness"}else{"counterexample"},"property":p.name,"weakening":control,"initial_state":chosen,"replay":if control=="None"{"full"}else{"partial"},"replay_note":"Host slot-lock steps replay on the real K-05 Attester.lock and store (redemption.TestV04AdmissionTraceReplay); contract and chain steps drive a controlled C-04 reader (H-03). Weakened traces replay their claim-model steps cut at the first divergence.","steps":steps,"claim_model_steps":claim_steps});
                fs::write(
                    format!("{out}/admission-traces/{control}-{}.json", p.name),
                    serde_json::to_vec_pretty(&t).unwrap(),
                )
                .unwrap();
            }
        }
        let pass = completed
            && if control == "None" {
                [GEN, VERSION, LOCK, EXPIRY, ID, ACTIVE, REPLACE]
                    .iter()
                    .all(|p| !discoveries[*p])
                    && discoveries
                        .iter()
                        .filter(|(p, _)| p.starts_with("W-"))
                        .all(|(_, v)| *v)
            } else {
                discoveries[m.target()]
            };
        ok &= pass;
        let report = serde_json::json!({"control":control,"target":m.target(),"completed_before_deadline":completed,"seconds":seconds,"unique_states":c.unique_state_count(),"generated_states":c.state_count(),"max_depth":c.max_depth(),"discoveries":discoveries,"pass":pass});
        eprintln!("admission {report}");
        reports.push(report);
    }
    fs::write(
        format!("{out}/admission-results.json"),
        serde_json::to_vec_pretty(&reports).unwrap(),
    )
    .unwrap();
    ok
}
