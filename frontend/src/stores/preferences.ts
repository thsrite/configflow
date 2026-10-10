import { ref, watch } from 'vue'

/**
 * 界面偏好（Tweaks 面板）：强调色、流量密度、动效、纸感纹理。
 * 只影响本机观感，存 localStorage；存储不可用时退回默认值，仅当次会话生效。
 */
export type AccentName = 'clay' | 'kraft' | 'sky'

interface Preferences {
  accent: AccentName
  /** 流向图粒子密度倍率 0.2 ~ 3 */
  density: number
  motion: boolean
  texture: boolean
}

const STORAGE_KEY = 'configflow-preferences'
const DEFAULTS: Preferences = { accent: 'clay', density: 1, motion: false, texture: true }

const read = (): Preferences => {
  try {
    const raw = JSON.parse(localStorage.getItem(STORAGE_KEY) || '{}')
    return {
      accent: ['clay', 'kraft', 'sky'].includes(raw.accent) ? raw.accent : DEFAULTS.accent,
      density:
        typeof raw.density === 'number' && raw.density >= 0.2 && raw.density <= 3
          ? raw.density
          : DEFAULTS.density,
      motion: typeof raw.motion === 'boolean' ? raw.motion : DEFAULTS.motion,
      texture: typeof raw.texture === 'boolean' ? raw.texture : DEFAULTS.texture
    }
  } catch {
    return { ...DEFAULTS }
  }
}

const prefs = ref<Preferences>(read())

const apply = (p: Preferences) => {
  const root = document.documentElement
  root.dataset.accent = p.accent
  root.dataset.motion = p.motion ? 'on' : 'off'
  root.dataset.texture = p.texture ? 'on' : 'off'
}

apply(prefs.value)

watch(
  prefs,
  value => {
    apply(value)
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(value))
    } catch {
      // 存储不可用时仅保持当前会话生效
    }
  },
  { deep: true }
)

/** 系统要求减少动态效果时，无论偏好如何都不播放流动动画；调用时再查询，不在模块加载时依赖 matchMedia */
const systemReducesMotion = (): boolean =>
  typeof window.matchMedia === 'function' && window.matchMedia('(prefers-reduced-motion: reduce)').matches

export const usePreferences = () => ({
  prefs,
  /** 实际是否播放动画：偏好开启且系统未要求减少动效 */
  motionEnabled: () => prefs.value.motion && !systemReducesMotion()
})
