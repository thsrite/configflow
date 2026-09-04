<template>
  <span class="num tabular-nums">{{ display }}</span>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { COUNT_DURATION } from '@/lib/motion'

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
let frame = 0

const run = (to: number) => {
  cancelAnimationFrame(frame)
  const reduce = window.matchMedia('(prefers-reduced-motion: reduce)').matches
  if (props.instant || reduce || !Number.isFinite(to)) {
    current.value = Number.isFinite(to) ? to : 0
    return
  }
  const from = current.value
  const start = performance.now()
  const duration = COUNT_DURATION * 1000
  const step = (now: number) => {
    const t = Math.min(1, (now - start) / duration)
    // easeOutExpo：起步快、收尾稳，读数不会在末尾长时间抖动
    const eased = t === 1 ? 1 : 1 - Math.pow(2, -10 * t)
    current.value = from + (to - from) * eased
    if (t < 1) frame = requestAnimationFrame(step)
  }
  frame = requestAnimationFrame(step)
}

watch(() => props.value, run, { immediate: true })

const display = computed(() =>
  current.value.toLocaleString('zh-CN', {
    minimumFractionDigits: props.precision,
    maximumFractionDigits: props.precision
  })
)
</script>
