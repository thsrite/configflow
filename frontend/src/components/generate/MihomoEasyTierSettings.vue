<script setup lang="ts">
import { computed, reactive, ref, useId, watch } from 'vue'
import { Eye, EyeOff } from '@lucide/vue'
import { Switch } from '@/components/ui/switch'
import FieldHelp from '@/components/common/FieldHelp.vue'
import type { BaseFieldDef } from '@/lib/mihomoBaseCopy'
import type { BaseFieldState, BaseIssue } from '@/lib/mihomoBaseSettings'
import {
  EASYTIER_GROUPS, LISTEN_FIELD, OUTPUT_FIELDS, easyTierIssues, isListening, type EasyTierSettings,
} from '@/lib/mihomoEasyTier'
import MihomoBaseCell from './MihomoBaseCell.vue'
import MihomoBaseField from './MihomoBaseField.vue'
import MihomoBaseGroup from './MihomoBaseGroup.vue'
import { CELL_INPUT } from './mihomoBaseCell'

/** EasyTier networking: one `type: easytier` node, its rules and keep-alive group, added to the generated config. */
const settings = defineModel<EasyTierSettings>({ required: true })
const props = defineProps<{ disabled?: boolean }>()
const emit = defineEmits<{ validation: [invalid: boolean] }>()
const id = useId()
const DOCS = 'https://wiki.metacubex.one/config/proxies/easytier/'

// Typing errors MihomoBaseField reports (e.g. a non-number MTU) block saving like the checks below.
const drafts = reactive<Record<string, string>>({})
const issues = computed(() => easyTierIssues(settings.value))
const invalid = computed(() => Object.values(drafts).some(Boolean) || Object.keys(issues.value).length > 0)
watch(invalid, value => emit('validation', value), { immediate: true })
const issueOf = (key: string): BaseIssue[] => issues.value[key] ? [{ field: `easytier.${key}`, message: issues.value[key], blocking: true }] : []
const fieldKey = (field: BaseFieldDef) => field.path.slice(1).join('.')
const optionKey = (field: BaseFieldDef) => String(field.path[1])
const state = (value: unknown): BaseFieldState => ({ value, present: value !== undefined, editable: true, reason: '' })

function update(change: (draft: EasyTierSettings) => void) {
  const next: EasyTierSettings = JSON.parse(JSON.stringify(settings.value))
  change(next)
  settings.value = next
}
function setOption(key: string, value: unknown) {
  update(next => {
    if (value === undefined || value === '' || (Array.isArray(value) && !value.length)) delete next.options[key]
    else next.options[key] = value as EasyTierSettings['options'][string]
    // Filled listeners listen by themselves; emptied while listening, the core's default listener stays on.
    if (key === 'listeners') {
      if (Array.isArray(next.options.listeners) && next.options.listeners.length) delete next.options['no-listener']
      else next.options['no-listener'] = false
    }
  })
}
const listening = computed(() => isListening(settings.value.options))
function setListening(on: boolean) {
  update(next => {
    delete next.options.listeners
    delete next.options['mapped-listeners']
    delete next.options['no-listener']
    if (on) next.options['no-listener'] = false
  })
}
const listenNote = computed(() => listening.value && !settings.value.keepalive.enabled
  ? '「启动即连接」未开启时，EasyTier 要等第一次访问才启动，在那之前别人连不进来。' : '')
