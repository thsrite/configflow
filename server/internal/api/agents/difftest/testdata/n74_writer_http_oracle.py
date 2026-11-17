"""Record the four explicit NEL HTTP writer differences; patch writers only.

Inputs were captured by the real Flask differential with its existing
nativeQueryVariant boundary. No business function or route is patched.
"""
import hashlib
import json
import os
from pathlib import Path
import sys
import tempfile

ROOT = Path(__file__).resolve().parent
REPO = ROOT.parents[5]
sys.path.insert(0, str(REPO / "server/internal/installer/testdata"))


def main():
    with tempfile.TemporaryDirectory() as data:
        os.environ.update(DATA_DIR=data, ADMIN_USERNAME="", ADMIN_PASSWORD="", LOG_FILE=str(Path(data) / "app.log"))
        from n74_writer_oracle import NativeWriter, writer_hooks, packed
        from flask import Flask
        from backend.routes import agents, agents_bp  # real route registration

        app = Flask(__name__)
        app.register_blueprint(agents_bp)
        client = app.test_client()
        native = NativeWriter(sys.argv[1])
        records = []
        try:
            for source in json.loads((ROOT / "n74_writer_http_inputs.json").read_text()):
                before, after = [], []
                with writer_hooks(native, False, before):
                    old = client.get(source["oracle_path"])
                assert old.status_code == source["status"] == 200
                assert old.content_type == source["content_type"]
                assert hashlib.sha256(old.data).hexdigest() == source["old_sha256"]
                with writer_hooks(native, True, after):
                    new = client.get(source["oracle_path"])
                assert old.status_code == new.status_code
                assert {k: v for k, v in old.headers if k != "Content-Length"} == {
                    k: v for k, v in new.headers if k != "Content-Length"}
                assert len(before) == len(after) == 1 and before[0] == after[0]
                changes = native.request(dict(op="compare", old=old.data.decode(), new=new.data.decode()))["changes"]
                assert changes and all(c["category"] == "unicode-line-separator-preservation" for c in changes)
                assert all(set(c["restored"]) == {"U+0085"} for c in changes)
                records.append(dict(**source, calls=after, expected_yaml=new.data.decode(), changes=changes))
            fixture = dict(records=records, graphs=native.graphs, unknown=0,
                           source_sha256=hashlib.sha256((ROOT / "n74_writer_http_inputs.json").read_bytes()).hexdigest())
            (ROOT / "n74_writer_http_expected.json").write_bytes(packed(fixture) + b"\n")
            print(json.dumps(dict(rows=len(records), graphs=len(native.graphs), NEL_rows=len(records), unknown=0)))
        finally:
            native.close()


if __name__ == "__main__":
    main()
