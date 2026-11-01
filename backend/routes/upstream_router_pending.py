"""Reminder data for pages that edit Mihomo: upstream routers with changes to publish."""
from concurrent.futures import TimeoutError as FutureTimeout

from flask import jsonify, request

from backend.common.auth import require_auth
from backend.routers import pending
from backend.routes import agents_bp


@agents_bp.route('/<agent_id>/upstream-routers', methods=['GET'])
@require_auth
def upstream_router_pending(agent_id):
    """Linked routers, optionally restricted to the editor's explicit profile.

    Read-only: each router gets the 上游路由 page's report and release preview.
    The last read is reused while its key holds; ``?refresh=1`` (after a save, a
    publish or the user's 「重新检查」) reads the routers again.
    """
    refresh = request.args.get('refresh') in ('1', 'true')
    try:
        data = pending.agent_routers(agent_id, refresh=refresh, profile_id=request.args.get('profile'))
        response, status = jsonify({'success': True, **data}), 200
    except FutureTimeout:
        response, status = jsonify({'success': False, 'message': '读取上游路由状态超时，请稍后重试。'}), 504
    response.headers['Cache-Control'] = 'no-store'
    return response, status
