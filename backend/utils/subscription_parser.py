"""订阅解析工具"""
import base64
import json
import requests
import yaml
from typing import List, Dict, Any
from urllib.parse import urlparse, parse_qs


def parse_subscription(url: str) -> List[Dict[str, Any]]:
    """
    解析订阅链接，返回节点列表
    支持 base64 编码的通用格式和 clash/mihomo yaml 格式
    """
    try:
        # 添加 User-Agent 请求头，模拟 Clash 客户端
        headers = {
            'User-Agent': 'clash.meta'
        }
        response = requests.get(url, headers=headers, timeout=30)
        response.raise_for_status()
        content = response.text

        # 尝试解析 YAML 格式 (Clash/Mihomo)
        if 'proxies:' in content or 'proxies :' in content:
            return parse_clash_yaml(content)

        # 尝试解析 base64 编码的通用格式
        try:
            decoded = base64.b64decode(content).decode('utf-8')
            return parse_uri_list(decoded)
        except:
            # 直接尝试解析 URI 列表
            return parse_uri_list(content)

    except Exception as e:
        raise Exception(f"订阅解析失败: {str(e)}")


def parse_clash_yaml(content: str) -> List[Dict[str, Any]]:
    """解析 Clash/Mihomo YAML 格式"""
    try:
        data = yaml.safe_load(content)
        proxies = data.get('proxies', [])

        nodes = []
        for proxy in proxies:
            node = {
                'name': proxy.get('name', 'Unnamed'),
                'type': proxy.get('type', '').lower(),
                'server': proxy.get('server', ''),
                'port': proxy.get('port', 0),
                'params': {k: v for k, v in proxy.items() if k not in ['name', 'type', 'server', 'port']}
            }
            nodes.append(node)

        return nodes
    except Exception as e:
        raise Exception(f"YAML 解析失败: {str(e)}")


def parse_uri_list(content: str) -> List[Dict[str, Any]]:
    """解析 URI 列表格式 (ss://, vmess://, trojan:// 等) 和 YAML/JSON 对象格式"""
    content = content.strip()

    # 检查是否是完整的 YAML 或 JSON 对象格式（可能是多行）
    if (content.startswith('{') or content.startswith('name:') or
        content.startswith('type:') or 'name:' in content[:50]):
        # 尝试作为完整的 YAML/JSON 解析
        node = parse_yaml_object(content)
        if node:
            return [node]

    # 检查是否是 YAML 列表格式
    if content.startswith('- name:') or content.startswith('- type:'):
        try:
            # 尝试解析 YAML 列表
            data = yaml.safe_load(content)
            if isinstance(data, list):
                nodes = []
                for item in data:
                    if isinstance(item, dict):
                        item['_raw_object'] = True
                        nodes.append(item)
                return nodes
        except:
            pass

    # 否则按行解析
    lines = content.split('\n')
    nodes = []

    for line in lines:
        line = line.strip()
        if not line:
            continue

        # 去除 YAML 列表标记
        if line.startswith('- '):
            line = line[2:].strip()

        try:
            if line.startswith('ss://'):
                node = parse_shadowsocks(line)
            elif line.startswith('ssr://'):
                node = parse_shadowsocksr(line)
            elif line.startswith('vmess://'):
                node = parse_vmess(line)
            elif line.startswith('vless://'):
                node = parse_vless(line)
            elif line.startswith('trojan://'):
                node = parse_trojan(line)
            elif line.startswith('hysteria://') or line.startswith('hysteria2://'):
                node = parse_hysteria(line)
            elif line.startswith('{') and line.endswith('}'):
                # 解析单行对象格式：{name: xxx, type: xxx, ...}
                node = parse_yaml_object(line)
            else:
                continue

            if node:
                nodes.append(node)
        except:
            continue

    return nodes


