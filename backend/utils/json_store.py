"""每个 Agent 一个 JSON 文件的简单存储：文件名清洗、原子写入与删除。

文件不存在或已损坏时读出 None，由子类决定默认值——这些都是可以重新累计的统计数据，
不应因为一个坏文件让主流程报错。并发由各子类自己的模块级锁保护。
"""

import json
import os
from typing import Any, Dict, Optional


class JsonFileStore:
    def __init__(self, data_dir):
        self.data_dir = str(data_dir)

    def _path(self, agent_id) -> str:
        safe_id = str(agent_id).replace('/', '_').replace('\\', '_')
        return os.path.join(self.data_dir, f'{safe_id}.json')

    def _read_json(self, agent_id) -> Optional[Dict[str, Any]]:
        try:
            with open(self._path(agent_id), encoding='utf-8') as handle:
                data = json.load(handle)
        except (OSError, ValueError):
            return None
        return data if isinstance(data, dict) else None

    def _write_json(self, agent_id, data: Dict[str, Any]) -> None:
        os.makedirs(self.data_dir, exist_ok=True)
        path = self._path(agent_id)
        temp_path = f'{path}.tmp'
        with open(temp_path, 'w', encoding='utf-8') as handle:
            json.dump(data, handle, ensure_ascii=False, separators=(',', ':'))
        os.replace(temp_path, path)

    def _remove(self, agent_id) -> None:
        try:
            os.remove(self._path(agent_id))
        except FileNotFoundError:
            pass
