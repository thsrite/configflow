/**
 * 搜索面板的「常用页面」：按本机打开次数排序，只存在浏览器里，不上报。
 * 还没有记录时用默认的几个常用页面补足。
 */
const KEY = 'cf.palette.visits'
const LIMIT = 5
/** 没有访问记录时的默认常用页面（桌面） */
export const DEFAULT_FREQUENT = ['/dashboard', '/oxidns', '/mihomo', '/rules', '/agents']
/** 有手机底栏时：底栏已直达的页面不再推荐，默认推荐只能从「更多」进入的常用页面 */
export const DEFAULT_FREQUENT_BEYOND_TABBAR = ['/nodes', '/subscriptions', '/proxy-groups', '/client-devices', '/generate']

type Visits = Record<string, { n: number; at: number }>

function read(): Visits {
  try {
    const value = JSON.parse(localStorage.getItem(KEY) || '{}')
    return value && typeof value === 'object' ? value as Visits : {}
  } catch {
    return {}
  }
}

export function recordVisit(path: string, now = Date.now()) {
  const visits = read()
  const entry = visits[path] || { n: 0, at: 0 }
  visits[path] = { n: entry.n + 1, at: now }
  try { localStorage.setItem(KEY, JSON.stringify(visits)) } catch { /* 隐私模式下不记录 */ }
}

/** 按次数、再按最近一次排序，取前 5 个；只保留可推荐的页面（valid），不足时用默认页面补足 */
export function frequentPages(valid: Set<string>, defaults: string[] = DEFAULT_FREQUENT): string[] {
  const visits = read()
  const ranked = Object.entries(visits)
    .filter(([path]) => valid.has(path))
    .sort(([, a], [, b]) => b.n - a.n || b.at - a.at)
    .map(([path]) => path)
  for (const path of defaults) if (valid.has(path) && !ranked.includes(path)) ranked.push(path)
  return ranked.slice(0, LIMIT)
}
