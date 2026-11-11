<template>
  <header class="cf-page-header relative mb-6 flex flex-wrap items-end gap-4 max-md:mb-4" :class="hideTitle && 'cf-page-header--untitled max-sm:mb-3'">
    <div v-if="$slots['title-extra'] || (mobile && mobileHelp && !hideTitle)" class="cf-page-heading flex min-w-0 flex-[1_1_240px] items-center gap-3">
      <h1 :class="cn('font-display m-0 shrink-0 text-[34px] leading-[1.12] text-foreground max-md:text-[26px]', hideTitle && 'sr-only')">
        {{ title }}
      </h1>
      <span v-if="mobile && mobileHelp && !hideTitle" class="cf-compact-help"><FieldHelp :label="title" :text="mobileHelp" /></span>
      <slot name="title-extra" />
    </div>
    <h1
      v-else
      :class="cn('font-display m-0 min-w-0 flex-[1_1_240px] text-[34px] leading-[1.12] text-foreground max-md:text-[26px]', hideTitle && 'sr-only')"
    >
      {{ title }}
    </h1>

    <div v-if="$slots.actions || (hideTitle && mobileHelp)" class="cf-page-actions flex min-w-0 max-w-full flex-wrap items-center gap-2 max-md:w-full">
      <!-- 标题隐藏时，页面说明跟着操作按钮放，不单独占一行 -->
      <span v-if="hideTitle && mobileHelp" class="cf-compact-help"><FieldHelp :label="title" :text="mobileHelp" /></span>
      <slot name="actions" />
    </div>
  </header>
</template>

<script setup lang="ts">
import { computed, inject } from 'vue'
import { useMediaQuery } from '@vueuse/core'
import { CURRENT_NAV_PATH, isNavPath } from '@/navigation'
import { cn } from '@/lib/utils'
import FieldHelp from './FieldHelp.vue'

defineProps<{ title: string; mobileHelp?: string }>()
const mobile = useMediaQuery('(max-width: 767px)')
const phone = useMediaQuery('(max-width: 639px)')
// 由应用外壳提供；单独挂载（如测试）时为空，按不在导航里处理
const navPath = inject(CURRENT_NAV_PATH, null)

/**
 * 手机上导航里的页面已由顶部分组按钮或底栏高亮写明所在页，大标题只留给读屏；
 * 不在导航里的页面（详情、向导等）照常显示。
 */
const hideTitle = computed(() => {
  return phone.value && !!navPath && isNavPath(navPath.value)
})
</script>
