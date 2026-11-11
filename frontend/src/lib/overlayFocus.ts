// 会弹出软键盘的输入控件
const TEXT_ENTRY = [
  'input:not([type="checkbox"]):not([type="radio"]):not([type="button"]):not([type="submit"]):not([type="reset"])'
    + ':not([type="range"]):not([type="color"]):not([type="file"]):not([type="image"])',
  'textarea',
  '[contenteditable=""]',
  '[contenteditable="true"]'
].join(', ')

/** 触屏设备：打开弹层时自动聚焦输入框会弹出软键盘，占掉半个屏幕 */
export function isTouchPointer(view: Window | null = typeof window === 'undefined' ? null : window) {
  return !!view?.matchMedia?.('(pointer: coarse)').matches
}

/** 把焦点放在弹层本身：仍在焦点陷阱里，读屏和 Tab 照常可用，但不弹键盘 */
function focusOverlayItself(container: HTMLElement, event: Event) {
  // Reka Popover dispatches from the positioning wrapper; only its inner dialog
  // has tabindex=-1. Keep fallback focus on that semantic content, not the wrapper.
  const focusTarget = container.matches('[data-reka-popper-content-wrapper]')
    ? container.querySelector<HTMLElement>(':scope > [role="dialog"][tabindex="-1"]') ?? container
    : container
  focusTarget.focus({ preventScroll: true })
  if (container.ownerDocument.activeElement === focusTarget) event.preventDefault()
}

/**
 * Keep automatic overlay focus on a control rather than opening label help.
 * 触屏设备上，第一个可聚焦项若是文字输入框（且没有显式 autofocus），改为聚焦弹层本身，
 * 等用户点输入框时再弹键盘。
 */
export function focusOverlayControl(event: Event) {
  if (event.defaultPrevented || !(event.target instanceof HTMLElement)) return

  const container = event.target
  const document = container.ownerDocument
  const view = document.defaultView
  if (!view) return

  const touch = isTouchPointer(view)
  let skippedHelp = false
  for (const candidate of container.querySelectorAll<HTMLElement>('*')) {
    if (candidate.tabIndex < 0 || typeof candidate.focus !== 'function') continue
    if (candidate.matches(':disabled, [aria-disabled="true"], input[type="hidden"]')) continue
    if (candidate.closest('[hidden], [inert]')) continue
    let visible = true
    for (let parent: HTMLElement | null = candidate; parent && container.contains(parent); parent = parent.parentElement) {
      const style = view.getComputedStyle(parent)
      if (style.display === 'none' || style.visibility === 'hidden' || style.visibility === 'collapse') {
        visible = false
        break
      }
    }
    if (!visible) continue

    if (touch && candidate.matches(TEXT_ENTRY) && !candidate.hasAttribute('autofocus')) {
      focusOverlayItself(container, event)
      return
    }

    const isHelp = candidate.matches('[data-field-help-trigger]')
    // Leave normal autofocus untouched, without scanning the rest of a large form.
    if (!skippedHelp && !isHelp) return
    if (isHelp) {
      skippedHelp = true
      continue
    }

    candidate.focus({ preventScroll: true })
    const focused = document.activeElement
    // Roving groups synchronously delegate focus from their container to an item.
    if (!(focused instanceof HTMLElement) || !candidate.contains(focused) || focused.closest('[data-field-help-trigger]')) continue
    if (candidate.tagName === 'INPUT') (candidate as HTMLInputElement).select()
    event.preventDefault()
    return
  }

  if (!skippedHelp) return
  focusOverlayItself(container, event)
}
