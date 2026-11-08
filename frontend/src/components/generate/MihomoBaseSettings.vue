<script setup lang="ts">
import { computed, onUnmounted, reactive, ref, watch } from 'vue'
import FieldHelp from '@/components/common/FieldHelp.vue'
import {
  ADVANCED_FIELDS, MANAGED_FIELDS, MANAGED_NOTES, PORT_FIELDS, allowLanField, bindAddressField, fakeIpFilterField, fakeIpFilterModeField,
  findProcessField, forceDomainField, logLevelField, skipDomainField, snifferEnableField, type BaseFieldDef,
} from '@/lib/mihomoBaseCopy'
import {
  applyDnsPreset, checkMihomoBase, editMihomoBase, pathId, readDnsPreset, readMihomoBase, splitHostPort,
  type BaseContext, type BaseIssue, type BasePath, type FieldKind, type MihomoTakeover,
} from '@/lib/mihomoBaseSettings'
import MihomoBaseField from './MihomoBaseField.vue'
import MihomoBaseCell from './MihomoBaseCell.vue'
import MihomoBaseGroup from './MihomoBaseGroup.vue'
import MihomoControllerSettings from './MihomoControllerSettings.vue'
import MihomoDnsPresetSettings from './MihomoDnsPresetSettings.vue'
import { shortLabel } from './mihomoBaseCell'

export interface MihomoBaseServerContext {
  takeover?: MihomoTakeover
  reserved_ports?: Array<{ port: number; owner: string }>
  local_addresses?: string[]
}
const content = defineModel<string>({ required: true })
const props = defineProps<{ savedContent: string; defaultConfig?: string; context?: MihomoBaseServerContext | null; disabled?: boolean; open?: boolean }>()
const emit = defineEmits<{ validation: [value: { invalid: boolean; dirty: boolean }] }>()

const view = computed(() => readMihomoBase(content.value, props.defaultConfig))
const checkContext = computed<BaseContext>(() => ({ reservedPorts: props.context?.reserved_ports || [], localAddresses: props.context?.local_addresses || [] }))
const issues = computed(() => checkMihomoBase(content.value, props.savedContent, props.defaultConfig, checkContext.value))
const issuesFor = (field: string): BaseIssue[] => issues.value.filter(item => item.field === field)
const drafts = reactive<Record<string, string>>({})
const editError = ref('')
const locked = computed(() => props.disabled || !!view.value.error)

function change(path: BasePath, value: unknown, kind?: FieldKind) {
  if (locked.value) return
  try {
    content.value = editMihomoBase(content.value, [{ path, value, kind }], props.defaultConfig)
    editError.value = ''
  } catch (failure) {
    editError.value = failure instanceof Error ? failure.message : '无法修改，请在下方 YAML 中调整。'
  }
}
function setDraft(field: string, error: string) {
  if (error) drafts[field] = error
  else delete drafts[field]
}
function changeSniffer(value: unknown) {
  if (locked.value) return
  const edits = [{ path: ['sniffer', 'enable'], value }]
  // Mihomo sniffs nothing without protocols; enabling fills the template's common ports once.
  if (value === true && view.value.value(['sniffer', 'sniff']) === undefined && view.value.value(['sniffer', 'sniffing']) === undefined) {
    edits.push({ path: ['sniffer', 'sniff'], value: { HTTP: { ports: [80, '8080-8880'], 'override-destination': true }, TLS: { ports: [443, 8443] }, QUIC: { ports: [443, 8443] } } } as never)
  }
  try {
    content.value = editMihomoBase(content.value, edits.map(edit => ({ ...edit, kind: edit.path[1] === 'sniff' ? 'any' : 'scalar' })), props.defaultConfig)
    editError.value = ''
  } catch (failure) {
    editError.value = failure instanceof Error ? failure.message : '无法修改，请在下方 YAML 中调整。'
  }
}
function choosePreset(preset: 'domestic' | 'split') {
  if (locked.value) return
  try {
    content.value = applyDnsPreset(content.value, preset, props.defaultConfig)
    editError.value = ''
  } catch (failure) {
    editError.value = failure instanceof Error ? failure.message : '无法切换 DNS 方案，请在下方 YAML 中调整。'
  }
}
// A port switched on gets its usual number, or the next one nothing else uses.
function freePort(suggested: number) {
  const used = new Set<number>()
  for (const field of PORT_FIELDS) {
    const value = view.value.value(field.path)
    if (typeof value === 'number') used.add(value)
  }
  for (const item of props.context?.reserved_ports || []) used.add(item.port)
  for (const listen of [view.value.value(['external-controller']), view.value.value(['dns', 'listen'])]) {
    const port = splitHostPort(listen)?.port
    if (port) used.add(port)
  }
  let port = suggested
  while (used.has(port) && port < 65535) port++
  return port
}

