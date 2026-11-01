<script setup lang="ts">
import { computed } from 'vue'
import { Copy } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import { notify } from '@/lib/feedback'
import { copyText } from '@/lib/clipboard'
import { deploymentErrorSummary } from '@/lib/agentFailure'
import { deploymentLabels, type AgentDeployment } from '@/composables/useAgentDeployments'
import AgentDNSChecks from './AgentDNSChecks.vue'

const props = withDefaults(defineProps<{ task: Partial<AgentDeployment>; showProgressMessage?: boolean; showDNSChecks?: boolean; expanded?: boolean }>(), { showProgressMessage: true, showDNSChecks: true })
const failed = computed(() => !!props.task.error || !!props.task.error_details || !!props.task.rollback_error || !!props.task.rollback_error_details || ['failed', 'rollback_failed'].includes(props.task.status || '') || (props.task.status === 'rolled_back' && !props.task.manual_rollback))
const sections = computed(() => [
  { key: 'publish', label: '发布失败详情', summary: props.task.error || ((props.showProgressMessage || failed.value) ? props.task.message : '') || '', details: props.task.error_details || '' },
  { key: 'rollback', label: '回滚失败详情', summary: props.task.rollback_error || props.task.rollback_message || '', details: props.task.rollback_error_details || '' },
].map(section => ({
  ...section,
  short: deploymentErrorSummary({ error: section.summary }),
  // Older Agents can return a long message without a separate details field.
  details: section.details.trim() ? section.details : (section.summary.trim() !== deploymentErrorSummary({ error: section.summary }) ? section.summary : ''),
})).filter(section => section.summary || section.details))
const stage = computed(() => props.task.failed_stage ? deploymentLabels[props.task.failed_stage] || props.task.failed_stage : '')

async function copy(text: string) {
  try {
    await copyText(text)
    notify.success('失败详情已复制')
  } catch { notify.error('复制失败，请手动选择下方内容复制') }
}
</script>

<template>
  <div v-if="sections.length || stage || (showDNSChecks && task.dns_checks?.length)" class="min-w-0 space-y-1 text-xs leading-relaxed [overflow-wrap:anywhere]" :class="failed ? 'text-destructive-accent' : 'text-muted-foreground'">
    <p v-if="stage" class="m-0">失败步骤：{{ stage }}</p>
    <div v-for="section in sections" :key="section.key" class="min-w-0">
      <p v-if="section.short" class="m-0">{{ section.key === 'rollback' ? '回滚：' : '' }}{{ section.short }}</p>
      <details v-if="section.details" :open="expanded" :key="`${task.deployment_id}:${section.key}:${section.details}`" class="mt-1 min-w-0 rounded-md border border-border bg-background/50 text-foreground">
        <summary class="min-h-11 cursor-pointer px-3 py-3 text-xs font-medium focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-accent">{{ section.label }}</summary>
        <div class="min-w-0 border-t border-border px-3 pb-3">
          <div class="flex justify-end"><Button type="button" variant="ghost" size="sm" class="min-h-11" :aria-label="`复制${section.label}`" @click="copy(section.details)"><Copy class="size-3.5" aria-hidden="true" />复制</Button></div>
          <pre class="m-0 max-h-64 max-w-full overflow-y-auto whitespace-pre-wrap font-mono text-[11.5px] leading-relaxed [overflow-wrap:anywhere]" tabindex="0">{{ section.details }}</pre>
        </div>
      </details>
    </div>
    <AgentDNSChecks v-if="showDNSChecks" :key="task.deployment_id" :checks="task.dns_checks" :checked-at="task.dns_checked_at" />
  </div>
</template>
