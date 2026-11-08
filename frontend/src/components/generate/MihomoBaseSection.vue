<script setup lang="ts">
import { ChevronDown } from '@lucide/vue'
import FieldHelp from '@/components/common/FieldHelp.vue'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'

/** A folded group of base settings; the header keeps the current values in view. */
defineProps<{ title: string; summary: string; help?: string; blocking?: number; warnings?: number; section: string }>()
const open = defineModel<boolean>('open', { default: false })
</script>

<template>
  <Collapsible v-model:open="open" class="group min-w-0 rounded-lg border border-border bg-card" :data-base-section="section">
    <div class="relative flex min-h-12 min-w-0 items-center gap-2 px-3 py-2">
      <CollapsibleTrigger type="button" :aria-label="`${title}：${summary}`" class="absolute inset-0 cursor-pointer rounded-lg outline-none focus-visible:ring-2 focus-visible:ring-primary-accent" />
      <span class="pointer-events-none flex min-w-0 flex-1 flex-wrap items-center gap-x-2 gap-y-0.5">
        <span class="flex shrink-0 items-center gap-1 text-[13px] font-medium">
          {{ title }}
          <span v-if="help" class="pointer-events-auto relative z-10"><FieldHelp :label="title" :text="help" /></span>
        </span>
        <span class="min-w-0 break-words text-xs text-muted-foreground" data-base-summary>{{ summary }}</span>
      </span>
      <span v-if="blocking" class="chip chip-bad pointer-events-none shrink-0">{{ blocking }} 项需修正</span>
      <span v-else-if="warnings" class="chip chip-warn pointer-events-none shrink-0">{{ warnings }} 项提醒</span>
      <ChevronDown class="pointer-events-none size-4 shrink-0 text-muted-foreground transition-transform group-data-[state=open]:rotate-180" aria-hidden="true" />
    </div>
    <CollapsibleContent class="min-w-0 border-t border-border p-3 md:p-4">
      <slot />
    </CollapsibleContent>
  </Collapsible>
</template>
