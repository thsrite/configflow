<template>
  <div class="space-y-1" :aria-busy="saving || undefined">
    <div class="flex min-h-11 flex-wrap items-center gap-x-4 gap-y-1">
      <RadioGroup
        v-if="dependency.lifecycle?.managed"
        :model-value="mode"
        orientation="horizontal"
        aria-label="Sub-Store 运行模式"
        :aria-describedby="`${id}-description`"
        class="flex flex-wrap gap-x-5 gap-y-0"
        :disabled="disabled"
        @update:model-value="saveMode"
      >
        <Label
          v-for="option in options"
          :key="option.value"
          :for="`${id}-${option.value}`"
          class="min-h-11 gap-2 text-[13px] transition-colors"
          :class="[
            disabled ? 'cursor-not-allowed opacity-50' : 'cursor-pointer hover:text-foreground',
            mode === option.value ? 'text-primary-accent' : 'text-muted-foreground'
          ]"
        >
          <RadioGroupItem
            :id="`${id}-${option.value}`"
            :value="option.value"
            class="border-border-strong shadow-none data-[state=checked]:border-primary-accent disabled:opacity-100"
          />
          {{ option.label }}
        </Label>
      </RadioGroup>
      <StatusDot
        :tone="status.tone"
        :label="status.label"
        :class="dependency.lifecycle?.managed && 'border-l border-border pl-4'"
      />
    </div>
    <p v-if="dependency.lifecycle?.managed" :id="`${id}-description`" class="m-0 text-[12px] leading-relaxed text-muted-foreground" aria-live="polite">
      {{ saving ? '正在保存…' : mode === 'on_demand' ? '需要时启动，空闲 5 分钟后休眠；首次转换稍需等待。' : '持续运行，转换时无需等待启动。' }}
    </p>
    <p v-if="dependency.lifecycle?.error" class="m-0 text-[12px] text-destructive-accent" role="status">
      {{ dependency.lifecycle.error }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, useId, watch } from 'vue'
import { isAxiosError } from 'axios'
import { dependenciesApi, type DependencyStatus } from '@/api'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import StatusDot from '@/components/common/StatusDot.vue'
import { notify } from '@/lib/feedback'

const props = defineProps<{ dependency: DependencyStatus }>()
const emit = defineEmits<{ (e: 'changed'): void }>()
const id = useId()
type Mode = 'on_demand' | 'always'
const options: Array<{ value: Mode; label: string }> = [
  { value: 'on_demand', label: '按需启动' }, { value: 'always', label: '常驻运行' }
]
const mode = ref<Mode>('on_demand')
const saving = ref(false)
const disabled = computed(() => saving.value || props.dependency.update.state === 'running' || props.dependency.lifecycle?.state === 'updating')
watch(() => props.dependency.lifecycle?.mode, value => { mode.value = value === 'always' ? 'always' : 'on_demand' }, { immediate: true })
const status = computed(() => {
  const state = props.dependency.lifecycle?.state || (props.dependency.running ? 'running' : 'failed')
  return {
    sleeping: { tone: 'muted', label: '休眠中' },
    starting: { tone: 'warning', label: '启动中' },
    updating: { tone: 'warning', label: '更新中' },
    running: { tone: 'success', label: '运行中' },
    failed: { tone: 'danger', label: '无法连接' }
  }[state] as { tone: 'muted' | 'warning' | 'success' | 'danger'; label: string }
})
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
