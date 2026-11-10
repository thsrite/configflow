/**
 * The OxiDNS device this browser last opened. Kept apart from useOxiDNSDevices
 * so the router can read it without importing the API client.
 */
export const OXIDNS_DEVICE_STORAGE_KEY = 'configflow:oxidns-monitor:agent'
export const oxidnsMonitorPath = (id: string) => '/agents/' + encodeURIComponent(id) + '/oxidns'

export function rememberedOxiDNSDevice(): string {
  try { return window.localStorage.getItem(OXIDNS_DEVICE_STORAGE_KEY) || '' } catch { return '' /* Selection still works without storage. */ }
}

export function rememberOxiDNSDevice(id: string) {
  try { window.localStorage.setItem(OXIDNS_DEVICE_STORAGE_KEY, id) } catch { /* Storage can be unavailable in private browsers. */ }
}

/** Forget a device that no longer exists, so opening the panel picks another one. */
export function forgetOxiDNSDevice(id: string) {
  try {
    if (window.localStorage.getItem(OXIDNS_DEVICE_STORAGE_KEY) === id) window.localStorage.removeItem(OXIDNS_DEVICE_STORAGE_KEY)
  } catch { /* Nothing was remembered. */ }
}
