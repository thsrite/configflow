<template>
  <Button variant="outline" class="max-sm:size-11 max-sm:gap-0" aria-label="批量测速地址" title="批量测速地址" :disabled="disabled || !eligibleGroups.length" @click="openDialog">
    <Timer class="size-4" />
    <span class="max-sm:sr-only">批量测速地址</span>
  </Button>

  <Dialog :open="open" @update:open="value => { if (!saving) open = value }">
    <DialogContent class="max-w-[560px]" :show-close-button="!saving" @escape-key-down="guardClose" @interact-outside="guardClose">
      <DialogHeader>
        <DialogTitle>批量修改测速地址</DialogTitle>
        <DialogDescription>选择当前配置空间中的策略组，统一设置测试 URL。跟随组沿用来源组的测速设置。</DialogDescription>
      </DialogHeader>

      <form class="grid min-w-0 gap-4" novalidate @submit.prevent="save">
        <div class="grid min-w-0 gap-1.5">
          <Label for="batch-health-url">测试 URL</Label>
          <Input id="batch-health-url" v-model="url" type="url" class="min-h-11 font-mono text-xs" :disabled="loading || saving" placeholder="https://www.gstatic.com/generate_204" />
        </div>

        <fieldset class="min-w-0" :disabled="saving">
          <legend class="sr-only">应用到策略组</legend>
          <label class="flex min-h-11 cursor-pointer items-center gap-2 text-sm">
            <Checkbox :model-value="allSelected" :disabled="saving" @update:model-value="value => { selectedIds = value === true ? eligibleGroups.map(group => group.id) : [] }" />
            全选 <span class="num text-muted-foreground">{{ selectedIds.length }} / {{ eligibleGroups.length }}</span>
          </label>
          <div class="max-h-[30dvh] overflow-y-auto overscroll-contain rounded-lg border border-border">
            <label v-for="group in eligibleGroups" :key="group.id" class="flex min-h-11 cursor-pointer items-center gap-3 border-b border-border px-3 py-2 last:border-b-0 hover:bg-accent/40">
              <Checkbox :model-value="selectedIds.includes(group.id)" :disabled="saving" :aria-label="`选择 ${group.name}`" @update:model-value="value => toggleGroup(group.id, value === true)" />
              <span class="min-w-0 flex-1">
                <span class="block break-words text-sm [overflow-wrap:anywhere]">{{ group.name }}</span>
                <span class="block break-all font-mono text-xs text-muted-foreground">{{ group.url || '未设置' }}</span>
              </span>
            </label>
          </div>
        </fieldset>

        <p v-if="error" role="alert" class="m-0 text-sm text-destructive-accent">{{ error }}</p>
        <p class="m-0 text-xs text-muted-foreground">保存到当前配置；设备需重新生成并发布后生效。</p>
        <DialogFooter class="flex-row justify-end">
          <Button type="button" variant="outline" class="min-h-11" :disabled="saving" @click="open = false">取消</Button>
          <Button type="submit" class="min-h-11" :disabled="loading || saving || !selectedIds.length || !url.trim()">
            <Loader2 v-if="loading || saving" class="size-4 animate-spin" />
            {{ saving ? '保存中…' : loading ? '读取默认地址…' : `应用到 ${selectedIds.length} 个策略组` }}
          </Button>
        </DialogFooter>
      </form>
    </DialogContent>
  </Dialog>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref } from 'vue'
import { Loader2, Timer } from '@lucide/vue'
import api, { proxyGroupApi } from '@/api'
import type { ProxyGroup } from '@/types'
import { notify } from '@/lib/feedback'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'

const props = defineProps<{ groups: ProxyGroup[]; profileId: string; disabled?: boolean }>()
const emit = defineEmits<{ updated: [patch: { ids: string[]; url: string }] }>()
const eligibleGroups = computed(() => props.groups.filter(group =>
  group.id && !group.follow_group && ['url-test', 'fallback', 'load-balance'].includes(group.type)
))
const open = ref(false)
const loading = ref(false)
const saving = ref(false)
const url = ref('')
const selectedIds = ref<string[]>([])
const error = ref('')
const allSelected = computed(() => eligibleGroups.value.length > 0 && selectedIds.value.length === eligibleGroups.value.length)
let active = true
let openVersion = 0
onUnmounted(() => { active = false })

async function openDialog() {
  if (props.disabled || !eligibleGroups.value.length || saving.value) return
  const version = ++openVersion
  selectedIds.value = eligibleGroups.value.map(group => group.id)
  url.value = ''
  error.value = ''
  open.value = true
  loading.value = true
  try {
    const { data } = await api.get('/proxy-groups/health-defaults')
    if (active && version === openVersion) url.value = data.group.url
  } catch {
    if (active && version === openVersion) error.value = '无法读取默认测速地址，请输入地址或关闭后重试'
  } finally {
    if (active && version === openVersion) loading.value = false
  }
}

function toggleGroup(id: string, checked: boolean) {
  if (saving.value) return
  selectedIds.value = checked ? [...new Set([...selectedIds.value, id])] : selectedIds.value.filter(value => value !== id)
}

function guardClose(event: Event) {
  if (saving.value) event.preventDefault()
}

async function save() {
  if (loading.value || saving.value || !selectedIds.value.length) return
  error.value = ''
  const nextUrl = url.value.trim()
  try {
    const parsed = new URL(nextUrl)
    if (!['http:', 'https:'].includes(parsed.protocol) || parsed.username || parsed.password) throw new Error()
  } catch {
    error.value = '请输入不含用户名或密码的 HTTP(S) 测速地址'
    return
  }
  const ids = [...selectedIds.value]
  saving.value = true
  try {
    const { data } = await proxyGroupApi.batchHealthUrl({ ids, url: nextUrl }, props.profileId)
    if (!active) return
    if (!data.success) throw new Error(data.message || '批量修改测速地址失败')
    emit('updated', { ids, url: nextUrl })
    open.value = false
    notify.success(`已更新 ${ids.length} 个策略组的测速地址`)
  } catch (failure: any) {
    if (active) error.value = failure.response?.data?.message || failure.message || '批量修改测速地址失败'
  } finally {
    saving.value = false
  }
}
</script>
