<template>
  <component :is="table ? TableBody : 'div'" ref="root" :class="{ '[overflow-anchor:none]': !full }">
    <template v-for="view in views" :key="view.key">
      <component v-if="'space' in view" :is="table ? 'tr' : 'div'" aria-hidden="true" class="col-span-full" :style="{ height: view.space + 'px' }" />
      <RenderNode v-else :node="view.node" :index="view.index" :entered="view.entered" />
    </template>
  </component>
</template>

<script setup lang="ts" generic="T">
import { computed, onScopeDispose, ref, watch, withDirectives, type ObjectDirective } from 'vue'
import { unrefElement, useEventListener, useResizeObserver } from '@vueuse/core'
import { defaultRangeExtractor, useWindowVirtualizer, windowScroll, type Range } from '@tanstack/vue-virtual'
import { TableBody } from '@/components/ui/table'

const props = defineProps<{
  items: T[]
  itemKey: (node: T) => string
  full: boolean
  table?: boolean
}>()
type Cell = { node: T; index: number; entered: boolean }
const slots = defineSlots<{
  default(props: Cell & { menuOpen: (open: boolean) => void }): any
}>()
const root = ref<HTMLElement | InstanceType<typeof TableBody> | null>(null)
const container = computed(() => unrefElement(root) as HTMLElement | null)
const columns = ref(1)
const gap = ref(0)
const estimate = ref(61)
const margin = ref(0)
const focusIndex = ref<number | null>(null)
const menuIndex = ref<number | null>(null)
const preparedStart = ref<number | null>(null)
const halfViewport = (range: { startIndex: number; endIndex: number }) => Math.ceil((range.endIndex - range.startIndex + 1) / 2)
const indices = computed(() => {
  const prepared = preparedStart.value
  const pins = [focusIndex.value, menuIndex.value].flatMap(index => index === null ? [] : [Math.floor(index / columns.value)])
  return (range: Range) => {
    const visible = defaultRangeExtractor(range)
    if (prepared !== null) {
      // 当前帧只移动到足以覆盖视口的位置。后续任务再向滚动方向预绘半屏。
      const length = Math.min(range.count, visible.length + halfViewport(range))
      const earliest = Math.max(0, visible[visible.length - 1] + 1 - length)
      const latest = Math.min(visible[0], range.count - length)
      const first = Math.min(latest, Math.max(earliest, prepared))
      visible.length = 0
      for (let i = first; i < first + length; i++) visible.push(i)
    }
    if (!pins.length) return visible
    // 焦点、菜单触发器及相邻 Tab 目标在视口外仍可操作。
    const pinned = pins.flatMap(row => [row - 1, row, row + 1]).filter(row => row >= 0 && row < range.count)
    return [...new Set([...visible, ...pinned])].sort((a, b) => a - b)
  }
})
const virtualizer = useWindowVirtualizer(computed(() => ({
  count: container.value && !props.full ? Math.ceil(props.items.length / columns.value) : 0,
  estimateSize: () => estimate.value,
  scrollMargin: margin.value,
  gap: gap.value,
  overscan: 1,
  rangeExtractor: indices.value,
  // 行高回报可能晚于新的滚动输入，补偿应以浏览器此刻的位置为准。
  scrollToFn: (offset, options, instance) => windowScroll(options.adjustments ? window.scrollY : offset, options, instance),
})))

// 可见项先提交给浏览器，下一项消息再按最新的滚动位置预绘半屏。
const prepare = new MessageChannel()
let queued = false
prepare.port1.onmessage = () => {
  queued = false
  const range = virtualizer.value.range
  if (!range) return
  preparedStart.value = range.startIndex - 1 - (virtualizer.value.scrollDirection === 'backward' ? halfViewport(range) : 0)
}
watch(() => virtualizer.value.range, range => {
  if (range && !queued) {
    queued = true
    prepare.port2.postMessage(null)
  }
})
onScopeDispose(() => {
  prepare.port1.close()
  prepare.port2.close()
})

const readOrigin = () => {
  if (container.value) margin.value = container.value.getBoundingClientRect().top + window.scrollY
}
const width = ref(-1)
const readLayout = () => {
  if (!container.value) return
  const style = getComputedStyle(container.value)
  const grid = style.display === 'grid'
  columns.value = grid ? style.gridTemplateColumns.split(' ').length : 1
  gap.value = grid ? parseFloat(style.rowGap) || 0 : 0
  estimate.value = grid ? 109 : 61
  readOrigin()
}
watch(container, readLayout, { flush: 'post' })
useResizeObserver(container, ([entry]) => {
  if (entry && entry.contentRect.width !== width.value) {
    width.value = entry.contentRect.width
    readLayout()
  }
})
useResizeObserver(() => document.body, readOrigin)
watch(() => props.items, (items, previous) => {
  for (const pin of [focusIndex, menuIndex]) {
    if (pin.value === null) continue
    const id = props.itemKey(previous[pin.value])
    const index = items.findIndex(item => props.itemKey(item) === id)
    pin.value = index < 0 ? null : index
  }
})

