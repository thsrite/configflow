<template>
  <span class="num tabular-nums">{{ display }}</span>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { COUNT_DURATION } from '@/lib/motion'
import { usePreferences } from '@/stores/preferences'

const props = withDefaults(
  defineProps<{
    value: number
    /** 小数位；默认取整 */
    precision?: number
    /** 关闭滚动动画（例如实时刷新的数值，滚动反而看不清） */
    instant?: boolean
  }>(),
  { precision: 0, instant: false }
)

const current = ref(0)
const { prefs } = usePreferences()
const media = typeof window.matchMedia === 'function'
  ? window.matchMedia('(prefers-reduced-motion: reduce)')
  : undefined
const reducedMotion = ref(media?.matches ?? false)
const pageHidden = ref(document.hidden)
let frame: number | null = null
let disposed = false

const cancelFrame = () => {
  if (frame !== null) cancelAnimationFrame(frame)
  frame = null
}

const run = (to: number) => {
  cancelFrame()
  if (!prefs.value.motion || props.instant || reducedMotion.value || pageHidden.value || !Number.isFinite(to)) {
    current.value = Number.isFinite(to) ? to : 0
    return
  }
  const from = current.value
  if (from === to) return
  const start = performance.now()
  const duration = COUNT_DURATION * 1000
  const step = (now: number) => {
    frame = null
    if (disposed) return
    const t = Math.min(1, (now - start) / duration)
    // easeOutExpo：起步快、收尾稳，读数不会在末尾长时间抖动
    const eased = t === 1 ? 1 : 1 - Math.pow(2, -10 * t)
    current.value = from + (to - from) * eased
    if (t < 1) frame = requestAnimationFrame(step)
  }
  frame = requestAnimationFrame(step)
}

watch(
  [() => props.value, () => props.instant, () => prefs.value.motion, reducedMotion, pageHidden],
  ([value]) => run(value),
  { immediate: true, flush: 'sync' }
)

const onMotionChange = (event: MediaQueryListEvent) => { reducedMotion.value = event.matches }
const onVisibilityChange = () => { pageHidden.value = document.hidden }

onMounted(() => {
  media?.addEventListener('change', onMotionChange)
  document.addEventListener('visibilitychange', onVisibilityChange)
})

onBeforeUnmount(() => {
  disposed = true
  cancelFrame()
  media?.removeEventListener('change', onMotionChange)
  document.removeEventListener('visibilitychange', onVisibilityChange)
})

const display = computed(() =>
  current.value.toLocaleString('zh-CN', {
    minimumFractionDigits: props.precision,
    maximumFractionDigits: props.precision
  })
)
</script>
