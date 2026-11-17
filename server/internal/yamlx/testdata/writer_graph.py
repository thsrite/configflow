"""Encode real Python safe_load values as a typed physical graph for Go tests.

This codec performs no YAML parsing or business transformations. Both writer
oracles call real yaml.safe_load themselves before passing its value here.
"""

import base64
import datetime
import math


def graph(value, writer_styles=False):
    nodes, memo = [], {}

    def visit(value):
        container = isinstance(value, (dict, list))
        image = writer_styles and type(value).__name__ == "ImageName"
        shared = container or image
        if shared and id(value) in memo:
            return memo[id(value)]
        index = len(nodes)
        node = {}
        if writer_styles and type(value).__name__ in ("FlowMap", "FlowList"):
            node["style"] = 32
        if image:
            node["anchor"] = "agent_image"
        nodes.append(node)
        if shared:
            memo[id(value)] = index
        if value is None:
            tag, text = "null", "null"
        elif isinstance(value, bool):
            tag, text = "bool", str(value).lower()
        elif isinstance(value, int):
            tag, text = "int", str(value)
        elif isinstance(value, float):
            tag = "float"
            text = ".nan" if math.isnan(value) else ("-.inf" if value < 0 else ".inf") if math.isinf(value) else repr(value)
        elif isinstance(value, str):
            tag, text = "str", value
        elif isinstance(value, bytes):
            tag, text = "binary", base64.encodebytes(value).decode("ascii")
        elif isinstance(value, datetime.datetime):
            tag, text = "timestamp", value.isoformat(" ")
        elif isinstance(value, datetime.date):
            tag, text = "timestamp", value.isoformat()
        elif isinstance(value, dict):
            node.update(kind=4, tag="!!map", children=[])
            for key, child in value.items():
                node["children"].extend((visit(key), visit(child)))
            return index
        elif isinstance(value, list):
            node.update(kind=2, tag="!!seq", children=[visit(child) for child in value])
            return index
        else:
            raise TypeError(type(value))
        node.update(kind=8, tag="!!" + tag, value=text)
        return index

    assert visit(value) == 0
    return nodes