def parse_shadowsocks(uri: str) -> Dict[str, Any]:
    """解析 Shadowsocks URI"""
    # ss://base64(method:password)@server:port#name
    uri = uri[5:]  # 移除 ss://

    if '@' in uri:
        if '#' in uri:
            uri, name = uri.split('#', 1)
        else:
            name = 'SS Node'

        if uri.count('@') == 1:
            # 可能是 base64 编码的 method:password
            user_info, server_info = uri.split('@', 1)
            try:
                decoded = base64.b64decode(user_info).decode('utf-8')
                method, password = decoded.split(':', 1)
            except:
                # 或者直接是 method:password
                method, password = user_info.split(':', 1)
        else:
            # 完整 base64 编码
            decoded = base64.b64decode(uri).decode('utf-8')
            user_info, server_info = decoded.split('@', 1)
            method, password = user_info.split(':', 1)
            if '#' in server_info:
                server_info, name = server_info.split('#', 1)

        server, port = server_info.split(':', 1)

        return {
            'name': name,
            'type': 'ss',
            'server': server,
            'port': int(port),
            'params': {
                'cipher': method,
                'password': password
            }
        }


def parse_shadowsocksr(uri: str) -> Dict[str, Any]:
    """解析 ShadowsocksR URI"""
    # ssr://base64(server:port:protocol:method:obfs:base64pass/?params)
    uri = uri[6:]  # 移除 ssr://
    decoded = base64.b64decode(uri).decode('utf-8')

    parts = decoded.split('/')
    main_part = parts[0]
    params_part = parts[1] if len(parts) > 1 else ''

    server, port, protocol, method, obfs, password_b64 = main_part.split(':', 5)
    password = base64.b64decode(password_b64).decode('utf-8')

    # 解析参数
    params = {}
    if '?' in params_part:
        query = params_part.split('?', 1)[1]
        for item in query.split('&'):
            if '=' in item:
                key, value = item.split('=', 1)
                params[key] = base64.b64decode(value).decode('utf-8')

    return {
        'name': params.get('remarks', 'SSR Node'),
        'type': 'ssr',
        'server': server,
        'port': int(port),
        'params': {
            'cipher': method,
            'password': password,
            'protocol': protocol,
            'obfs': obfs,
            'protocol-param': params.get('protoparam', ''),
            'obfs-param': params.get('obfsparam', '')
        }
    }


def parse_vmess(uri: str) -> Dict[str, Any]:
    """解析 VMess URI"""
    # vmess://base64(json)
    uri = uri[8:]  # 移除 vmess://
    decoded = base64.b64decode(uri).decode('utf-8')
    data = json.loads(decoded)

    return {
        'name': data.get('ps', 'VMess Node'),
        'type': 'vmess',
        'server': data.get('add', ''),
        'port': int(data.get('port', 0)),
        'params': {
            'uuid': data.get('id', ''),
            'alterId': int(data.get('aid', 0)),
            'cipher': data.get('scy', 'auto'),
            'network': data.get('net', 'tcp'),
            'tls': data.get('tls', '') == 'tls',
            'ws-opts': {
                'path': data.get('path', '/'),
                'headers': {'Host': data.get('host', '')}
            } if data.get('net') == 'ws' else {}
        }
    }


