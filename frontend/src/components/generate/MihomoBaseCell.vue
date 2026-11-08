<script setup lang="ts">
import FieldHelp from '@/components/common/FieldHelp.vue'
import type { BaseIssue } from '@/lib/mihomoBaseSettings'

/** One cell of the base settings sheet: a short name over its value, which is edited in place. */
defineProps<{
  label: string
  help?: string
  htmlFor?: string
  status?: string
  warn?: boolean
  messages?: Array<Pick<BaseIssue, 'message' | 'blocking'>>
}>()
</script>

<template>
  <div class="grid min-w-0 content-start gap-0.5 rounded-md px-0.5 py-1.5">
    <!-- One label height with or without help, so cells side by side line up (32px on a phone, as cf-compact-help). -->
    <div class="flex min-h-8 min-w-0 items-center gap-0.5 px-1.5 text-[11.5px] text-muted-foreground md:min-h-6">
      <label v-if="htmlFor" :for="htmlFor" class="min-w-0 truncate">{{ label }}</label>
      <span v-else class="min-w-0 truncate">{{ label }}</span>
      <span v-if="help" class="cf-compact-help"><FieldHelp :label="label" :text="help" /></span>
    </div>
    <slot />
    <p v-if="status" class="m-0 break-words px-1.5 text-[11.5px] leading-snug" :class="warn ? 'text-warning-accent' : 'text-muted-foreground'" data-base-status>{{ status }}</p>
    <p v-for="item in messages" :key="item.message" class="m-0 break-words px-1.5 text-[11.5px] leading-snug"
      :class="item.blocking ? 'text-destructive-accent' : 'text-warning-accent'" :role="item.blocking ? 'alert' : undefined" data-base-issue>
      {{ item.blocking ? item.message : `已保存的配置：${item.message}` }}
    </p>
  </div>
</template>
