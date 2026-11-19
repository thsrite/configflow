"""Independent real-Python renderer oracle for approved integer/timeout policy.

Only ASCII decimal parsing, removal of the interpreter digit limit and the
actual signed time.Duration-seconds bound are changed. No Go output is read.
"""
import gzip
import hashlib
import inspect
import json
import re
import sys
from pathlib import Path
from backend.agents import go_install_script as source
from backend.agents import gateway_install_script as gateway
from backend.agents import install_output as layout
ROOT=Path(__file__).resolve().parent
SPACE=' \t\n\r\v\f\u0085\u00a0\u1680'+''.join(map(chr,range(0x2000,0x200b)))+'\u2028\u2029\u202f\u205f\u3000'
MAX_SECONDS=(2**63-1)//1_000_000_000

def sha(value):return hashlib.sha256(value).hexdigest()
def native_int(value):
    if isinstance(value,str):
        value=value.strip(SPACE)
        if re.fullmatch(r'[+-]?[0-9]+(?:_[0-9]+)*',value) is None:raise ValueError('Invalid installer integer')
    return int(value)
def native_timeout(value):
    value=native_int(value)
    if not -MAX_SECONDS<=value<=MAX_SECONDS:raise ValueError('Invalid deployment health timeout')
    return value

def clone(function):
    scope=dict(function.__globals__)
    scope.update(int=native_int,native_timeout=native_timeout,SPACE=SPACE)
    text=inspect.getsource(function).replace("body.rstrip()","body.rstrip(SPACE)").replace('"deployment_health_timeout": int(deployment_health_timeout),','"deployment_health_timeout": native_timeout(deployment_health_timeout),')
    exec(compile(text,inspect.getfile(function),'exec'),scope)
    return scope[function.__name__]
original={'go_agent':source.generate_go_agent_install_script,'aio':source.generate_go_aio_install_script,'gateway':gateway.generate_gateway_install_script,'with_install_output':layout.with_install_output}
native={name:clone(function) for name,function in original.items()}
# Gateway uses the same AIO owner; only its integer parser changes.
native['gateway'].__globals__['generate_go_aio_install_script']=native['aio']

def outcome(function,kwargs):
    try:
        text=function(**kwargs)
        return {'output':text,'sha256':sha(text.encode()),'length':len(text.encode())}
    except Exception as exc:
        return {'error':{'class':type(exc).__name__,'message':str(exc),'value_error':isinstance(exc,ValueError)}}
def same(a,b):
    if 'error' in a and 'error' in b:
        old,new=a['error'],b['error']
        if old['value_error']!=new['value_error']:return False
        return old['message']==new['message'] or not re.search('[\u4e00-\u9fff]',old['message'])
    return a==b
rows=[]
for name in ('matrix.jsonl','fuzz.jsonl.gz'):
    archive=ROOT/name
    raw=archive.read_bytes();lines=gzip.decompress(raw).splitlines() if name.endswith('.gz') else raw.splitlines()
    for line in lines:
        row=json.loads(line);fn=row['fn']
        if fn not in original:continue
        kwargs=row['kwargs']
        old=outcome(original[fn],kwargs)
        limit=sys.get_int_max_str_digits()
        try:
            sys.set_int_max_str_digits(0)
            after=outcome(native[fn],kwargs)
        finally:sys.set_int_max_str_digits(limit)
        if same(old,after):continue
        if row['ok']:
            if old.get('sha256')!=row['sha256'] or old.get('length')!=row['length']:raise RuntimeError('original Python rendered bytes changed')
        elif 'error' not in old or row['error']!=old['error']['class']+': '+old['error']['message']:raise RuntimeError('original Python refusal changed')
        rows.append(dict(archive=name,archive_sha256=sha(raw),row_sha256=sha(line),original=row,python=old,native=after))
print(json.dumps({'policy':'N8-10 native installer integers, time.Duration seconds and layout whitespace','rows':rows},ensure_ascii=False,separators=(',',':')))