def parse_vless(uri: str) -> Dict[str, Any]:
    """解析 VLESS URI"""
    # vless://uuid@server:port?params#name
    uri = uri[8:]  # 移除 vless://

    if '#' in uri:
        uri, name = uri.split('#', 1)
    else:
        name = 'VLESS Node'

    # 解析 uuid@server:port?params
    if '@' not in uri:
        return None

    uuid_part, server_info = uri.split('@', 1)

    # 解析 server:port?params
    if '?' in server_info:
        server_port, query = server_info.split('?', 1)
        query_params = parse_qs(query)
    else:
        server_port = server_info
        query_params = {}

    if ':' not in server_port:
        return None

    server, port = server_port.rsplit(':', 1)

    # 提取查询参数
    params = {}
    for key, value in query_params.items():
        params[key] = value[0] if len(value) == 1 else value

    # 构建节点配置
    node_params = {
        'uuid': uuid_part,
        'encryption': params.get('encryption', 'none'),
        'security': params.get('security', 'none'),
        'type': params.get('type', 'tcp'),
    }

    # 添加 TLS/Reality 相关参数
    if params.get('security') in ['tls', 'reality']:
        node_params['sni'] = params.get('sni', '')
        node_params['fp'] = params.get('fp', '')

    # Reality 特有参数
    if params.get('security') == 'reality':
        node_params['pbk'] = params.get('pbk', '')  # public key
        node_params['sid'] = params.get('sid', '')  # short id
        node_params['spx'] = params.get('spx', '')  # spider-x

    # 传输协议相关参数
    if params.get('type') == 'ws':
        node_params['ws-opts'] = {
            'path': params.get('path', '/'),
            'headers': {'Host': params.get('host', '')}
        }
    elif params.get('type') == 'grpc':
        node_params['grpc-opts'] = {
            'grpc-service-name': params.get('serviceName', '')
        }
    elif params.get('type') == 'tcp':
        header_type = params.get('headerType', 'none')
        if header_type != 'none':
            node_params['network'] = 'tcp'
            node_params['tcp-opts'] = {
                'header': {
                    'type': header_type
                }
            }

    # 流控参数
    if params.get('flow'):
        node_params['flow'] = params.get('flow')

    # 其他可选参数
    if params.get('alpn'):
        node_params['alpn'] = params.get('alpn').split(',')

    return {
        'name': name,
        'type': 'vless',
        'server': server,
        'port': int(port),
        'params': node_params
    }


def parse_trojan(uri: str) -> Dict[str, Any]:
    """解析 Trojan URI"""
    # trojan://password@server:port#name
    uri = uri[9:]  # 移除 trojan://

    if '#' in uri:
        uri, name = uri.split('#', 1)
    else:
        name = 'Trojan Node'

    password, server_info = uri.split('@', 1)
    server, port = server_info.split(':', 1)

    # 处理查询参数
    params = {}
    if '?' in port:
        port, query = port.split('?', 1)
        query_params = parse_qs(query)
        for key, value in query_params.items():
            params[key] = value[0] if len(value) == 1 else value

    return {
        'name': name,
        'type': 'trojan',
        'server': server,
        'port': int(port),
        'params': {
            'password': password,
            'sni': params.get('sni', ''),
            'skip-cert-verify': params.get('allowInsecure', 'false') == 'true'
        }
    }


def parse_hysteria(uri: str) -> Dict[str, Any]:
    """解析 Hysteria URI"""
    is_v2 = uri.startswith('hysteria2://')
    prefix_len = 12 if is_v2 else 11
    uri = uri[prefix_len:]

    if '#' in uri:
        uri, name = uri.split('#', 1)
    else:
        name = f'Hysteria{"2" if is_v2 else ""} Node'

    # 解析 auth@server:port?params
    if '@' in uri:
        auth, server_info = uri.split('@', 1)
    else:
        auth = ''
        server_info = uri

    if '?' in server_info:
        server_port, query = server_info.split('?', 1)
        query_params = parse_qs(query)
    else:
        server_port = server_info
        query_params = {}

    server, port = server_port.split(':', 1)

    return {
        'name': name,
        'type': 'hysteria2' if is_v2 else 'hysteria',
        'server': server,
        'port': int(port),
        'params': {
            'password': auth,
            'obfs': query_params.get('obfs', [''])[0],
            'sni': query_params.get('peer', [''])[0] or query_params.get('sni', [''])[0],
            'skip-cert-verify': query_params.get('insecure', ['0'])[0] == '1'
        }
    }


def parse_yaml_object(line: str) -> Dict[str, Any]:
    """解析 YAML 对象格式：{name: xxx, type: ss, server: xxx, ...} 或 JSON 格式"""
    try:
        # 先尝试作为 JSON 解析
        obj = json.loads(line)
        if isinstance(obj, dict):
            # 标记为直接使用的对象格式
            obj['_raw_object'] = True
            return obj
    except:
        pass

    try:
        # 如果 JSON 解析失败，尝试作为 YAML 解析
        obj = yaml.safe_load(line)
        if not isinstance(obj, dict):
            return None

        # 标记为直接使用的对象格式
        obj['_raw_object'] = True
        return obj
    except:
        return None
