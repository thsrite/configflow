from backend.utils.rule_matcher import _iter_ruleset_lines
from backend.agents.push_diff import _node_change,_rule_change,diff_config
import time
cases={'syntax':'payload: [','nested':'payload: [[example.com]]','recursive':'payload: &p [*p]'}
chain='l0: &l0 [entry]\n'+''.join(f'l{i}: &l{i} [*l{i-1}, *l{i-1}]\n' for i in range(1,41))+'payload: *l40\n'
cases['bomb']=chain
for name,source in cases.items():
 start=time.monotonic(); assert list(_iter_ruleset_lines(source,'yaml'))==[]
 print('Python matcher',name,'no-match',round(time.monotonic()-start,6),flush=True)
for name,data in {'syntax':b'proxies: [','recursive':b'proxies: &p [*p]'}.items():
 summary,details=_node_change(None,data,None)
 assert summary=={'kind':'size','before':None,'after':len(data)} and details is None
 print('Python provider summary',name,summary,flush=True)
print('Python malformed rule summary',_rule_change(None,b'payload: [',None),flush=True)
assert diff_config(b'x: [',b'x: 1')['status']=='unparsable'
print('Python main config malformed: unparsable')
