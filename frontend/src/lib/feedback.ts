/**
 * 全站反馈层：轻提示与确认框的唯一入口。
 *
 * 取代 element-plus 的 ElMessage / ElMessageBox：
 * - `notify.*` 走 vue-sonner，无需在组件里引入任何东西
 * - `confirm()` 返回 Promise<boolean>，由 App.vue 挂载的 ConfirmHost 渲染
 *   （EP 的 ElMessageBox 用 reject 表示取消，这里统一用 false，调用方不必再 try/catch）
 */
import { reactive } from 'vue'
import { toast } from 'vue-sonner'

export const notify = {
  success: (message: string, description?: string) => toast.success(message, { description }),
  error: (message: string, description?: string) => toast.error(message, { description }),
  warning: (message: string, description?: string) => toast.warning(message, { description }),
  info: (message: string, description?: string) => toast.info(message, { description }),
  loading: (message: string) => toast.loading(message),
  dismiss: (id?: string | number) => toast.dismiss(id)
}

export interface ConfirmOptions {
  title?: string
  description?: string
  confirmText?: string
  cancelText?: string
  /** 破坏性操作用红色确认按钮 */
  danger?: boolean
}

interface ConfirmState extends Required<ConfirmOptions> {
  open: boolean
  resolve: ((ok: boolean) => void) | null
}

export const confirmState = reactive<ConfirmState>({
  open: false,
  title: '确认操作',
  description: '',
  confirmText: '确定',
  cancelText: '取消',
  danger: false,
  resolve: null
})

export const confirm = (
  description: string,
  options: ConfirmOptions = {}
): Promise<boolean> => {
  // 上一个确认框还没关就再次调用时，先把旧的判为取消，避免 promise 悬空
  confirmState.resolve?.(false)

  confirmState.title = options.title ?? '确认操作'
  confirmState.description = options.description ?? description
  confirmState.confirmText = options.confirmText ?? '确定'
  confirmState.cancelText = options.cancelText ?? '取消'
  confirmState.danger = options.danger ?? false
  confirmState.open = true

  return new Promise<boolean>(resolve => {
    confirmState.resolve = resolve
  })
}

/** 破坏性确认的快捷写法 */
export const confirmDanger = (description: string, options: ConfirmOptions = {}) =>
  confirm(description, { danger: true, confirmText: '删除', ...options })

export const settleConfirm = (ok: boolean): void => {
  confirmState.open = false
  const resolve = confirmState.resolve
  confirmState.resolve = null
  resolve?.(ok)
}

/* ---------- 单输入框询问框 ---------- */

export interface PromptOptions {
  title?: string
  description?: string
  placeholder?: string
  defaultValue?: string
  confirmText?: string
  cancelText?: string
  /** 校验失败返回错误文案，通过返回空字符串 */
  validate?: (value: string) => string
}

interface PromptState {
  open: boolean
  title: string
  description: string
  placeholder: string
  value: string
  error: string
  confirmText: string
  cancelText: string
  validate: ((value: string) => string) | null
  resolve: ((value: string | null) => void) | null
}

export const promptState = reactive<PromptState>({
  open: false,
  title: '',
  description: '',
  placeholder: '',
  value: '',
  error: '',
  confirmText: '确定',
  cancelText: '取消',
  validate: null,
  resolve: null
})

/** 取消返回 null，确认返回输入值 */
export const prompt = (options: PromptOptions = {}): Promise<string | null> => {
  promptState.resolve?.(null)

  promptState.title = options.title ?? '请输入'
  promptState.description = options.description ?? ''
  promptState.placeholder = options.placeholder ?? ''
  promptState.value = options.defaultValue ?? ''
  promptState.error = ''
  promptState.confirmText = options.confirmText ?? '确定'
  promptState.cancelText = options.cancelText ?? '取消'
  promptState.validate = options.validate ?? null
  promptState.open = true

  return new Promise<string | null>(resolve => {
    promptState.resolve = resolve
  })
}

export const settlePrompt = (value: string | null): void => {
  if (value !== null && promptState.validate) {
    const error = promptState.validate(value)
    if (error) {
      promptState.error = error
      return
    }
  }
  promptState.open = false
  const resolve = promptState.resolve
  promptState.resolve = null
  resolve?.(value)
}
