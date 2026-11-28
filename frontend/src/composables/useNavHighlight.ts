import { nextTick, ref, watch } from 'vue'
import { usePreferences } from '@/stores/preferences'

/** 换页前记录旧高亮的位置，新的高亮从同一位置滑入。 */
export function useNavHighlight(activePath: () => string) {
  const navigation = ref<HTMLElement>()
  const { motionEnabled } = usePreferences()
  const highlight = () => navigation.value?.querySelector<HTMLElement>('[data-nav-highlight]')
  watch(activePath, async () => {
    if (!motionEnabled()) return
    const previous = highlight()
    const from = previous?.getBoundingClientRect()
    if (!from?.width) return
    await nextTick()
    const next = highlight()
    if (!next || next === previous) return
    const to = next.getBoundingClientRect()
    if (!to.width) return
    next.style.setProperty('--cf-nav-from',
      `translate(${from.left - to.left}px, ${from.top - to.top}px) scale(${from.width / to.width}, ${from.height / to.height})`)
    next.style.animationName = 'cf-nav-slide'
  })
  return navigation
}
