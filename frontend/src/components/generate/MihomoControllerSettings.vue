<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Eye, EyeOff } from '@lucide/vue'
import FormField from '@/components/common/FormField.vue'
import RadioChips from '@/components/common/RadioChips.vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { controllerHelp, externalUiHelp, externalUiUrlHelp, secretHelp, type BaseFieldDef } from '@/lib/mihomoBaseCopy'
import { joinHostPort, splitHostPort, type BaseFieldState, type BaseIssue, type BasePath } from '@/lib/mihomoBaseSettings'
import MihomoBaseField from './MihomoBaseField.vue'

const props = defineProps<{
  secret: BaseFieldState
  controller: BaseFieldState
  ui: BaseFieldState
  uiUrl: BaseFieldState
  issues: (field: string) => BaseIssue[]
  disabled?: boolean
}>()
const emit = defineEmits<{ change: [path: BasePath, value: unknown]; draft: [field: string, error: string] }>()
const DEFAULT_PORT = 9090
const DEFAULT_UI = 'ui'
const DEFAULT_UI_URL = 'https://github.com/Zephyruso/zashboard/archive/refs/heads/gh-pages.zip'
const uiUrlField: BaseFieldDef = { path: ['external-ui-url'], label: '面板下载地址（external-ui-url）', type: 'text', help: externalUiUrlHelp, placeholder: DEFAULT_UI_URL, wide: true }

// The saved password is in the YAML below anyway; here it starts hidden.
const showSecret = ref(false)
const secretText = computed(() => typeof props.secret.value === 'string' ? props.secret.value : props.secret.value === undefined || props.secret.value === null ? '' : String(props.secret.value))
const secretStatus = computed(() => {
  if (!props.secret.editable) return props.secret.reason
  if (!props.secret.present || secretText.value === '') return '未设置密码。'
  if (secretText.value === '123456') return '仍是初始模板的默认密码 123456，请改成自己的密码。'
  return '已设置密码。'
})
function changeSecret(value: string | number) {
  const text = String(value)
  emit('change', ['secret'], text === '' ? undefined : text)
}

const parsed = computed(() => splitHostPort(props.controller.value))
const scopeLabels = { lan: '局域网可访问', local: '仅本机', custom: '自定义' } as const
type Scope = keyof typeof scopeLabels
const scopeOptions = (Object.keys(scopeLabels) as Scope[]).map(value => ({ value, label: scopeLabels[value] }))
const scope = computed<Scope | ''>(() => {
  if (!props.controller.present) return ''
  if (!parsed.value) return 'custom'
  return parsed.value.host === '0.0.0.0' ? 'lan' : parsed.value.host === '127.0.0.1' ? 'local' : 'custom'
})
const customOpen = ref(false)
const shownScope = computed(() => customOpen.value ? 'custom' : scope.value)
const hostDraft = ref('')
const portDraft = ref('')
const portError = ref('')
watch(() => props.controller.value, () => {
  customOpen.value = false
  hostDraft.value = parsed.value ? parsed.value.host : typeof props.controller.value === 'string' ? props.controller.value : ''
  portDraft.value = parsed.value ? String(parsed.value.port) : ''
  portError.value = ''
  emit('draft', 'external-controller', '')
}, { immediate: true })
const currentPort = () => parsed.value?.port ?? DEFAULT_PORT
function chooseScope(value: string) {
  if (value === 'custom') {
    customOpen.value = true
    return
  }
  customOpen.value = false
  emit('change', ['external-controller'], joinHostPort(value === 'lan' ? '0.0.0.0' : '127.0.0.1', currentPort()))
}
function changeHost(value: string | number) {
  hostDraft.value = String(value).trim()
  customOpen.value = true
  if (/^\d+$/.test(portDraft.value)) emit('change', ['external-controller'], joinHostPort(hostDraft.value, portDraft.value))
}
function changePort(value: string | number) {
  portDraft.value = String(value).trim()
  portError.value = /^\d+$/.test(portDraft.value) ? '' : '控制面板端口需为整数'
  emit('draft', 'external-controller', portError.value)
  if (portError.value) return
  const host = shownScope.value === 'custom' ? hostDraft.value : shownScope.value === 'local' ? '127.0.0.1' : '0.0.0.0'
  emit('change', ['external-controller'], joinHostPort(host, portDraft.value))
}
const controllerNote = computed(() => {
  if (!props.controller.editable) return props.controller.reason
  if (!props.controller.present) return '未设置控制面板：Agent 无法读取连接和流量，网页面板也打不开。选择一种监听方式即可写入（端口默认 9090）。'
  if (scope.value === 'lan') return '同一局域网的设备都能打开控制面板，靠密码保护；Agent 改用 127.0.0.1 访问。'
  if (scope.value === 'local') return '只有这台设备自己能访问控制面板；Agent 照常读取连接和流量。'
  return '自定义地址需为 ::（所有地址）或 ::1、localhost 等回环地址。'
})
const uiOn = computed(() => props.ui.present)
function toggleUi(value: boolean) {
  emit('change', ['external-ui'], value ? (typeof props.ui.value === 'string' && props.ui.value ? props.ui.value : DEFAULT_UI) : undefined)
  if (value && !props.uiUrl.present) emit('change', ['external-ui-url'], DEFAULT_UI_URL)
}
const messages = (field: string, extra = '') => [...(extra ? [{ message: extra, blocking: true }] : []), ...props.issues(field)]
</script>

