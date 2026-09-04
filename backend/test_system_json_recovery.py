"""system.json 损坏时的启动恢复与备份保护

线上事故：磁盘写满后 /data/system.json 变成空文件，ProfileRepository 直接抛
ProfileRepositoryError，Flask 反复启动失败，supervisor 最终放弃。
备份文件本身也可能在同一次磁盘满时被 shutil.copy2 截断，导致最后一份好数据一起丢失。
"""
import json
import os
from pathlib import Path

import pytest

from backend.common.config_repository import ProfileRepository, ProfileRepositoryError


def _bootstrap(tmp_path: Path) -> ProfileRepository:
    """初始化并写入两次，使备份中已包含 alpha

    备份保存的是「写入前」的那一份，因此始终落后一个版本。要让备份里出现
    alpha，必须在创建 alpha 之后再发生一次写入。
    """
    repo = ProfileRepository(tmp_path)
    repo.create_profile({"id": "alpha", "name": "Alpha"})
    repo.create_profile({"id": "beta", "name": "Beta"})
    return repo


def test_empty_system_json_recovers_from_backup(tmp_path):
    _bootstrap(tmp_path)
    system_file = tmp_path / "system.json"
    backup_file = tmp_path / "system.json.bak"
    assert backup_file.exists(), "每次写入都应留下备份"

    # 复现磁盘写满：文件被截断为 0 字节
    system_file.write_text("", encoding="utf-8")

    repo = ProfileRepository(tmp_path)

    profiles = {p["id"] for p in repo.get_system()["profiles"]}
    assert "alpha" in profiles, "应从备份恢复而不是抛异常"
    assert json.loads(system_file.read_text(encoding="utf-8")), "恢复后 system.json 必须是有效 JSON"


def test_truncated_system_json_recovers_from_backup(tmp_path):
    _bootstrap(tmp_path)
    system_file = tmp_path / "system.json"

    # 半截 JSON，同样是磁盘满的典型产物
    system_file.write_text('{"schema_version": 2, "profil', encoding="utf-8")

    repo = ProfileRepository(tmp_path)
    assert "alpha" in {p["id"] for p in repo.get_system()["profiles"]}


def test_corrupt_system_json_is_preserved_for_diagnosis(tmp_path):
    _bootstrap(tmp_path)
    system_file = tmp_path / "system.json"
    system_file.write_text("", encoding="utf-8")

    ProfileRepository(tmp_path)

    corrupt_copies = list(tmp_path.glob("system.json.corrupt-*"))
    assert corrupt_copies, "损坏的原文件应保留副本供排查，而不是被直接覆盖"


def test_backup_survives_a_failed_write(tmp_path, monkeypatch):
    repo = _bootstrap(tmp_path)
    backup_file = tmp_path / "system.json.bak"
    good_backup = backup_file.read_text(encoding="utf-8")

    real_replace = os.replace

    def failing_replace(src, dst, *args, **kwargs):
        # 模拟磁盘满：新内容始终无法提交
        if str(dst).endswith("system.json"):
            raise OSError(28, "No space left on device")
        return real_replace(src, dst, *args, **kwargs)

    monkeypatch.setattr(os, "replace", failing_replace)

    with pytest.raises(Exception):
        repo.create_profile({"id": "beta", "name": "Beta"})

    monkeypatch.undo()
    assert backup_file.read_text(encoding="utf-8") == good_backup, (
        "写入失败时不得破坏已有备份"
    )


def test_both_system_and_backup_corrupt_raises_clear_error(tmp_path):
    _bootstrap(tmp_path)
    (tmp_path / "system.json").write_text("", encoding="utf-8")
    (tmp_path / "system.json.bak").write_text("", encoding="utf-8")

    # 两份都坏时不应静默造出空配置，必须明确报错让人工介入
    with pytest.raises(ProfileRepositoryError):
        ProfileRepository(tmp_path)
