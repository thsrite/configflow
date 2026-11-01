<template>
  <div class="oxidns-overview min-w-0 space-y-4 max-sm:space-y-3">
    <p v-if="overview.statistics?.publication_count" class="m-0 text-xs leading-relaxed text-muted-foreground">已接续 {{ overview.statistics.publication_count }} 次配置发布，沿用原统计起点；发布切换期间可能有少量漏计。</p>
    <p v-if="overview.statistics?.partial" class="m-0 text-xs leading-relaxed text-warning-accent">部分指标缺少基线或插件已重新计数，本轮统计可能不完整；无法确认的值显示「—」。</p>
    <div class="grid min-w-0 grid-cols-2 gap-3 max-sm:gap-2 xl:grid-cols-3">
      <OxiDNSMetric label="DNS 请求" :value="overview.metrics.requests_total" :hint="periodLabel" />
      <OxiDNSMetric label="近期请求速率" :value="qps" unit="次/秒" :precision="2" :hint="qps === null ? '采样中，需两次数据计算' : '最近两次采样之间的平均值'" />
      <OxiDNSMetric label="平均响应时间" :value="overview.metrics.avg_latency_ms" unit="ms" :precision="2" :hint="periodLabel" />
      <OxiDNSMetric label="缓存命中率" :value="cacheRate" unit="%" :precision="1" :hint="selectedCache === '__all__' ? '全部缓存 · 命中数 / 查找次数' : selectedTag + ' · 命中数 / 查找次数'" />
      <OxiDNSMetric label="失败请求" :value="overview.metrics.failed_total" :hint="periodLabel" />
      <OxiDNSMetric label="当前处理中" :value="overview.metrics.inflight" hint="采样那一刻尚未完成的请求，0 表示当时已处理完" />
    </div>


    <section class="min-w-0 space-y-3" aria-label="DNS 请求排行">
      <header class="flex flex-col items-start justify-between gap-3 px-1 sm:flex-row">
        <div class="min-w-0 flex-1">
          <h2 class="m-0 flex items-center gap-2 text-sm font-semibold">请求排行<FieldHelp label="请求排行" :text="rankingsDescription" /></h2>
          <p v-if="rankings" class="mb-0 mt-1 text-xs leading-relaxed text-muted-foreground">
            {{ rankingSampleDescription }}<span v-if="rankings.blocked.available"> 拦截排行另基于 {{ formatMonitorNumber(rankings.blocked.sample_size) }} 条查询记录。</span>
            <span v-if="rankings.clients.at_limit || rankings.domains.at_limit || rankings.blocked.at_limit" class="text-warning-accent">已达到样本上限，可能未覆盖本轮所有请求。</span>
          </p>
        </div>
        <Select v-if="overview.recorders.length > 1" :model-value="recorder" :disabled="resetting" @update:model-value="$emit('recorder', String($event))">
          <SelectTrigger class="min-h-11 w-full sm:w-[180px] max-w-full" aria-label="排行记录器"><SelectValue placeholder="选择记录器" /></SelectTrigger>
          <SelectContent><SelectItem v-for="tag in overview.recorders" :key="tag" :value="tag">{{ tag }}</SelectItem></SelectContent>
        </Select>
      </header>
      <SectionCard v-if="!rankings" :padded="false">
        <EmptyState title="暂无法统计请求排行" :description="rankingsUnavailableReason" />
      </SectionCard>
      <div v-else class="grid min-w-0 grid-cols-1 gap-4 max-sm:gap-3 lg:grid-cols-3">
        <OxiDNSRankingTable title="客户端排行" name-label="客户端" :entries="clientRankings"
          empty-title="暂无客户端请求" empty-description="当前统计范围内还没有客户端查询记录。" />
        <OxiDNSRankingTable title="请求域名排行" name-label="域名" :entries="rankings.domains.rows.map(row => ({ name: row.key, count: row.count }))"
          empty-title="暂无请求域名" empty-description="当前统计范围内还没有域名查询记录。" />
        <OxiDNSRankingTable v-if="rankings.blocked.available" title="被拦截域名排行" name-label="域名" :entries="rankings.blocked.rows.map(row => ({ name: row.key, count: row.count }))"
          empty-title="暂无已识别的拦截域名" empty-description="仅统计明确命中拦截规则的查询，不将普通 NXDOMAIN 或 IPv4 优先的空应答计为拦截。" />
        <OxiDNSRankingTable v-else title="被拦截域名排行" name-label="域名" :entries="[]"
          empty-title="暂无法识别拦截查询" :empty-description="rankings.blocked.reason || '当前配置没有可识别的拦截规则，不能仅凭应答结果判断。'" />
      </div>
    </section>

    <SectionCard class="min-w-0" title="上游服务器排行" :help="periodLabel + '，按请求尝试次数排列 · 前 10 项。竞速或重试可能为一次 DNS 查询产生多次上游请求；平均响应时间包含成功和失败的已完成尝试。'" :padded="false">
      <EmptyState v-if="!upstreams.length" title="暂无上游请求统计" description="尚无上游请求，或当前核心未返回具体上游服务器的指标。" />
      <div v-else class="cf-table-wrap max-w-full" tabindex="0" role="region" aria-label="上游服务器排行">
        <table class="cf-table">
          <thead><tr><th class="min-w-40">上游服务器</th><th class="text-right">请求尝试</th><th class="text-right">失败</th><th class="text-right">平均响应时间</th></tr></thead>
          <tbody>
            <tr v-for="upstream in upstreams" :key="upstream.id || upstream.tag + ':' + upstream.upstream">
              <td><span class="block break-all font-mono text-xs">{{ upstream.address || upstream.upstream }}</span><span class="mt-1 block break-all text-xs text-muted-foreground">{{ upstream.tag }}<template v-if="upstream.address && upstream.address !== upstream.upstream"> · {{ upstream.upstream }}</template></span></td>
              <td class="num whitespace-nowrap text-right">{{ formatMonitorNumber(upstream.requests) }}</td>
              <td class="num whitespace-nowrap text-right">{{ formatMonitorNumber(upstream.failed) }}</td>
              <td class="num whitespace-nowrap text-right">{{ formatMonitorNumber(upstream.avg_latency_ms, 2) }}<span v-if="monitorNumber(upstream.avg_latency_ms)"> ms</span></td>
            </tr>
          </tbody>
        </table>
      </div>
    </SectionCard>

    <SectionCard title="缓存统计" :help="periodLabel + '。命中包含新鲜与过期缓存，命中率按缓存查找次数计算；条目数为当前值。'" :padded="false">
      <template v-if="overview.caches.length > 1" #actions>
        <Select v-model="selectedCache">
          <SelectTrigger class="min-h-11 w-[160px] max-w-full sm:w-[180px]" aria-label="选择缓存实例"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value="__all__">全部缓存</SelectItem>
            <SelectItem v-for="cache in overview.caches" :key="cache.tag" :value="'cache:' + cache.tag">{{ cache.tag }}</SelectItem>
          </SelectContent>
        </Select>
      </template>
      <EmptyState v-if="!overview.caches.length" title="暂无缓存统计" description="当前核心没有返回缓存实例的统计数据。" />
      <div v-else class="cf-table-wrap" tabindex="0" role="region" aria-label="缓存实例统计">
        <table class="cf-table w-full">
          <thead><tr><th class="text-left">实例</th><th class="text-right">命中率</th><th class="text-right">查找</th><th class="text-right">新鲜命中</th><th class="text-right">过期命中</th><th class="text-right">未命中</th><th class="text-right">条目</th></tr></thead>
          <tbody>
            <tr v-for="cache in visibleCaches" :key="cache.tag">
              <td class="font-mono text-xs">{{ cache.tag }}</td>
              <td class="num text-right">{{ formatMonitorNumber(cache.hit_rate, 1) }}<span v-if="monitorNumber(cache.hit_rate)">%</span></td>
              <td class="num text-right">{{ formatMonitorNumber(cache.lookups) }}</td>
              <td class="num text-right">{{ formatMonitorNumber(cache.fresh_hits) }}</td>
              <td class="num text-right">{{ formatMonitorNumber(cache.stale_hits) }}</td>
              <td class="num text-right">{{ formatMonitorNumber(cache.misses) }}</td>
              <td class="num text-right">{{ formatMonitorNumber(cache.entries) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </SectionCard>

    <SectionCard title="监听服务统计" :help="'各 DNS 监听服务' + periodLabel + '的统计。处理中是采样那一刻尚未完成的请求数，0 表示当时已处理完，并不代表没有收到请求。'" :padded="false">
      <EmptyState v-if="!overview.servers.length" title="暂无监听服务统计" description="核心尚未返回监听服务指标。" />
      <div v-else class="cf-table-wrap" tabindex="0" role="region" aria-label="DNS 监听服务统计">
        <table class="cf-table w-full">
          <thead><tr><th class="text-left">服务</th><th class="text-left">协议</th><th class="text-right">请求</th><th class="text-right">失败</th><th class="text-right">平均耗时</th><th class="text-right">处理中</th></tr></thead>
          <tbody>
            <tr v-for="server in overview.servers" :key="server.tag + ':' + server.protocol">
              <td class="font-mono text-xs">{{ server.tag }}</td>
              <td class="font-mono text-xs">{{ server.protocol || '—' }}</td>
              <td class="num text-right">{{ formatMonitorNumber(server.requests) }}</td>
              <td class="num text-right">{{ formatMonitorNumber(server.failed) }}</td>
              <td class="num text-right">{{ formatMonitorNumber(server.avg_latency_ms, 2) }}<span v-if="monitorNumber(server.avg_latency_ms)"> ms</span></td>
              <td class="num text-right">{{ formatMonitorNumber(server.inflight) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </SectionCard>
  </div>
</template>
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import SectionCard from '@/components/common/SectionCard.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import FieldHelp from '@/components/common/FieldHelp.vue'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import OxiDNSMetric from './OxiDNSMetric.vue'
import OxiDNSRankingTable from './OxiDNSRankingTable.vue'
import { deviceNote } from '@/lib/device-notes'
import { formatMonitorNumber, monitorNumber, type OxiDNSOverview, type OxiDNSRankings } from '@/types/oxidnsMonitor'
const props = defineProps<{ overview: OxiDNSOverview; qps: number | null; recorder?: string; resetting?: boolean; notes?: Record<string, string> }>()
defineEmits<{ recorder: [value: string] }>()
const periodLabel = computed(() => props.overview.statistics ? '自本轮统计开始以来' : '自核心启动以来')
const rankings = computed<OxiDNSRankings | null>(() => props.overview.rankings && 'success' in props.overview.rankings && props.overview.rankings.success === true ? props.overview.rankings : null)
const clientRankings = computed(() => (rankings.value?.clients.rows || []).map(row => ({
  name: row.key, count: row.count, note: deviceNote(props.notes || {}, row.key)
})))
const rankingSampleDescription = computed(() => {
  if (!rankings.value) return ''
  const { clients, domains } = rankings.value
  if (clients.sample_size === domains.sample_size) {
    return '基于 ' + formatMonitorNumber(clients.sample_size) + ' 条 DNS 查询记录统计。'
  }
  return '客户端排行基于 ' + formatMonitorNumber(clients.sample_size) + ' 条查询记录；域名排行基于 ' + formatMonitorNumber(domains.sample_size) + ' 条查询记录。'
})
const rankingsDescription = computed(() => rankings.value
  ? '从本轮起点起，在记录器「' + rankings.value.recorder + '」保留的记录中，分别取最近 ' + formatMonitorNumber(rankings.value.sample_limit) + ' 条统计前 10 项；这是记录样本排行，不是全量累计。样本数是查询记录条数，不是客户端数或去重域名数。域名按问题名计数，一条查询可包含多个问题。' + (monitorNumber(rankings.value.retention_days) ? '核心保留期 ' + formatMonitorNumber(rankings.value.retention_days) + ' 天。' : '')
  : '排行依据核心保留的查询记录计算。')
const rankingsUnavailableReason = computed(() => props.overview.rankings && 'reason' in props.overview.rankings
  ? props.overview.rankings.reason : '当前 Agent 未返回排行数据，请确认已启用查询记录并升级 Agent。')
const upstreams = computed(() => [...(props.overview.upstreams || [])]
  .sort((left, right) => (right.requests ?? -1) - (left.requests ?? -1) || left.tag.localeCompare(right.tag) || left.upstream.localeCompare(right.upstream)).slice(0, 10))
const selectedCache = ref('__all__')
const selectedTag = computed(() => selectedCache.value.startsWith('cache:') ? selectedCache.value.slice(6) : '')
watch(() => props.overview.caches, caches => {
  if (!caches.some(cache => cache.tag === selectedTag.value)) selectedCache.value = '__all__'
})
const visibleCaches = computed(() => selectedCache.value === '__all__' ? props.overview.caches : props.overview.caches.filter(cache => cache.tag === selectedTag.value))
const cacheRate = computed(() => selectedCache.value === '__all__' ? props.overview.metrics.cache_hit_rate : visibleCaches.value[0]?.hit_rate ?? null)
</script>

<style scoped>
.oxidns-overview :deep(.cf-table th.text-right) {
  text-align: right;
}
</style>
