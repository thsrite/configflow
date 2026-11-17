"""Audit actual custom_config scalar tags; no user text is printed."""
import collections
import gzip
import json
import pathlib
import subprocess
import sys

root = pathlib.Path(__file__).resolve().parent
process = subprocess.Popen([sys.argv[1]], stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True)
sources = {}
occurrences = collections.Counter()
for name in ("mihomo_corpus.jsonl.gz", "mihomo_recorded.jsonl.gz", "review_mihomo.jsonl.gz"):
    with gzip.open(root / name, "rt") as stream:
        for line in stream:
            row = json.loads(line)
            def walk(value):
                if isinstance(value, dict):
                    for key, child in value.items():
                        if key == "custom_config" and isinstance(child, str) and child.strip():
                            occurrences[child] += 1
                        else:
                            walk(child)
                elif isinstance(value, list):
                    for child in value:
                        walk(child)
            walk(row.get("args"))
counts = collections.Counter()
for source, count in occurrences.items():
    process.stdin.write(json.dumps(dict(mode="convert-document", source=source)) + "\n")
    process.stdin.flush()
    answer = json.loads(process.stdout.readline())
    sources[source] = answer
    nodes = answer.get("parse_nodes", [])
    def visit(identity, path, seen):
        if identity in seen:
            return
        node = nodes[identity - 1]
        seen = seen | {identity}
        if node["kind"] == 8 and (node["tag"] in ("!!timestamp", "!!binary") or node["tag"] == "!!float" and node["value"].lower().lstrip("+-") in (".inf", ".nan")):
            counts[(node["tag"], path)] += count
        children = node.get("content") or []
        if node["kind"] == 4:
            for i in range(0, len(children), 2):
                key = nodes[children[i] - 1]["value"]
                visit(children[i+1], path + "." + key, seen)
        else:
            for child in children:
                visit(child, path + "[]", seen)
        if node.get("alias"):
            visit(node["alias"], path, seen)
    if nodes:
        visit(1, "", set())
process.stdin.close()
assert process.wait() == 0
print(json.dumps(dict(unique_sources=len(sources), occurrences=sum(occurrences.values()), fields=[dict(tag=tag, path=path, occurrences=count) for (tag,path),count in sorted(counts.items())]), ensure_ascii=True))
