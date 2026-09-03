"""订阅聚合排序接口回归测试

聚合是共享资源中六类可排序对象之一，但 /api/aggregations/reorder 长期缺失。
前端拖动排序的请求得到 405 后被 catch 成「保存排序失败」，故障一直不可见。
"""
from flask import Flask

from backend.common.config_repository import ProfileRepository
from backend.common import config as config_module
from backend.routes import register_blueprints


def _make_client(tmp_path):
    repository = ProfileRepository(tmp_path)
    config_module.set_repository(repository)
    app = Flask(__name__)
    register_blueprints(app)
    return app.test_client()


def _seed(client, names):
    """创建聚合并返回服务端分配的 id，顺序与传入名称一致"""
    created = []
    for name in names:
        response = client.post(
            "/api/aggregations",
            json={"name": name, "subscriptions": [], "nodes": [], "enabled": True},
        )
        assert response.status_code == 200, response.get_data(as_text=True)
        created.append(response.get_json()["data"]["id"])
    return created


def test_reorder_aggregations_by_ids_persists_new_order(tmp_path):
    client = _make_client(tmp_path)
    first, second, third = _seed(client, ["a", "b", "c"])

    response = client.post(
        "/api/aggregations/reorder", json={"ids": [third, first, second]}
    )

    assert response.status_code == 200
    assert response.get_json()["success"] is True

    listed = client.get("/api/aggregations").get_json()
    assert [item["id"] for item in listed] == [third, first, second]


def test_reorder_aggregations_rejects_unknown_id(tmp_path):
    client = _make_client(tmp_path)
    first, second = _seed(client, ["a", "b"])

    response = client.post(
        "/api/aggregations/reorder", json={"ids": [first, "agg_missing"]}
    )

    assert response.status_code == 404
    # 未知 id 必须报错而不是静默丢数据
    listed = client.get("/api/aggregations").get_json()
    assert [item["id"] for item in listed] == [first, second]
