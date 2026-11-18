#!/usr/bin/env python3
"""Run the registered black-box faults against already built binaries."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import time

from core import SCENARIOS, Source, World, isolation, require
import scenarios  # noqa: F401: registry declarations


class Environment:
    def __init__(self, root, args, catalog):
        self.root, self.args, self.catalog = root, args, catalog
        self.production_binary = args.production_binary
        self.worlds = []
        self.sources = []
        self.contender_log = root / "contender.log"
        self.contender_handles = []

    def world(self, *, suffix=None, dav=False, source=None, clone_from=None):
        name = suffix or f"world-{len(self.worlds)}"
        w = World(self.root / name, self.args.binary, self.args.fixture, self.catalog, dav=dav, source=source, clone_from=clone_from)
        self.worlds.append(w)
        return w

    def shared_source(self, factory=Source):
        source = factory()
        self.sources.append(source)
        return source

    def spawn_contender(self, world):
        env = dict(world.process_env)
        env["CONFIGFLOW_BACKEND_PORT"] = "59999"
        handle = self.contender_log.open("wb")
        self.contender_handles.append(handle)
        process = subprocess.Popen([self.args.binary, "-frontend="], env=env, stdout=handle, stderr=subprocess.STDOUT)
        world.extra_processes.append(process)
        return process

    def close(self):
        for world in reversed(self.worlds):
            world.close()
        for handle in self.contender_handles:
            handle.close()
        for source in self.sources:
            source.close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", required=True)
    parser.add_argument("--fixture", required=True)
    parser.add_argument("--production-binary", required=True)
    parser.add_argument("--output", required=True)
    parser.add_argument("--scenario", action="append", choices=sorted(SCENARIOS.items))
    args = parser.parse_args()
    isolation()
    root = Path(args.output).resolve()
    root.mkdir(mode=0o700)  # Never reuse/clean an existing data directory.
    os.chmod(root, 0o700)
    catalog = json.loads(subprocess.check_output([args.fixture, "-mode=catalog"], timeout=5))
    results = []
    for name in args.scenario or SCENARIOS.items:
        case_root = root / name
        case_root.mkdir(mode=0o700)
        env = Environment(case_root, args, catalog)
        started = time.monotonic()
        fn, covers = SCENARIOS.items[name]
        result = {"name": name, "covers": covers}
        try:
            fn(env)
            require(env.worlds and any(w.checks for w in env.worlds), "scenario has no assertions")
            result["passed"] = True
            result["checks"] = [check for w in env.worlds for check in w.checks]
            result["observations"] = [w.observations for w in env.worlds if w.observations]
        except Exception as error:
            result.update(passed=False, error=f"{type(error).__name__}: {error}")
        finally:
            env.close()
        result["seconds"] = round(time.monotonic() - started, 3)
        results.append(result)
        print(json.dumps(result, ensure_ascii=False), flush=True)
    report = {"synthetic_only": True, "isolation": "network namespace: lo only", "cases": results,
              "binary_sha256": hashlib.sha256(Path(args.binary).read_bytes()).hexdigest(),
              "future_hr_extensions": ["A14", "A16", "A17", "A18", "A22", "R12", "R14", "R17"]}
    (root / "report.json").write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")
    passed = all(item["passed"] for item in results)
    print(f"FAULTS {'PASS' if passed else 'FAIL'}: {sum(r['passed'] for r in results)}/{len(results)}; report={root / 'report.json'}")
    return 0 if passed else 1


if __name__ == "__main__":
    raise SystemExit(main())
