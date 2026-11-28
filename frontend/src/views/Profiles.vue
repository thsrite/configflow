<template>
  <LoadingRows v-if="!profileStore.profileReady.value" :rows="3" />
  <div v-else>
    <PageHeader title="配置空间">
      <template #actions>
        <DropdownMenu v-if="isPhone">
          <DropdownMenuTrigger as-child>
            <Button variant="outline" class="border-border/60 bg-background/40" aria-label="导入配置">
              <Upload class="size-4" />
              导入
              <ChevronDown class="size-3.5" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem @select="pickImportFile">
              <Upload class="size-4" />
              导入备份到当前
            </DropdownMenuItem>
            <DropdownMenuItem @select="openClientImport">
              <FileInput class="size-4" />
              导入客户端配置
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
        <template v-else>
          <Button variant="outline" class="border-border/60 bg-background/40" @click="pickImportFile">
            <Upload class="size-4" />
            导入备份到当前
          </Button>
          <Button variant="outline" class="border-border/60 bg-background/40" @click="openClientImport">
            <FileInput class="size-4" />
            导入客户端配置
          </Button>
        </template>
        <Button aria-label="新建配置空间" @click="openCreate">
          <Plus class="size-4" />
          {{ isPhone ? '新建' : '新建配置空间' }}
        </Button>
        <input ref="importInput" type="file" accept="application/json,.json" hidden @change="importProfile" />
      </template>
    </PageHeader>

    <LoadingRows v-if="loading && !profiles.length" :rows="3" />

    <SectionCard v-else-if="!profiles.length" :padded="false">
      <EmptyState
        :icon="Boxes"
        title="还没有配置空间"
        description="先创建一个配置空间，为家庭或办公等场景选择资源、设置策略和规则。"
      >
        <Button @click="openCreate">
          <Plus class="size-4" />
          新建配置空间
        </Button>
      </EmptyState>
    </SectionCard>

    <div v-else class="grid grid-cols-[repeat(auto-fill,minmax(320px,1fr))] gap-4 max-md:grid-cols-1 max-sm:gap-2">
      <div
        v-for="(profile, index) in profiles"
        :key="profile.id"
        class="cf-list-in"
        :style="listItemStyle(index)"
        :class="[
          'group relative flex flex-col gap-4 overflow-hidden rounded-[18px] border bg-card/90 p-5 shadow-surface transition-colors duration-200 hover:border-border-strong max-sm:gap-2 max-sm:rounded-xl max-sm:p-3',
          profile.id === activeProfileId
            ? 'border-primary-accent'
            : 'border-border'
        ]"
      >
        <header class="flex items-start justify-between gap-3">
          <div class="flex min-w-0 flex-1 items-center gap-3">
            <span
              class="relative grid size-10 shrink-0 place-items-center rounded-xl border border-border/50 bg-background/50 text-primary-accent max-sm:size-8"
            >
              <span
                class="absolute inset-0 rounded-xl bg-primary-soft"
                aria-hidden="true"
              />
              <Boxes class="relative size-5" :stroke-width="2" aria-hidden="true" />
            </span>
            <div class="min-w-0">
              <p class="m-0 truncate text-[15px] font-semibold tracking-[-0.01em] text-foreground" :title="profile.name">
                {{ profile.name }}
              </p>
              <p class="mt-0.5 mb-0 flex min-w-0 items-center gap-1.5 text-[11.5px] text-muted-foreground">
                <span class="truncate font-mono">{{ profile.id }}</span>
                <Badge v-if="isDefaultProfile(profile)" variant="outline" class="shrink-0 px-1.5 py-0 text-[10.5px]">默认</Badge>
              </p>
            </div>
          </div>

          <Badge v-if="profile.id === activeProfileId" variant="brand" class="shrink-0 gap-1">
            <CircleCheck class="size-3" aria-hidden="true" />
            使用中
          </Badge>
        </header>

        <p class="m-0 min-h-10 break-words text-[12.5px] leading-relaxed text-muted-foreground max-sm:min-h-0">
          {{ profile.description || '暂无说明' }}
        </p>

        <Button v-if="profile.bound_agents?.length" variant="outline" class="h-auto min-h-11 justify-start whitespace-normal text-left" @click="openBindings(profile)">
          <span class="min-w-0 break-words text-xs">{{ profile.bound_agents.length }} 个 Agent 绑定：{{ profile.bound_agents.slice(0, 2).map(agent => agent.name).join('、') }}{{ profile.bound_agents.length > 2 ? ' 等' : '' }} · 查看 / 解绑</span>
        </Button>
        <p v-else class="m-0 text-xs text-muted-foreground">暂无 Agent 绑定</p>

        <footer class="mt-auto flex flex-wrap items-center gap-2 border-0 border-t border-dashed border-border/50 pt-4 max-sm:pt-2">
          <Button
            v-if="profile.id !== activeProfileId"
            size="sm"
            :disabled="scopedRequests > 0"
            @click="switchProfile(profile.id)"
          >
            <CircleCheck class="size-3.5" />
            使用
          </Button>
          <span v-else class="text-[12.5px] font-medium text-muted-foreground max-sm:hidden">当前正在使用</span>

          <div class="ml-auto flex items-center gap-1">
            <Button variant="ghost" size="icon-sm" title="编辑" aria-label="编辑" @click="openEdit(profile)">
              <Pencil class="size-4" />
            </Button>
            <Button v-if="!isDefaultProfile(profile)" variant="ghost" size="icon-sm" title="设为默认" aria-label="设为默认" @click="makeDefault(profile)">
              <Star class="size-4" />
            </Button>
            <Button variant="ghost" size="icon-sm" title="创建副本" aria-label="创建副本" @click="clone(profile)">
              <Copy class="size-4" />
            </Button>
            <Button
              variant="ghost"
              size="icon-sm"
              title="导出"
              aria-label="导出"
              @click="exportProfile(profile.id)"
            >
              <Download class="size-4" />
            </Button>
            <Button
              v-if="!isDefaultProfile(profile) || profiles.length > 1"
              variant="ghost"
              size="icon-sm"
              class="text-destructive-accent hover:bg-destructive-soft"
              title="删除"
              aria-label="删除"
              :disabled="Boolean(checkingDelete)"
              @click="remove(profile)"
            >
              <Loader2 v-if="checkingDelete === profile.id" class="size-4 animate-spin" />
              <Trash2 v-else class="size-4" />
            </Button>
          </div>
        </footer>
      </div>
    </div>

    <ProfileAgentBindings v-if="bindingProfile" :key="bindingProfile.id" :profile="bindingProfile" :delete-requested="bindingDeleteRequested" @close="bindingProfile = null" @changed="refreshBindings" @continue-delete="continueDelete" />

    <Dialog v-model:open="dialogVisible">
      <DialogContent class="max-w-[460px]">
        <DialogHeader>
          <DialogTitle>{{ editingId ? '编辑配置空间' : '新建配置空间' }}</DialogTitle>
          <DialogDescription>
            为配置空间填写名称和说明。标识 ID 用于访问配置和命名生成文件，创建后不可修改。
          </DialogDescription>
        </DialogHeader>

        <form class="flex flex-col gap-4" @submit.prevent="submit">
          <div class="flex flex-col gap-1.5">
            <Label for="profile-id">标识 ID</Label>
            <Input
              id="profile-id"
              v-model="form.id"
              class="bg-background/50 font-mono"
              :disabled="Boolean(editingId)"
              maxlength="64"
              placeholder="例如 home、work"
            />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="profile-name">名称</Label>
            <Input id="profile-name" v-model="form.name" class="bg-background/50" maxlength="120" />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="profile-desc">说明</Label>
            <Textarea
              id="profile-desc"
              v-model="form.description"
              class="bg-background/50"
              :rows="3"
              maxlength="500"
            />
          </div>
        </form>

        <DialogFooter>
          <Button variant="outline" @click="dialogVisible = false">取消</Button>
          <Button :disabled="saving" @click="submit">
            <Loader2 v-if="saving" class="size-4 animate-spin" />
            保存
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <Dialog v-model:open="defaultDelete.visible">
      <DialogContent class="max-w-[460px]">
        <DialogHeader>
          <DialogTitle>删除默认配置空间</DialogTitle>
          <DialogDescription>
            「{{ defaultDelete.profile?.name }}」是默认配置空间。删除前请选择一个配置空间接任默认：未指定配置空间的配置链接、新注册的 Agent 和 MCP 工具都会改用它。删除后不可撤销。
          </DialogDescription>
        </DialogHeader>

        <div class="flex min-w-0 flex-col gap-1.5">
          <Label for="default-successor">新的默认配置空间</Label>
          <Select v-model="defaultDelete.successor">
            <SelectTrigger id="default-successor" class="w-full bg-background/50"><SelectValue placeholder="请选择配置空间" /></SelectTrigger>
            <SelectContent>
              <SelectItem
                v-for="option in profiles.filter(item => item.id !== defaultDelete.profile?.id)"
                :key="option.id"
                :value="option.id"
              >
                {{ option.name }}（{{ option.id }}）
              </SelectItem>
            </SelectContent>
          </Select>
        </div>

        <DialogFooter>
          <Button variant="outline" @click="defaultDelete.visible = false">取消</Button>
          <Button variant="destructive" :disabled="!defaultDelete.successor || defaultDelete.saving" @click="confirmDefaultDelete">
            <Loader2 v-if="defaultDelete.saving" class="size-4 animate-spin" />
            删除
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <Dialog v-model:open="clientImport.visible">
      <DialogContent :class="clientImport.expanded ? 'max-w-[min(1100px,calc(100vw-2rem))]' : 'max-w-[640px]'">
        <DialogHeader>
          <DialogTitle>导入客户端配置</DialogTitle>
          <DialogDescription>
            将 Mihomo、Surge、Loon 或 Shadowrocket 的配置文件导入为新的配置空间。节点、订阅和规则集加入共用资源，定义相同的资源直接复用；策略组和规则只属于新配置空间。
          </DialogDescription>
        </DialogHeader>

        <form class="flex min-w-0 flex-col gap-4" @submit.prevent="submitClientImport(false)">
          <div class="grid grid-cols-2 gap-3 max-sm:grid-cols-1">
            <div class="flex flex-col gap-1.5">
              <Label for="client-type">配置类型</Label>
              <Select v-model="clientImport.clientType">
                <SelectTrigger id="client-type" class="w-full bg-background/50"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem v-for="option in CLIENT_TYPES" :key="option.value" :value="option.value">
                    {{ option.label }}
                  </SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div class="flex flex-col gap-1.5">
              <Label for="client-name">配置名称</Label>
              <Input id="client-name" v-model="clientImport.name" class="bg-background/50" maxlength="120" />
            </div>
            <div class="col-span-2 flex flex-col gap-1.5 max-sm:col-span-1">
              <Label for="client-id">标识 ID（可选）</Label>
              <Input
                id="client-id"
                v-model="clientImport.id"
                class="bg-background/50 font-mono"
                maxlength="64"
                placeholder="留空自动生成"
              />
            </div>
          </div>

          <div class="flex flex-col gap-1.5">
            <div class="flex items-center justify-between gap-2">
              <Label for="client-content">配置内容</Label>
              <div class="flex items-center gap-1">
                <Button type="button" variant="ghost" size="sm" @click="clientFileInput?.click()">
                  <Upload class="size-3.5" />
                  选择文件
                </Button>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  :title="clientImport.expanded ? '收起编辑' : '展开编辑'"
                  :aria-label="clientImport.expanded ? '收起编辑' : '展开编辑'"
                  :aria-pressed="clientImport.expanded"
                  @click="clientImport.expanded = !clientImport.expanded"
                >
                  <Minimize2 v-if="clientImport.expanded" class="size-3.5" />
                  <Maximize2 v-else class="size-3.5" />
                </Button>
              </div>
              <input
                ref="clientFileInput"
                type="file"
                hidden
                @change="loadClientFile"
              />
            </div>
            <Textarea
              id="client-content"
              v-model="clientImport.content"
              :class="[
                'resize-none overflow-auto bg-background/50 font-mono text-[12px] field-sizing-fixed',
                clientImport.expanded ? 'h-[max(12rem,min(60dvh,calc(100dvh-24rem)))]' : 'h-48 max-sm:h-36'
              ].join(' ')"
              :placeholder="clientImport.fileName ? '' : '粘贴配置内容，或选择配置文件'"
            />
            <p v-if="clientImport.fileName" class="m-0 text-[12px] text-muted-foreground">
              已读取 {{ clientImport.fileName }}
            </p>
          </div>

          <div v-if="subscriptions.length" class="flex flex-col gap-1.5">
            <Label>为筛选型策略组关联订阅（可选）</Label>
            <p class="m-0 text-[12px] text-muted-foreground">
              配置中按正则筛选全部节点的策略组（如 include-all、Shadowrocket 的 policy-regex-filter）会同时使用这里选择的订阅。
            </p>
            <div class="flex max-h-28 flex-wrap gap-2 overflow-y-auto">
              <label
                v-for="sub in subscriptions"
                :key="sub.id"
                class="flex cursor-pointer items-center gap-2 rounded-lg border border-border/50 bg-background/40 px-3 py-1.5 text-[12.5px]"
              >
                <Checkbox
                  :model-value="clientImport.subscriptionIds.includes(sub.id)"
                  @update:model-value="toggleImportSubscription(sub.id, $event === true)"
                />
                {{ sub.name }}
              </label>
            </div>
          </div>

          <div
            v-if="clientImport.result"
            class="flex flex-col gap-2 rounded-lg border border-border/50 bg-background/40 p-3 text-[12.5px]"
          >
            <p class="m-0 font-medium text-foreground">
              {{ clientImport.result.committed ? '导入完成' : '解析结果' }}
            </p>
            <div class="flex flex-wrap gap-1.5">
              <Badge v-for="item in summaryItems" :key="item" variant="outline">{{ item }}</Badge>
            </div>
            <ul
              v-if="clientImport.result.warnings.length"
              class="m-0 flex max-h-40 flex-col gap-1 overflow-y-auto overscroll-contain pl-4 text-muted-foreground"
            >
              <li v-for="warning in clientImport.result.warnings" :key="warning" class="[overflow-wrap:anywhere]">{{ warning }}</li>
            </ul>
          </div>
        </form>

        <!-- 展开编辑时按钮固定在弹窗底部，矮屏也不用滚到最下面 -->
        <DialogFooter
          :class="clientImport.expanded
            ? 'sticky -bottom-6 -mx-6 -mb-6 border-t border-border/60 bg-card px-6 py-4 max-sm:-bottom-5 max-sm:-mx-5 max-sm:-mb-5 max-sm:px-5'
            : ''"
        >
          <Button variant="outline" @click="clientImport.visible = false">
            {{ clientImport.result?.committed ? '关闭' : '取消' }}
          </Button>
          <template v-if="!clientImport.result?.committed">
            <Button variant="outline" :disabled="clientImport.saving" @click="submitClientImport(true)">
              解析预览
            </Button>
            <Button :disabled="clientImport.saving" @click="submitClientImport(false)">
              <Loader2 v-if="clientImport.saving" class="size-4 animate-spin" />
              导入
            </Button>
          </template>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useMediaQuery } from '@vueuse/core'
