<template>
  <Popover v-model:open="open">
    <PopoverTrigger as-child>
      <Button variant="ghost" size="icon-sm" title="外观设置" aria-label="外观设置">
        <SlidersHorizontal class="size-[17px]" />
      </Button>
    </PopoverTrigger>
    <TweaksContent v-if="hasOpened" @close="open = false" />
  </Popover>
</template>

<script setup lang="ts">
import { computed, defineAsyncComponent, ref } from 'vue'
import { SlidersHorizontal } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import { Popover, PopoverTrigger } from '@/components/ui/popover'

const TweaksContent = defineAsyncComponent(() => import('./TweaksContent.vue'))
const open = ref(false)
const hasOpened = computed<boolean>(previous => open.value || !!previous)
</script>
