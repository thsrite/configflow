<template>
  <nav
    class="glass-strong fixed inset-x-0 bottom-0 z-900 hidden grid-cols-4 border-t border-border/60 pb-[env(safe-area-inset-bottom)] max-[900px]:grid"
    aria-label="主导航"
  >
    <button
      v-for="group in groups"
      :key="group.scope"
      type="button"
      :class="cn(
        'relative flex min-h-(--cf-tabbar-h) cursor-pointer flex-col items-center gap-1 border-0 bg-transparent px-0 pt-2.5 pb-1.5 text-[10.5px] font-semibold transition-colors duration-200',
        group.scope === activeScope ? 'text-primary-accent' : 'text-muted-foreground'
      )"
      :aria-current="group.scope === activeScope ? 'page' : undefined"
      @click="$emit('select', group)"
    >
      <Motion
        v-if="group.scope === activeScope"
        layout-id="tab-active"
        class="absolute top-0 h-0.5 w-9 rounded-full bg-primary-accent shadow-[0_0_10px_var(--primary-accent)]"
        :transition="SPRING"
        aria-hidden="true"
      />
      <component :is="iconOf(group.tabIcon)" class="size-5" :stroke-width="2" aria-hidden="true" />
      <span>{{ group.tabLabel }}</span>
    </button>
  </nav>
</template>

<script setup lang="ts">
import { Motion } from 'motion-v'
import { NAV_GROUPS, type NavGroup, type NavScope } from '@/navigation'
import { iconOf } from '@/lib/icons'
import { SPRING } from '@/lib/motion'
import { cn } from '@/lib/utils'

defineProps<{ activeScope?: NavScope }>()
defineEmits<{ (e: 'select', group: NavGroup): void }>()

const groups = NAV_GROUPS
</script>
