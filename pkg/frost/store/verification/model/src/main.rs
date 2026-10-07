//! Runs the K-02 host model: the claim model with all safety properties and
//! witnesses, then each declared weakening. Writes machine-readable results
//! and every discovered trace. Exit code 0 only when every run completed
//! within its limits, no claim-model safety property has a counterexample,
//! every witness is discovered, and every weakening breaks its declared
//! property.

use k02_host_model::{
    boundary_traces, run, HostModel, Trace, Weakening, BOUNDARY_WITNESS, DEFAULT_MAX_COMMITS, SAFETY, WITNESSES,
};
use serde::Serialize;
use std::collections::BTreeMap;
use std::fs;
use std::path::PathBuf;
use std::time::Duration;

#[derive(Serialize)]
struct Results {
    tool: &'static str,
    stateright: &'static str,
    max_commits: u8,
    threads: usize,
    timeout_secs: u64,
    runs: Vec<k02_host_model::RunReport>,
    /// property -> pass | fail | blocked, for the claim model
    claim_model: BTreeMap<String, String>,
    /// weakening -> declared property -> counterexample found
    weakenings: BTreeMap<String, BTreeMap<String, bool>>,
    witnesses: BTreeMap<String, String>,
    traces: Vec<String>,
    overall: String,
}

fn main() {
    let mut out: Option<PathBuf> = None;
    let mut threads = 12usize;
    let mut timeout = 600u64;
    let mut max_commits = DEFAULT_MAX_COMMITS;
    let mut skip_weakenings = false;
    let mut args = std::env::args().skip(1);
    while let Some(a) = args.next() {
        match a.as_str() {
            "--out" => out = Some(PathBuf::from(args.next().expect("--out DIR"))),
            "--threads" => threads = args.next().expect("--threads N").parse().expect("threads"),
            "--timeout-secs" => timeout = args.next().expect("--timeout-secs S").parse().expect("timeout"),
            "--max-commits" => max_commits = args.next().expect("--max-commits K").parse().expect("max commits"),
            "--claim-only" => skip_weakenings = true,
            other => panic!("unknown argument {other}"),
        }
    }
    let out = out.expect("--out DIR is required");
    fs::create_dir_all(out.join("traces")).expect("create output directory");
    let timeout = Duration::from_secs(timeout);

    let mut results = Results {
        tool: "k02-host-model",
        stateright: "0.31.0",
        max_commits,
        threads,
        timeout_secs: timeout.as_secs(),
        runs: Vec::new(),
        claim_model: BTreeMap::new(),
        weakenings: BTreeMap::new(),
        witnesses: BTreeMap::new(),
        traces: Vec::new(),
        overall: "pass".into(),
    };
    let mut all_traces: Vec<Trace> = Vec::new();
    let mut ok = true;

    eprintln!("claim model: max_commits={max_commits} threads={threads} timeout={}s", timeout.as_secs());
    let claim = run(HostModel { weakening: Weakening::None, max_commits }, threads, timeout);
    eprintln!(
        "  completed={} unique={} generated={} depth={} seconds={:.1}",
        claim.report.completed, claim.report.unique_states, claim.report.generated_states, claim.report.max_depth, claim.report.seconds
    );
    for name in SAFETY {
        let found = claim.report.discoveries.get(name).copied().unwrap_or(false);
        let verdict = if !claim.report.completed {
            "blocked"
        } else if found {
            "fail"
        } else {
            "pass"
        };
        if verdict != "pass" {
            ok = false;
        }
        eprintln!("  {name}: {verdict}");
        results.claim_model.insert(name.to_string(), verdict.to_string());
    }
    for (name, predicate, _) in WITNESSES {
        let found = claim.report.discoveries.get(name).copied().unwrap_or(false);
        let verdict = if found {
            "discovered"
        } else if !claim.report.completed {
            "blocked"
        } else {
            "missing"
        };
        if verdict != "discovered" {
            ok = false;
        }
        eprintln!("  {name} ({predicate}): {verdict}");
        results.witnesses.insert(name.to_string(), verdict.to_string());
    }
    let both_lost = claim.report.discoveries.get(BOUNDARY_WITNESS).copied().unwrap_or(false);
    eprintln!("  {BOUNDARY_WITNESS} (assumption A-05 boundary): {}", if both_lost { "reachable" } else { "not reached" });
    results.witnesses.insert(BOUNDARY_WITNESS.to_string(), if both_lost { "reachable".into() } else { "not reached".into() });
    results.runs.push(claim.report);
    all_traces.extend(claim.traces);

    if !skip_weakenings {
        for w in Weakening::ALL {
            eprintln!("weakening {w:?}");
            let r = run(HostModel { weakening: w, max_commits }, threads, timeout);
            let declared = w.declared_failure();
            let found = r.report.discoveries.get(declared).copied().unwrap_or(false);
            eprintln!(
                "  completed={} unique={} generated={} seconds={:.1} {declared} counterexample={found}",
                r.report.completed, r.report.unique_states, r.report.generated_states, r.report.seconds
            );
            if !found {
                ok = false;
            }
            let mut m = BTreeMap::new();
            for name in SAFETY {
                m.insert(name.to_string(), r.report.discoveries.get(name).copied().unwrap_or(false));
            }
            results.weakenings.insert(format!("{w:?}"), m);
            results.runs.push(r.report);
            all_traces.extend(r.traces);
        }
    }

    all_traces.extend(boundary_traces(max_commits));
    for t in &all_traces {
        let name = format!("{}.json", t.id);
        fs::write(out.join("traces").join(&name), serde_json::to_vec_pretty(t).expect("serialize trace")).expect("write trace");
        results.traces.push(name);
    }
    results.overall = if ok { "pass".into() } else { "fail".into() };
    fs::write(out.join("results.json"), serde_json::to_vec_pretty(&results).expect("serialize results")).expect("write results");
    eprintln!("overall: {} ({} traces)", results.overall, results.traces.len());
    if !ok {
        std::process::exit(1);
    }
}
