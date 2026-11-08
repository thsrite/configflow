const shellQuote = (value: string) => `'${value.replace(/'/g, `'"'"'`)}'`
export type CoreInstallTarget = 'both' | 'mihomo' | 'oxidns'
export const INSTALLER_DOWNLOADERS = ['curl', 'wget'] as const
export type InstallerDownloader = typeof INSTALLER_DOWNLOADERS[number]
export const CORE_INSTALL_INTERFACE = '# ConfigFlow core installer interface: 1'
export const AGENT_CORE_INSTALL_INTERFACE = '# ConfigFlow Agent + core installer interface: 1'
export const DNS_HA_PREPARE_INTERFACE = '# ConfigFlow DNS HA preparation interface: 1'

/** The short URL identifies the exact server-generated script, including its install options. */
export function buildInstallCommand(url: string, downloader: InstallerDownloader = 'curl'): string {
  if (!(INSTALLER_DOWNLOADERS as readonly string[]).includes(downloader)) throw new Error('请选择 curl 或 wget')
  const download = downloader === 'curl'
    ? `curl -fsSL ${shellQuote(url)} -o configflow-install.sh`
    : `wget -O configflow-install.sh ${shellQuote(url)}`
  return `${download} && sh configflow-install.sh`
}
