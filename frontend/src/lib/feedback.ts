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
