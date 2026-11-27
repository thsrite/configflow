export type DockerHANetwork = 'host' | 'macvlan'
export interface DockerHAMount { type: 'bind' | 'volume'; source: string }
export interface DockerHAHostForm {
  mode: 'new' | 'migrate'
  agent_name: string
  agent_ip: string
  oxidns_port: number
  data_dir: string
  container_name: string
  existing_agent_name: string
  agent_mount: DockerHAMount
  core_mount: DockerHAMount
  ha_data_dir: string
  backup_dir: string
  ha_host_id_path: string
  create_host_identity: boolean
  ha_parent: string
  ha_subnet: string
  ha_gateway: string
}
export interface DockerHAGuideRequest extends Partial<DockerHAHostForm> {
  action?: 'inspect' | 'generate' | 'verify' | 'check'
  install_method?: 'compose_compact' | 'compose_direct' | 'compose' | 'script'
  output?: string
  vip?: string
  server_url?: string
  network_mode?: DockerHANetwork
  image?: string
  enable_dns_ha?: boolean
  enable_oxidns?: boolean
  enable_mihomo?: boolean
}
export interface DockerHAComposeBundle {
  project_name: string
  filename: string
  content_base64: string
  prepare_command: string
  start_command: string
}
export interface DockerHAComposeDeployment {
  filename: string
  project_name: string
  command: string
  identity_mode: 'file' | 'docker-volume' | 'installation'
  service_count?: number
}
export interface DockerHAGuideResult {
  success: boolean
  message?: string
  inspection_command: string
  identity_command: string
  compose: string
  install_command: string
  rollback_command: string
  compose_bundle?: DockerHAComposeBundle
  compose_deployment?: DockerHAComposeDeployment
  preflight_report?: DockerHAPreflightReport
  verification_command?: string
  command_blocks?: { id: string; title: string; command: string }[]
  warnings?: string[]
}
export interface DockerHAEnvironment {
  success: boolean
  message?: string
  agent_id: string
  service_type: string
  oxidns_ha_protocols: number[]
  oxidns_ha_container_protocols: number[]
  oxidns_ha_handoff_protocols: number[]
  oxidns_ha_environment: {
    missing_tools?: string[]
    kind: string
    ready: boolean
    network?: string
    host_id?: string
    host_id_source?: 'file' | 'docker-volume' | 'installation' | 'unknown'
    host_boot_id?: string
    blockers?: string[]
  }
}

export type DockerHAPreflightStatus = 'pass' | 'warning' | 'fail' | 'unknown'
export interface DockerHAPreflightCheck {
  id: string
  label: string
  status: DockerHAPreflightStatus
  detail: string
  remedy?: string
}
export interface DockerHAPreflightReport {
  status: DockerHAPreflightStatus
  summary: string
  checks: DockerHAPreflightCheck[]
  host_fingerprint?: string
}
