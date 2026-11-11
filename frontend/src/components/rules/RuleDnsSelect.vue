<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Check, ChevronDown, ChevronsUpDown, Loader2 } from '@lucide/vue'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from '@/components/ui/command'
import { useScrollToCurrent } from '@/lib/scrollToCurrent'
import FieldHelp from '@/components/common/FieldHelp.vue'
import { cn } from '@/lib/utils'
import { BLOCK_ROUTES, CHOICE_LABELS, DNS_COPY, ROUTE_LABELS, choiceLabel, forwardLabel, upstreamOptions } from '@/lib/ruleDns'
import type { RuleDnsUpstream, RuleDnsValue } from '@/types'

/**
 * 规则 DNS 处理的分组选择器：表单字段（field）与列表单元格（cell）共用。
 * 每个处理方式带分层说明；打开说明不会选中该项。
 */
const props = withDefaults(defineProps<{
  modelValue: RuleDnsValue
  /** 读屏名称，例如「DNS（OxiDNS）」或「example.com 的 DNS：黑洞，点击修改」 */
  label: string
  upstreams?: RuleDnsUpstream[]
  /** 当前固定的上游名称；已不在上游列表时仍作为只读项显示 */
  upstreamLabel?: string
  /** 当前策略对应的跟随路由，用于「按当前配置：代理 DNS」 */
  policyRoute?: 'direct' | 'proxy' | null
  defaultForward?: string
  variant?: 'field' | 'cell'
  disabled?: boolean
  loading?: boolean
  id?: string
  triggerClass?: string
  contentClass?: string
}>(), { variant: 'field', upstreams: () => [] })

const emit = defineEmits<{ 'update:modelValue': [value: RuleDnsValue] }>()
const open = ref(false)
// 打开时滚到当前选中的选项
useScrollToCurrent(open)

watch(() => [props.disabled, props.loading], () => {
  if (props.disabled || props.loading) open.value = false
})

const optionClass = 'min-h-11 cursor-pointer gap-2 px-2 py-1.5 md:min-h-9 [@media(pointer:coarse)]:min-h-11'
const defaultOptions = computed(() => [
  { value: 'follow' as const, label: CHOICE_LABELS.follow, detail: `按当前配置：${props.policyRoute ? ROUTE_LABELS[props.policyRoute] : '默认上游'}`, ...DNS_COPY.follow },
  { value: 'default' as const, label: CHOICE_LABELS.default, detail: `未匹配时：${forwardLabel(props.defaultForward)}`, ...DNS_COPY.default }
])
const resolveOptions = computed(() => (['direct', 'proxy'] as const).map(value => ({ value, label: ROUTE_LABELS[value], ...DNS_COPY[value] })))
const blockOptions = computed(() => BLOCK_ROUTES.map(value => ({ value, label: ROUTE_LABELS[value], ...DNS_COPY[value] })))
const pinned = computed(() => upstreamOptions(props.upstreams, { value: props.modelValue, label: props.upstreamLabel }))
const currentUpstream = computed(() => pinned.value.find(option => option.value === props.modelValue)?.label || props.upstreamLabel)
const triggerText = computed(() => choiceLabel(props.modelValue, { policyRoute: props.policyRoute, upstreamLabel: currentUpstream.value }))

function choose(value: RuleDnsValue, disabled = false) {
  if (!open.value || disabled || props.disabled) return
  open.value = false
  // 重选当前值也是一次明确选择（例如新建时确认「跟随策略」），由使用方决定是否忽略。
  emit('update:modelValue', value)
}
</script>

