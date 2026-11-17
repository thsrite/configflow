"""N7-2 public relink + repository transaction differential oracle.

This is a test HTTP shell, not ConfigFlow's unimplemented Go proxy-group route.
C33 must exercise the actual route after it is wired.
"""
import json
import sys
from pathlib import Path

from flask import Flask, jsonify, request
from backend.common import config
from backend.common.config_repository import ProfileRepository
from backend.utils.mihomo_listeners import relink_group_listeners


def run(case, directory):
    repo = ProfileRepository(directory)
    config.set_repository(repo)
    repo.update_profile_transaction("default", lambda profile: profile.update(
        proxy_groups=[{"id": "g", "name": "Pinned", "type": "select", "manual_nodes": ["DIRECT"], "future": "keep"}],
        mihomo={"custom_config": case["source"]},
    ))
    before = repo.head()
    app = Flask(__name__)

    @app.post("/relink")
    def relink():
        data = request.get_json()
        changed = []
        def mutate(profile):
            # Mutate before relink to prove transaction rollback is atomic.
            profile["proxy_groups"][0]["name"] = data["new"] if data["new"] is not None else "Disabled"
            changed[:] = relink_group_listeners(profile, "Pinned", data["new"])
            if data.get("fail_after"):
                raise ValueError("synthetic rollback")
        try:
            repo.update_profile_transaction("default", mutate)
            return jsonify(success=True, changed=changed)
        except Exception as error:
            return jsonify(success=False, error=str(error)), 400

    response = app.test_client().post("/relink", json={"new": case["new"], "fail_after": case["fail_after"]})
    saved = repo.get_profile("default")
    return dict(status=response.status_code, body=response.get_json(),
                source=saved["mihomo"]["custom_config"], groups=saved["proxy_groups"],
                committed=repo.head() != before)


cases = json.loads(Path(sys.argv[1]).read_text())
base = Path(sys.argv[3])
results = [run(case, base / str(index)) for index, case in enumerate(cases)]
Path(sys.argv[2]).write_text(json.dumps(results, ensure_ascii=False))
