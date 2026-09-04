<template>
  <nav
    class="cf-tabs fixed inset-x-0 bottom-0 z-900 hidden grid-cols-4 border-t border-border bg-background/92 pb-[env(safe-area-inset-bottom)] backdrop-blur-xl backdrop-saturate-150 max-[900px]:grid"
    aria-label="主导航"
  >
    <button
      v-for="group in groups"
      :key="group.scope"
      type="button"
      :class="cn(
        'flex min-h-(--cf-tabbar-h) cursor-pointer flex-col items-center gap-1 border-0 bg-transparent px-0 pt-2 pb-1.5 text-[10.5px] font-semibold text-muted-foreground transition-colors',
        group.scope === activeScope && 'text-primary-accent'
      )"
      :aria-current="group.scope === activeScope ? 'page' : undefined"
      @click="$emit('select', group)"
    >
      <component :is="iconOf(group.tabIcon)" class="size-5" :stroke-width="2" aria-hidden="true" />
      <span>{{ group.tabLabel }}</span>
    </button>
  </nav>
</template>

<script setup lang="ts">
import { NAV_GROUPS, type NavGroup, type NavScope } from '@/navigation'
import { iconOf } from '@/lib/icons'
import { cn } from '@/lib/utils'

defineProps<{ activeScope?: NavScope }>()
defineEmits<{ (e: 'select', group: NavGroup): void }>()

const groups = NAV_GROUPS
</script>