import {
  Boxes,
  ChevronDown,
  CircleCheck,
  Copy,
  Download,
  FileInput,
  Loader2,
  Maximize2,
  Minimize2,
  Pencil,
  Plus,
  Star,
  Trash2,
  Upload
} from '@lucide/vue'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger
} from '@/components/ui/dropdown-menu'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from '@/components/ui/dialog'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import EmptyState from '@/components/common/EmptyState.vue'
import LoadingRows from '@/components/common/LoadingRows.vue'
import PageHeader from '@/components/common/PageHeader.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import ProfileAgentBindings from '@/components/ProfileAgentBindings.vue'
import { profileApi, subscriptionApi } from '@/api'
import { confirm, confirmDanger, notify, prompt } from '@/lib/feedback'
import { listItemStyle } from '@/lib/motion'
import { isDefaultProfile, useProfileStore, type Profile } from '@/stores/profile'

const profileStore = useProfileStore()
const isPhone = useMediaQuery('(max-width: 639px)')
const { profiles, loading, scopedRequests, activeProfileId, refreshProfiles, switchProfile } = profileStore
const dialogVisible = ref(false)
const saving = ref(false)
const editingId = ref('')
const importInput = ref<HTMLInputElement>()
const form = reactive({ id: '', name: '', description: '' })

