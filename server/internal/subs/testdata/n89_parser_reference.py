"""Real parser business with native schema/number consumption boundaries only.

Reuses the separately audited N7 loader-value archive, never a Go parser result.
VMess JSON fields use the already-approved shared String/Int projection. Raw URI
ports use the native decimal protocol grammar; base64 acceptance is unchanged.
"""
import base64
import contextlib
import decimal
import gzip
import hashlib
import io
import json
import math
import pathlib
import re
from unittest import mock

with contextlib.redirect_stdout(io.StringIO()):
    from backend.utils import subscription_parser as parser

root = pathlib.Path(__file__).parent
native_bytes = (root / "n7_native_values.json.gz").read_bytes()
native = json.loads(gzip.decompress(native_bytes))
n7 = json.loads(gzip.decompress((root / "n7_reader_expected.json.gz").read_bytes()))["cases"]
original_vmess = parser.parse_vmess
unregistered=set()
current_fields=set()
SPACE = "\t\n\v\f\r \x85\xa0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000"


class NativeText(str):
    def strip(self, chars=None):
        result=super().strip(SPACE if chars is None else chars)
        if chars is None and result!=super().strip():
            current_fields.add("uri.text/native-whitespace")
        return NativeText(result)

    def __getitem__(self, key):
        value = super().__getitem__(key)
        return NativeText(value) if isinstance(value, str) else value

    def split(self, *args, **kwargs):
        return [NativeText(value) for value in super().split(*args, **kwargs)]

    def rsplit(self, *args, **kwargs):
        return [NativeText(value) for value in super().rsplit(*args, **kwargs)]


class Number(str):
    pass


def protocol_int(value):
    if isinstance(value, str):
        value = value.strip(SPACE)
        if re.fullmatch(r"[+-]?[0-9]+", value) is None:
            try:
                int(value)
                current_fields.add("uri.port/native-decimal-grammar")
            except ValueError:
                pass
            raise ValueError("invalid URI decimal port")
    elif not isinstance(value, int) or isinstance(value, bool):
        raise ValueError("invalid URI integer")
    result = int(value)
    if result < -(1 << 63) or result >= 1 << 63:
        current_fields.add("uri.port/int64-range")
        raise ValueError("URI port outside int64")
    return result


def model_int(value):
    if isinstance(value,Number):
        number=decimal.Decimal(value)
        if number!=number.to_integral_value():
            raise ValueError("wire integer must be integral")
        result=int(number)
    elif isinstance(value,str) and re.fullmatch(r"-?[0-9]+",value):
        result=int(value)
    else:
        raise ValueError("wire integer must match shared Int schema")
    if not -(1<<63)<=result<(1<<63):
        raise ValueError("wire integer outside int64")
    return result


def projected_vmess(uri):
    raw = base64.b64decode(uri[8:]).decode("utf-8")
    def invalid_constant(value):
        current_fields.add("vmess/native-finite-json")
        raise ValueError("non-finite wire JSON")
    value = json.loads(raw, parse_int=Number, parse_float=Number, parse_constant=invalid_constant)
    if not isinstance(value, dict):
        raise ValueError("VMess record must be object")
    tokens=value
    value=json.loads(raw)
    for field in ("port","aid"):
        if field not in tokens:
            value[field]=0
            continue
        try:
            value[field]=model_int(tokens[field])
        except ValueError:
            current_fields.add("vmess."+field+"/native-int-validation")
            raise
    for field in ("ps","add"):
        token=tokens.get(field)
        if isinstance(token,Number):
            value[field]=str(token)
            current_fields.add("vmess."+field+"/shared-number-text")
        elif field in tokens and token is None:
            current_fields.add("vmess."+field+"/null-absent")
            if field=="ps":
                value.pop(field,None)
            else:
                value[field]=""
    encoded = base64.b64encode(json.dumps(value, ensure_ascii=False).encode()).decode()
    return original_vmess("vmess://" + encoded)


def loader_value(wire, key=False):
    kind = wire["kind"]
    if kind == "scalar":
        return wire["value"]
    if kind == "tagged-key":
        return ("__yaml_key__", wire["tag"], wire["value"])
    if kind == "sequence":
        items = [loader_value(item) for item in wire["items"]]
        return tuple(items) if key else items
    pairs = {loader_value(pair[0], True): loader_value(pair[1]) for pair in wire["pairs"]}
    return tuple(pairs.items()) if key else pairs


def load(source, *args, **kwargs):
    key = hashlib.sha256(("parser\0" + source).encode()).hexdigest()
    if key not in native:
        unregistered.add(key)
        raise AssertionError("unregistered native YAML source " + key)
    record = native[key]
    if record.get("error"):
        raise ValueError(record["error"])
    return loader_value(record["native"])


def freeze(value):
    if isinstance(value,float) and not math.isfinite(value):
        return {"__nonfinite__":repr(value)}
    if isinstance(value,list):
        return [freeze(item) for item in value]
    if isinstance(value,dict):
        return {key:freeze(item) for key,item in value.items()}
    return value


archives = {}
cases = {}
for name in ("parser", "review_c10"):
    source = (root / (name + ".jsonl.gz")).read_bytes()
    archives[name] = hashlib.sha256(source).hexdigest()
    rows = gzip.decompress(source).decode().rstrip("\n").split("\n")
    with mock.patch.object(parser, "parse_vmess", projected_vmess), mock.patch.object(parser, "int", protocol_int, create=True), mock.patch.object(parser.yaml, "safe_load", load), mock.patch.object(parser.yaml, "load", load):
        for index, line in enumerate(rows):
            row = json.loads(line)
            if row["fn"] == "parse_uri_list":
                current_fields.clear()
                expected = freeze(parser.parse_uri_list(NativeText(row["in"])))
                if unregistered:
                    raise AssertionError("unregistered physical YAML inputs: " + repr(sorted(unregistered)))
                previous = n7["subs/" + name + "/" + str(index)]["expected"]
                if "value" in previous and expected != previous["value"]:
                    if not current_fields:
                        raise AssertionError("unclassified parser change " + name + "/" + str(index))
                    cases[name + "/" + str(index)] = {"input_sha256": hashlib.sha256(line.encode()).hexdigest(), "output": expected, "classes":sorted(current_fields)}
            elif row["fn"] == "int":
                try:
                    expected = {"output": protocol_int(row["in"])}
                except ValueError:
                    expected = {"reject": True}
                cases[name + "/" + str(index)] = dict(expected, input_sha256=hashlib.sha256(line.encode()).hexdigest(), **{"classes": ["native-uri-int-helper"]})

payload = json.dumps({"sources": archives, "native_loader_sha256": hashlib.sha256(native_bytes).hexdigest(), "cases": cases}, ensure_ascii=False, separators=(",", ":"),allow_nan=False).encode()
with gzip.GzipFile(filename=str(root / "n89_parser_reference.json.gz"), mode="wb", mtime=0) as stream:
    stream.write(payload)
print(f"native parser reference: {len(cases)} exact real-Python projected cases")
