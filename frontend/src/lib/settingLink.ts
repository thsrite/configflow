import { inject, nextTick, onBeforeUnmount, watch } from 'vue'
import { routerKey, type LocationQuery } from 'vue-router'
import { notify } from '@/lib/feedback'

export const queryText = (value: LocationQuery[string]): string => typeof value === 'string' && value.length <= 256 ? value : ''

/** Consume a setting link only after its data loaded successfully, including same-page navigation. */
export function useSettingLink(options: {
  keys: string[]
  ready: () => boolean
  matches: (query: LocationQuery) => boolean
  open: (query: LocationQuery, current: () => boolean) => void | Promise<void>
}) {
  const router = inject(routerKey, null)
  let generation = 0
  let disposed = false
  onBeforeUnmount(() => { disposed = true; generation++ })
  watch(() => [JSON.stringify(router?.currentRoute.value.query), options.ready()], async (_next, _old, onCleanup) => {
    if (!router || !options.ready()) return
    const query = { ...router.currentRoute.value.query }
    if (!options.matches(query)) return
    const request = ++generation
    onCleanup(() => { if (generation === request) generation++ })
    const current = () => !disposed && generation === request
    try {
      await options.open(query, current)
    } catch {
      if (current()) notify.error('未能打开对应设置，请重新尝试。')
    } finally {
      if (current()) {
        const remaining = { ...router.currentRoute.value.query }
        options.keys.forEach(key => { delete remaining[key] })
        await router.replace({ query: remaining, hash: router.currentRoute.value.hash })
      }
    }
  }, { immediate: true, flush: 'post' })
}

export async function focusSetting(element?: HTMLElement | null) {
  await nextTick()
  if (!element) return
  document.querySelectorAll('[data-notification-focus]').forEach(node => node.removeAttribute('data-notification-focus'))
  element.setAttribute('data-notification-focus', '')
  if (!element.matches('button, a[href], input, textarea, select, [tabindex]')) element.setAttribute('tabindex', '-1')
  element.focus({ preventScroll: true })
  element.scrollIntoView?.({ block: 'center', behavior: 'auto' })
}