const sections = {
  controller: ['secret', 'external-controller', 'external-ui', 'external-ui-url'],
  ports: [...PORT_FIELDS.map(field => field.key), 'listeners', 'dns.listen'],
  lan: ['allow-lan', 'bind-address'],
  dns: ['dns.default-nameserver', 'dns.nameserver', 'dns.proxy-server-nameserver', 'dns.direct-nameserver', 'dns.fallback', 'dns.nameserver-policy', 'dns.proxy-server-nameserver-policy', 'dns.respect-rules', 'dns.direct-nameserver-follow-policy'],
  fakeip: ['dns.fake-ip-filter-mode', 'dns.fake-ip-filter'],
  sniffer: ['sniffer.enable', 'sniffer.skip-domain', 'sniffer.force-domain'],
  process: ['find-process-mode', 'log-level'],
  advanced: ADVANCED_FIELDS.map(field => pathId(field.path)),
} as const
type Section = keyof typeof sections
const count = (section: Section, blocking: boolean) => issues.value.filter(item => item.blocking === blocking && (sections[section] as readonly string[]).includes(item.field)).length
  + (blocking ? Object.keys(drafts).filter(field => (sections[section] as readonly string[]).includes(field)).length : 0)
const blockingCount = computed(() => issues.value.filter(item => item.blocking).length + Object.keys(drafts).length)
watch(() => [blockingCount.value, Object.keys(drafts).length, props.open] as const, () => {
  emit('validation', { invalid: !!props.open && blockingCount.value > 0, dirty: !!props.open && Object.keys(drafts).length > 0 })
}, { immediate: true })
onUnmounted(() => emit('validation', { invalid: false, dirty: false }))
watch(() => [props.open, props.savedContent], () => { editError.value = '' })

const value = (path: BasePath) => view.value.value(path)
const shown = (item: unknown) => item === undefined ? '未填写' : Array.isArray(item) ? (item.length ? item.join(', ') : '[]') : typeof item === 'object' && item !== null ? JSON.stringify(item) : String(item)
const takeover = computed(() => props.context?.takeover ?? null)
const managedNote = computed(() => MANAGED_NOTES[takeover.value ?? 'none'])
const syncHelp = '表单与下方 YAML 双向同步：点值直接修改，几选一和列表在弹出的小窗里改，Esc 撤回正在输入的修改。修改表单只改对应的键，其他键和大部分注释原样保留（表单会按 YAML 标准格式重新排版这份文本，例如统一缩进和引号）；在 YAML 中修改后表单立即更新。YAML 无法解析时表单停用，不会覆盖你的内容。新规则只拦截本次改动引入的问题，已保存配置中原有的问题显示为提醒，不阻止保存其他改动。保存时服务器做同样的检查。'
const fieldState = (field: BaseFieldDef) => view.value.field(field.path, field.type === 'list' ? 'list' : 'scalar')
</script>

