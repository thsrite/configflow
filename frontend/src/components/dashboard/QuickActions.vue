<template>
  <Card class="gap-0 py-0" role="region" aria-label="快速操作">
    <h2 class="m-0 px-5 pt-4 pb-3 text-sm font-semibold text-foreground">快速操作</h2>
    <div class="flex flex-col gap-2 px-5 pb-4">
      <!-- 一个视图只有一个主操作，其余为次要操作 -->
      <Button
        v-for="action in actions"
        :key="action.label"
        :variant="action.primary ? 'default' : 'outline'"
        class="h-10 w-full justify-start px-3 text-[13px]"
        :disabled="action.disabled"
        @click="$emit('run', action)"
      >
        <component :is="iconOf(action.icon)" class="size-4" :stroke-width="2" />
        <span class="min-w-0 flex-1 truncate text-left">{{ action.label }}</span>
        <ChevronRight class="size-4 opacity-50" />
      </Button>
    </div>
  </Card>
</template>

<script setup lang="ts">
import { ChevronRight } from '@lucide/vue'
import { Card } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { iconOf } from '@/lib/icons'

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
