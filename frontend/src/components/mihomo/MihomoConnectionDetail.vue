<template>
  <Dialog :open="!!row" @update:open="value => { if (!value) emit('close') }">
    <DialogContent class="max-h-[90dvh] overflow-y-auto max-md:[&_[data-slot=dialog-close]]:min-h-11 max-md:[&_[data-slot=dialog-close]]:min-w-11" :class="tab === 'proxy' ? 'max-w-[960px]' : 'max-w-[720px]'">
      <DialogHeader>
        <DialogTitle class="flex flex-wrap items-center gap-2">
          连接详情
          <span v-if="row?.closedAt" class="chip">已关闭</span>
          <span v-else class="chip chip-ok">活跃</span>
        </DialogTitle>
        <DialogDescription class="break-all font-mono">{{ row?.target }}</DialogDescription>
      </DialogHeader>
      <Tabs v-if="row" v-model="tab" class="min-w-0">
        <TabsList class="grid min-h-11 w-full grid-cols-3">
          <TabsTrigger value="overview" class="min-h-11">概览</TabsTrigger>
          <TabsTrigger value="raw" class="min-h-11">原始数据</TabsTrigger>
          <TabsTrigger value="proxy" class="min-h-11">代理</TabsTrigger>
        </TabsList>
        <TabsContent value="overview" class="mt-4 space-y-3">
          <section v-for="section in sections" :key="section.title" class="rounded-xl border border-border bg-background p-4">
            <h3 class="m-0 mb-2 text-[13px] font-semibold text-foreground">{{ section.title }}</h3>
            <dl class="m-0 grid grid-cols-[6.5rem_minmax(0,1fr)] gap-x-3 gap-y-1.5 text-[13px]">
              <template v-for="item in section.items" :key="item.label">
                <dt class="text-muted-foreground">{{ item.label }}</dt>
                <dd class="m-0 break-all" :class="{ 'font-mono text-xs leading-5': item.mono }">{{ item.value }}</dd>
              </template>
            </dl>
          </section>
        </TabsContent>
        <TabsContent value="raw" class="mt-4">
          <pre class="m-0 max-h-[60dvh] overflow-auto rounded-xl border border-border bg-background p-4 font-mono text-xs leading-5 text-foreground">{{ rawText }}</pre>
        </TabsContent>
        <TabsContent value="proxy" class="mt-4">
          <MihomoConnectionProxy v-if="tab === 'proxy'" :agent-id="agentId" :row="row" @changed="emit('proxyChanged')" />
        </TabsContent>
      </Tabs>
      <!-- 原来只在列表每行的「⋯」菜单里；手机列表的行不放按钮，统一在这里操作。固定在弹窗底部，信息很长也不用滑到底 -->
      <div v-if="row" class="sticky -bottom-6 z-1 -mx-6 -mb-6 grid grid-cols-2 gap-2 border-t border-border bg-card px-6 py-3 max-sm:-bottom-4 max-sm:-mx-4 max-sm:-mb-4 max-sm:px-4 sm:flex sm:flex-wrap sm:justify-end" data-connection-actions>
        <Button variant="outline" class="min-h-11" :disabled="!ruleValues.length" :title="ruleValues.length ? undefined : '此连接没有可用的域名或真实目标 IP'" @click="emit('addRule', row, 'proxy')">加入代理规则集</Button>
        <Button variant="outline" class="min-h-11" :disabled="!ruleValues.length" :title="ruleValues.length ? undefined : '此连接没有可用的域名或真实目标 IP'" @click="emit('addRule', row, 'direct')">加入直连规则集</Button>
        <Button variant="outline" class="min-h-11" :disabled="!deviceAddress(row.metadata.sourceIP)" @click="emit('editNote', row)">编辑设备备注</Button>
        <Button v-if="!row.closedAt" variant="outline" class="min-h-11 border-destructive-accent/40 text-destructive-accent" @click="emit('disconnect', row)">断开连接</Button>
      </div>
    </DialogContent>
  </Dialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Button } from '@/components/ui/button'
import { connectionRuleValues, deviceAddress } from '@/lib/device-notes'
import { formatBytes } from '@/lib/format'
import { sourceLabel, type ConnectionRow } from '@/types/mihomo'
import MihomoConnectionProxy from './MihomoConnectionProxy.vue'

const props = defineProps<{ agentId: string; row: ConnectionRow | null; sourceName?: string }>()
const emit = defineEmits<{ close: []; proxyChanged: []; editNote: [row: ConnectionRow]; addRule: [row: ConnectionRow, target: 'proxy' | 'direct']; disconnect: [row: ConnectionRow] }>()
const ruleValues = computed(() => props.row ? connectionRuleValues(props.row.metadata) : [])
const tab = ref('overview')
watch(() => props.row?.id, () => { tab.value = 'overview' })

const speedText = (value: number) => formatBytes(value) + '/s'
const timeText = (value: number) => Number.isFinite(value) ? new Date(value).toLocaleString() : '—'
const dash = (value: string | undefined | null) => value || '—'

const ruleText = computed(() => props.row ? (props.row.rulePayload ? `${props.row.rule}: ${props.row.rulePayload}` : props.row.rule) : '')
const rawText = computed(() => props.row ? JSON.stringify(props.row.raw, null, 2) : '')

const sections = computed(() => {
  const row = props.row
  if (!row) return []
  const meta = row.metadata
  const started = Date.parse(row.start)
  const ended = row.closedAt ?? Date.now()
  const seconds = Math.max(0, Math.round((ended - started) / 1000))
  const inbound = [meta.inboundName, meta.inboundUser].filter(Boolean).join(' · ')
  return [
    {
      title: '基本',
      items: [
        { label: 'ID', value: row.id, mono: true },
        { label: '类型', value: `${meta.type} | ${meta.network}` },
        { label: '连接时间', value: timeText(started) },
        { label: row.closedAt ? '关闭时间' : '已持续', value: row.closedAt ? timeText(row.closedAt) : `${seconds} 秒` },
        { label: '规则', value: ruleText.value },
        { label: '入站', value: dash(inbound) }
      ]
    },
    {
      title: '来源与目的',
      items: [
        ...(props.sourceName ? [{ label: '设备备注', value: props.sourceName }] : []),
        { label: '源 IP', value: sourceLabel(row), mono: true },
        { label: '源端口', value: dash(meta.sourcePort), mono: true },
        { label: '主机', value: row.target, mono: true },
        { label: '嗅探域名', value: dash(meta.sniffHost), mono: true },
        { label: '解析地址', value: dash(meta.destinationIP), mono: true },
        { label: '解析类型', value: meta.destinationIP ? (meta.destinationIP.includes(':') ? 'IPv6' : 'IPv4') : '—' },
        { label: '远端地址', value: dash(meta.remoteDestination), mono: true },
        { label: '进程', value: dash(meta.processPath || meta.process), mono: true }
      ]
    },
    {
      title: '流量',
      items: [
        { label: '下载', value: formatBytes(row.download) },
        { label: '上传', value: formatBytes(row.upload) },
        { label: '下载速度', value: row.closedAt ? '—' : speedText(row.downSpeed) },
        { label: '上传速度', value: row.closedAt ? '—' : speedText(row.upSpeed) }
      ]
    },
    {
      title: '出站节点',
      items: [
        { label: '代理链', value: dash(row.chain) },
        { label: '出站节点', value: dash(row.chains[0]) }
      ]
    }
  ]
})
</script>