const PROFILE_ID = /^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$/

const openCreate = () => {
  editingId.value = ''
  Object.assign(form, { id: '', name: '', description: '' })
  dialogVisible.value = true
}

const openEdit = (profile: Profile) => {
  editingId.value = profile.id
  Object.assign(form, {
    id: profile.id,
    name: profile.name,
    description: profile.description || ''
  })
  dialogVisible.value = true
}

const submit = async () => {
  if (!form.name.trim() || (!editingId.value && !form.id.trim())) {
    notify.warning('请填写标识 ID 和名称')
    return
  }
  saving.value = true
  try {
    if (editingId.value) {
      await profileApi.update(editingId.value, { name: form.name, description: form.description })
    } else {
      await profileApi.create({ id: form.id.trim(), name: form.name, description: form.description })
    }
    await refreshProfiles()
    dialogVisible.value = false
    notify.success('配置空间已保存')
  } catch (error: any) {
    notify.error(error.response?.data?.message || '配置空间保存失败')
  } finally {
    saving.value = false
  }
}


const makeDefault = async (profile: Profile) => {
  const approved = await confirm(
    `未指定配置空间的配置链接、新注册的 Agent 和 MCP 工具会改用「${profile.name}」，新打开的页面也默认进入它。已绑定的设备保持原配置空间，不受影响。`,
    { title: '设为默认配置空间？', confirmText: '设为默认' }
  )
  if (!approved) return
  try {
    await profileApi.setDefault(profile.id)
    await refreshProfiles()
    notify.success(`默认配置空间已改为「${profile.name}」`)
  } catch (error: any) {
    notify.error(error.response?.data?.message || '设为默认失败')
  }
}

