<script setup lang="ts">
import { computed, ref } from 'vue'
import { Bell, CircleX, CircleDashed, Loader2, RefreshCw, TriangleAlert } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import { SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import EmptyState from '@/components/common/EmptyState.vue'
import type { NotificationItem } from '@/api/notifications'
import { useProfileStore } from '@/stores/profile'
import { checkedAgo, relativeTime } from '@/lib/format'

const props = defineProps<{
  items: NotificationItem[]; reading: boolean; navigating: boolean
  loading: boolean; loaded: boolean; error: string; clock: number
}>()
const emit = defineEmits<{ read: [NotificationItem[]]; go: [NotificationItem]; refresh: []; closed: [] }>()
const profiles = useProfileStore()
const scope = ref('all')
const scoped = computed(() => props.items.filter(item => scope.value === 'all' || item.scope !== 'profile' || item.profile_id === scope.value))
const scopeLabel = (item: NotificationItem) => item.scope === 'shared' ? '共享资源' : item.scope === 'system' ? '系统' : item.profile_name || item.profile_id
const when = (item: NotificationItem) => {
  void props.clock
  return item.source === 'router-check' ? checkedAgo(item.checked_at) : relativeTime(item.checked_at || item.last_seen_at)
}
const tone = (item: NotificationItem) => item.severity === 'error' ? 'text-destructive-accent' : item.severity === 'warning' ? 'text-warning-accent' : 'text-muted-foreground'
const icon = (item: NotificationItem) => item.severity === 'error' ? CircleX : item.severity === 'warning' ? TriangleAlert : CircleDashed
</script>

<template>
  <SheetContent overlay-class="bg-transparent" class="w-full gap-0 bg-card shadow-overlay sm:max-w-[440px] transition-none ease-(--ease-flow) data-[state=open]:duration-200 data-[state=closed]:duration-150 data-[state=open]:fade-in-0 data-[state=closed]:fade-out-0 data-[state=open]:slide-in-from-right-3 data-[state=closed]:slide-out-to-right-2 max-sm:[&>button]:size-11 max-sm:[&>button]:right-2 max-sm:[&>button]:top-2 max-sm:[&>button]:grid max-sm:[&>button]:place-items-center" data-notifications-panel @close-auto-focus="emit('closed')">
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
        <Button variant="ghost" size="sm" class="max-sm:min-h-11" :disabled="reading || !scoped.length" @click="emit('read', scoped)">全部已读</Button>
        <Button variant="ghost" size="icon-sm" class="max-sm:min-h-11 max-sm:min-w-11" aria-label="刷新通知" :disabled="loading" @click="emit('refresh')"><Loader2 v-if="loading" class="size-4 animate-spin" /><RefreshCw v-else class="size-4" /></Button>
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
            <Button variant="ghost" size="sm" class="min-h-9 text-xs max-sm:min-h-11" aria-label="标为已读" :disabled="reading" @click="emit('read', [item])">已读</Button>
            <Button variant="outline" size="sm" class="min-h-9 shrink-0 text-xs max-sm:min-h-11" :disabled="navigating || reading" @click="emit('go', item)">{{ item.target.label }}</Button>
          </div>
        </div>
      </article>
    </div>
    <p class="m-0 shrink-0 border-t border-border px-4 py-3 text-xs text-muted-foreground">已读或恢复后自动清理；检查类提醒以标注时间为准。</p>
  </SheetContent>
</template>
