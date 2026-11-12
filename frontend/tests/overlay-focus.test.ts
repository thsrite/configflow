import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { defineComponent, ref, type Component } from 'vue'
import { DialogRoot, DialogTitle, DialogTrigger, PopoverRoot, PopoverTrigger } from 'reka-ui'
import FieldHelp from '@/components/common/FieldHelp.vue'
import DialogContent from '@/components/ui/dialog/DialogContent.vue'
import DialogScrollContent from '@/components/ui/dialog/DialogScrollContent.vue'
import SheetContent from '@/components/ui/sheet/SheetContent.vue'
import PopoverContent from '@/components/ui/popover/PopoverContent.vue'

type Overlay = { name: string; content: Component; popover?: boolean }
const overlays: Overlay[] = [
  { name: 'DialogContent', content: DialogContent },
  { name: 'DialogScrollContent', content: DialogScrollContent },
  { name: 'SheetContent', content: SheetContent },
  { name: 'PopoverContent', content: PopoverContent, popover: true },
]
const wrappers: VueWrapper[] = []
const help = '<FieldHelp label="选项" text="按需查看的说明" />'
const input = '<input id="real-control" value="current value" />'
const element = <T extends HTMLElement = HTMLElement>(id: string) => document.getElementById(id) as T
const overlayTrigger = () => document.querySelector<HTMLButtonElement>('[data-testid="open-overlay"]')!
const overlayContainer = () => document.querySelector<HTMLElement>('[data-testid="focus-overlay"]')!
const helpButton = () => document.querySelector<HTMLButtonElement>('[data-field-help-trigger]')!
const visibleHelp = () => document.querySelector('[data-field-help-content]')

beforeEach(() => {
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} })
})

afterEach(async () => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  // Reka restores the previous focus in a timer when a scope unmounts.
  await new Promise(resolve => setTimeout(resolve, 0))
  await flushPromises()
  document.body.innerHTML = ''
  vi.unstubAllGlobals()
})

async function openOverlay(overlay: Overlay, body: string, onFocus: (event: Event) => void = () => {}) {
  const wrapper = mount(defineComponent({
    components: {
      Root: overlay.popover ? PopoverRoot : DialogRoot,
      Trigger: overlay.popover ? PopoverTrigger : DialogTrigger,
      Content: overlay.content, DialogTitle, FieldHelp,
    },
    setup: () => ({ open: ref(false), onFocus }),
    template: `<Root v-model:open="open">
      <Trigger as-child><button type="button" data-testid="open-overlay">打开设置</button></Trigger>
      <Content data-testid="focus-overlay" :show-close-button="false" :aria-describedby="undefined" @open-auto-focus="onFocus">
        ${overlay.popover ? '<h2>设置</h2>' : '<DialogTitle>设置</DialogTitle>'}
        ${body}
      </Content>
    </Root>`,
  }), { attachTo: document.body })
  wrappers.push(wrapper)
  await flushPromises()
  await vi.waitFor(() => expect(overlayTrigger()).toBeTruthy())
  const trigger = overlayTrigger()
  trigger.focus()
  trigger.click()
  await flushPromises()
  await vi.waitFor(() => expect(overlayContainer()).toBeTruthy())
  return wrapper
}

