import json
from backend.utils.provider_delivery import DeliverySnapshot
def out(f):
    try:
        return ['ok', repr(f())]
    except Exception as e:
        return [type(e).__name__, str(e)]
P=[{'name':'A','type':'http','server':'a.test','port':80}]
s=DeliverySnapshot.capture('default', {'proxies':[], 'proxy-providers': {'Feed': 1}})
print(json.dumps(out(lambda: s.validate(P, provider_name='Feed')), ensure_ascii=False))
s=DeliverySnapshot.capture('default', {'proxies':['x'], 'proxy-providers': {}})
print(json.dumps(out(lambda: s.validate(P)), ensure_ascii=False))
s=DeliverySnapshot.capture('default', {'proxies':[['x']], 'proxy-providers': {}})
print(json.dumps(out(lambda: s.validate(P)), ensure_ascii=False))
