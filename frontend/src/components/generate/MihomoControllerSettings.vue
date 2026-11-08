<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ChevronDown, Eye, EyeOff } from '@lucide/vue'
import RadioChips from '@/components/common/RadioChips.vue'
import { Input } from '@/components/ui/input'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Switch } from '@/components/ui/switch'
import { controllerHelp, externalUiHelp, externalUiUrlHelp, secretHelp, type BaseFieldDef } from '@/lib/mihomoBaseCopy'
import { joinHostPort, splitHostPort, type BaseFieldState, type BaseIssue, type BasePath } from '@/lib/mihomoBaseSettings'
import MihomoBaseCell from './MihomoBaseCell.vue'
import MihomoBaseField from './MihomoBaseField.vue'
import { CELL_INPUT, CELL_POPOVER, CELL_SWITCH, CELL_TRIGGER } from './mihomoBaseCell'

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
  if (!props.secret.present || secretText.value === '') return '未设置密码，局域网内谁都能打开控制面板。'
  // 123456 is reported by the checks below the field (a warning when saved, blocking when typed).
  return ''
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
  scopeOpen.value = false
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
const scopeShown = computed(() => !props.controller.present ? '未设置' : scope.value === 'custom' ? String(props.controller.value) : scopeLabels[scope.value as Scope])
const scopeOpen = ref(false)
// Missing or locked: the note is a warning kept in view; otherwise it describes the choice inside the menu.
const controllerWarning = computed(() => !props.controller.editable || !props.controller.present)
const controllerNote = computed(() => {
  if (!props.controller.editable) return props.controller.reason
  if (!props.controller.present) return '未设置控制面板：Agent 无法读取连接和流量，网页面板也打不开。选择一种监听方式即可写入（端口默认 9090）。'
  if (scope.value === 'lan') return '同一局域网的设备都能打开控制面板，靠密码保护；Agent 改用 127.0.0.1 访问。'
  if (scope.value === 'local') return '只有这台设备自己能访问控制面板；Agent 照常读取连接和流量。'
  return '自定义地址需为 ::（所有地址）或 ::1、localhost 等回环地址。'
})
const uiOn = computed(() => props.ui.present)
const label = (field: 'secret' | 'controller' | 'port' | 'ui') => ({ secret: '访问密码', controller: '监听范围', port: '端口', ui: '网页面板' })[field]
function toggleUi(value: boolean) {
  emit('change', ['external-ui'], value ? (typeof props.ui.value === 'string' && props.ui.value ? props.ui.value : DEFAULT_UI) : undefined)
  if (value && !props.uiUrl.present) emit('change', ['external-ui-url'], DEFAULT_UI_URL)
}
const messages = (field: string, extra = '') => [...(extra ? [{ message: extra, blocking: true }] : []), ...props.issues(field)]
</script>

<template>
  <div class="contents" data-base-controller>
    <MihomoBaseCell :label="label('controller')" :help="`YAML 键：external-controller\n${controllerHelp}`" :status="controllerWarning ? controllerNote : ''" warn
      :messages="messages('external-controller')" data-base-field="external-controller">
      <Popover v-model:open="scopeOpen">
        <PopoverTrigger as-child>
          <button type="button" :class="CELL_TRIGGER" :disabled="disabled || !controller.editable" :aria-label="`控制面板监听：${scopeShown}`" data-base-controller-scope>
            <span class="min-w-0 flex-1 truncate" :class="[!controller.present && 'text-warning-accent', scope === 'custom' && 'font-mono']">{{ scopeShown }}</span>
            <ChevronDown class="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
          </button>
        </PopoverTrigger>
        <PopoverContent align="start" :class="CELL_POPOVER">
          <p class="m-0 text-[13px] font-medium">控制面板监听（external-controller）</p>
          <RadioChips :model-value="shownScope" :options="scopeOptions" aria-label="控制面板监听" @update:model-value="chooseScope" />
          <label v-if="shownScope === 'custom'" class="grid min-w-0 gap-1 text-xs text-muted-foreground">
            监听地址
            <Input :model-value="hostDraft" placeholder="::" autocomplete="off" autocapitalize="off" :spellcheck="false" class="min-h-11 font-mono text-foreground"
              @update:model-value="changeHost" />
          </label>
          <p v-if="!controllerWarning" class="m-0 break-words text-xs leading-relaxed text-muted-foreground" data-base-controller-note>{{ controllerNote }}</p>
        </PopoverContent>
      </Popover>
    </MihomoBaseCell>

    <MihomoBaseCell :label="label('port')" html-for="mihomo-base-controller-port" :messages="portError ? [{ message: portError, blocking: true }] : []">
      <input id="mihomo-base-controller-port" :value="portDraft" inputmode="numeric" placeholder="9090" autocomplete="off" :class="CELL_INPUT"
        :disabled="disabled || !controller.editable || !shownScope" :aria-invalid="!!portError || messages('external-controller').some(item => item.blocking)"
        @input="changePort(($event.target as HTMLInputElement).value)" @keydown.enter.prevent="($event.target as HTMLElement).blur()" />
    </MihomoBaseCell>

    <MihomoBaseCell :label="label('secret')" html-for="mihomo-base-secret" :help="`YAML 键：secret\n${secretHelp}`" :status="secretStatus" warn
      :messages="messages('secret')" data-base-field="secret">
      <div class="flex min-w-0 items-center gap-0.5">
        <input id="mihomo-base-secret" :value="secretText" :type="showSecret ? 'text' : 'password'" autocomplete="new-password" :spellcheck="false"
          :disabled="disabled || !secret.editable" placeholder="未设置" :class="CELL_INPUT"
          :aria-invalid="messages('secret').some(item => item.blocking)" @input="changeSecret(($event.target as HTMLInputElement).value)"
          @keydown.enter.prevent="($event.target as HTMLElement).blur()" />
        <button type="button" class="inline-flex size-11 shrink-0 items-center justify-center rounded-md text-muted-foreground outline-none hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-primary-accent disabled:opacity-50 sm:size-8"
          :disabled="disabled || !secret.editable" :aria-label="showSecret ? '隐藏控制面板密码' : '显示控制面板密码'" :aria-pressed="showSecret" @click="showSecret = !showSecret">
          <EyeOff v-if="showSecret" class="size-4" /><Eye v-else class="size-4" />
        </button>
      </div>
    </MihomoBaseCell>

    <MihomoBaseCell :label="label('ui')" :help="`YAML 键：external-ui\n${externalUiHelp}`" :status="ui.editable ? '' : ui.reason" warn
      :messages="messages('external-ui')" data-base-field="external-ui">
      <div :class="CELL_SWITCH">
        <Switch id="mihomo-base-external-ui" :model-value="uiOn" :disabled="disabled || !ui.editable" aria-label="网页控制面板" @update:model-value="toggleUi" />
        <label for="mihomo-base-external-ui" class="min-w-0 cursor-pointer truncate">{{ uiOn ? '开' : '关' }}<span v-if="uiOn" class="font-mono text-muted-foreground"> · {{ String(ui.value) }}</span></label>
      </div>
    </MihomoBaseCell>
    <MihomoBaseField v-if="uiOn" :field="uiUrlField" :state="uiUrl" :issues="issues('external-ui-url')" :disabled="disabled"
      @change="emit('change', ['external-ui-url'], $event)" />
  </div>
</template>
