export const installCommandUpdateMessage = '当前服务不支持短安装链接，请更新 ConfigFlow 后重试'

export const readInstallCommand = (data: unknown, baseUrl: URL) => {
  if (!data || typeof data !== 'object') throw new Error(installCommandUpdateMessage)
  const { script, download_path: path, expires_at: expiresAt } = data as Record<string, unknown>
  if (typeof script !== 'string' || !script.startsWith('#!')) throw new Error(installCommandUpdateMessage)
  if (typeof path !== 'string' || !/^\/api\/agents\/i\/[A-Za-z0-9_-]{22}$/.test(path)) {
    throw new Error('服务返回的安装链接无效，请更新 ConfigFlow 后重试')
  }
  if (typeof expiresAt !== 'number' || !Number.isSafeInteger(expiresAt) || expiresAt <= Date.now() / 1000) {
    throw new Error('安装链接已失效，请重新生成')
  }
  const url = new URL(baseUrl)
  url.pathname = `${url.pathname.replace(/\/+$/, '')}${path}`
  return {
    script, url: url.toString(),
    expiry: new Date(expiresAt * 1000).toLocaleString('zh-CN', {
      month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit', hour12: false
    })
  }
}

