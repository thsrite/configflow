"""Agent 安装脚本生成器"""
import os
import re
import ipaddress
import shlex
import yaml
from typing import Dict, Any

from backend.utils.logger import get_logger

logger = get_logger(__name__)

# 获取当前文件所在目录
CURRENT_DIR = os.path.dirname(os.path.abspath(__file__))
SCRIPTS_DIR = os.path.join(CURRENT_DIR, "scripts")


def _cli_output_script() -> str:
    """Embed the shared output helper so downloaded scripts remain standalone."""
    with open(os.path.join(SCRIPTS_DIR, "cli-output.sh"), encoding="utf-8") as source:
        return source.read()


def validate_agent_name(name: str) -> None:
    """验证 Agent 名称安全性"""
    if not re.match(r'^[a-zA-Z0-9_-]+$', name):
        raise ValueError("Invalid agent name: only alphanumeric characters, hyphens, and underscores are allowed.")


def generate_lightweight_install_script(
    server_url: str,
    agent_name: str,
    service_type: str,
    agent_port: int = 8080,
    agent_ip: str = "",
    config_path: str = None,
    restart_command: str = None
) -> str:
    """生成轻量级 Shell Agent 安装脚本"""
    if service_type == "oxidns":
        raise ValueError("OxiDNS requires the Go Agent for native validation and transactional rollback")
    # 验证名称
    validate_agent_name(agent_name)

    # 读取脚本模板
    install_script_path = os.path.join(SCRIPTS_DIR, "install.sh")
    agent_script_path = os.path.join(SCRIPTS_DIR, "agent.sh")

    try:
        with open(install_script_path, "r", encoding="utf-8") as f:
            install_template = f.read()

        with open(agent_script_path, "r", encoding="utf-8") as f:
            agent_content = f.read()
        cli_output = _cli_output_script()
    except Exception as e:
        logger.error(f"Error reading template files: {e}")
        return f"#!/bin/sh\n# Error reading template files: {str(e)}"

    # 处理默认值
    if not config_path:
        if service_type == "mihomo":
            config_path = "/etc/mihomo/config.yaml"
        elif service_type == "mosdns":
            config_path = "/etc/mosdns/config.yaml"
        else:
            config_path = "/etc/config.yaml"

    if not restart_command:
        if service_type == "mihomo":
            restart_command = "systemctl restart mihomo"
        elif service_type == "mosdns":
            restart_command = "systemctl restart mosdns"
        else:
            restart_command = "echo 'No restart command configured'"

    # 设置服务名称
    service_name = service_type

    # One pass prevents user values from expanding code/template placeholders.
    replacements = {
        "{cli_output}": cli_output,
        "{agent_shell_content}": agent_content,
        "{server_url}": server_url,
        "{agent_name}": agent_name,
        "{agent_host}": agent_ip, # 暂时使用 agent_ip 作为 host
        "{agent_port}": str(agent_port),
        "{agent_ip}": agent_ip,
        "{service_type}": service_type,
        "{service_name}": service_name,
        "{config_path}": config_path,
        "{restart_command}": restart_command
    }

    return re.sub(
        "|".join(re.escape(key) for key in replacements),
        lambda match: str(replacements[match.group(0)]),
        install_template,
    )


def _docker_ha_network(agent_name, agent_ip, network_mode, enable_mihomo, enable_mosdns,
                       enable_oxidns, ha_parent, ha_subnet, ha_gateway):
    if not enable_oxidns or enable_mihomo or enable_mosdns:
        raise ValueError("Docker DNS 高可用仅支持单独运行 OxiDNS")
    if network_mode not in ("host", "macvlan"):
        raise ValueError("Docker DNS 高可用需选择 Host 或 Macvlan，不支持 bridge")
    try:
        address = ipaddress.IPv4Address(agent_ip)
        if address.is_loopback or address.is_multicast or address.is_link_local or address.is_unspecified or address.is_reserved:
            raise ValueError()
    except ValueError as exc:
        raise ValueError("请填写有效的固定局域网 IPv4 地址；Host 模式填写宿主机地址，Macvlan 模式填写容器地址") from exc
    if network_mode == "host":
        return None
    if not re.fullmatch(r"[A-Za-z0-9_][A-Za-z0-9_.-]{0,14}", ha_parent):
        raise ValueError("请填写有效的宿主机网卡名称，如 eth0 或 enp1s0")
    try:
        subnet = ipaddress.IPv4Network(ha_subnet, strict=True)
        address = ipaddress.IPv4Address(agent_ip)
        gateway = ipaddress.IPv4Address(ha_gateway) if ha_gateway else None
        if subnet.prefixlen > 30 or any(ip.is_loopback or ip.is_multicast or ip.is_link_local or ip.is_unspecified
                                       for ip in (subnet.network_address, address)):
            raise ValueError()
        for ip in (address, gateway):
            if ip is not None and (ip not in subnet or ip in (subnet.network_address, subnet.broadcast_address)):
                raise ValueError()
        if address == gateway:
            raise ValueError()
    except ValueError as exc:
        raise ValueError("请填写有效的 IPv4 局域网网段、容器固定地址和可选网关；容器地址须在该网段内且不能与网关相同") from exc
    ipam = {'subnet': str(subnet)}
    if gateway:
        ipam['gateway'] = str(gateway)
    return {
        'name': f'{agent_name}-oxidns-ha-net',
        'driver': 'macvlan',
        'driver_opts': {'parent': ha_parent},
        'ipam': {'config': [ipam]},
    }


