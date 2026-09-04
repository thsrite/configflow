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

const apply = (mode: ThemeMode): void => {
  document.documentElement.dataset.theme = mode
  const meta = document.querySelector('meta[name="theme-color"]')
  if (meta) meta.setAttribute('content', mode === 'dark' ? '#0d0f13' : '#f6f7f9')
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
