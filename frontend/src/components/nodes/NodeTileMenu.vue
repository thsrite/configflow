<script setup lang="ts">
import { ref } from 'vue'
import { MoreHorizontal } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import { DropdownMenu, DropdownMenuContent, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'

defineProps<{ label: string }>()
const emit = defineEmits<{ 'update:open': [open: boolean] }>()
const hasOpened = ref(false)
function onOpen(open: boolean) {
  if (open) hasOpened.value = true
  emit('update:open', open)
}
</script>

<template>
  <DropdownMenu @update:open="onOpen">
    <DropdownMenuTrigger as-child>
      <Button
        variant="ghost"
        size="icon-sm"
        class="node-tile-menu cf-reorder-mute -my-1 -mr-1.5 size-7 shrink-0 opacity-60 group-hover:opacity-100"
        :aria-label="label"
      >
        <MoreHorizontal class="size-4" />
      </Button>
    </DropdownMenuTrigger>
    <DropdownMenuContent v-if="hasOpened" align="end" class="glass-strong min-w-[168px]">
      <slot />
    </DropdownMenuContent>
  </DropdownMenu>
</template>
