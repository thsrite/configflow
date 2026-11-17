"""Repair attribution from config-findings-fixes.md and git commit subjects.

This adds provenance only after an actual successful recheck. It cannot turn
different/skipped/unrun cases into passes and never changes raw differences.
"""

METADATA_GROUPS = {
    'delivery-graph', 'delivery-graph-collisions', 'merged-provider-data', 'merged-provider-timezone',
    'subscription-provider-formats', 'subscription-provider-head', 'subscription-provider-selector',
    'subscription-provider-timezone', 'subscription-provider-validation', 'provider-concurrent-clients',
    'provider-last-good-lifecycle', 'provider-auth-environments',
}


def repair_commits(item):
    if item['status'] != 'closed':
        return []
    group = item['group']
    paths = {difference['path'] for difference in item['differences']}
    if paths and paths <= {'files.notifications.json'}:
        return ['8f3d36ad']
    if group in METADATA_GROUPS:
        return ['7b904725']
    if group == 'merged-provider-remote':
        return ['cb0cc9fa'] if item['case'] == 'R222-F11' else ['7b904725']
    if group in ('discovery-init', 'discovery-region-route'):
        return ['041ab5a6']
    if group == 'subscriptions-cleanup':
        return ['e9187b9f']
    if group == 'settings-webdav':
        return ['abdf47ef', 'd01fdf32', 'f9963a7f']
    return []


def annotate(records):
    for item in records:
        item['fix_commits'] = repair_commits(item)
    return records
