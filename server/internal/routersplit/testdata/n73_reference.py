"""Independent Python boundary for inputs normalized by the native YAML policy."""
import json
import sys
from backend.common import router_split as split
from backend.common.linux_gateway import LinuxGatewayError

for line in sys.stdin:
    request = json.loads(line)
    name, args = request['fn'], request['args']
    try:
        if name == 'full_tun_base_yaml':
            result = split.full_tun_base_yaml(args.get('text'))
        elif name == 'base_is_full_tun':
            result = split.base_is_full_tun(args.get('config'))
        elif name == 'check_generation':
            result = split.check_generation(args.get('config'))
        elif name == 'mode_from_context':
            result = split.mode_from_context(args.get('config'), args.get('context'))
        elif name == 'router_split_state':
            result = split.router_split_state(None, args.get('profile_id'), config=args.get('config'), context=args.get('context'))
        else:
            raise RuntimeError('not a YAML reader boundary')
        answer = {'out': result}
    except Exception as error:
        answer = {'err': {'class': type(error).__name__, 'msg': str(error), 'gateway': isinstance(error, LinuxGatewayError)}}
    print(json.dumps(answer, ensure_ascii=False), flush=True)
