<template>
  <Dialog :open="open" @update:open="close">
    <DialogContent class="flex max-h-[85dvh] max-w-xl flex-col gap-0 overflow-hidden p-0 max-sm:p-0" :show-close-button="!saving"
      @escape-key-down="event => { if (saving) event.preventDefault() }"
      @interact-outside="event => { if (saving) event.preventDefault() }">
      <DialogHeader class="shrink-0 border-b border-border px-6 pb-4 pt-6 max-sm:px-4">
        <DialogTitle>{{ ids.length > 1 ? `删除 ${ids.length} 个节点` : '删除节点' }}</DialogTitle>
        <DialogDescription>确认后解除下列引用并删除节点，删除后无法恢复。</DialogDescription>
      </DialogHeader>
      <div class="min-h-0 space-y-4 overflow-y-auto overscroll-contain px-6 py-4 text-sm max-sm:px-4">
        <p v-if="loading" role="status" class="m-0 text-muted-foreground">正在检查节点引用…</p>
        <p v-if="error" role="alert" class="m-0 break-words text-destructive-accent">{{ error }}</p>
        <template v-if="preview">
          <section class="space-y-2">
            <h3 class="m-0 font-medium">待删除节点</h3>
            <ul class="m-0 space-y-1 pl-4"><li v-for="node in preview.nodes" :key="node.id" class="break-words">{{ node.name }}</li></ul>
          </section>
          <section class="space-y-2 border-t border-border pt-4">
            <h3 class="m-0 font-medium">将解除的引用</h3>
            <ul v-if="aggregations.length || preview.groups.length" class="m-0 space-y-1 pl-4">
              <li v-for="usage in aggregations" :key="usage.id" class="break-words">聚合「{{ usage.name }}」</li>
              <li v-for="group in preview.groups" :key="`${group.profile_id}/${group.id}`" class="break-words">{{ group.profile_name }} → 策略组「{{ group.name }}」</li>
            </ul>
            <p v-else class="m-0 text-muted-foreground">没有需要清理的聚合或普通策略组引用。</p>
          </section>
          <section v-if="profiles.length" class="space-y-2 border-t border-border pt-4">
            <h3 class="m-0 font-medium">受影响的配置空间</h3>
            <p class="m-0 break-words">{{ profiles.map(profile => profile.name).join('、') }}</p>
            <p class="m-0 text-xs leading-relaxed text-muted-foreground">包括通过聚合间接使用节点的配置。之后生成或更新来源会采用删除后的结果。</p>
          </section>
          <div v-if="preview.empty_sources.length" role="alert" class="space-y-2 rounded-md bg-warning-soft p-3 text-warning-accent">
            <p class="m-0">以下聚合或策略组将没有已选择的来源，请确认符合预期：</p>
            <ul class="m-0 space-y-1 pl-4"><li v-for="source in preview.empty_sources" :key="source" class="break-words">{{ source }}</li></ul>
          </div>
          <div v-if="preview.blockers.length" role="alert" class="space-y-2 rounded-md bg-destructive-soft p-3 text-destructive-accent">
            <p class="m-0 font-medium">需要先处理以下依赖</p>
            <ul class="m-0 space-y-1 pl-4"><li v-for="blocker in preview.blockers" :key="blocker" class="break-words">{{ blocker }}</li></ul>
          </div>
          <p class="m-0 text-xs leading-relaxed text-muted-foreground">只移除所选节点及其引用，保留聚合和策略组。本次不自动向设备发布配置。</p>
        </template>
      </div>
      <DialogFooter class="shrink-0 border-t border-border p-4">
        <Button variant="outline" class="min-h-11" :disabled="saving" @click="close(false)">取消</Button>
        <Button v-if="error || preview?.blockers.length" variant="outline" class="min-h-11" :disabled="loading || saving" @click="loadPreview">重新读取影响</Button>
        <Button variant="destructive" class="min-h-11" :disabled="loading || saving || !preview || !!preview.blockers.length" @click="remove">
          <Loader2 v-if="saving" class="size-4 animate-spin" />{{ saving ? '正在删除…' : hasReferences ? '解除引用并删除' : '删除节点' }}
        </Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { Loader2 } from '@lucide/vue'
import { nodeApi, type NodeDeletionPreview } from '@/api'
import { notify } from '@/lib/feedback'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'

const emit = defineEmits<{ deleted: [ids: string[]] }>()
const open = ref(false)
const ids = ref<string[]>([])
const preview = ref<NodeDeletionPreview | null>(null)
const loading = ref(false)
const saving = ref(false)
const error = ref('')
let request = 0
const aggregations = computed(() => preview.value?.usages.filter(usage => usage.kind === 'aggregation') || [])
const profiles = computed(() => preview.value?.usages.filter(usage => usage.kind === 'profile') || [])
const hasReferences = computed(() => !!aggregations.value.length || !!preview.value?.groups.length)

function close(value: boolean) {
  if (saving.value) return
  open.value = value
  if (!value) request++
}

async function loadPreview() {
  const current = ++request
  loading.value = true
  error.value = ''
  preview.value = null
  try {
    const { data } = await nodeApi.previewDeletion([...ids.value])
    if (current === request && open.value) preview.value = data
  } catch (failure: any) {
    if (current === request) error.value = failure?.response?.data?.message || failure?.message || '读取删除影响失败'
  } finally {
    if (current === request) loading.value = false
  }
}

function show(nodeIds: string[]) {
  if (saving.value || !nodeIds.length) return
  ids.value = [...nodeIds]
  open.value = true
  void loadPreview()
}

async function remove() {
  if (!preview.value || loading.value || saving.value || preview.value.blockers.length) return
  saving.value = true
  try {
    const { data } = await nodeApi.deleteWithReferences([...ids.value], preview.value.confirmation)
    notify.success(`已删除 ${data.deleted_ids.length} 个节点，并清理相关引用`)
    open.value = false
    emit('deleted', data.deleted_ids)
  } catch (failure: any) {
    error.value = failure?.response?.data?.message || failure?.message || '删除失败，请重新读取影响后重试'
    // Never resubmit an outdated confirmation, including after an uncertain response.
    preview.value = null
  } finally {
    saving.value = false
  }
}

defineExpose({ show })
</script>
