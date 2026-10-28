#!/usr/bin/env python3
"""Recompile one contract of a Hardhat build with a patched source (controls only).

usage: solc-overlay.py <solc> <build-info.json> <source path> <patched file> <artifact.json>

Takes the exact standard-JSON input recorded in the build info, replaces one
source with the patched file, compiles it with the given solc (the version the
build info names), and rewrites the contract's bytecode fields in the Hardhat
artifact. Run with python3 -I. The artifact must be a scratch copy.
"""
import json, subprocess, sys

solc, info_path, source, patched, artifact_path = sys.argv[1:6]
info = json.load(open(info_path))
standard = info["input"]
if source not in standard["sources"]:
    sys.exit(f"{source} is not in the build input")
standard["sources"][source]["content"] = open(patched).read()
version = subprocess.run([solc, "--version"], capture_output=True, text=True, check=True).stdout
if info["solcLongVersion"] not in version:
    sys.exit(f"solc is not {info['solcLongVersion']}")
out = json.loads(subprocess.run([solc, "--standard-json"], input=json.dumps(standard), capture_output=True, text=True, check=True).stdout)
errors = [e for e in out.get("errors", []) if e["severity"] == "error"]
if errors:
    sys.exit("\n".join(e["formattedMessage"] for e in errors))
artifact = json.load(open(artifact_path))
contract = out["contracts"][source][artifact["contractName"]]
artifact["bytecode"] = "0x" + contract["evm"]["bytecode"]["object"]
artifact["deployedBytecode"] = "0x" + contract["evm"]["deployedBytecode"]["object"]
artifact["linkReferences"] = contract["evm"]["bytecode"]["linkReferences"]
artifact["deployedLinkReferences"] = contract["evm"]["deployedBytecode"]["linkReferences"]
json.dump(artifact, open(artifact_path, "w"), indent=2)
print(f"recompiled {artifact['contractName']} from {source}")