def _docker_ha_identity(path):
    if not isinstance(path, str) or not os.path.isabs(path) or any(char in path for char in ',:\x00\r\n'):
        raise ValueError("宿主机身份文件须为绝对路径，且不能包含逗号、冒号或换行")
    return {'type': 'bind', 'source': path, 'target': '/run/configflow-host-machine-id',
            'read_only': True, 'bind': {'create_host_path': False}}


def generate_docker_agent_compose(
    server_url: str,
    agent_name: str = "agent",
    agent_ip: str = "",
    data_dir: str = "./agent_data",
    network_mode: str = "host",
    enable_mihomo: bool = True,
    enable_mosdns: bool = False,
    mihomo_port: int = 8080,
    mosdns_port: int = 8081,
    enable_oxidns: bool = False,
    oxidns_port: int = 8082,
    enable_dns_ha: bool = False,
    ha_parent: str = "",
    ha_subnet: str = "",
    ha_gateway: str = "",
    ha_host_id_path: str = "/etc/machine-id",
) -> str:
    """生成统一的 Docker Compose 配置"""
    # 验证名称
    validate_agent_name(agent_name)
    ha_network = None
    if enable_dns_ha:
        ha_network = _docker_ha_network(agent_name, agent_ip, network_mode, enable_mihomo,
                                        enable_mosdns, enable_oxidns, ha_parent, ha_subnet, ha_gateway)
        if not isinstance(oxidns_port, int) or not 1024 <= oxidns_port <= 65535:
            raise ValueError("OxiDNS Agent 端口须为 1024–65535")
        identity = _docker_ha_identity(ha_host_id_path)
    if enable_mosdns and enable_oxidns:
        raise ValueError("MosDNS 与 OxiDNS 默认都监听 53 端口，请使用独立容器或只启用一个 DNS 核心")

    services_enabled = []
    if enable_mihomo:
        services_enabled.append("mihomo")
    if enable_mosdns:
        services_enabled.append("mosdns")
    if enable_oxidns:
        services_enabled.append("oxidns")

    service_suffix = "-".join(services_enabled) if services_enabled else "agent"
    container_name = f"{agent_name}-{service_suffix}"

    # 环境变量列表
    environment = [
        f"SERVER_URL={server_url}",
        "TZ=Asia/Shanghai",
        f"ENABLE_MIHOMO={'true' if enable_mihomo else 'false'}",
        f"ENABLE_MOSDNS={'true' if enable_mosdns else 'false'}",
        f"ENABLE_OXIDNS={'true' if enable_oxidns else 'false'}"
    ]

    if agent_ip:
        environment.append(f"AGENT_IP={agent_ip}")

    if enable_mihomo:
        environment.extend([
            f"AGENT_MIHOMO_NAME={agent_name}-mihomo",
            f"AGENT_MIHOMO_PORT={mihomo_port}"
        ])

    if enable_mosdns:
        environment.extend([
            f"AGENT_MOSDNS_NAME={agent_name}-mosdns",
            f"AGENT_MOSDNS_PORT={mosdns_port}"
        ])

    if enable_oxidns:
        environment.extend([f"AGENT_OXIDNS_NAME={agent_name}-oxidns", f"AGENT_OXIDNS_PORT={oxidns_port}"])

    # 卷映射
    volumes = [f"{data_dir}/agent:/opt/configflow-agent"]
    if enable_mihomo:
        volumes.append(f"{data_dir}/mihomo:/etc/mihomo")
    if enable_mosdns:
        volumes.append(f"{data_dir}/mosdns:/etc/mosdns")
    if enable_oxidns:
        volumes.append(f"{data_dir}/oxidns:/etc/oxidns")
    if enable_dns_ha:
        environment.extend(["CONFIGFLOW_DNS_HA=1", f"CONFIGFLOW_DNS_HA_NETWORK={network_mode}"])
        volumes.extend([
            f"{data_dir}/ha:/var/lib/configflow-ha",
            identity,
        ])
        if network_mode == 'host':
            volumes.append({'type': 'bind', 'source': '/proc/1/ns/net', 'target': '/run/configflow-host-netns',
                            'read_only': True, 'bind': {'create_host_path': False}})

    # 构建服务配置
    service_config = {
        'image': 'thsrite/config-flow-agent:latest',
        'container_name': container_name,
        'restart': 'unless-stopped',
        'network_mode': network_mode,
        'environment': environment,
        'volumes': volumes,
        'logging': {
            'driver': 'json-file',
            'options': {
                'max-size': '10m',
                'max-file': '3'
            }
        }
    }
    if ha_network:
        service_config.pop('network_mode')
        service_config.update({
            'cap_add': ['NET_ADMIN', 'NET_RAW'],
            'stop_grace_period': '30s',
            'networks': {'dns-ha': {'ipv4_address': agent_ip}},
        })
    elif enable_dns_ha:
        service_config.update({'stop_grace_period': '30s', 'depends_on': ['ha-companion']})

    # 端口映射
    if network_mode == "bridge":
        port_mappings = []
        if enable_mihomo:
            port_mappings.extend([
                f"{mihomo_port}:{mihomo_port}",
                "53:53/udp",
                "7890:7890",
                "9090:9090"
            ])
        if enable_mosdns:
            port_mappings.append(f"{mosdns_port}:{mosdns_port}")
            # 避免重复添加 53 端口
            if "53:53/udp" not in port_mappings:
                port_mappings.append("53:53/udp")
        
        if enable_oxidns:
            port_mappings.extend([f"{oxidns_port}:{oxidns_port}", "53:53/tcp"])
            if "53:53/udp" not in port_mappings:
                port_mappings.append("53:53/udp")
        if port_mappings:
            service_config['ports'] = port_mappings

    # 构建完整的 Compose 配置
    compose_config = {
        'version': '3.8',
        'services': {
            'agent': service_config
        }
    }
    if ha_network:
        compose_config['networks'] = {'dns-ha': ha_network}
    elif enable_dns_ha:
        compose_config['services']['ha-companion'] = {
            'image': 'thsrite/config-flow-agent:latest',
            'container_name': container_name + '-ha',
            'restart': 'unless-stopped',
            'network_mode': 'host',
            'cap_add': ['NET_ADMIN', 'NET_RAW'],
            'stop_grace_period': '30s',
            'environment': ['SERVICE_TYPE=ha-companion', 'CONFIGFLOW_DNS_HA=1',
                            'CONFIGFLOW_DNS_HA_NETWORK=host', 'TZ=Asia/Shanghai'],
            'volumes': [f'{data_dir}/agent:/opt/configflow-agent:ro', f'{data_dir}/oxidns:/etc/oxidns:ro',
                        f'{data_dir}/ha:/var/lib/configflow-ha', identity],
            'logging': service_config['logging'],
        }

    return yaml.dump(compose_config, sort_keys=False, allow_unicode=True)


