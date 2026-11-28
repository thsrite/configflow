<template>
  <tbody ref="anchor" aria-hidden="true" />
  <template v-for="(row, i) in rows" :key="row.key.toString()">
    <tbody v-if="row.start > previousEnd(i)" aria-hidden="true">
      <tr :style="{ height: `${row.start - previousEnd(i)}px`, padding: 0 }" />
    </tbody>
    <tbody
      :ref="measureRow"
      :data-index="row.index"
      :class="row.index === items.length - 1 && '[&_tr:last-child]:border-0'"
      @focusin="focused = row.index"
      @focusout="focused = null"
    >
      <slot :item="items[row.index]" :index="row.index" />
    </tbody>
  </template>
  <tbody v-if="remaining > 0" aria-hidden="true">
    <tr :style="{ height: `${remaining}px`, padding: 0 }" />
  </tbody>
</template>

<script setup lang="ts" generic="T">
import { computed, onMounted, ref, watch, type ComponentPublicInstance } from 'vue'
import { useResizeObserver } from '@vueuse/core'
import { defaultRangeExtractor, measureElement, useWindowVirtualizer, type Range, type Virtualizer } from '@tanstack/vue-virtual'

const props = defineProps<{
  items: T[]
  itemKey: (item: T) => string
  estimatedHeight: number
}>()
defineSlots<{ default(props: { item: T; index: number }): any }>()

const anchor = ref<HTMLTableSectionElement>()
const scrollMargin = ref(0)
const focused = ref<number | null>(null)
// 保留正在操作的行和相邻的 Tab 目标，即使用户同时用滚轮移走视口。
const indices = computed(() => {
  const index = focused.value
  return (range: Range) => {
    const visible = defaultRangeExtractor(range)
    if (index === null || index >= range.count) return visible
    const neighbors = [index - 1, index, index + 1].filter(i => i >= 0 && i < range.count)
    return [...new Set([...visible, ...neighbors])].sort((a, b) => a - b)
  }
})

const virtualizer = useWindowVirtualizer(computed(() => {
  const { items, itemKey, estimatedHeight } = props
  return {
    count: anchor.value ? items.length : 0,
    getItemKey: (index: number) => itemKey(items[index]),
    estimateSize: () => estimatedHeight,
    // 等浏览器统一布局后由 ResizeObserver 回报实际行高，避免挂载每一行都同步布局。
    measureElement: (el: HTMLTableSectionElement, entry: ResizeObserverEntry | undefined, instance: Virtualizer<Window, HTMLTableSectionElement>) =>
      entry ? measureElement(el, entry, instance) : instance.itemSizeCache.get(itemKey(items[instance.indexFromElement(el)])) ?? estimatedHeight,
    scrollMargin: scrollMargin.value,
    rangeExtractor: indices.value,
    overscan: 3,
  }
}))
const rows = computed(() => virtualizer.value.getVirtualItems())
const previousEnd = (index: number) => index ? rows.value[index - 1].end : scrollMargin.value
const remaining = computed(() => virtualizer.value.getTotalSize() + scrollMargin.value - previousEnd(rows.value.length))
const measureRow = (element: Element | ComponentPublicInstance | null) =>
  virtualizer.value.measureElement(element as HTMLTableSectionElement | null)

// 上方工具栏与配置区可展开，滚动坐标始终以表格正文为起点。
const measureOffset = () => {
  if (anchor.value) scrollMargin.value = window.scrollY + anchor.value.getBoundingClientRect().top
}
onMounted(measureOffset)
useResizeObserver(() => document.body, measureOffset)
watch(() => [props.items, props.estimatedHeight], () => virtualizer.value.measure())
</script>
