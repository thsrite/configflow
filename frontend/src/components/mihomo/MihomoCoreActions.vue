<template>
  <DropdownMenu>
    <DropdownMenuTrigger as-child>
      <Button variant="outline" class="relative cf-choice-height min-h-11 max-sm:w-11 max-sm:min-w-0 max-sm:gap-0" :disabled="disabled || !!running" :aria-label="buttonLabel" :title="buttonLabel" :aria-busy="!!running">
        <Loader2 v-if="running" class="size-4 animate-spin" /><Cpu v-else class="size-4" /><span class="max-sm:sr-only">{{ running || '内核' }}</span>
        <span v-if="update && !running" data-testid="core-update-dot" class="absolute right-1 top-1 size-2 rounded-full bg-primary-accent" aria-hidden="true" />
      </Button>
    </DropdownMenuTrigger>
    <DropdownMenuContent align="end" class="min-w-[180px]">
      <DropdownMenuItem @select="reloadConfig"><FileCog class="size-4" />重载配置</DropdownMenuItem>
      <DropdownMenuItem @select="flush('fakeip')"><Eraser class="size-4" />清空 FakeIP 缓存</DropdownMenuItem>
      <DropdownMenuItem @select="flush('dns')"><Eraser class="size-4" />清空 DNS 缓存</DropdownMenuItem>
      <DropdownMenuSeparator />
      <DropdownMenuItem v-if="update" class="text-primary-accent" @select="offerUpdate(update)">
        <ArrowUpCircle class="size-4" />更新到 <span class="font-mono">{{ update.latest }}</span>
      </DropdownMenuItem>
      <DropdownMenuItem v-else @select="checkUpdate"><ArrowUpCircle class="size-4" />检查更新</DropdownMenuItem>
      <DropdownMenuItem class="text-destructive-accent" @select="restartCore"><Power class="size-4" />重启内核</DropdownMenuItem>
    </DropdownMenuContent>
  </DropdownMenu>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { ArrowUpCircle, Cpu, Eraser, FileCog, Loader2, Power } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger
} from '@/components/ui/dropdown-menu'
import { agentApi, mihomoPanelApi } from '@/api'
import { confirm, confirmDanger, notify } from '@/lib/feedback'
import { panelError } from '@/types/mihomo'

/** coreVersion：面板读到的内核版本，读到后自动检查一次更新 */
const props = defineProps<{ agentId: string; disabled?: boolean; coreVersion?: string }>()
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

const UPGRADE_NOTE = 'Mihomo 会从官方发布页下载新版本并替换自身，完成后自动重启，期间网络会短暂中断。面板可显示请求和重新上线阶段；内核未提供逐文件下载、校验进度。'

interface CoreUpdateCheck { current: string; latest: string; channel: string; has_update: boolean; error: string }

/** 自动检查发现的新版本：按钮上显示圆点，菜单项改为「更新到 …」 */
const update = ref<CoreUpdateCheck | null>(null)
const buttonLabel = computed(() => running.value || (update.value ? `内核操作：有新版本 ${update.value.latest}` : '内核操作'))
const channelNote = (check: CoreUpdateCheck) => check.channel === 'alpha' ? '（Alpha 通道）' : ''

/** 打开面板、切换设备或内核版本变化后自动检查（服务端缓存 1 天），失败不打扰 */
let checkSeq = 0
watch(() => [props.agentId, props.coreVersion] as const, async ([agentId, version]) => {
  const seq = ++checkSeq
  update.value = null
  if (!agentId || !version || version === '未知版本') return
  try {
    const { data } = await mihomoPanelApi.checkCoreUpdate(agentId)
    if (seq === checkSeq && !disposed) update.value = data.data?.has_update ? data.data : null
  } catch {
    // 自动检查只做提示；手动「检查更新」会显示原因
  }
}, { immediate: true })

async function offerUpdate(check: CoreUpdateCheck) {
  if (await confirm(`发现新版本 ${check.latest}${channelNote(check)}，当前 ${check.current}。\n\n${UPGRADE_NOTE}`, { title: '内核有更新', confirmText: '更新' })) await upgradeCore()
}

/** 手动检查跳过缓存：有新版才提供更新，查不到时可让内核自己检查 */
async function checkUpdate() {
  let check: CoreUpdateCheck | undefined
  checkSeq++
  await run('检查更新…', async () => {
    try {
      const { data } = await mihomoPanelApi.checkCoreUpdate(props.agentId, true)
      check = data.data
    } catch (failure) {
      notify.error('检查更新失败', panelError(failure))
    }
  })
  if (!check) return
  update.value = check.has_update ? check : null
  if (check.error) {
    if (await confirm(`${check.error}\n\n当前版本 ${check.current}${channelNote(check)}。仍可让 Mihomo 自己联网检查，有新版时直接更新；已是最新时会提示无需更新。\n\n${UPGRADE_NOTE}`, { title: '无法检查更新', confirmText: '仍然更新' })) await upgradeCore()
    return
  }
  if (!check.has_update) {
    notify.success('已是最新版本', `当前版本 ${check.current}${channelNote(check)}`)
    return
  }
  await offerUpdate(check)
}

async function upgradeCore() {
  await run('请求 Mihomo 更新…', async () => {
    try {
      await mihomoPanelApi.upgradeCore(props.agentId)
    } catch (failure) {
      const message = panelError(failure)
      // 内核自己比对后拒绝：版本与发布页一致，不算失败
      const latest = /already using latest version\s*(\S*)/.exec(message)
      if (latest) {
        update.value = null
        notify.success('已是最新版本', latest[1] ? `当前版本 ${latest[1]}` : undefined)
      }
      else notify.error('更新内核失败', message)
      return
    }
    update.value = null
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
