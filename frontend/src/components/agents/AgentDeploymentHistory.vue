<template>
  <Dialog :open="true" @update:open="open => !open && emit('close')">
    <DialogContent class="max-h-[85dvh] max-w-[calc(100vw-2rem)] overflow-y-auto sm:max-w-[820px]" @open-auto-focus="focusRecord">
      <DialogHeader>
        <DialogTitle>{{ agent.name }} · 发布记录</DialogTitle>
        <DialogDescription>查看已记录的配置发布。当前最新成功部署可恢复到这次发布前的配置和受管规则；state/ 运行数据保留。</DialogDescription>
      </DialogHeader>
      <p class="text-xs leading-relaxed text-muted-foreground">首版只支持回滚当前仍生效的最新成功部署。Agent 会检查备份、活动版本与服务健康；较旧记录用于查看，不能直接切换到任意历史版本。</p>
      <EmptyState v-if="!records.length" title="暂无发布记录" description="首次推送配置后会在这里显示发布结果。" />
      <div v-else class="cf-table-wrap">
        <table class="cf-table cf-mobile-records w-full text-sm" role="table">
          <thead role="rowgroup"><tr role="row"><th role="columnheader" class="text-left">发布</th><th role="columnheader" class="text-left">结果</th><th role="columnheader" class="text-right">操作</th></tr></thead>
          <tbody role="rowgroup">
            <tr role="row" v-for="record in records" :key="record.deployment_id" :data-deployment-record="record.deployment_id">
              <td role="cell" data-label="发布" class="cf-record-wide max-w-[260px] align-top max-sm:max-w-none">
                <div class="break-all font-mono text-xs" :title="record.deployment_id">{{ record.deployment_id }}</div>
                <div class="mt-1 text-xs text-muted-foreground">{{ formatDate(record.updated_at || record.created_at) }}<template v-if="record.config_revision !== undefined"> · 修订 {{ record.config_revision }}</template></div>
                <div v-if="record.deployment_id === latestId" class="mt-1 text-xs text-primary-accent">最新记录</div>
              </td>
              <td role="cell" data-label="结果" class="cf-record-wide max-w-[280px] align-top max-sm:max-w-none">
                <AgentDeploymentStatus :task="record" compact>
                  <template v-if="deploymentNeedsNotice(record)" #notice>
                    <AgentOperationSteps :task="record" :show-current="false" :show-steps="false" />
                    <AgentFailureDetails :show-progress-message="false" :showDNSChecks="false" :task="record" :expanded="record.deployment_id === focusDeployment" />
                  </template>
                  <template #details>
                    <AgentOperationSteps :task="record" :show-current="false" :show-message="!deploymentNeedsNotice(record)" embedded />
                  </template>
                </AgentDeploymentStatus>
                <AgentDNSChecks :key="record.deployment_id" :checks="record.dns_checks" :checked-at="record.dns_checked_at" compact />
              </td>
              <td role="cell" class="cf-record-wide text-right align-top">
                <Button v-if="record.deployment_id === latestId && record.status === 'succeeded'" variant="outline" size="sm" class="min-h-11 whitespace-nowrap" :disabled="blocked || busy || agent.status !== 'online'" @click="emit('rollback', record)">回滚到发布前</Button>
                <span v-else class="text-xs text-muted-foreground">—</span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <p v-if="agent.status !== 'online'" class="text-xs text-muted-foreground">Agent 离线，暂时无法发起回滚。</p>
      <DialogFooter><Button variant="outline" class="min-h-11" @click="emit('close')">关闭</Button></DialogFooter>
    </DialogContent>
  </Dialog>
</template>

<script setup lang="ts">
import { computed, nextTick, watch } from 'vue'
import { focusSetting } from '@/lib/settingLink'
import { notify } from '@/lib/feedback'
import type { Agent } from '@/types'
import { deploymentNeedsNotice, type AgentDeployment } from '@/composables/useAgentDeployments'
import EmptyState from '@/components/common/EmptyState.vue'
import AgentFailureDetails from './AgentFailureDetails.vue'
import AgentOperationSteps from './AgentOperationSteps.vue'
import AgentDeploymentStatus from './AgentDeploymentStatus.vue'
import AgentDNSChecks from './AgentDNSChecks.vue'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'

const props = defineProps<{ agent: Agent; progress?: AgentDeployment; busy: boolean; blocked: boolean; focusDeployment?: string }>()
const emit = defineEmits<{ (event: 'close'): void; (event: 'rollback', record: AgentDeployment): void }>()
const latestId = computed(() => props.agent.latest_deployment?.deployment_id || props.progress?.deployment_id)
const records = computed(() => {
  const records = new Map(Object.values(props.agent.deployments || {}).map(record => [record.deployment_id, record]))
  if (props.agent.latest_deployment) records.set(props.agent.latest_deployment.deployment_id, props.agent.latest_deployment)
  if (props.progress) records.set(props.progress.deployment_id, props.progress)
  return [...records.values()].sort((a, b) => {
    if (a.deployment_id === latestId.value) return -1
    if (b.deployment_id === latestId.value) return 1
    return (b.updated_at || b.created_at || '').localeCompare(a.updated_at || a.created_at || '')
  })
})
const formatDate = (date?: string) => date ? new Date(date).toLocaleString() : '时间未记录'
async function focusRecord(event?: Event) {
  const id = props.focusDeployment
  if (!id) return
  event?.preventDefault()
  await nextTick()
  const row = Array.from(document.querySelectorAll<HTMLElement>('[data-deployment-record]'))
    .find(element => element.dataset.deploymentRecord === id)
  if (row) await focusSetting(row)
  else notify.warning('该发布记录已清理，当前显示此设备仍保留的记录。')
}
watch(() => props.focusDeployment, () => { void focusRecord() }, { flush: 'post' })
</script>