let scrolled = false
useEventListener(window, 'scroll', () => { scrolled = true }, { passive: true, once: true })
useEventListener(container, 'focusin', event => {
  const cell = (event.target as Element).closest<HTMLElement>('[data-node-index]')
  focusIndex.value = cell ? Number(cell.dataset.nodeIndex) : null
})
useEventListener(container, 'focusout', () => { focusIndex.value = null })

// 网格里同一行的卡片等高。只在布局完成后读浏览器给出的尺寸，滚动时不查询样式或布局。
const observer = typeof ResizeObserver === 'undefined' ? undefined : new ResizeObserver(entries => {
  for (const entry of entries) {
    const index = Number((entry.target as HTMLElement).dataset.nodeIndex)
    if (!props.full && index % columns.value === 0) {
      const height = entry.borderBoxSize?.[0]?.blockSize ?? (entry.target as HTMLElement).offsetHeight
      virtualizer.value.resizeItem(index / columns.value, height)
    }
  }
})
onScopeDispose(() => observer?.disconnect())
watch([() => props.items, columns, width], () => {
  virtualizer.value.measure()
  if (!observer || props.full) return
  for (const el of container.value?.querySelectorAll<HTMLElement>('[data-node-index]') ?? []) {
    if (Number(el.dataset.nodeIndex) % columns.value === 0) {
      observer.unobserve(el)
      observer.observe(el, { box: 'border-box' })
    }
  }
}, { flush: 'post' })
type RowMeasurement = { index: number; observed: boolean }
const bindRow = (el: HTMLElement, value: RowMeasurement, old?: RowMeasurement | null) => {
  if (old?.index === value.index && old.observed === value.observed) return
  if (old?.observed) observer?.unobserve(el)
  el.dataset.nodeIndex = String(value.index)
  // 复用同一个元素时也重新观察，以新节点的实际高度更新对应行。
  if (value.observed) observer?.observe(el, { box: 'border-box' })
}
const measurement: ObjectDirective<HTMLElement, RowMeasurement> = {
  mounted(el, { value }) { bindRow(el, value) },
  updated(el, { value, oldValue }) { bindRow(el, value, oldValue) },
  unmounted(el) { observer?.unobserve(el) },
}

// 每个可见项单独渲染，列表滚动时只更新绑定了新节点的实例。
const RenderNode = (cell: Cell) => withDirectives(slots.default({
  ...cell,
  menuOpen: (open: boolean) => {
    if (open) menuIndex.value = cell.index
    else if (menuIndex.value === cell.index) menuIndex.value = null
  },
})[0], [[measurement, { index: cell.index, observed: !props.full && cell.index % columns.value === 0 }]])

type Space = { key: string; space: number }
type ItemView = Cell & { key: number; id: string }
const positions = computed<Array<number | Space>>(() => {
  if (props.full) return props.items.map((_, i) => i)
  const result: Array<number | Space> = []
  let end = margin.value
  for (const row of virtualizer.value.getVirtualItems()) {
    const space = row.start - end - (result.length ? 2 * gap.value : gap.value)
    if (space > 0) result.push({ key: 'gap:' + row.index, space })
    const first = row.index * columns.value
    for (let i = first; i < Math.min(first + columns.value, props.items.length); i++) result.push(i)
    end = row.end
  }
  const tail = virtualizer.value.getTotalSize() + margin.value - end - gap.value
  if (tail > 0) result.push({ key: 'tail', space: tail })
  return result
})

let nextKey = 0
let lastItems: T[] | undefined
const views = computed<Array<ItemView | Space>>((previous = []) => {
  const old = new Map<string, ItemView>()
  for (const view of previous) {
    if ('node' in view) old.set(view.id, view)
  }
  const next = positions.value.map(position => {
    if (typeof position !== 'number') return position
    const node = props.items[position]
    const id = props.itemKey(node)
    const retained = old.get(id)
    old.delete(id)
    return { node, index: position, id, retained }
  })
  // 按原 DOM 顺序复用移出视口的实例。切换筛选或排序时按节点身份保留焦点和菜单。
  const free = lastItems === props.items ? old.values() : undefined
  lastItems = props.items
  return next.map(view => {
    if ('space' in view) return view
    const { node, index, id, retained } = view
    return { node, index, id, key: retained?.key ?? free?.next().value?.key ?? nextKey++, entered: retained?.entered ?? scrolled }
  })
})
</script>
