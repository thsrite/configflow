"""Synthetic full-storage differential for the N8-5a transaction core."""
import json
import os
from pathlib import Path
import sqlite3
import sys

sys.path.insert(0, os.environ["CONFIGFLOW_REPO"])
from backend.common.config_repository import ProfileRepository


def main():
    directory, output, action = Path(sys.argv[1]), Path(sys.argv[2]), sys.argv[3]
    repository = ProfileRepository(directory)
    if action == "seed":
        with repository._document(write=True) as document:
            document["future_root"] = {"ordered": [1, 1.25, None], "unknown": True}
            document["profiles"]["default"]["description"] = {"malformed": [None, "kept"]}
            settings = document["system"]["system_config"]
            settings["default_profile_id"] = None
            settings["future_setting"] = {"nested": {"future": "<值>"}}
            document["system"]["backup"]["auto_backup"] = "malformed"
        (directory / "future.json").write_text(' {"null":null,"unknown":[1.0]}\n')
    else:
        with repository._document(write=True) as document:
            if action == "rename":
                document["profiles"]["default"]["name"] = "review renamed"
            elif action == "same-row":
                document["system"]["system_config"]["server_domain"] = "cf.example.com"
                document["system"]["backup"]["webdav_path"] = "/review"
            elif action == "replace":
                document["profiles"]["default"]["description"] = "repaired"
            elif action == "clear":
                document["system"]["system_config"].pop("default_profile_id", None)
                document["system"]["backup"].pop("auto_backup", None)
            else:
                raise AssertionError("unknown synthetic action")
    with sqlite3.connect(directory / "configflow.sqlite3") as connection:
        tables = {table: connection.execute(f"SELECT * FROM {table} ORDER BY {order}").fetchall()
                  for table, order in (("meta", "key"), ("sections", "scope, owner"),
                                       ("fields", "scope, owner, field"))}
    files = {path.relative_to(directory).as_posix(): path.read_text()
             for path in sorted(directory.rglob("*"))
             if path.is_file() and path.name != "configflow.sqlite3"}
    output.write_text(json.dumps({"tables": tables, "files": files,
                                  "export": repository.export_all()}, ensure_ascii=False))


if __name__ == "__main__":
    main()
