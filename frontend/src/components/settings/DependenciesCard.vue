<template>
  <SectionCard
    title="第三方依赖"
    help="检测内置组件的版本，有新版本时可在线更新。访问 GitHub 时使用上方的规则下载代理和 GitHub 代理域名设置。"
    :padded="false"
    class="dependencies-card min-w-0"
  >
    <template #actions>
      <Button variant="ghost" size="sm" class="h-8 px-2 text-xs text-muted-foreground" :disabled="checking" @click="emit('check')">
        <Loader2 v-if="checking" class="size-3.5 animate-spin" />
        <RefreshCw v-else class="size-3.5" />
        {{ checking ? '正在检测…' : '检查更新' }}
      </Button>
    </template>

    <p v-if="!dependencies.length" class="m-0 border-t border-border px-4 py-3 text-[13px] text-muted-foreground" role="status">
      {{ checking ? '正在检测…' : '暂无依赖信息' }}
    </p>
    <table v-else class="cf-table cf-mobile-records dependencies-table" aria-label="第三方依赖">
      <colgroup><col /><col class="dependency-version-col" /><col class="dependency-status-col" /><col class="dependency-mode-col" /></colgroup>
      <thead>
        <tr><th scope="col">组件</th><th scope="col">版本</th><th scope="col">状态</th><th scope="col">运行方式</th></tr>
      </thead>
      <tbody>
        <tr v-for="dep in dependencies" :key="dep.key">
          <td data-label="组件">
            <div class="dependency-value">
              <a :href="dep.homepage" target="_blank" rel="noopener noreferrer" class="font-semibold text-foreground hover:underline">{{ dep.name }}</a>
            </div>
            <p class="dependency-note">
              <template v-if="dep.runtime">Node.js <span class="font-mono">{{ dep.runtime }}</span></template>
              <template v-else>{{ dep.description }}</template>
            </p>
          </td>
          <td data-label="版本">
            <div class="dependency-value font-mono">{{ dep.current_version || '未知' }}</div>
            <div v-if="dep.update.state === 'running'" class="dependency-note flex items-start gap-1.5" role="status">
              <Loader2 class="mt-0.5 size-3.5 shrink-0 animate-spin" />
              <span>{{ dep.update.message || '正在更新…' }}</span>
            </div>
            <template v-else>
              <div v-if="dep.has_update" class="flex flex-wrap items-center gap-1.5">
                <Button v-if="dep.updatable" size="sm" class="h-7 px-2 text-xs" :disabled="updatingKey === dep.key" @click="emit('update', dep)">
                  <Loader2 v-if="updatingKey === dep.key" class="size-3.5 animate-spin" />
                  更新到 {{ dep.latest_version }}
                </Button>
                <span v-else class="text-[12px] text-warning-accent">最新 {{ dep.latest_version }}</span>
                <a v-if="dep.release_url" :href="dep.release_url" target="_blank" rel="noopener noreferrer" class="dependency-release inline-flex items-center justify-center rounded-sm text-muted-foreground hover:text-primary-accent" :aria-label="`${dep.name} ${dep.latest_version} 更新说明`">
                  <ExternalLink class="size-3.5" />
                </a>
              </div>
              <component
                :is="dep.release_url ? 'a' : 'span'"
                v-else-if="dep.current_version && dep.latest_version && !dep.check_error"
                :href="dep.release_url || undefined"
                :target="dep.release_url ? '_blank' : undefined"
                :rel="dep.release_url ? 'noopener noreferrer' : undefined"
                class="dependency-note block text-success-accent"
                :class="dep.release_url && 'hover:underline'"
              >已是最新</component>
              <span v-else-if="!dep.check_error" class="dependency-note">{{ checking ? '正在检测…' : '尚未检查' }}</span>
            </template>
            <p v-if="dep.check_error" class="dependency-error" role="alert">{{ dep.check_error }}</p>
            <p v-if="dep.update.state === 'failed'" class="dependency-error" role="alert">{{ dep.update.message }}</p>
          </td>
          <td data-label="状态">
            <div class="dependency-value" aria-live="polite"><StatusDot :tone="runtimeStatus(dep).tone" :label="runtimeStatus(dep).label" /></div>
            <p v-if="dep.lifecycle?.error" class="dependency-error" role="status">{{ dep.lifecycle.error }}</p>
          </td>
          <td data-label="运行方式">
            <SubStoreRuntime v-if="dep.key === 'sub-store'" :dependency="dep" @changed="emit('changed')" />
            <span v-else class="dependency-value text-muted-foreground">—</span>
          </td>
        </tr>
      </tbody>
    </table>
  </SectionCard>
