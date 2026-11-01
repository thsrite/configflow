from flask import Blueprint, jsonify, request

from backend.common.auth import require_auth
from backend.common.config import get_repository
from backend import notifications
from backend.utils import notification_store

notifications_bp = Blueprint('notifications', __name__, url_prefix='/api/notifications')


@notifications_bp.get('')
@require_auth
def list_notifications():
    try:
        items = notifications.snapshot()
    except (OSError, ValueError):
        return jsonify(success=False, message='通知暂时无法读取，请稍后重试。'), 503
    response = jsonify(success=True, items=items, checked_at=notification_store.now())
    response.headers['Cache-Control'] = 'no-store'
    return response


@notifications_bp.post('/read')
@require_auth
def read_notifications():
    body = request.get_json(silent=True)
    versions = body.get('versions') if isinstance(body, dict) else None
    if not isinstance(versions, dict) or len(versions) > 2000 or any(
            not isinstance(key, str) or len(key) > 1024 or not isinstance(value, str) or len(value) > 100
            for key, value in versions.items()):
        return jsonify(success=False, message='请指定要标为已读的通知及其版本。'), 400
    notification_store.mark_read(get_repository().data_dir, versions)
    return jsonify(success=True)