const clone = async (profile: Profile) => {
  const id = await prompt({
    title: `复制 ${profile.name}`,
    description: '填写新副本的标识 ID。保留策略、规则和模板，共用资源；不复制路由器接入信息及自动同步设置。',
    defaultValue: `${profile.id}-copy`,
    confirmText: '创建副本',
    validate: value =>
      PROFILE_ID.test(value.trim()) ? '' : 'ID 须为 1–64 个字符，以字母或数字开头，仅含字母、数字、下划线和短横线'
  })
  if (id === null) return

  try {
    await profileApi.clone(profile.id, { id: id.trim(), name: `${profile.name} 副本` })
    await refreshProfiles()
    notify.success('配置空间副本已创建')
  } catch (error: any) {
    notify.error(error.response?.data?.message || '创建配置空间副本失败')
  }
}

const defaultDelete = reactive({
  visible: false,
  saving: false,
  profile: null as Profile | null,
  successor: ''
})

const checkingDelete = ref('')
const bindingProfile = ref<Profile | null>(null)
const bindingDeleteRequested = ref(false)
const openBindings = (profile: Profile, deleting = false) => {
  bindingDeleteRequested.value = deleting
  bindingProfile.value = profile
}
const refreshBindings = async () => {
  try { await refreshProfiles() }
  catch { notify.warning('绑定已调整，配置空间列表刷新失败，请稍后刷新页面') }
}
const continueDelete = () => {
  const profile = bindingProfile.value
  bindingProfile.value = null
  if (profile) void remove(profile)
}
const deletionFailed = (profile: Profile, error: any) => {
  if (error.response?.data?.usages?.some((usage: any) => usage.kind === 'agent')) {
    defaultDelete.visible = false
    openBindings(profile, true)
  } else {
    notify.error('配置空间删除失败', error.response?.data?.message)
  }
}

