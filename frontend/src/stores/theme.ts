import { ref, watch } from 'vue'

export type ThemeMode = 'dark' | 'light'

const STORAGE_KEY = 'configflow-theme'

const readStored = (): ThemeMode => {
  try {
    const value = localStorage.getItem(STORAGE_KEY)
    if (value === 'dark' || value === 'light') return value
  } catch {
    // 隐私模式或禁用存储时静默回落到默认主题
  }
  return 'dark'
}

const theme = ref<ThemeMode>(readStored())

/* 地址栏底色跟随 theme.css 的 --background，避免颜色在两处各写一遍而走样。
 * 读不到（样式尚未就绪）时回落到与当前 token 等价的近似值。
 */
const FALLBACK_BG: Record<ThemeMode, string> = { dark: '#0e0e11', light: '#f7f7f8' }

const apply = (mode: ThemeMode): void => {
  document.documentElement.dataset.theme = mode
  const meta = document.querySelector('meta[name="theme-color"]')
  if (!meta) return
  const bg = getComputedStyle(document.documentElement).getPropertyValue('--background').trim()
  meta.setAttribute('content', bg || FALLBACK_BG[mode])
}

apply(theme.value)

watch(theme, mode => {
  apply(mode)
  try {
    localStorage.setItem(STORAGE_KEY, mode)
  } catch {
    // 存储不可用时仅保持当前会话生效
  }
})

export const useThemeStore = () => ({
  theme,
  toggleTheme: (): void => {
    theme.value = theme.value === 'dark' ? 'light' : 'dark'
  },
  setTheme: (mode: ThemeMode): void => {
    theme.value = mode
  }
})
