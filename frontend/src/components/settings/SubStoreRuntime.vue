<template>
  <div :aria-busy="saving || undefined">
    <template v-if="dependency.lifecycle?.managed">
      <Select :model-value="mode" :disabled="disabled" @update:model-value="saveMode">
        <SelectTrigger size="sm" class="w-32 max-w-full bg-background/40 text-[13px] shadow-none" aria-label="Sub-Store 运行方式" :aria-describedby="`${id}-description`">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="on_demand">按需启动</SelectItem>
          <SelectItem value="always">常驻运行</SelectItem>
        </SelectContent>
      </Select>
      <p :id="`${id}-description`" class="m-0 mt-0.5 text-[12px] leading-[18px] text-muted-foreground" aria-live="polite">
        {{ saving ? '正在保存…' : mode === 'on_demand' ? '空闲 5 分钟后休眠' : '持续运行，随时就绪' }}
      </p>
    </template>
    <span v-else class="inline-flex min-h-8 items-center text-[12px] text-muted-foreground">由外部服务管理</span>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, useId, watch } from 'vue'
import { isAxiosError } from 'axios'
import { dependenciesApi, type DependencyStatus } from '@/api'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { notify } from '@/lib/feedback'

const props = defineProps<{ dependency: DependencyStatus }>()
const emit = defineEmits<{ (e: 'changed'): void }>()
const id = useId()
type Mode = 'on_demand' | 'always'
const mode = ref<Mode>('on_demand')
const saving = ref(false)
const disabled = computed(() => saving.value || props.dependency.update.state === 'running' || props.dependency.lifecycle?.state === 'updating')
watch(() => props.dependency.lifecycle?.mode, value => { mode.value = value === 'always' ? 'always' : 'on_demand' }, { immediate: true })
async function saveMode(value: unknown) {
  if ((value !== 'on_demand' && value !== 'always') || disabled.value || value === mode.value) return
  saving.value = true
  try {
    await dependenciesApi.setSubStoreMode(value)
    mode.value = value
    notify.success(value === 'on_demand' ? '已启用按需启动' : '已启用常驻运行')
    emit('changed')
  } catch (error) {
    notify.error(isAxiosError(error) ? error.response?.data?.message || '运行模式保存失败' : '运行模式保存失败')
  } finally {
    saving.value = false
  }
}
</script>
