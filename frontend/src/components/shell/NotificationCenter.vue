<script setup lang="ts">
import { computed, defineAsyncComponent, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRouter, isNavigationFailure } from 'vue-router'
import { Bell } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import { Sheet, SheetTrigger } from '@/components/ui/sheet'
import { notificationsApi, type NotificationItem } from '@/api/notifications'
import { canReloadPage } from '@/lib/pageReloadGuard'
import { notify } from '@/lib/feedback'

const router = useRouter()
const open = ref(false)
const NotificationPanel = defineAsyncComponent(() => import('./NotificationPanel.vue'))
const hasOpened = computed<boolean>(previous => open.value || !!previous)
const items = ref<NotificationItem[]>([])
const loading = ref(false)
const loaded = ref(false)
const error = ref('')
const reading = ref(false)
const navigating = ref(false)
const clock = ref(Date.now())
let timer: ReturnType<typeof setInterval> | undefined
let controller: AbortController | undefined
let disposed = false
let destination: NotificationItem | undefined
const acknowledged = new Map<string, string>()
async function load() {
  if (loading.value || disposed) return
  loading.value = true
  controller = new AbortController()
  try {
    const { data } = await notificationsApi.list(controller.signal)
    if (disposed) return
    if (!data.success || !Array.isArray(data.items)) throw new Error('通知响应无效')
    items.value = data.items.filter(item => item.state === 'pending' && !item.read && acknowledged.get(item.id) !== item.version)
    for (const id of acknowledged.keys()) {
      if (!data.items.some(item => item.id === id)) acknowledged.delete(id)
    }
    loaded.value = true
    error.value = ''
  } catch (failure: any) {
    if (!disposed) error.value = failure?.response?.data?.message || '无法读取通知，保留上次结果，请重试。'
  } finally { if (!disposed) loading.value = false }
}
async function read(rows: NotificationItem[]) {
  if (reading.value) return
  const versions = Object.fromEntries(rows.filter(item => !item.read).map(item => [item.id, item.version]))
  if (!Object.keys(versions).length) return
  reading.value = true
  try {
    await notificationsApi.read(versions)
    if (!disposed) {
      Object.entries(versions).forEach(([id, version]) => acknowledged.set(id, version))
      items.value = items.value.filter(item => versions[item.id] !== item.version)
    }
  } catch { if (!disposed) notify.error('未能标为已读，请重试。') }
  finally { if (!disposed) reading.value = false }
}
const paths = new Set(['/subscriptions', '/rule-library', '/agents', '/routers', '/system-settings'])
function go(item: NotificationItem) {
  if (navigating.value || !paths.has(item.target.path)) return
  navigating.value = true
  destination = item
  open.value = false
}
async function followDestination() {
  const item = destination
  if (!item) return
  destination = undefined
  // Reka emits closeAutoFocus after the exit animation. Let the old dialog
  // unmount before navigation mounts another overlay or focuses its target.
  await nextTick()
  if (disposed) return
  try {
    if (!canReloadPage()) {
      notify.warning('请先保存或关闭当前编辑窗口，再打开通知中的设置。')
      return
    }
    const failure = await router.push({ path: item.target.path, query: item.target.query })
    if (!isNavigationFailure(failure)) await read([item])
  } catch { notify.error('未能打开对应设置，请重新尝试。') }
  finally { navigating.value = false }
}
function onVisibility() { if (!document.hidden) void load() }
watch(open, value => { if (value) void load() })
onMounted(() => {
  void load()
  timer = setInterval(() => { clock.value = Date.now(); if (!document.hidden) void load() }, 30000)
  document.addEventListener('visibilitychange', onVisibility)
})
onUnmounted(() => { disposed = true; controller?.abort(); clearInterval(timer); document.removeEventListener('visibilitychange', onVisibility) })
</script>

<template>
  <Sheet v-model:open="open">
    <SheetTrigger as-child>
      <Button variant="ghost" size="icon-sm" class="relative min-h-11 min-w-11" :aria-label="error ? '通知，状态更新失败' : `通知，${items.length} 条未读`" title="通知中心" data-notifications-trigger>
        <Bell class="size-[17px]" />
        <span v-if="items.length" class="num absolute right-0 top-0 min-w-4 rounded-full px-1 text-[10px] leading-4" :class="items.some(item => item.severity === 'error') ? 'bg-destructive text-destructive-foreground' : 'bg-warning text-warning-foreground'" data-notification-count>{{ items.length > 99 ? '99+' : items.length }}</span>
        <span v-else-if="error" class="absolute right-0 top-0 text-xs text-warning-accent" aria-hidden="true">!</span>
      </Button>
    </SheetTrigger>
    <NotificationPanel v-if="hasOpened" :items="items" :reading="reading" :navigating="navigating"
      :loading="loading" :loaded="loaded" :error="error" :clock="clock"
      @read="read" @go="go" @refresh="load" @closed="followDestination" />
  </Sheet>
</template>
