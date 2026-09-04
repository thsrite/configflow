"""UI 不得宣称尚未实现的跨配置空间共享

订阅、节点、订阅聚合、规则库在 PROFILE_FIELDS 中，按配置空间隔离。
界面上曾把它们标为「共享资源 · 所有配置空间共用」，会让用户误以为
改一处对所有配置生效。此测试防止该类文案回归。

存储层真正实现共享后，应连同本测试一起更新，而不是绕过它。
"""
import re
from pathlib import Path

import pytest

from backend.common.config_repository import ProfileRepository

FRONTEND_SRC = Path(__file__).resolve().parents[1] / "frontend" / "src"

FORBIDDEN = [
    "所有配置空间共同使用",
    "所有配置空间共用",
    "所有配置空间引用",
    "共享节点库",
]


def _source_files():
    for pattern in ("**/*.vue", "**/*.ts"):
        yield from FRONTEND_SRC.glob(pattern)


def test_profile_scoped_resources_are_not_advertised_as_shared():
    """这四类资源仍按配置空间隔离，界面不得宣称共享"""
    for field in ("subscriptions", "nodes", "subscription_aggregations", "rule_library"):
        assert field in ProfileRepository.PROFILE_FIELDS, (
            f"{field} 已不在 PROFILE_FIELDS，共享可能已实现，请同步更新本测试"
        )

    offenders = []
    for path in _source_files():
        text = path.read_text(encoding="utf-8")
        for line_no, line in enumerate(text.splitlines(), 1):
            stripped = line.strip()
            # 允许注释里出现这些词用于说明禁令本身
            if stripped.startswith("//") or stripped.startswith("*"):
                continue
            for phrase in FORBIDDEN:
                if phrase in line:
                    offenders.append(f"{path.name}:{line_no}: {stripped[:80]}")

    assert not offenders, "界面出现未兑现的共享声明：\n" + "\n".join(offenders)