_SHELL_SAFE = re.compile(r"[A-Za-z0-9@%+=:,./_-]+")


def _shell_word(value, plain=True) -> str:
    """Quote panel input for the generated command; ordinary values keep their form."""
    value = str(value)
    if _SHELL_SAFE.fullmatch(value):
        return value if plain else f'"{value}"'
    return shlex.quote(value)


def generate_docker_agent_run(
    server_url: str,
    agent_name: str = "agent",
    agent_ip: str = "",
    data_dir: str = "./agent_data",
    network_mode: str = "host",
    enable_mihomo: bool = True,
    enable_mosdns: bool = False,
    mihomo_port: int = 8080,
    mosdns_port: int = 8081,
    enable_oxidns: bool = False,
    oxidns_port: int = 8082,
    enable_dns_ha: bool = False,
    ha_parent: str = "",
    ha_subnet: str = "",
    ha_gateway: str = "",
    ha_host_id_path: str = "/etc/machine-id",
) -> str:
    """生成统一的 Docker Run 命令"""
    # 验证名称
    validate_agent_name(agent_name)
    if enable_dns_ha:
        network = _docker_ha_network(agent_name, agent_ip, network_mode, enable_mihomo,
                                     enable_mosdns, enable_oxidns, ha_parent, ha_subnet, ha_gateway)
        if not isinstance(oxidns_port, int) or not 1024 <= oxidns_port <= 65535:
            raise ValueError("OxiDNS Agent 端口须为 1024–65535")
        _docker_ha_identity(ha_host_id_path)
        return _generate_docker_ha_run(server_url, agent_name, agent_ip, data_dir, oxidns_port, network, ha_host_id_path)
    if enable_mosdns and enable_oxidns:
        raise ValueError("MosDNS 与 OxiDNS 默认都监听 53 端口，请使用独立容器或只启用一个 DNS 核心")

    services = []
    if enable_mihomo:
        services.append("mihomo")
    if enable_mosdns:
        services.append("mosdns")
    if enable_oxidns:
        services.append("oxidns")

    service_suffix = "-".join(services) if services else "agent"
    container_name = f"{agent_name}-{service_suffix}"

    # 服务描述
    if enable_mihomo and enable_mosdns:
        service_desc = "Mihomo + MosDNS"
    elif enable_mihomo:
        service_desc = "Mihomo"
    elif enable_mosdns:
        service_desc = "MosDNS"
    else:
        service_desc = "Agent"

    if enable_oxidns:
        service_desc = "Mihomo + OxiDNS" if enable_mihomo else "OxiDNS"

    # 构建端口映射
    ports_args = ""
    if network_mode == "bridge":
        ports = []
        if enable_mihomo:
            ports.extend([
                f"-p {mihomo_port}:{mihomo_port}",
                "-p 7890:7890",
                "-p 9090:9090",
                "-p 53:53/udp"
            ])
        
        if enable_mosdns:
            ports.append(f"-p {mosdns_port}:{mosdns_port}")
            # 避免重复添加 53 端口
            if not enable_mihomo:
                ports.append("-p 53:53/udp")
                
        if enable_oxidns:
            ports.extend([f"-p {oxidns_port}:{oxidns_port}", "-p 53:53/tcp"])
            if "-p 53:53/udp" not in ports:
                ports.append("-p 53:53/udp")
        if ports:
            ports_args = " ".join(ports) + " \\"

    # 构建卷映射
    volumes_args = ""
    # A data directory with spaces must stay one argument.
    volumes = ["-v " + _shell_word(f"{data_dir}/agent:/opt/configflow-agent")]
    if enable_mihomo:
        volumes.append("-v " + _shell_word(f"{data_dir}/mihomo:/etc/mihomo"))
    if enable_mosdns:
        volumes.append("-v " + _shell_word(f"{data_dir}/mosdns:/etc/mosdns"))
    if enable_oxidns:
        volumes.append("-v " + _shell_word(f"{data_dir}/oxidns:/etc/oxidns"))
        
    if volumes:
        volumes_args = " ".join(volumes) + " \\"

    # 构建命令
    cmd = f"""#!/bin/bash
# 运行 Agent + {service_desc}
docker run -d \\
  --name {container_name} \\
  --restart unless-stopped \\
  --network {_shell_word(network_mode)} \\"""

    if ports_args:
        cmd += f"\n  {ports_args}"

    cmd += f"""
  -e SERVER_URL={_shell_word(server_url, plain=False)} \\
  -e ENABLE_MIHOMO="{'true' if enable_mihomo else 'false'}" \\
  -e ENABLE_MOSDNS="{'true' if enable_mosdns else 'false'}" \\"""

    if agent_ip:
        cmd += f"\n  -e AGENT_IP={_shell_word(agent_ip)} \\"

    if enable_mihomo:
        cmd += f"""
  -e AGENT_MIHOMO_NAME={agent_name}-mihomo \\
  -e AGENT_MIHOMO_PORT={mihomo_port} \\"""

    if enable_mosdns:
        cmd += f"""
  -e AGENT_MOSDNS_NAME={agent_name}-mosdns \\
  -e AGENT_MOSDNS_PORT={mosdns_port} \\"""

    if enable_oxidns:
        cmd += f'\n  -e ENABLE_OXIDNS="true" -e AGENT_OXIDNS_NAME={agent_name}-oxidns -e AGENT_OXIDNS_PORT={oxidns_port} \\'

    if volumes_args:
        cmd += f"\n  {volumes_args}"
    cmd += """
  thsrite/config-flow-agent:latest
"""
    # This is a user-run installer, unlike the long-running Docker entrypoint.
    # Capture Docker progress/container output in the same log as other installers.
    cmd = cmd.replace("docker run -d", 'cf_run "启动 Docker Agent" docker run -d', 1)
    header, body = cmd.split("\n", 1)
    return (
        header + "\n" + _cli_output_script()
        + '\ncf_verbose_options "$@"\ncf_init "Docker Agent 安装" || exit $?\nset -e\n'
        + body
        + f'cf_notice "容器：{container_name}"\n'
        + 'cf_result "Docker Agent 安装完成"\n'
    )


