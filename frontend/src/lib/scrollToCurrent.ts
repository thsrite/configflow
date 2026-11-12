import { nextTick, watch, type Ref } from 'vue'

/**
 * 下拉列表打开时，把当前选中的选项（带 `data-current` 的项）滚到列表中间。
 * 只调整列表自身的 scrollTop，不用 scrollIntoView，以免带动背后的页面或弹窗一起滚动。
 */
export function useScrollToCurrent(open: Ref<boolean>) {
  watch(open, async value => {
    if (!value) return
    await nextTick()
    requestAnimationFrame(() => {
      const item = document.querySelector<HTMLElement>('[data-slot="command-list"] [data-current]')
      const list = item?.closest<HTMLElement>('[data-slot="command-list"]')
      if (!item || !list) return
      const offset = item.getBoundingClientRect().top - list.getBoundingClientRect().top
      list.scrollTop += offset - (list.clientHeight - item.offsetHeight) / 2
    })
  })
}
