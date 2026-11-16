"""Every explicitly listed P1/P2 guard, before handler/body/object validation."""
from urllib.parse import quote

from cases.admin_auth import concrete
from harness import Pair, DesignError
from inventory import missing_profile_rows


def all_missing_profiles(binary, recorder):
    with Pair(binary, recorder) as pair:
        for rid, route, first in missing_profile_rows():
            method, template = route.split(' ', 1)
            for offset, (profile, status) in enumerate([('missing-synthetic', 404), ('bad!id', 400)]):
                path = concrete(template, profile if '<profile_id>' in template else pair.default_profile)
                if '<profile_id>' not in template:
                    path += '?profile=' + quote(profile, safe='')
                expected = 'Profile not found: ' + profile if status == 404 else 'Invalid profile id'
                def check(reply):
                    if reply.json().get('message') != expected:
                        raise DesignError(rid + ' profile guard fixture did not reach the expected branch')
                pair.request(f'{rid}-F{first+offset:02}', route, path, body='{}' if method in ('POST', 'PUT', 'PATCH') else None,
                             variant='P' + str(offset+1) + '/all-profile-guards', expect=status, check=check, writes=True)


GROUPS = {'profile-guards-all': all_missing_profiles}