<template>
  <div class="grid min-w-0 grid-cols-1 gap-4 sm:grid-cols-2" data-base-controller>
    <FormField label="控制面板密码（secret）" html-for="mihomo-base-secret" :help="secretHelp" class="sm:col-span-2" data-base-field="secret">
      <div class="flex min-w-0 max-w-md gap-2">
        <Input id="mihomo-base-secret" :model-value="secretText" :type="showSecret ? 'text' : 'password'" autocomplete="new-password" :spellcheck="false"
          :disabled="disabled || !secret.editable" placeholder="8–64 个字母、数字或符号" class="min-h-11 min-w-0 font-mono"
          :aria-invalid="messages('secret').some(item => item.blocking)" @update:model-value="changeSecret" />
        <Button type="button" variant="outline" size="icon" class="size-11 shrink-0" :disabled="disabled || !secret.editable"
          :aria-label="showSecret ? '隐藏控制面板密码' : '显示控制面板密码'" :aria-pressed="showSecret" @click="showSecret = !showSecret">
          <EyeOff v-if="showSecret" class="size-4" /><Eye v-else class="size-4" />
        </Button>
      </div>
      <p class="m-0 break-words text-xs leading-relaxed" :class="secretText === '123456' || !secret.editable ? 'text-warning-accent' : 'text-muted-foreground'" data-base-secret-status>{{ secretStatus }}</p>
      <p v-for="item in messages('secret')" :key="item.message" class="m-0 break-words text-xs leading-relaxed" :class="item.blocking ? 'text-destructive-accent' : 'text-warning-accent'" :role="item.blocking ? 'alert' : undefined" data-base-issue>
        {{ item.blocking ? item.message : `已保存的配置：${item.message}` }}
      </p>
    </FormField>

    <FormField label="控制面板监听（external-controller）" html-for="mihomo-base-controller-port" :help="controllerHelp" class="sm:col-span-2" data-base-field="external-controller">
      <RadioChips :model-value="shownScope" :options="scopeOptions" aria-label="控制面板监听" :disabled="disabled || !controller.editable" @update:model-value="chooseScope" />
      <div class="flex min-w-0 flex-wrap items-end gap-2">
        <label v-if="shownScope === 'custom'" class="grid min-w-0 flex-1 basis-48 gap-1 text-xs text-muted-foreground">
          监听地址
          <Input :model-value="hostDraft" placeholder="::" autocomplete="off" autocapitalize="off" :spellcheck="false" class="min-h-11 font-mono text-foreground"
            :disabled="disabled || !controller.editable" @update:model-value="changeHost" />
        </label>
        <label class="grid w-32 gap-1 text-xs text-muted-foreground" for="mihomo-base-controller-port">
          端口
          <Input id="mihomo-base-controller-port" :model-value="portDraft" inputmode="numeric" placeholder="9090" autocomplete="off" class="min-h-11 font-mono text-foreground"
            :disabled="disabled || !controller.editable || !shownScope" :aria-invalid="!!portError || messages('external-controller').some(item => item.blocking)" @update:model-value="changePort" />
        </label>
      </div>
      <p class="m-0 break-words text-xs leading-relaxed" :class="controller.present && controller.editable ? 'text-muted-foreground' : 'text-warning-accent'" data-base-controller-note>{{ controllerNote }}</p>
      <p v-for="item in messages('external-controller', portError)" :key="item.message" class="m-0 break-words text-xs leading-relaxed" :class="item.blocking ? 'text-destructive-accent' : 'text-warning-accent'" :role="item.blocking ? 'alert' : undefined" data-base-issue>
        {{ item.blocking ? item.message : `已保存的配置：${item.message}` }}
      </p>
    </FormField>

    <FormField label="网页控制面板（external-ui）" html-for="mihomo-base-external-ui" :help="externalUiHelp" data-base-field="external-ui">
      <div class="flex min-h-11 items-center gap-3">
        <Switch id="mihomo-base-external-ui" :model-value="uiOn" :disabled="disabled || !ui.editable" aria-label="网页控制面板" @update:model-value="toggleUi" />
        <label for="mihomo-base-external-ui" class="cursor-pointer text-[13px]">{{ uiOn ? `开启，目录 ${String(ui.value)}` : '关闭' }}</label>
      </div>
      <p v-if="!ui.editable" class="m-0 text-xs text-warning-accent">{{ ui.reason }}</p>
      <p v-for="item in messages('external-ui')" :key="item.message" class="m-0 break-words text-xs" :class="item.blocking ? 'text-destructive-accent' : 'text-warning-accent'" data-base-issue>{{ item.message }}</p>
    </FormField>
    <MihomoBaseField v-if="uiOn" :field="uiUrlField" :state="uiUrl" :issues="issues('external-ui-url')" :disabled="disabled"
      @change="emit('change', ['external-ui-url'], $event)" />
  </div>
</template>
