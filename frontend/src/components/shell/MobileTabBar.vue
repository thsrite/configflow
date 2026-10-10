<template>
  <nav
    class="glass-strong fixed inset-x-3 bottom-[calc(env(safe-area-inset-bottom)+10px)] z-30 hidden h-[62px] grid-cols-5 gap-1 rounded-[20px] border border-border-strong p-1.5 shadow-overlay max-[900px]:grid"
    aria-label="主导航"
  >
    <component
      :is="tab.path ? 'router-link' : 'button'"
      v-for="tab in TABS"
      :key="tab.label"
      :to="tab.path"
      :type="tab.path ? undefined : 'button'"
      :class="cn(
        'relative flex min-h-11 cursor-pointer flex-col items-center justify-center gap-0.5 rounded-[14px] border-0 bg-transparent px-0 text-[10.5px] font-semibold no-underline transition-colors duration-200 focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none',
        isActive(tab) ? 'text-primary-accent' : 'text-muted-foreground'
      )"
      :aria-current="isActive(tab) ? 'page' : undefined"
      @click="!tab.path && emit('more')"
    >
      <Motion
        v-if="isActive(tab)"
        layout-id="tab-active"
        class="absolute inset-0 rounded-[14px] bg-primary-soft"
        :transition="SPRING"
        aria-hidden="true"
      />
      <component :is="tab.icon" class="relative size-5" :stroke-width="2" aria-hidden="true" />
      <span class="relative">{{ tab.label }}</span>
    </component>
  </nav>
</template>

<script setup lang="ts">
import type { Component } from 'vue'
import { Motion } from 'motion-v'
import { Download, LayoutDashboard, Link2, MoreHorizontal, Network } from '@lucide/vue'
import { SPRING } from '@/lib/motion'
import { cn } from '@/lib/utils'

/* 高频的四个页面直达，其余页面与动作收进「更多」（命令面板） */
interface Tab {
  label: string
  icon: Component
  path?: string
}

const TABS: Tab[] = [
  { label: '总览', icon: LayoutDashboard, path: '/dashboard' },
  { label: '订阅', icon: Link2, path: '/subscriptions' },
  { label: '节点', icon: Network, path: '/nodes' },
  { label: '生成', icon: Download, path: '/generate' },
  { label: '更多', icon: MoreHorizontal }
]

const props = defineProps<{ activePath: string }>()
const emit = defineEmits<{ (e: 'more'): void }>()

const isActive = (tab: Tab) => !!tab.path && props.activePath === tab.path
</script>
