import { computed, nextTick, onBeforeUnmount, ref, type Ref } from 'vue'
import Sortable from 'sortablejs'

export interface ReorderOptions<T> {
  /** 被排序的列表；进入排序模式时会做快照，取消时还原 */
  items: Ref<T[]>
  /** 承载可拖动子元素的容器 */
  container: Ref<HTMLElement | null | undefined>
  /** 持久化新顺序；抛错即视为失败并回滚 */
  persist: (items: T[]) => Promise<unknown>
  /** 条目展示名，用于无障碍播报 */
  labelOf: (item: T) => string
}

/**
 * 六类可排序资源共用的排序交互。
 *
 * 显式排序模式：进入后才可拖动，避免与页面滚动、行点击争抢手势；
 * 桌面鼠标拖动、移动端长按拖动与键盘方向键三种输入产生同一结果。
 */
export const useReorder = <T>(options: ReorderOptions<T>) => {
  const { items, container, persist, labelOf } = options

  const active = ref(false)
  const saving = ref(false)
  /** 键盘抓取中的条目索引，null 表示未抓取 */
  const grabbedIndex = ref<number | null>(null)
  const announcement = ref('')

  let snapshot: T[] = []
  let sortable: Sortable | null = null

  const total = computed(() => items.value.length)

  const announce = (message: string): void => {
    announcement.value = message
  }

  const destroySortable = (): void => {
    sortable?.destroy()
    sortable = null
  }

  const createSortable = (): void => {
    if (!container.value || sortable) return
    sortable = Sortable.create(container.value, {
      animation: 150,
      handle: '[data-reorder-handle]',
      draggable: '[data-reorder-item]',
      ghostClass: 'cf-reorder-ghost',
      chosenClass: 'cf-reorder-chosen',
      dragClass: 'cf-reorder-drag',
      // 移动端需长按才开始拖动，短触仍归页面滚动
      delay: 220,
      delayOnTouchOnly: true,
      touchStartThreshold: 6,
      forceFallback: true,
      fallbackTolerance: 4,
      onEnd: (event: Sortable.SortableEvent) => {
        const { oldIndex, newIndex } = event
        if (oldIndex === undefined || newIndex === undefined || oldIndex === newIndex) return
        // Sortable 已移动 DOM，这里把数据同步成同一顺序
        const next = [...items.value]
        const [moved] = next.splice(oldIndex, 1)
        next.splice(newIndex, 0, moved)
        items.value = next
        announce(`${labelOf(moved)} 移动到第 ${newIndex + 1} 项，共 ${next.length} 项`)
      }
    })
  }

  const enter = (): void => {
    if (active.value) return
    snapshot = [...items.value]
    active.value = true
    grabbedIndex.value = null
    announce('已进入排序模式，拖动手柄或使用上移下移按钮调整顺序')
    nextTick(createSortable)
  }

  const cancel = (): void => {
    if (!active.value) return
    items.value = [...snapshot]
    active.value = false
    grabbedIndex.value = null
    destroySortable()
    announce('已取消排序，顺序已还原')
  }

  const save = async (): Promise<boolean> => {
    if (!active.value || saving.value) return false
    saving.value = true
    const proposed = [...items.value]
    try {
      await persist(proposed)
      snapshot = proposed
      active.value = false
      grabbedIndex.value = null
      destroySortable()
      announce('顺序已保存')
      return true
    } catch (error) {
      // 失败时恢复进入排序模式前的顺序，不留下前端与后端不一致的中间态
      items.value = [...snapshot]
      announce('保存顺序失败，顺序已还原')
      throw error
    } finally {
      saving.value = false
    }
  }

  /** 按钮与键盘共用的移动逻辑 */
  const move = (index: number, delta: number): number => {
    const target = index + delta
    if (target < 0 || target >= items.value.length) return index
    const next = [...items.value]
    const [moved] = next.splice(index, 1)
    next.splice(target, 0, moved)
    items.value = next
    announce(`${labelOf(moved)} 移动到第 ${target + 1} 项，共 ${next.length} 项`)
    return target
  }

  const moveUp = (index: number): void => {
    move(index, -1)
  }

  const moveDown = (index: number): void => {
    move(index, 1)
  }

  /** 手柄获得焦点后：空格抓取/放下，方向键移动，Esc 放弃抓取 */
  const onHandleKeydown = (event: KeyboardEvent, index: number): void => {
    if (!active.value) return

    if (event.key === ' ' || event.key === 'Spacebar') {
      event.preventDefault()
      if (grabbedIndex.value === index) {
        grabbedIndex.value = null
        announce(`${labelOf(items.value[index])} 已放下，位于第 ${index + 1} 项`)
      } else {
        grabbedIndex.value = index
        announce(`已抓取 ${labelOf(items.value[index])}，第 ${index + 1} 项，共 ${total.value} 项，用方向键移动`)
      }
      return
    }

    if (event.key === 'Escape' && grabbedIndex.value !== null) {
      event.preventDefault()
      grabbedIndex.value = null
      announce('已放弃抓取')
      return
    }

    if (grabbedIndex.value !== index) return

    if (event.key === 'ArrowUp' || event.key === 'ArrowDown') {
      event.preventDefault()
      const next = move(index, event.key === 'ArrowUp' ? -1 : 1)
      grabbedIndex.value = next
      // 焦点跟随移动后的条目，否则连续按键会作用到错误的行
      nextTick(() => {
        const handles = container.value?.querySelectorAll<HTMLElement>('[data-reorder-handle]')
        handles?.[next]?.focus()
      })
    }
  }

  const positionLabel = (index: number): string => `第 ${index + 1} 项，共 ${total.value} 项`

  onBeforeUnmount(destroySortable)

  return {
    active,
    saving,
    grabbedIndex,
    announcement,
    total,
    enter,
    cancel,
    save,
    moveUp,
    moveDown,
    onHandleKeydown,
    positionLabel
  }
}
