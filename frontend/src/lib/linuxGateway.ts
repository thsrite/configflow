import type { LinuxGatewaySettings, LinuxGatewaySpec } from '@/api/linuxGateway'

export const emptyGatewaySpec = (): LinuxGatewaySpec => ({ interface: '', address: '', lan_cidr: '', upstream_gateway: '' })
export const cleanGatewaySpec = (spec: LinuxGatewaySpec): LinuxGatewaySpec => ({
  interface: spec.interface.trim(), address: spec.address.trim(),
  lan_cidr: spec.lan_cidr.trim(), upstream_gateway: spec.upstream_gateway.trim(),
})
export const gatewaySettingsKey = (settings: LinuxGatewaySettings) => JSON.stringify({
  enabled: settings.enabled, rule_set_ids: [...new Set(settings.rule_set_ids)].sort(),
})
export const gatewaySpecKey = (spec: LinuxGatewaySpec) => JSON.stringify(cleanGatewaySpec(spec))

function ipv4(value: string): number | null {
  if (!/^(?:\d{1,3}\.){3}\d{1,3}$/.test(value)) return null
  if (value.split('.').some(part => part.length > 1 && part.startsWith('0'))) return null
  const octets = value.split('.').map(Number)
  if (octets.some(part => part > 255)) return null
  return octets.reduce((sum, part) => (sum * 256) + part, 0)
}

export function gatewaySpecErrors(draft: LinuxGatewaySpec): string[] {
  const spec = cleanGatewaySpec(draft)
  const errors: string[] = []
  if (!/^[A-Za-z0-9_][A-Za-z0-9_.-]{0,14}$/.test(spec.interface) || spec.interface === 'lo') errors.push('LAN 网卡名需为 1–15 个字母、数字、下划线、短横线或点，不能以短横线或点开头，也不能使用 lo。')
  const address = ipv4(spec.address)
  const gateway = ipv4(spec.upstream_gateway)
  if (address === null) errors.push('Linux 地址需要填写 IPv4 地址，不带端口或掩码。')
  if (gateway === null) errors.push('上游网关需要填写主路由的 IPv4 地址。')
  const unusable = (ip: number) => ip < 2 ** 24 || Math.floor(ip / 2 ** 24) === 127
    || Math.floor(ip / 2 ** 16) === 169 * 256 + 254 || ip >= 224 * 2 ** 24
  if (address !== null && unusable(address)) errors.push('Linux 地址不能是回环、链路本地、组播或保留地址。')
  if (gateway !== null && unusable(gateway)) errors.push('主路由地址不能是回环、链路本地、组播或保留地址。')
  const match = spec.lan_cidr.match(/^((?:\d{1,3}\.){3}\d{1,3})\/(\d{1,2})$/)
  const network = match ? ipv4(match[1]) : null
  const prefix = match ? Number(match[2]) : -1
  if (network === null || prefix < 8 || prefix > 30) {
    errors.push('LAN 网段需要使用 /8 到 /30 的 CIDR 格式，例如 192.0.2.0/24。')
  } else {
    const size = 2 ** (32 - prefix)
    if (network % size !== 0) errors.push('LAN 网段请填写网络地址，例如 192.0.2.0/24，而不是设备地址。')
    if (address !== null && Math.floor(address / size) !== Math.floor(network / size)) errors.push('Linux 地址必须在填写的 LAN 网段内。')
    if (gateway !== null && Math.floor(gateway / size) !== Math.floor(network / size)) errors.push('此向导用于单网卡同网段网关，主路由地址必须在 LAN 网段内。')
    if (address !== null && (address % size === 0 || address % size === size - 1)) errors.push('Linux 地址不能使用该网段的网络地址或广播地址。')
    if (gateway !== null && (gateway % size === 0 || gateway % size === size - 1)) errors.push('主路由地址不能使用该网段的网络地址或广播地址。')
  }
  if (address !== null && address === gateway) errors.push('Linux 地址与主路由地址不能相同。')
  return errors
}

export function gatewayErrorMessage(error: any, hostOperation = false): string {
  if (hostOperation && error?.response?.status === 404) return '此 Agent 尚不支持 Linux 网关管理，请在 Agent 页面升级后重试。'
  return error?.response?.data?.message || error?.message || '请求未完成，请稍后重试。'
}
