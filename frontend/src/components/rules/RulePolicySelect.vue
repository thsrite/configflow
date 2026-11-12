<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Check, ChevronDown, Loader2 } from '@lucide/vue'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from '@/components/ui/command'
import { useScrollToCurrent } from '@/lib/scrollToCurrent'
import { cn } from '@/lib/utils'

/**
 * 列表与卡片里的策略快速切换：与 DNS 共用轻量文字外观，
 * 常驻淡箭头标明可修改，悬停、聚焦或展开时加深。
 */
const props = defineProps<{
  modelValue: string
  /** 内置策略与策略组名称，顺序即展示顺序 */
  policies: string[]
  /** 读屏名称，例如「example.com 的策略：DIRECT，点击修改」 */
  label: string
  disabled?: boolean
  loading?: boolean
  triggerClass?: string
}>()
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
const open = ref(false)
// 打开时滚到当前选中的选项
useScrollToCurrent(open)

watch(() => [props.disabled, props.loading], () => {
  if (props.disabled || props.loading) open.value = false
})

const BUILTIN = ['DIRECT', 'REJECT', 'REJECT-DROP', 'PASS']
// 当前值不在可选列表里时仍列出：内置策略（如导入的 REJECT-DROP）照常显示，策略组标注已不存在
const missing = computed(() => !!props.modelValue && !props.policies.includes(props.modelValue))
const builtin = computed(() => {
  const list = props.policies.filter(policy => BUILTIN.includes(policy))
  return missing.value && BUILTIN.includes(props.modelValue) ? [...list, props.modelValue] : list
})
const groups = computed(() => {
  const names = props.policies.filter(policy => !BUILTIN.includes(policy))
  return missing.value && !BUILTIN.includes(props.modelValue) ? [props.modelValue, ...names] : names
})
const optionClass = 'min-h-11 cursor-pointer gap-2 px-2 py-1.5 md:min-h-9 [@media(pointer:coarse)]:min-h-11'

function choose(value: string) {
  if (!open.value || props.disabled) return
  open.value = false
  if (value !== props.modelValue) emit('update:modelValue', value)
}
</script>

<template>
  <Popover v-model:open="open">
    <PopoverTrigger as-child>
      <button
        type="button"
        :disabled="disabled || loading"
        :aria-label="label"
        :title="label"
        :class="cn('group/policy rule-policy-trigger relative inline-flex h-[30px] w-full max-w-full min-w-0 items-center gap-1.5 rounded-md px-2 text-left text-[12px] transition-colors hover:bg-accent/60 outline-none focus-visible:ring-2 focus-visible:ring-primary-accent disabled:cursor-default', triggerClass)"
        @click.stop
      >
        <Loader2 v-if="loading" class="size-3.5 shrink-0 animate-spin text-primary-accent" aria-hidden="true" />
        <span class="min-w-0 flex-1 truncate"><slot>{{ modelValue }}</slot></span>
        <ChevronDown
          v-if="!disabled && !loading"
          class="size-3 shrink-0 text-muted-foreground opacity-60 transition-opacity group-hover/policy:opacity-100 group-focus-visible/policy:opacity-100 group-data-[state=open]/policy:opacity-100"
          aria-hidden="true"
        />
      </button>
    </PopoverTrigger>
    <PopoverContent
      align="start"
      :collision-padding="12"
      class="w-[18rem] max-w-[min(calc(100vw-1.5rem),var(--reka-popover-content-available-width))] overflow-hidden p-0 shadow-overlay"
      :aria-label="`${label} 选择`"
    >
      <Command v-if="open" class="[&_[data-slot=command-input-wrapper]]:min-h-11 md:[&_[data-slot=command-input-wrapper]]:min-h-9 [@media(pointer:coarse)]:[&_[data-slot=command-input-wrapper]]:min-h-11">
        <CommandInput :aria-label="`${label} 搜索`" placeholder="搜索策略" />
        <CommandList :aria-label="`${label} 可选策略`" class="max-h-[min(22rem,calc(var(--reka-popover-content-available-height)-3rem))] p-1">
          <CommandEmpty class="px-3 py-6 text-muted-foreground">没有匹配的策略</CommandEmpty>
          <CommandGroup v-if="builtin.length" heading="内置">
            <CommandItem v-for="policy in builtin" :key="policy" :value="policy" :data-current="modelValue === policy || undefined" :class="optionClass" @select="choose(policy)">
              <Check class="size-4 shrink-0" :class="modelValue === policy ? 'text-primary-accent' : 'opacity-0'" aria-hidden="true" />
              <span class="min-w-0 flex-1 font-mono text-[12.5px]">{{ policy }}</span>
            </CommandItem>
          </CommandGroup>
          <CommandGroup v-if="groups.length" heading="策略组">
            <CommandItem v-for="policy in groups" :key="policy" :value="policy" :data-current="modelValue === policy || undefined" :class="optionClass" @select="choose(policy)">
              <Check class="size-4 shrink-0" :class="modelValue === policy ? 'text-primary-accent' : 'opacity-0'" aria-hidden="true" />
              <span class="min-w-0 flex-1 break-all text-sm">{{ policy }}</span>
              <span v-if="!policies.includes(policy)" class="shrink-0 text-xs text-warning-accent">已不存在</span>
            </CommandItem>
          </CommandGroup>
        </CommandList>
      </Command>
    </PopoverContent>
  </Popover>
</template>

<style scoped>
/* 视觉 30px 以内，上下透明区域补足 44px 点击区 */
.rule-policy-trigger::before {
  content: '';
  position: absolute;
  inset: calc((30px - var(--cf-touch, 44px)) / 2) 0;
}
</style>
