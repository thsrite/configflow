"""Verify a publish guard against the actually committed public storage format."""
from compare import database, Difference
from harness import DesignError


def expected_guard(directory, profile, service):
    rows = database(directory)['fields']
    return {'expected_profile_id': profile, 'expected_service_type': service,
            'expected_config_revision': rows['profile/' + profile + '/_revision']['value'] + rows['shared//_revision']['value']}


def check(pair, profile, service):
    def reference(reply):
        intended = expected_guard(pair.dirs[0], profile, service)
        if reply.json().get('publish_guard') != intended:
            raise DesignError('Python publish_guard does not name the committed profile/service/revision')
    return reference


def capture(pair, profile, service):
    def observe(replies, policy):
        out = []
        for index, (reply, directory) in enumerate(zip(replies, pair.dirs)):
            if reply.status != 200:
                continue
            intended = expected_guard(directory, profile, service)
            actual = reply.json().get('publish_guard')
            if actual != intended:
                if index == 0:
                    raise DesignError('Python publish_guard changed during request')
                out.append(Difference('body.publish_guard', 'not-committed-snapshot', intended, actual))
        return out
    return observe
