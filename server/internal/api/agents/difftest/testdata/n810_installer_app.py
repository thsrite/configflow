"""Independent Flask policy reference for native HTTP installer bindings.

The original Flask service still receives every unchanged corpus request first.
This second service changes only Go whitespace, ASCII machine integers and the
native binding's sorted member order. It never reads Go output.
"""
import inspect
import re
import runpy
import sys
from backend.routes import agents

SPACE=' \t\n\r\v\f\u0085\u00a0\u1680'+''.join(map(chr,range(0x2000,0x200b)))+'\u2028\u2029\u202f\u205f\u3000'
def native_int(value):
    if isinstance(value,str):
        value=value.strip(SPACE)
        if re.fullmatch(r'[+-]?[0-9]+(?:_[0-9]+)*',value) is None:
            raise ValueError('Invalid installer integer')
    return int(value)

def strict_port(value):
    if not isinstance(value,str) or re.fullmatch(r'[0-9]+',value) is None or not 1<=native_int(value)<=65535:
        raise ValueError('Invalid Agent port')

original=agents._install_script_response
text=inspect.getsource(original).replace('.strip()', '.strip(SPACE)')
text=text.replace("agent_port = params.get('port', '8080')", """agent_port = params.get('port', '8080')
            native_int(agent_port)
            if re.fullmatch(r'[0-9]+', agent_port) is None:
                raise ValueError('Invalid Agent port')""")
text=text.replace('script = generate_go_aio_install_script(', "strict_port(params.get('mihomo_agent_port', '8080'))\n            strict_port(params.get('oxidns_agent_port', '8082'))\n            script = generate_go_aio_install_script(")
scope=dict(original.__globals__)
scope.update(SPACE=SPACE,int=native_int,native_int=native_int,re=re,strict_port=strict_port)
exec(compile(text,inspect.getfile(original),'exec'),scope)
agents._install_script_response=scope['_install_script_response']
original_params=agents._install_command_params
def native_params(payload):
    return original_params(dict(sorted(payload.items())) if isinstance(payload,dict) else payload)
agents._install_command_params=native_params
sys.set_int_max_str_digits(0)
runpy.run_module('backend.app',run_name='__main__')
