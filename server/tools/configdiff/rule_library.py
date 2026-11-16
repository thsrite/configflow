"""Only explicitly unordered async result arrays are aligned by unique IDs."""
import copy


def by_id(value, index=0):
    value = copy.deepcopy(value)
    if isinstance(value, dict) and isinstance(value.get('results'), list):
        rows = value['results']
        if all(isinstance(row, dict) and isinstance(row.get('id'), str) for row in rows):
            ids = [row['id'] for row in rows]
            if len(ids) == len(set(ids)):
                value['results'] = sorted(rows, key=lambda row: row['id'])
    return value
