"""Agent 版本管理"""

# Agent 最新版本号
LATEST_AGENT_VERSION = "1.0.7-go"

# 版本更新日志（可选）
VERSION_CHANGELOG = {
    "1.0.7-go": {
        "date": "2026-02-07",
        "features": [
            "优化配置推送逻辑"
        ]
    },
    "1.0.6-go": {
        "date": "2026-01-21",
        "features": [
            "logrotate日志轮转"
        ]
    },
    "1.0.5-go": {
        "date": "2026-01-19",
        "features": [
            "支持自定义日志路径",
            "开源"
        ]
    },
    "1.0.4-go": {
        "date": "2025-11-13",
        "features": [
            "推送更新逻辑",
        ]
    },
    "1.0.3-go": {
        "date": "2025-11-13",
        "features": [
            "推送mihomo配置时同时推送规则和订阅配置",
            "备份mihomo和mosdns只备份关键配置项",
        ]
    },
    "1.0.2-go": {
        "date": "2025-11-12",
        "features": [
            "新增系统监控功能：CPU、内存、磁盘、网络",
            "支持监控数据历史记录（24小时）",
            "提供监控数据统计和图表展示",
            "监控功能可配置开关（默认开启）",
            "完善的异常处理机制，监控失败不影响心跳"
        ]
    },
    "1.0.1-go": {
        "date": "2025-11-12",
        "features": [
            "支持agent在线更新"
        ]
    },
    "1.0.0-go": {
        "date": "2025-10-12",
        "features": [
            "初始版本",
            "支持 Mihomo 和 MosDNS 配置管理",
            "支持远程配置推送",
            "支持服务重启",
            "支持日志查看"
        ]
    }
}

def get_latest_version() -> str:
    """获取最新的 Agent 版本号"""
    return LATEST_AGENT_VERSION

def compare_versions(version1: str, version2: str) -> int:
    """
    比较两个版本号

    Args:
        version1: 版本号1
        version2: 版本号2

    Returns:
        int: -1 如果 version1 < version2
              0 如果 version1 == version2
              1 如果 version1 > version2
    """
    # 移除后缀（如 "-go"）
    v1_parts = version1.split('-')[0].split('.')
    v2_parts = version2.split('-')[0].split('.')

    # 补齐长度
    max_len = max(len(v1_parts), len(v2_parts))
    v1_parts.extend(['0'] * (max_len - len(v1_parts)))
    v2_parts.extend(['0'] * (max_len - len(v2_parts)))

    # 比较每个部分
    for p1, p2 in zip(v1_parts, v2_parts):
        try:
            n1, n2 = int(p1), int(p2)
            if n1 < n2:
                return -1
            elif n1 > n2:
                return 1
        except ValueError:
            # 如果无法转换为整数，按字符串比较
            if p1 < p2:
                return -1
            elif p1 > p2:
                return 1

    return 0

def has_update(current_version: str) -> bool:
    """
    检查是否有新版本可用

    Args:
        current_version: 当前版本号

    Returns:
        bool: True 如果有新版本，False 否则
    """
    return compare_versions(current_version, LATEST_AGENT_VERSION) < 0
