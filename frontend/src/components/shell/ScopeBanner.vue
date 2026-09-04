<template>
  <div :class="cn('mb-4 flex items-start gap-2.5 rounded-lg border px-3.5 py-3 text-[12.5px] leading-snug', tone.box)" role="note">
    <component :is="tone.icon" :class="cn('mt-px size-3.5 shrink-0', tone.icon_)" :stroke-width="2" aria-hidden="true" />
    <div>
      <b class="font-semibold text-foreground">{{ title }}</b>
      <span v-if="description" class="mt-px block text-muted-foreground">{{ description }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { IdCard, Layers, Server } from '@lucide/vue'
import { cn } from '@/lib/utils'

const props = defineProps<{
  /**
   * resource = 订阅/节点/规则集等资源（按配置空间隔离）
   * profile  = 策略与生成配置（按配置空间隔离）
   * system   = 真正跨配置空间共有的数据，目前只有 Agent 与配置空间本身
   */
  scope: 'resource' | 'profile' | 'system'
  profileName?: string
  description?: string
}>()

/* 作用域用图标 + 语义软底共同表意，与侧栏分组色标保持一致 */
const TONES = {
  resource: { icon: Layers, icon_: 'text-info-accent', box: 'border-info-accent/30 bg-info-soft' },
  profile: { icon: IdCard, icon_: 'text-primary-accent', box: 'border-primary-accent/30 bg-primary-soft' },
  system: { icon: Server, icon_: 'text-success-accent', box: 'border-success-accent/30 bg-success-soft' }
} as const

const tone = computed(() => TONES[props.scope])

const title = computed(() => {
  if (props.scope === 'system') return '系统级 · 所有配置空间共有'
  if (props.scope === 'resource') return `资源 · 当前配置「${props.profileName || '未选择'}」`
  return `当前配置 · ${props.profileName || '未选择'}`
})
</script>