<template>
  <Popover v-model:open="open">
    <PopoverTrigger as-child>
      <button
        v-if="variant === 'field'"
        :id="id"
        type="button"
        :disabled="disabled || loading"
        :aria-label="`${label}：${triggerText}`"
        :class="cn('flex h-9 w-full min-w-0 items-center gap-2 rounded-md border border-input bg-background/50 px-3 text-left text-sm shadow-xs outline-none transition-[color,box-shadow] hover:border-border-strong focus-visible:ring-2 focus-visible:ring-primary-accent disabled:cursor-not-allowed disabled:opacity-60 max-md:min-h-11', triggerClass)"
      >
        <Loader2 v-if="loading" class="size-3.5 shrink-0 animate-spin text-primary-accent" aria-hidden="true" />
        <span class="min-w-0 flex-1 truncate">{{ triggerText }}</span>
        <ChevronsUpDown class="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
      </button>
      <button
        v-else
        type="button"
        :disabled="disabled || loading"
        :aria-label="label"
        :title="label"
        :class="cn('rule-dns-cell-trigger relative inline-flex h-[30px] w-full max-w-full min-w-0 items-center gap-1.5 rounded-md bg-transparent px-2 text-left text-[12px] transition-colors hover:bg-accent/60 outline-none focus-visible:ring-2 focus-visible:ring-primary-accent disabled:cursor-default', triggerClass)"
      >
        <Loader2 v-if="loading" class="size-3.5 shrink-0 animate-spin text-primary-accent" aria-hidden="true" />
        <slot />
        <ChevronDown v-if="!disabled && !loading" class="ml-auto size-3 shrink-0 text-muted-foreground opacity-60" aria-hidden="true" />
      </button>
    </PopoverTrigger>
    <PopoverContent
      align="start"
      :collision-padding="12"
      :class="cn('w-[24rem] max-w-[min(calc(100vw-1.5rem),var(--reka-popover-content-available-width))] overflow-hidden p-0 shadow-overlay', contentClass)"
      :aria-label="`${label} 选择`"
    >
      <Command v-if="open" class="[&_[data-slot=command-input-wrapper]]:min-h-11 md:[&_[data-slot=command-input-wrapper]]:min-h-9 [@media(pointer:coarse)]:[&_[data-slot=command-input-wrapper]]:min-h-11">
        <CommandInput :aria-label="`${label} 搜索`" placeholder="搜索 DNS 处理或上游" />
        <CommandList :aria-label="`${label} 可选处理`" class="max-h-[min(24rem,calc(var(--reka-popover-content-available-height)-3rem))] p-1">
          <CommandEmpty class="px-3 py-6 text-muted-foreground">没有匹配的 DNS 处理</CommandEmpty>
          <CommandGroup heading="跟随与默认">
            <CommandItem v-for="option in defaultOptions" :key="option.value" :value="option.value" :data-current="modelValue === option.value || undefined" :class="optionClass" @select="choose(option.value)">
              <Check class="size-4 shrink-0" :class="modelValue === option.value ? 'text-primary-accent' : 'opacity-0'" aria-hidden="true" />
              <span class="min-w-0 flex-1">
                <span class="block text-sm font-medium">{{ option.label }}</span>
                <span class="block text-xs text-muted-foreground">{{ option.detail }}</span>
                <span class="hidden" aria-hidden="true">{{ option.hint }} {{ option.help }}</span>
              </span>
              <FieldHelp :label="option.label" :text="`${option.hint}\n\n${option.help}`" content-class="z-[80]" />
            </CommandItem>
          </CommandGroup>
          <CommandGroup heading="解析">
            <CommandItem v-for="option in resolveOptions" :key="option.value" :value="option.value" :data-current="modelValue === option.value || undefined" :class="optionClass" @select="choose(option.value)">
              <Check class="size-4 shrink-0" :class="modelValue === option.value ? 'text-primary-accent' : 'opacity-0'" aria-hidden="true" />
              <span class="min-w-0 flex-1">
                <span class="block text-sm font-medium">{{ option.label }}</span>
                <span class="hidden" aria-hidden="true">{{ option.hint }} {{ option.help }}</span>
              </span>
              <FieldHelp :label="option.label" :text="`${option.hint}\n\n${option.help}`" content-class="z-[80]" />
            </CommandItem>
          </CommandGroup>
          <CommandGroup heading="指定上游">
            <CommandItem
              v-for="option in pinned"
              :data-current="modelValue === option.value || undefined"
              :key="option.value"
              :value="option.value"
              :disabled="option.linked"
              :class="optionClass"
              @select="choose(option.value, option.linked)"
            >
              <Check class="size-4 shrink-0" :class="modelValue === option.value ? 'text-primary-accent' : 'opacity-0'" aria-hidden="true" />
              <span class="min-w-0 flex-1">
                <span class="block break-all font-mono text-[12.5px]">{{ option.label }}</span>
                <span v-if="option.linked" class="block text-xs text-warning-accent">已绑定 DNS 联动，请改选直连 DNS</span>
                <span class="hidden" aria-hidden="true">指定上游</span>
              </span>
            </CommandItem>
            <p v-if="!pinned.length" class="m-0 px-2 py-1.5 text-xs text-muted-foreground">没有可固定的上游</p>
          </CommandGroup>
          <CommandGroup heading="拦截">
            <CommandItem v-for="option in blockOptions" :key="option.value" :value="option.value" :data-current="modelValue === option.value || undefined" :class="optionClass" @select="choose(option.value)">
              <Check class="size-4 shrink-0" :class="modelValue === option.value ? 'text-primary-accent' : 'opacity-0'" aria-hidden="true" />
              <span class="min-w-0 flex-1">
                <span class="block text-sm font-medium">{{ option.label }}</span>
                <span class="block text-xs text-muted-foreground">{{ option.hint }}</span>
                <span class="hidden" aria-hidden="true">{{ option.help }}</span>
              </span>
              <FieldHelp :label="option.label" :text="`${option.hint}\n\n${option.help}`" content-class="z-[80]" />
            </CommandItem>
          </CommandGroup>
        </CommandList>
      </Command>
    </PopoverContent>
  </Popover>
</template>

<style scoped>
/* 视觉 30px，上下透明区域补足 44px 点击区 */
.rule-dns-cell-trigger::before {
  content: '';
  position: absolute;
  inset: calc((30px - var(--cf-touch, 44px)) / 2) 0;
}
</style>
