<template>
  <SectionCard title="快速操作" role="region" aria-label="快速操作">
    <div class="flex flex-col gap-2">
      <!-- 一个视图只有一个主操作，其余为次要操作 -->
      <Motion
        v-for="(action, index) in actions"
        :key="action.label"
        v-bind="listItem(index)"
      >
        <Button
          :variant="action.primary ? 'default' : 'outline'"
          :class="[
            'group h-10 w-full justify-start px-3 text-[13px]',
            action.primary && 'shadow-glow-soft',
            !action.primary && 'border-border/60 bg-background/40'
          ]"
          :disabled="action.disabled"
          @click="$emit('run', action)"
        >
          <component :is="iconOf(action.icon)" class="size-4" :stroke-width="2" />
          <span class="min-w-0 flex-1 truncate text-left">{{ action.label }}</span>
          <ChevronRight
            class="size-4 opacity-50 transition-transform duration-200 group-hover:translate-x-0.5 group-hover:opacity-90"
          />
        </Button>
      </Motion>
    </div>
  </SectionCard>
</template>

<script setup lang="ts">
import { Motion } from 'motion-v'
import { ChevronRight } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import SectionCard from '@/components/common/SectionCard.vue'
import { iconOf } from '@/lib/icons'
import { listItem } from '@/lib/motion'

export interface QuickAction {
  label: string
  icon: string
  /** 一个视图只有一个主操作 */
  primary?: boolean
  disabled?: boolean
  route?: string
}

defineProps<{ actions: QuickAction[] }>()
defineEmits<{ (e: 'run', action: QuickAction): void }>()
</script>