const shownFields = (group: (typeof EASYTIER_GROUPS)[number]) => group.id === 'listen' && !listening.value ? [] : group.fields
const outputValue = (key: keyof typeof OUTPUT_FIELDS): unknown => {
  const value = settings.value
  switch (key) {
    case 'name': return value.name
    case 'apply_to': return value.apply_to
    case 'routes': return value.routes.length ? value.routes : undefined
    case 'magic_dns': return value.magic_dns
    case 'keepalive': return value.keepalive.enabled
    case 'keepalive_url': return value.keepalive.url || undefined
    case 'keepalive_interval': return value.keepalive.interval
  }
}
function setOutput(key: keyof typeof OUTPUT_FIELDS, value: unknown) {
  update(next => {
    switch (key) {
      case 'name': next.name = typeof value === 'string' ? value : ''; break
      case 'apply_to': next.apply_to = (value as EasyTierSettings['apply_to']) || 'subscription'; break
      case 'routes': next.routes = Array.isArray(value) ? value.map(String) : []; break
      case 'magic_dns': next.magic_dns = value === true; break
      case 'keepalive': next.keepalive.enabled = value === true; break
      case 'keepalive_url': next.keepalive.url = typeof value === 'string' ? value : ''; break
      case 'keepalive_interval': next.keepalive.interval = typeof value === 'number' ? value : 0; break
    }
  })
}
const outputIssueKey: Record<string, string> = { keepalive_url: 'keepalive.url', keepalive_interval: 'keepalive.interval' }
const outputFields = computed(() => (Object.keys(OUTPUT_FIELDS) as Array<keyof typeof OUTPUT_FIELDS>)
  .filter(key => !key.startsWith('keepalive_') || settings.value.keepalive.enabled))

// Without an IPv4 address the core asks DHCP for one, so the switch shows that default.
function fieldFor(field: BaseFieldDef): BaseFieldDef {
  if (optionKey(field) !== 'dhcp') return field
  return { ...field, coreDefault: !String(settings.value.options.ipv4 ?? '').trim() }
}
const groupBlocking = (fields: EasyTierSettingsFieldList) => fields.filter(item => issues.value['secret' in item ? item.secret : optionKey(item)]).length
type EasyTierSettingsFieldList = (typeof EASYTIER_GROUPS)[number]['fields']
const configured = (fields: EasyTierSettingsFieldList) => fields.filter(item => settings.value.options['secret' in item ? item.secret : optionKey(item)] !== undefined).length

const shown = reactive<Record<string, boolean>>({})
const advancedOpen = ref(false)
const summary = computed(() => {
  const value = settings.value
  const name = String(value.options['network-name'] ?? '').trim()
  const target = { subscription: '订阅和下载', agent: 'Agent 设备', all: '订阅、下载和 Agent 设备' }[value.apply_to]
  return name ? `加入网络「${name}」，下发到${target}` : `下发到${target}`
})
</script>

