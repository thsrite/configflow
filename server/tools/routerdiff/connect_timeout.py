"""Real TCP connect timeout and recovery, never an injected Python exception."""
import probes


def cases():
    yield probes.probe('connect-before-drop', 'ikuai', ok=True, entries=('H0222', 'H0223'))
    # A new, previously unused endpoint defeats legitimate HTTP connection-pool
    # reuse on either side. Draft settings edits themselves do not contact it.
    controls = ({'rpc': 'http', 'method': 'PUT', 'path': '/api/routers/r-ik',
                 'document': {'endpoint': 'http://10.44.0.3:18082'}, 'expected': 200},
                {'rpc': 'network_fault', 'enabled': True, 'expected': {'enabled': True, 'saw_syn': False}})
    cleanup = ({'rpc': 'network_fault', 'enabled': False, 'expected': {'enabled': False, 'saw_syn': True}},)
    yield probes.probe('connect-syn-dropped', 'ikuai', controls,
        message='连接 iKuai 超时（10 秒）：请检查管理地址、端口，以及 ConfigFlow 所在主机到路由器的网络是否畅通。',
        entries=('H0222', 'H0223'), elapsed_min=9, finalize=cleanup,
        expected_requests=({'match': {'peer': 'ikuai'}, 'count': 0},))
    yield probes.probe('connect-after-drop-removed', 'ikuai', ok=True, entries=('H0222', 'H0223'))