const confirmDefaultDelete = async () => {
  if (!defaultDelete.profile || !defaultDelete.successor) return
  defaultDelete.saving = true
  try {
    await profileApi.delete(defaultDelete.profile.id, defaultDelete.successor)
    defaultDelete.visible = false
    await refreshProfiles()
    notify.success('配置空间已删除', `默认配置空间已切换为「${profileStore.profileName(defaultDelete.successor)}」`)
  } catch (error: any) {
    deletionFailed(defaultDelete.profile, error)
  } finally {
    defaultDelete.saving = false
  }
}

const remove = async (profile: Profile) => {
  if (checkingDelete.value) return
  checkingDelete.value = profile.id
  try {
    const { data } = await profileApi.agentBindings(profile.id)
    if (data.agents.length) {
      openBindings(profile, true)
      return
    }
  } catch (error: any) {
    notify.error('无法检查 Agent 绑定', error.response?.data?.message || '请稍后重试')
    return
  } finally {
    checkingDelete.value = ''
  }
  if (isDefaultProfile(profile)) {
    // 默认配置空间需要先选接任者，后端会在同一事务里切换默认并删除
    const others = profiles.value.filter(item => item.id !== profile.id)
    Object.assign(defaultDelete, {
      visible: true,
      saving: false,
      profile,
      successor: others.find(item => item.id === activeProfileId.value)?.id || others[0]?.id || ''
    })
    return
  }
  const ok = await confirmDanger(
    `确定删除配置空间「${profile.name}」的设置、缓存和生成文件吗？该空间的配置链接将失效，共享资源保留。此操作不可撤销。`,
    { title: '删除配置空间' }
  )
  if (!ok) return

  try {
    await profileApi.delete(profile.id)
    await refreshProfiles()
    notify.success('配置空间已删除')
  } catch (error: any) {
    deletionFailed(profile, error)
  }
}

