<script setup lang="ts">
import { computed } from 'vue'
import { deploymentStatusLabel } from '@/composables/useAgentDeployments'
import type { AgentHAPublication } from '@/types/agentHAPublication'

const props = defineProps<{ publication: AgentHAPublication }>()
const phase = computed(() => ({
  preparing: '准备两台的配置与规则', standby: '更新未持有虚拟 IP 的设备',
  handoff: '迁移虚拟 IP 并确认解析', primary: '更新原持有虚拟 IP 的设备',
  keepalived: '启用主备切换', done: '主备发布',
})[props.publication.phase])
const auto = computed(() => props.publication.auto_complete)
// 等待与进行中由所在行的状态显示；只有无法自动完成时单独写明原因。
const blocked = computed(() => auto.value?.status === 'blocked' && !!auto.value.message)
const memberStatus = (status: string) => ({ waiting: '等待前一台完成', pending: '等待前一台完成',
  succeeded: '发布成功', skipped: '未执行', blocked: '未执行',
} as Record<string, string>)[status] || deploymentStatusLabel({ deployment_id: '', status })
</script>

<template>
  <div class="min-w-0 space-y-1 text-xs leading-relaxed" role="status" aria-live="polite" data-ha-publication>
    <p class="m-0 break-words text-foreground">
      <span v-if="auto?.attempt" class="chip mr-1.5" data-ha-auto-attempt>自动补完</span>{{ publication.message || phase }}
    </p>
    <ol class="m-0 list-inside list-decimal space-y-1 p-0 text-muted-foreground">
      <li v-for="member in publication.members" :key="member.agent_id" class="break-words"
        :class="{ 'text-primary-accent': member.agent_id === publication.active_agent_id,
          'text-destructive-accent': ['failed', 'rolled_back', 'rollback_failed'].includes(member.status) }">
        {{ member.name }}：{{ memberStatus(member.status) }}
      </li>
    </ol>
    <p v-if="blocked" class="m-0 break-words font-semibold text-destructive-accent" data-ha-auto-complete>{{ auto?.message }}</p>
  </div>
</template>
