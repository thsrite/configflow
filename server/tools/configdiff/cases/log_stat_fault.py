"""Real permission denial while statting a log outside the database directory."""
from compare import differences
from harness import Pair
from cases.setup import Stable
from cases.subscriptions import require


class StatFault(Stable):
    def __init__(self, pair, path):
        super().__init__(pair)
        self.path = path
        self.observed = [None, None]

    def denied(self):
        try:
            self.path.stat()
        except PermissionError:
            return True
        except OSError:
            return False
        return False

    def before(self, index):
        super().before(index)
        self.path.parent.chmod(0)
        self.active = self.denied()
        if index == 0:
            require(self.active, 'normal-user stat actually fails on the private log parent before API call')

    def after(self, index, reply):
        try:
            try:
                payload = reply.json()
            except (ValueError, UnicodeError):
                payload = {}
            error = payload.get('error', '') if isinstance(payload, dict) else ''
            try:
                mode_unchanged = self.path.parent.stat().st_mode & 0o777 == 0
            except OSError:
                mode_unchanged = False
            self.observed[index] = {'active_stat_fault': self.active, 'fault_still_denied': self.denied(),
                'mode_unchanged': mode_unchanged,
                'permission_error': isinstance(error, str) and 'permission' in error.lower() and 'denied' in error.lower(),
                'assigned_path': isinstance(error, str) and str(self.path) in error}
            if index == 0:
                require(all(self.observed[0].values()), 'stat failure reaches intended permission cause and exact private path')
        finally:
            if self.path.parent.exists():
                self.path.parent.chmod(0o700)
        super().after(index, reply)

    def compare(self, policy):
        return super().compare(policy) + differences(*self.observed, 'log.stat-permission', policy)


def stat_failure(binary, recorder):
    pair = Pair(binary, recorder)
    folder = pair.root / 'denied-log'
    folder.mkdir()
    path = folder / 'synthetic.log'
    pair.options['environment'] = {'LOG_FILE': str(path)}
    try:
        with pair:
            pair.policy.internal_errors['body.error'] = ''
            pair.request('R075-F08', 'GET /api/logs/info', expect=500, writes=True,
                interaction=StatFault(pair, path), variant='real-stat-EACCES-with-database-still-readable')
    finally:
        if folder.exists():
            folder.chmod(0o700)


GROUPS = {'logs-real-stat-permission': stat_failure}