const exportProfile = async (profileId: string) => {
  try {
    const response = await profileApi.export(profileId)
    const url = URL.createObjectURL(new Blob([response.data], { type: 'application/json' }))
    const link = document.createElement('a')
    link.href = url
    link.download = `${profileId}.json`
    link.click()
    URL.revokeObjectURL(url)
  } catch (error: any) {
    notify.error(error.response?.data?.message || '配置空间导出失败')
  }
}

const pickImportFile = () => importInput.value?.click()

const importProfile = async (event: Event) => {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  const profileId = activeProfileId.value
  try {
    const data = JSON.parse(await file.text())
    await profileApi.import(profileId, data)
    await refreshProfiles()
    notify.success('配置已导入当前配置空间')
  } catch (error: any) {
    notify.error(error.response?.data?.message || '配置空间导入失败')
  } finally {
    input.value = ''
  }
}

const CLIENT_TYPES = [
  { value: 'mihomo', label: 'Mihomo / Clash Meta' },
  { value: 'surge', label: 'Surge' },
  { value: 'loon', label: 'Loon' },
  { value: 'shadowrocket', label: 'Shadowrocket' }
]

interface ImportSummary {
  nodes: number
  nodes_reused: number
  subscriptions: number
  subscriptions_reused: number
  rule_library: number
  rule_library_reused: number
  proxy_groups: number
  rules: number
  rulesets: number
  custom_config: string | null
}