</template>

<script setup lang="ts">
import { ExternalLink, Loader2, RefreshCw } from '@lucide/vue'
import type { DependencyStatus } from '@/api'
import SectionCard from '@/components/common/SectionCard.vue'
import StatusDot from '@/components/common/StatusDot.vue'
import { Button } from '@/components/ui/button'
import SubStoreRuntime from './SubStoreRuntime.vue'

defineProps<{ dependencies: DependencyStatus[]; checking: boolean; updatingKey: string }>()
const emit = defineEmits<{ check: []; update: [dependency: DependencyStatus]; changed: [] }>()
const statuses = {
  sleeping: { tone: 'muted', label: '休眠中' },
  starting: { tone: 'warning', label: '启动中' },
  updating: { tone: 'warning', label: '更新中' },
  running: { tone: 'success', label: '运行中' },
  failed: { tone: 'danger', label: '无法连接' }
} as const
function runtimeStatus(dependency: DependencyStatus) {
  return statuses[dependency.lifecycle?.state || (dependency.running ? 'running' : 'failed')]
}
</script>

<style scoped>
.dependencies-card :deep(.cf-section-header) {
  gap: var(--cf-sp-2);
  padding: var(--cf-sp-1) var(--cf-sp-4);
}
.dependencies-table { table-layout: fixed; }
.dependency-version-col { width: 12rem; }
.dependency-status-col { width: 7.5rem; }
.dependency-mode-col { width: 12rem; }
.dependencies-table > thead > tr > th {
  padding-block: var(--cf-sp-1);
  border-top: 1px solid var(--border);
  background: var(--muted);
  font-weight: 500;
}
.dependencies-table > tbody > tr > td {
  padding-block: var(--cf-sp-2);
  vertical-align: top;
  overflow-wrap: anywhere;
}
.dependency-value { display: flex; min-height: 32px; align-items: center; }
.dependency-note { margin: 0; font-size: 12px; line-height: 18px; color: var(--muted-foreground); }
.dependency-note.text-success-accent { color: var(--success-accent); }
.dependency-error { margin: var(--cf-sp-1) 0 0; font-size: 12px; line-height: 18px; color: var(--destructive-accent); }
.dependency-release { min-width: 28px; min-height: 28px; }
@media (max-width: 767px), (pointer: coarse) {
  .dependency-release { min-width: var(--cf-touch); min-height: var(--cf-touch); }
  .dependency-value { min-height: var(--cf-touch); }
}
@media (max-width: 639px) {
  .dependencies-card :deep(.cf-section-header) { padding: var(--cf-sp-1) var(--cf-sp-3); }
  .dependencies-table > colgroup { display: none; }
  .dependencies-table > tbody > tr {
    gap: var(--cf-sp-2) var(--cf-sp-3);
    padding: var(--cf-sp-2) var(--cf-sp-3) var(--cf-sp-3);
    border-top: 1px solid var(--border);
  }
  .dependencies-table > tbody > tr > td { padding: 0; }
  .dependencies-table > tbody > tr > td::before { margin-bottom: 0; }
  .dependencies-table > tbody > tr > td:nth-child(-n + 2) .dependency-value { min-height: 28px; }
}
</style>
