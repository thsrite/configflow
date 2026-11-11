<template>
  <DropdownMenu>
    <DropdownMenuTrigger as-child>
      <Button variant="outline" class="cf-choice-height min-h-11 max-sm:w-11 max-sm:min-w-0 max-sm:gap-0" :disabled="disabled || !!running" :aria-label="running || '内核操作'" :title="running || '内核操作'" :aria-busy="!!running">
        <Loader2 v-if="running" class="size-4 animate-spin" /><Cpu v-else class="size-4" /><span class="max-sm:sr-only">{{ running || '内核' }}</span>
      </Button>
    </DropdownMenuTrigger>
    <DropdownMenuContent align="end" class="min-w-[180px]">
      <DropdownMenuItem @select="reloadConfig"><FileCog class="size-4" />重载配置</DropdownMenuItem>
      <DropdownMenuItem @select="flush('fakeip')"><Eraser class="size-4" />清空 FakeIP 缓存</DropdownMenuItem>
      <DropdownMenuItem @select="flush('dns')"><Eraser class="size-4" />清空 DNS 缓存</DropdownMenuItem>
      <DropdownMenuSeparator />
      <DropdownMenuItem @select="upgradeCore"><ArrowUpCircle class="size-4" />更新内核</DropdownMenuItem>
      <DropdownMenuItem class="text-destructive-accent" @select="restartCore"><Power class="size-4" />重启内核</DropdownMenuItem>
    </DropdownMenuContent>
  </DropdownMenu>
</template>

<script setup lang="ts">
import { onUnmounted, ref, watch } from 'vue'
import { ArrowUpCircle, Cpu, Eraser, FileCog, Loader2, Power } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger
} from '@/components/ui/dropdown-menu'
import { agentApi, mihomoPanelApi } from '@/api'
import { confirm, confirmDanger, notify } from '@/lib/feedback'
import { panelError } from '@/types/mihomo'

const props = defineProps<{ agentId: string; disabled?: boolean }>()
/** 内核状态可能已变化（重载、重启、更新后），父组件据此刷新版本、模式和各标签数据 */
const emit = defineEmits<{ changed: []; 'status-change': [status: string] }>()

/** 正在执行的操作名，同一时间只允许一个内核操作 */
const running = ref('')
watch(running, status => emit('status-change', status))
let disposed = false
onUnmounted(() => { disposed = true })

async function run(label: string, action: () => Promise<void>) {
  running.value = label
  try {
    await action()
  } finally {
    running.value = ''
  }
}

/** 重启、更新后内核会短暂不可用，等控制接口恢复再刷新 */
async function waitForCore(timeoutMs = 60000): Promise<string | null> {
  const deadline = Date.now() + timeoutMs
  // 先等一下，避免读到正在退出的旧进程
  await new Promise(resolve => setTimeout(resolve, 2000))
  while (!disposed && Date.now() < deadline) {
    try {
      const { data } = await mihomoPanelApi.version(props.agentId)
      return data.data?.version || ''
    } catch {
      await new Promise(resolve => setTimeout(resolve, 2000))
    }
  }
  return null
}

async function reloadConfig() {
  if (!await confirm('从内核当前使用的配置文件重新加载。代理组选择会重置为配置默认值，部分连接可能中断。', { title: '重载配置', confirmText: '重载' })) return
  await run('重载中…', async () => {
    try {
      await mihomoPanelApi.reloadConfig(props.agentId)
      notify.success('配置已重载')
      emit('changed')
    } catch (failure) {
      notify.error('重载配置失败', panelError(failure))
    }
  })
}

async function flush(kind: 'fakeip' | 'dns') {
  const label = kind === 'fakeip' ? 'FakeIP' : 'DNS'
  await run('清空中…', async () => {
    try {
      await (kind === 'fakeip' ? mihomoPanelApi.flushFakeIP(props.agentId) : mihomoPanelApi.flushDNS(props.agentId))
      notify.success(`已清空 ${label} 缓存`)
    } catch (failure) {
      notify.error(`清空 ${label} 缓存失败`, panelError(failure))
    }
  })
}

async function upgradeCore() {
  if (!await confirm('Mihomo 会从官方发布页下载新版本并替换自身，完成后自动重启，期间网络会短暂中断。面板可显示请求和重新上线阶段；内核未提供逐文件下载、校验进度。', { title: '更新内核', confirmText: '更新' })) return
  await run('请求 Mihomo 更新…', async () => {
    try {
      await mihomoPanelApi.upgradeCore(props.agentId)
    } catch (failure) {
      notify.error('更新内核失败', panelError(failure))
      return
    }
    running.value = '等待 Mihomo 重新上线…'
    const version = await waitForCore(90000)
    if (version === null) notify.warning('内核已开始更新，但暂未恢复连接', '请稍后刷新，或到 Agent 页查看日志。')
    else notify.success('内核已更新', version ? `当前版本 ${version}` : undefined)
    emit('changed')
  })
}

async function restartCore() {
  if (!await confirmDanger('通过 Agent 重启 Mihomo 服务，所有连接会断开，网络会短暂中断。', { title: '重启内核', confirmText: '重启' })) return
  await run('请求重启 Mihomo…', async () => {
    try {
      const { data } = await agentApi.restart(props.agentId)
      if (data?.success === false) throw new Error(data.message || '重启失败')
    } catch (failure) {
      notify.error('重启内核失败', panelError(failure))
      return
    }
    running.value = '等待 Mihomo 重新上线…'
    const version = await waitForCore()
    if (version === null) notify.warning('已发送重启，但内核暂未恢复', '请稍后刷新，或到 Agent 页查看日志。')
    else notify.success('内核已重启')
    emit('changed')
  })
}
</script>
