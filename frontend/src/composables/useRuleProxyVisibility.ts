import { computed, ref, type Ref } from 'vue'

/** 普通代理地址便于核对；包含认证信息的地址默认遮挡，手动显隐优先。 */
export function useRuleProxyVisibility(address: Ref<string>) {
  const override = ref<boolean>()
  return computed({
    get() {
      return override.value ?? !/^https?:\/\/[^/?#]*@/i.test(address.value.trim())
    },
    set(visible: boolean) {
      override.value = visible
    },
  })
}