def _generate_docker_ha_run(server_url, agent_name, agent_ip, data_dir, port, network, host_id_path):
    """A new HA installation, never an in-place container upgrade."""
    quote = shlex.quote
    setup = ''
    if network:
        ipam = network['ipam']['config'][0]
        create = ['docker', 'network', 'create', '--driver', 'macvlan',
                  '--subnet', ipam['subnet'], '--opt', 'parent=' + network['driver_opts']['parent']]
        if ipam.get('gateway'):
            create.extend(['--gateway', ipam['gateway']])
        create.append(network['name'])
        setup = ('# 新安装专用：已有同名网络时停止，请沿用原 Compose 或启动参数维护。\n'
                 + 'cf_run "创建专用 macvlan 网络" ' + ' '.join(map(quote, create)) + '\n')
    env = {
        'SERVER_URL': server_url, 'TZ': 'Asia/Shanghai',
        'ENABLE_MIHOMO': 'false', 'ENABLE_MOSDNS': 'false', 'ENABLE_OXIDNS': 'true',
        'CONFIGFLOW_DNS_HA': '1', 'AGENT_IP': agent_ip,
        'CONFIGFLOW_DNS_HA_NETWORK': 'macvlan' if network else 'host',
        'AGENT_OXIDNS_NAME': agent_name + '-oxidns', 'AGENT_OXIDNS_PORT': str(port),
    }
    identity = '--mount ' + quote(f'type=bind,src={host_id_path},dst=/run/configflow-host-machine-id,readonly')
    run = [
        'cf_run "启动 OxiDNS 高可用容器" docker run -d',
        '--name ' + quote(agent_name + '-oxidns'), '--restart unless-stopped', '--stop-timeout 30',
        *(['--network ' + quote(network['name']), '--ip ' + quote(agent_ip), '--cap-add NET_ADMIN --cap-add NET_RAW'] if network
          else ['--network host', '--mount type=bind,src=/proc/1/ns/net,dst=/run/configflow-host-netns,readonly']),
        identity,
        '-v "$cf_ha_data/agent:/opt/configflow-agent"',
        '-v "$cf_ha_data/oxidns:/etc/oxidns"', '-v "$cf_ha_data/ha:/var/lib/configflow-ha"',
        *['-e ' + quote(key + '=' + value) for key, value in env.items()],
        'thsrite/config-flow-agent:latest',
    ]
    companion = [] if network else [
        'cf_run "启动独立高可用容器" docker run -d',
        '--name ' + quote(agent_name + '-oxidns-ha'), '--restart unless-stopped', '--stop-timeout 30',
        '--network host', '--cap-add NET_ADMIN --cap-add NET_RAW', identity,
        '-v "$cf_ha_data/agent:/opt/configflow-agent:ro"',
        '-v "$cf_ha_data/oxidns:/etc/oxidns:ro"', '-v "$cf_ha_data/ha:/var/lib/configflow-ha"',
        '-e SERVICE_TYPE=ha-companion -e CONFIGFLOW_DNS_HA=1 -e CONFIGFLOW_DNS_HA_NETWORK=host',
        'thsrite/config-flow-agent:latest',
    ]
    return ('#!/bin/bash\n' + _cli_output_script()
            + '\ncf_verbose_options "$@"\ncf_init "Docker OxiDNS 高可用安装" || exit $?\nset -e\n'
            + '[ "$(uname -s)" = Linux ] || { cf_error "仅支持 Linux Docker 宿主机"; exit 1; }\n'
            + '[ -f ' + quote(host_id_path) + ' ] && [ -r ' + quote(host_id_path) + ' ] || { cf_error "无法读取宿主机身份文件"; exit 1; }\n'
            + setup
            + 'mkdir -p -- ' + quote(data_dir) + '\n'
            + 'cf_ha_data=$(cd -- ' + quote(data_dir) + ' && pwd -P)\n'
            + 'mkdir -p "$cf_ha_data/agent" "$cf_ha_data/oxidns" "$cf_ha_data/ha"\n'
            + (' \\\n  '.join(companion) + '\n' if companion else '')
            + ' \\\n  '.join(run) + '\n'
            + 'cf_notice "待两台 Agent 上线后，在 DNS 高可用设置中配对；Agent 使用的固定地址不是虚拟 IP"\n'
            + 'cf_result "Docker OxiDNS 高可用容器安装完成"\n')
