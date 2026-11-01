"""Profile client devices: a shared name plus an optional OxiDNS DNS policy.

Names live in device_notes and policies in the OxiDNS settings; this module
joins them into one list and edits one address at a time. A rename never
touches the OxiDNS settings, so it needs no publication.
"""
from backend.common.device_notes import normalize_name, read_notes, set_note
from backend.common.oxidns_device_policy import apply_policies, normalize_client_range, normalize_policies, read_policies
from backend.common.oxidns_settings import OxidnsSettingsError

MODES = ('', 'smart', 'direct')


class DeviceChanged(Exception):
    """The row no longer matches what the editor showed; nothing is saved."""


def list_devices(config):
    policies = read_policies(config.get('oxidns') or {}, config)
    devices = [dict(row) for row in policies['policies']]
    listed = {row['ip'] for row in devices}
    devices += [{'ip': ip, 'name': name, 'mode': ''} for ip, name in read_notes(config).items() if ip not in listed]
    from backend.common.client_fixed_ip import bindings
    for row in devices:
        if row['ip'] in bindings(config):
            row['fixed_ip'] = dict(bindings(config)[row['ip']])
    return {'devices': devices, 'editable': policies['editable'], 'reason': policies['reason'],
            'side_entries': policies.get('side_entries', [])}


def normalize_change(body):
    ip = normalize_client_range(body.get('ip'))
    name, previous_name = normalize_name(body.get('name')), normalize_name(body.get('previous_name'))
    mode, previous_mode = body.get('mode'), body.get('previous_mode')
    if mode not in MODES or previous_mode not in MODES:
        raise OxidnsSettingsError('DNS 策略只能是默认、智能分流或直连。')
    return ip, name, mode, previous_name, previous_mode


def update_device(config, change, base_url=''):
    """Apply one row; the caller commits name and policy together."""
    ip, name, mode, previous_name, previous_mode = change
    current = next((row for row in list_devices(config)['devices'] if row['ip'] == ip), {'name': '', 'mode': ''})
    if (current['name'], current['mode']) != (previous_name, previous_mode):
        raise DeviceChanged('此设备已在其他页面修改，请重新载入后再保存。')
    if mode != current['mode']:
        # Rows keep a copy of the current names for older versions after a rollback.
        names = {**read_notes(config), ip: name}
        rows = [{**row, 'name': names.get(row['ip'], '')}
                for row in normalize_policies((config.get('oxidns') or {}).get('device_policies', []))]
        index = next((i for i, row in enumerate(rows) if row['ip'] == ip), None)
        if not mode:
            del rows[index]
        elif index is None:
            rows.append({'ip': ip, 'name': name, 'mode': mode})
        else:
            rows[index]['mode'] = mode
        config['oxidns'] = apply_policies(config.get('oxidns') or {}, config, rows, base_url)
    # After the policy change, so a removed row leaves no empty name behind.
    set_note(config, ip, name)
    if not name and not mode and 'client_fixed_ips' in config:
        # Removing a record keeps router reservations unless separately previewed and confirmed.
        config['client_fixed_ips'].pop(ip, None)
