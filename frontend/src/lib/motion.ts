/**
 * motion-v 动效预设：全站进出场语汇的唯一来源。
 *
 * 统一用 spring，避免不同页面各写一套时长导致节奏不一致。
 * 需要减少动效时由 CSS 的 prefers-reduced-motion 兜底（motion-v 自身也会遵循）。
 */
export const SPRING = { type: 'spring', stiffness: 320, damping: 30, mass: 0.7 } as const
export const SPRING_SOFT = { type: 'spring', stiffness: 200, damping: 26 } as const

/** 卡片 / 区块进场：轻微上浮 */
export const riseIn = {
  initial: { opacity: 0, y: 10 },
  animate: { opacity: 1, y: 0 },
  transition: SPRING
}

/** 列表项进场，配合 index 做 stagger */
export const listItem = (index: number) => ({
  initial: { opacity: 0, y: 8 },
  animate: { opacity: 1, y: 0 },
  transition: { ...SPRING, delay: Math.min(index, 12) * 0.035 }
})

/** 弹层 / 面板 */
export const popIn = {
  initial: { opacity: 0, scale: 0.97 },
  animate: { opacity: 1, scale: 1 },
  exit: { opacity: 0, scale: 0.97 },
  transition: SPRING
}

/** 数值滚动时长（秒） */
export const COUNT_DURATION = 0.9