const subscriptions = ref<{ id: string; name: string }[]>([])
const clientFileInput = ref<HTMLInputElement>()
const clientImport = reactive({
  visible: false,
  saving: false,
  expanded: false,
  clientType: 'mihomo',
  name: '',
  id: '',
  content: '',
  fileName: '',
  subscriptionIds: [] as string[],
  result: null as null | { committed: boolean; summary: ImportSummary; warnings: string[] }
})

const summaryItems = computed(() => {
  const summary = clientImport.result?.summary
  if (!summary) return []
  const withReuse = (label: string, added: number, reused: number) =>
    reused ? `${label} 新增 ${added}，复用 ${reused}` : `${label} 新增 ${added}`
  const items = [
    withReuse('节点', summary.nodes, summary.nodes_reused),
    withReuse('订阅', summary.subscriptions, summary.subscriptions_reused),
    withReuse('规则集', summary.rule_library, summary.rule_library_reused),
    `策略组 ${summary.proxy_groups}`,
    `规则 ${summary.rules}`,
    `规则集引用 ${summary.rulesets}`
  ]
  if (summary.custom_config) items.push(`${summary.custom_config} 基础配置`)
  return items
})

const openClientImport = async () => {
  Object.assign(clientImport, {
    visible: true,
    saving: false,
    expanded: false,
    clientType: 'mihomo',
    name: '',
    id: '',
    content: '',
    fileName: '',
    subscriptionIds: [],
    result: null
  })
  try {
    const response = await subscriptionApi.getAll()
    subscriptions.value = response.data || []
  } catch {
    subscriptions.value = []
  }
}

const guessClientType = (fileName: string, content: string) => {
  const lower = fileName.toLowerCase()
  if (lower.endsWith('.yaml') || lower.endsWith('.yml')) return 'mihomo'
  if (lower.endsWith('.lcf') || /^\s*\[Remote (Proxy|Rule|Filter)\]/m.test(content)) return 'loon'
  return clientImport.clientType
}

const loadClientFile = async (event: Event) => {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  try {
    clientImport.content = await file.text()
    clientImport.fileName = file.name
    clientImport.clientType = guessClientType(file.name, clientImport.content)
    if (!clientImport.name.trim()) clientImport.name = file.name.replace(/\.[^.]+$/, '')
    clientImport.result = null
  } catch {
    notify.error('读取文件失败')
  } finally {
    input.value = ''
  }
}

const toggleImportSubscription = (id: string, checked: boolean) => {
  const ids = clientImport.subscriptionIds.filter(item => item !== id)
  clientImport.subscriptionIds = checked ? [...ids, id] : ids
}

const submitClientImport = async (dryRun: boolean) => {
  if (!clientImport.name.trim()) {
    notify.warning('请填写配置名称')
    return
  }
  if (clientImport.id.trim() && !PROFILE_ID.test(clientImport.id.trim())) {
    notify.warning('ID 须为 1–64 个字符，以字母或数字开头，仅含字母、数字、下划线和短横线')
    return
  }
  if (!clientImport.content.trim()) {
    notify.warning('请粘贴配置内容或选择配置文件')
    return
  }
  clientImport.saving = true
  try {
    const response = await profileApi.importClient({
      client_type: clientImport.clientType,
      content: clientImport.content,
      name: clientImport.name.trim(),
      id: clientImport.id.trim() || undefined,
      default_subscription_ids: clientImport.subscriptionIds,
      dry_run: dryRun
    })
    clientImport.result = {
      committed: !dryRun,
      summary: response.data.summary,
      warnings: response.data.warnings || []
    }
    if (!dryRun) {
      await refreshProfiles()
      notify.success(`已导入配置空间「${response.data.profile.name}」`)
    }
  } catch (error: any) {
    clientImport.result = null
    notify.error(error.response?.data?.message || '配置导入失败')
  } finally {
    clientImport.saving = false
  }
}

onMounted(() => {
  refreshProfiles().catch(() => notify.error('加载配置空间失败'))
})
</script>
