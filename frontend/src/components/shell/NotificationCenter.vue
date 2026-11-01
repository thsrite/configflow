<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRouter, isNavigationFailure } from 'vue-router'
import { Bell, CircleX, CircleDashed, Loader2, RefreshCw, TriangleAlert } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle, SheetTrigger } from '@/components/ui/sheet'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import EmptyState from '@/components/common/EmptyState.vue'
import { notificationsApi, type NotificationItem } from '@/api/notifications'
import { useProfileStore } from '@/stores/profile'
import { canReloadPage } from '@/lib/pageReloadGuard'
import { checkedAgo, relativeTime } from '@/lib/format'
import { notify } from '@/lib/feedback'

const router = useRouter()
const profiles = useProfileStore()
const open = ref(false)
const scope = ref('all')
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
const scoped = computed(() => items.value.filter(item => scope.value === 'all' || item.scope !== 'profile' || item.profile_id === scope.value))
const scopeLabel = (item: NotificationItem) => item.scope === 'shared' ? '共享资源' : item.scope === 'system' ? '系统' : item.profile_name || item.profile_id
const when = (item: NotificationItem) => {
  void clock.value
  return item.source === 'router-check' ? checkedAgo(item.checked_at) : relativeTime(item.checked_at || item.last_seen_at)
}
const tone = (item: NotificationItem) => item.severity === 'error' ? 'text-destructive-accent' : item.severity === 'warning' ? 'text-warning-accent' : 'text-muted-foreground'
const icon = (item: NotificationItem) => item.severity === 'error' ? CircleX : item.severity === 'warning' ? TriangleAlert : CircleDashed

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
    <SheetContent overlay-class="bg-transparent" class="w-full gap-0 bg-card shadow-overlay sm:max-w-[440px] transition-none ease-(--ease-flow) data-[state=open]:duration-200 data-[state=closed]:duration-150 data-[state=open]:fade-in-0 data-[state=closed]:fade-out-0 data-[state=open]:slide-in-from-right-3 data-[state=closed]:slide-out-to-right-2 max-sm:[&>button]:size-11 max-sm:[&>button]:right-2 max-sm:[&>button]:top-2 max-sm:[&>button]:grid max-sm:[&>button]:place-items-center" data-notifications-panel @close-auto-focus="followDestination">
      <SheetHeader class="shrink-0 pr-12">
        <SheetTitle class="font-display text-2xl">通知中心</SheetTitle>
        <SheetDescription>查看失败和待操作事项，直接前往对应设置。</SheetDescription>
      </SheetHeader>
      <div class="flex shrink-0 flex-wrap items-center justify-between gap-2 border-b border-border px-4 pb-3">
        <Select v-model="scope"><SelectTrigger class="h-9 w-[185px] max-sm:min-h-11" aria-label="通知配置空间"><SelectValue /></SelectTrigger><SelectContent>
          <SelectItem value="all">全部配置空间</SelectItem>
          <SelectItem v-for="profile in profiles.profiles.value" :key="profile.id" :value="profile.id">{{ profile.name }}与共享事项</SelectItem>
        </SelectContent></Select>
        <div class="flex items-center gap-1">
          <Button variant="ghost" size="sm" class="max-sm:min-h-11" :disabled="reading || !scoped.length" @click="read(scoped)">全部已读</Button>
          <Button variant="ghost" size="icon-sm" class="max-sm:min-h-11 max-sm:min-w-11" aria-label="刷新通知" :disabled="loading" @click="load"><Loader2 v-if="loading" class="size-4 animate-spin" /><RefreshCw v-else class="size-4" /></Button>
        </div>
      </div>
      <div class="min-h-0 flex-1 overflow-y-auto px-4 pb-4">
        <p v-if="error" role="alert" class="my-3 text-xs leading-relaxed text-warning-accent">{{ error }}</p>
        <p v-if="loading && !loaded" role="status" class="py-10 text-center text-sm text-muted-foreground">正在读取通知…</p>
        <EmptyState v-else-if="loaded && !scoped.length" :icon="Bell" title="暂无新通知" description="失败或需要手动处理时会在这里提醒。" />
        <article v-for="item in scoped" :key="item.id" class="border-b border-border py-4 last:border-0" :data-notification="item.id">
          <div class="flex items-center gap-2 text-xs" :class="tone(item)"><component :is="icon(item)" class="size-4 shrink-0" aria-hidden="true" /><span>{{ item.status_label }}</span></div>
          <p class="mb-1 mt-2 break-words text-sm font-semibold">{{ item.title }}</p>
          <p class="m-0 whitespace-pre-wrap break-words text-[13px] leading-relaxed">{{ item.message }}</p>
          <p v-if="item.impact" class="mt-1 break-words text-xs text-muted-foreground">{{ item.impact }}</p>
          <p v-if="item.failure_count && item.failure_count > 1" class="mt-1 text-xs text-muted-foreground">连续失败 {{ item.failure_count }} 次</p>
          <p v-if="item.stale && item.state === 'pending'" class="mt-1 text-xs text-warning-accent">配置已变化，以下是上次检查发现的问题，请前往重新检查。</p>
          <div class="mt-3 flex flex-wrap items-center justify-between gap-2">
            <span class="min-w-0 break-words text-xs text-muted-foreground">{{ scopeLabel(item) }} · {{ when(item) }}</span>
            <div class="flex flex-wrap items-center gap-1">
              <Button variant="ghost" size="sm" class="min-h-9 text-xs max-sm:min-h-11" aria-label="标为已读" :disabled="reading" @click="read([item])">已读</Button>
              <Button variant="outline" size="sm" class="min-h-9 shrink-0 text-xs max-sm:min-h-11" :disabled="navigating || reading" @click="go(item)">{{ item.target.label }}</Button>
            </div>
          </div>
        </article>
      </div>
      <p class="m-0 shrink-0 border-t border-border px-4 py-3 text-xs text-muted-foreground">已读或恢复后自动清理；检查类提醒以标注时间为准。</p>
    </SheetContent>
  </Sheet>
</template>