describe.each(overlays)('$name automatic focus', overlay => {
  it('starts on the real field without opening the preceding help', async () => {
    const onFocus = vi.fn()
    await openOverlay(overlay, help + input, onFocus)
    await vi.waitFor(() => expect(document.activeElement).toBe(element('real-control')))
    expect(visibleHelp()).toBeNull()
    expect(onFocus).toHaveBeenCalledTimes(1)
    expect(helpButton().tabIndex).toBe(0)
  })

  it('skips hidden, disabled, inert and non-tabbable fields after the help', async () => {
    await openOverlay(overlay, `${help}
      <button disabled>暂不可用</button>
      <fieldset disabled><input id="disabled-fieldset" /></fieldset>
      <div hidden><input id="hidden-ancestor" /></div>
      <div style="display:none"><input id="display-none" /></div>
      <div style="visibility:hidden"><input id="invisible" /></div>
      <div inert><input id="inert-field" /></div>
      <input tabindex="-1" id="negative-tabindex" />
      <input type="hidden" id="hidden-input" />
      ${input}`)
    await vi.waitFor(() => expect(document.activeElement).toBe(element('real-control')))
    expect(visibleHelp()).toBeNull()
  })

  it.each(['button', 'input'])('keeps focus delegated by a composite control to its inner %s', async kind => {
    const child = kind === 'button'
      ? '<button type="button" id="delegated-control" tabindex="-1">当前选项</button>'
      : '<input id="delegated-control" tabindex="-1" value="当前选项" />'
    await openOverlay(overlay, `${help}
      <div tabindex="0" @focus="$event.currentTarget.querySelector('#delegated-control').focus()">
        ${child}
      </div>
      <input id="later-control" @focus="$event.currentTarget.setAttribute('data-was-focused', 'true')" />`)
    await vi.waitFor(() => expect(document.activeElement).toBe(element('delegated-control')))
    expect(element('delegated-control').tabIndex).toBe(-1)
    expect(element('later-control').hasAttribute('data-was-focused')).toBe(false)
    expect(visibleHelp()).toBeNull()
  })

  it('honors the page-owned automatic focus target', async () => {
    const onFocus = vi.fn((event: Event) => {
      event.preventDefault()
      element('preferred-control').focus({ preventScroll: true })
    })
    await openOverlay(overlay, help + input + '<button id="preferred-control">自定义入口</button>', onFocus)
    await vi.waitFor(() => expect(document.activeElement).toBe(element('preferred-control')))
    expect(onFocus).toHaveBeenCalledTimes(1)
    expect(visibleHelp()).toBeNull()
  })

  it('does not assign focus when the page opts out of automatic focus', async () => {
    const onFocus = vi.fn((event: Event) => event.preventDefault())
    await openOverlay(overlay, help + input, onFocus)
    await vi.waitFor(() => expect(onFocus).toHaveBeenCalledTimes(1))
    expect(document.activeElement).toBe(overlayTrigger())
    expect(visibleHelp()).toBeNull()
  })

  it('keeps the normal first-field focus and text selection when help is not first', async () => {
    await openOverlay(overlay, input + help)
    const field = element<HTMLInputElement>('real-control')
    await vi.waitFor(() => expect(document.activeElement).toBe(field))
    expect(field.selectionStart).toBe(0)
    expect(field.selectionEnd).toBe(field.value.length)
    expect(visibleHelp()).toBeNull()
  })

  it('keeps help keyboard-focusable and allows opening it after the initial focus', async () => {
    const wrapper = await openOverlay(overlay, help + input)
    await vi.waitFor(() => expect(document.activeElement).toBe(element('real-control')))
    const button = helpButton()
    expect(button.tabIndex).toBe(0)
    // jsdom does not implement Tab navigation; focus the still-tabbable button.
    button.focus()
    await flushPromises()
    expect(visibleHelp()?.textContent).toContain('按需查看的说明')
    button.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    await flushPromises()
    expect(visibleHelp()).toBeNull()
    expect((wrapper.vm as any).open).toBe(true)
    // A browser activates a focused button by generating click for Enter.
    button.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }))
    button.click()
    await flushPromises()
    expect(visibleHelp()).not.toBeNull()
    expect((wrapper.vm as any).open).toBe(true)
  })
})

describe.each(overlays.filter(item => item.name === 'DialogContent' || item.name === 'PopoverContent'))('$name help-only content', overlay => {
  it('focuses the overlay itself without removing help from the tab order', async () => {
    await openOverlay(overlay, help)
    await vi.waitFor(() => expect(document.activeElement).toBe(overlayContainer()))
    expect(visibleHelp()).toBeNull()
    expect(helpButton().tabIndex).toBe(0)
    helpButton().focus()
    await flushPromises()
    expect(visibleHelp()).not.toBeNull()
  })
})

describe.each(overlays)('$name on a touch screen', overlay => {
  const coarse = () => vi.stubGlobal('matchMedia', vi.fn((query: string) => ({
    matches: query.includes('pointer: coarse'), media: query, onchange: null,
    addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false
  })))

  it('does not focus a text field on open, so the soft keyboard stays down', async () => {
    coarse()
    await openOverlay(overlay, input + '<button id="later-button">按钮</button>')
    await vi.waitFor(() => expect(document.activeElement).not.toBe(overlayTrigger()))
    expect(document.activeElement).not.toBe(element('real-control'))
    // 焦点仍在弹层里
    expect(overlayContainer().contains(document.activeElement) || document.activeElement?.contains(overlayContainer())).toBe(true)
  })

  it('still focuses a field explicitly marked autofocus', async () => {
    coarse()
    await openOverlay(overlay, '<input id="wanted" autofocus />')
    await vi.waitFor(() => expect(document.activeElement).toBe(element('wanted')))
  })

  it('still focuses a non-text control first, which does not raise a keyboard', async () => {
    coarse()
    await openOverlay(overlay, '<button id="first-button">选项</button>' + input)
    await vi.waitFor(() => expect(document.activeElement).toBe(element('first-button')))
  })
})