<template>
  <section class="min-w-0 shrink-0 border-b border-border" aria-labelledby="mihomo-easytier-title" data-mihomo-easytier>
    <div class="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 px-4 py-3 md:px-5">
      <div class="flex min-w-0 flex-1 items-center gap-1">
        <h3 :id="'mihomo-easytier-title'" class="m-0 whitespace-nowrap text-sm font-semibold">EasyTier 组网</h3>
        <FieldHelp label="EasyTier 组网" text="在生成的配置里加一个 EasyTier 节点（Mihomo v1.19.31 起支持），不用另装 EasyTier 客户端就能访问组网里的设备和网段。适合手机、笔记本等外出设备连回家；家里节点建议继续用原生 EasyTier。" />
        <span v-if="settings.enabled" class="hidden min-w-0 truncate text-xs text-muted-foreground sm:inline" data-easytier-summary>{{ summary }}</span>
      </div>
      <a :href="DOCS" target="_blank" rel="noreferrer" class="inline-flex min-h-11 items-center px-1 text-xs text-primary-accent underline-offset-2 outline-none hover:underline focus-visible:ring-2 focus-visible:ring-primary-accent sm:min-h-8">参数文档</a>
      <label class="flex min-h-11 cursor-pointer items-center gap-2 text-sm sm:min-h-8">
        <Switch :model-value="settings.enabled" :disabled="disabled" aria-label="启用 EasyTier 组网" data-easytier-enabled
          @update:model-value="update(next => { next.enabled = $event === true })" />
        <span>{{ settings.enabled ? '已启用' : '未启用' }}</span>
      </label>
    </div>
    <p v-if="!settings.enabled" class="m-0 px-4 pb-3 text-xs text-muted-foreground md:px-5">开启后可填写网络名、密钥和入网节点，并选择哪些网段走 EasyTier。</p>

    <div v-else class="min-w-0 border-t border-border" :inert="disabled" data-easytier-form>
      <MihomoBaseGroup title="生成方式" section="easytier-output" help="ConfigFlow 如何把 EasyTier 加进生成的配置：节点名、下发范围、分流网段和保活。"
        :blocking="['name', 'routes', 'keepalive.url', 'keepalive.interval'].filter(key => issues[key]).length">
        <MihomoBaseField v-for="key in outputFields" :key="key" :field="OUTPUT_FIELDS[key]" :state="state(outputValue(key))"
          :issues="issueOf(outputIssueKey[key] || key)" :disabled="disabled"
          @change="setOutput(key, $event)" @draft="drafts[`out.${key}`] = $event" />
      </MihomoBaseGroup>

      <template v-for="group in EASYTIER_GROUPS" :key="group.id">
        <MihomoBaseGroup v-if="advancedOpen || ['network', 'listen'].includes(group.id) || groupBlocking(group.fields) > 0" :title="group.title" :section="`easytier-${group.id}`"
          :help="group.help" :blocking="groupBlocking(group.fields)">
          <MihomoBaseField v-if="group.id === 'listen'" :field="LISTEN_FIELD" :state="state(listening)" :note="listenNote"
            :issues="issueOf('no-listener')" :disabled="disabled" data-easytier-listen @change="setListening($event === true)" />
          <template v-for="item in shownFields(group)" :key="'secret' in item ? item.secret : fieldKey(item)">
            <MihomoBaseCell v-if="'secret' in item" :label="item.label.replace(/（[^（）]*）$/, '')" :help="`YAML 键：${item.secret}\n${item.help}`"
              :html-for="`${id}-${item.secret}`" :messages="issueOf(item.secret)" :data-base-field="`easytier.${item.secret}`">
              <div class="flex min-w-0 items-center gap-0.5">
                <input :id="`${id}-${item.secret}`" :value="String(settings.options[item.secret] ?? '')" :type="shown[item.secret] ? 'text' : 'password'"
                  autocomplete="new-password" :spellcheck="false" placeholder="未填写" :class="CELL_INPUT" :disabled="disabled"
                  :aria-invalid="!!issues[item.secret]" @input="setOption(item.secret, ($event.target as HTMLInputElement).value)"
                  @keydown.enter.prevent="($event.target as HTMLElement).blur()" />
                <button type="button" class="inline-flex size-11 shrink-0 items-center justify-center rounded-md text-muted-foreground outline-none hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-primary-accent disabled:opacity-50 sm:size-8"
                  :disabled="disabled" :aria-label="`${shown[item.secret] ? '隐藏' : '显示'}${item.label}`" :aria-pressed="!!shown[item.secret]"
                  @click="shown[item.secret] = !shown[item.secret]">
                  <EyeOff v-if="shown[item.secret]" class="size-4" /><Eye v-else class="size-4" />
                </button>
              </div>
            </MihomoBaseCell>
            <MihomoBaseField v-else :field="fieldFor(item)" :state="state(settings.options[optionKey(item)])" :issues="issueOf(optionKey(item))"
              :disabled="disabled" @change="setOption(optionKey(item), $event)" @draft="drafts[optionKey(item)] = $event" />
          </template>
        </MihomoBaseGroup>
      </template>
      <div class="flex min-w-0 flex-wrap items-center gap-2 border-t border-border px-4 py-2 md:px-5">
        <button type="button" class="min-h-11 rounded-md px-2 text-xs text-primary-accent outline-none hover:bg-accent focus-visible:ring-2 focus-visible:ring-primary-accent sm:min-h-8"
          :aria-expanded="advancedOpen" data-easytier-advanced @click="advancedOpen = !advancedOpen">
          {{ advancedOpen ? '收起更多参数' : '更多参数' }}
        </button>
        <span v-if="!advancedOpen" class="text-xs text-muted-foreground">
          路由与出口、安全、传输、高级（已填写 {{ EASYTIER_GROUPS.filter(group => !['network', 'listen'].includes(group.id)).reduce((sum, group) => sum + configured(group.fields), 0) }} 项）
        </span>
      </div>
    </div>
  </section>
</template>