<template>
  <section class="min-w-0 shrink-0 border-b border-border px-4 py-3 md:px-5" aria-label="基础参数" data-mihomo-base-settings>
    <div class="flex min-h-11 min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
      <span class="flex items-center gap-1 text-[13px] font-medium">基础参数<FieldHelp label="基础参数" :text="syncHelp" /></span>
      <span class="min-w-0 text-xs text-muted-foreground">表单与下方 YAML 同步，点值直接修改</span>
    </div>
    <p v-if="view.error" class="mb-2 mt-0 break-words text-xs leading-relaxed text-warning-accent" role="alert" data-base-yaml-error>
      {{ view.error }} 表单已停用，请先在下方 YAML 中修正。
    </p>
    <p v-else-if="blockingCount" class="mb-2 mt-0 break-words text-xs leading-relaxed text-destructive-accent" role="alert" data-base-blocking>
      有 {{ blockingCount }} 处需要修正后才能保存，见标红的分组。
    </p>
    <p v-if="editError" class="mb-2 mt-0 break-words text-xs leading-relaxed text-warning-accent" role="alert">{{ editError }}</p>
    <!-- Unreadable YAML has no current values; the sheet stays hidden rather than showing defaults as if they were set. -->
    <div v-if="!view.error" class="min-w-0 overflow-hidden rounded-lg border border-border-strong bg-card" :inert="locked" :aria-disabled="locked" data-base-sheet>
      <MihomoBaseGroup section="controller" title="控制面板" :blocking="count('controller', true)" :warnings="count('controller', false)">
        <MihomoControllerSettings :secret="view.field(['secret'])" :controller="view.field(['external-controller'])" :ui="view.field(['external-ui'])"
          :ui-url="view.field(['external-ui-url'])" :issues="issuesFor" :disabled="locked" @change="change" @draft="setDraft" />
      </MihomoBaseGroup>

      <MihomoBaseGroup section="ports" title="代理端口" :blocking="count('ports', true)" :warnings="count('ports', false)">
        <MihomoBaseField v-for="field in PORT_FIELDS" :key="field.key" :field="field" :state="fieldState(field)" :issues="issuesFor(field.key)"
          :toggle="freePort(field.suggested)" :disabled="locked" @change="change(field.path, $event)" @draft="setDraft(field.key, $event)" />
        <p v-for="item in [...issuesFor('dns.listen'), ...issuesFor('listeners')]" :key="item.message" class="col-span-full m-0 break-words px-2 text-[11.5px]"
          :class="item.blocking ? 'text-destructive-accent' : 'text-warning-accent'" :role="item.blocking ? 'alert' : undefined" data-base-issue>{{ item.message }}</p>
      </MihomoBaseGroup>

      <MihomoBaseGroup section="lan" title="局域网访问" :blocking="count('lan', true)" :warnings="count('lan', false)">
        <MihomoBaseField v-for="field in [allowLanField, bindAddressField]" :key="pathId(field.path)" :field="field" :state="fieldState(field)"
          :issues="issuesFor(pathId(field.path))" :disabled="locked" @change="change(field.path, $event)" />
      </MihomoBaseGroup>

      <MihomoBaseGroup section="dns" title="DNS 上游" :blocking="count('dns', true)" :warnings="count('dns', false)">
        <MihomoDnsPresetSettings :view="view" :issues="issuesFor" :disabled="locked" @change="change" @preset="choosePreset" />
      </MihomoBaseGroup>

      <MihomoBaseGroup section="fakeip" title="Fake-IP 过滤" :blocking="count('fakeip', true)" :warnings="count('fakeip', false)">
        <MihomoBaseField v-for="field in [fakeIpFilterModeField, fakeIpFilterField]" :key="pathId(field.path)" :field="field" :state="fieldState(field)"
          :issues="issuesFor(pathId(field.path))" :disabled="locked" @change="change(field.path, $event, field.type === 'list' ? 'list' : 'scalar')" />
      </MihomoBaseGroup>

      <MihomoBaseGroup section="sniffer" title="域名嗅探" :blocking="count('sniffer', true)" :warnings="count('sniffer', false)">
        <MihomoBaseField :field="snifferEnableField" :state="fieldState(snifferEnableField)" :issues="issuesFor('sniffer.enable')" :disabled="locked"
          :note="value(['sniffer', 'enable']) === true ? '' : '关闭后，按 IP 进入的连接无法匹配域名规则。'" @change="changeSniffer" />
        <MihomoBaseField v-for="field in [skipDomainField, forceDomainField]" :key="pathId(field.path)" :field="field" :state="fieldState(field)"
          :issues="issuesFor(pathId(field.path))" :disabled="locked" @change="change(field.path, $event, 'list')" />
      </MihomoBaseGroup>

      <MihomoBaseGroup section="process" title="进程匹配与日志" :blocking="count('process', true)" :warnings="count('process', false)">
        <MihomoBaseField v-for="field in [findProcessField, logLevelField]" :key="pathId(field.path)" :field="field" :state="fieldState(field)"
          :issues="issuesFor(pathId(field.path))" :disabled="locked" @change="change(field.path, $event)" />
      </MihomoBaseGroup>

      <MihomoBaseGroup section="advanced" title="进阶" :blocking="count('advanced', true)" :warnings="count('advanced', false)"
        help="TUN 协议栈与 MTU、延迟与并发、TLS 指纹、TCP 保活、缓存和下载 User-Agent。一般保持初始模板的值即可，遇到兼容性问题再调整。">
        <MihomoBaseField v-for="field in ADVANCED_FIELDS" :key="pathId(field.path)" :field="field" :state="fieldState(field)"
          :issues="issuesFor(pathId(field.path))" :disabled="locked" @change="change(field.path, $event)" @draft="setDraft(pathId(field.path), $event)" />
      </MihomoBaseGroup>

      <MihomoBaseGroup section="managed" title="接管与 DNS 监听" readonly>
        <div class="contents" data-base-managed>
          <MihomoBaseCell v-for="field in MANAGED_FIELDS" :key="pathId(field.path)" :label="shortLabel(field.label)" :help="`YAML 键：${pathId(field.path)}\n${field.help}`"
            :data-base-managed-field="pathId(field.path)">
            <p class="m-0 flex min-h-8 items-center break-all px-1.5 font-mono text-[13px] text-muted-foreground" data-base-managed-value>{{ shown(value(field.path)) }}</p>
          </MihomoBaseCell>
        </div>
        <p class="col-span-full m-0 break-words px-2 pt-1 text-[11.5px] leading-relaxed text-muted-foreground" data-base-managed-note>{{ managedNote }}</p>
      </MihomoBaseGroup>
    </div>
  </section>
</template>
