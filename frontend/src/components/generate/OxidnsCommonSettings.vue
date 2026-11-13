<template>
  <section class="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden rounded-lg border border-border" aria-label="OxiDNS 常用设置">
    <Tabs v-model="tab" class="flex min-h-0 flex-1 flex-col gap-0">
      <TabsList variant="strip" class="h-auto w-full shrink-0 justify-start gap-0.5 overflow-x-auto rounded-none border-b border-border bg-muted/30 p-2">
        <TabsTrigger v-for="item in tabs" :key="item.value" :value="item.value" class="h-11 min-h-11 flex-none px-3 text-xs md:flex-1" :disabled="disabled">{{ item.label }}</TabsTrigger>
      </TabsList>
      <div class="min-h-0 min-w-0 flex-1 overflow-y-auto p-3 md:p-5">
        <TabsContent v-if="visitedTabs.has('core')" force-mount value="core" class="mt-0 space-y-5 data-[state=inactive]:hidden">
          <div class="flex flex-wrap items-center gap-1">
            <h3 class="mr-auto text-sm font-semibold">核心设置</h3>
            <Button variant="outline" type="button" class="min-h-11 text-xs" :disabled="disabled || !canApplyPerformance || Boolean(activeError())" @click="applyPerformance">填入已验证性能参数</Button>
            <FieldHelp label="已验证性能参数" :text="canApplyPerformance ? optionHelp('想使用已验证的一组性能参数，且当前结构支持编辑。', '填入 2 线程、3600 秒过期缓存保留及并发和备用参数。保留上游地址、规则及自定义参数，仅改草稿，需统一保存。') : '当前性能设置使用自定义结构，请通过完整参数编辑。'" />
          </div>
          <OxidnsFields :settings="draft" :fields="coreFields" :disabled="disabled" :capabilities="fieldCapabilities" :navigation-disabled="navigationDisabled" @change="change" @error="setError" @navigate="navigate" />
          <OxidnsFields class="border-t border-border pt-4" :settings="draft" :fields="hostFields" :disabled="disabled" :capabilities="fieldCapabilities" :navigation-disabled="navigationDisabled" @change="change" @error="setError" @navigate="navigate" />
        </TabsContent>
        <TabsContent v-if="visitedTabs.has('upstreams')" force-mount value="upstreams" class="mt-0 space-y-4 data-[state=inactive]:hidden"><h3 class="text-sm font-semibold">DNS 上游</h3><OxidnsFields :settings="draft" :fields="upstreamFields" :disabled="disabled" :capabilities="fieldCapabilities" :navigation-disabled="navigationDisabled" @change="change" @error="setError" @navigate="navigate" /></TabsContent>
        <TabsContent v-if="visitedTabs.has('cache')" force-mount value="cache" class="mt-0 space-y-4 data-[state=inactive]:hidden"><h3 class="text-sm font-semibold">缓存</h3><OxidnsFields :settings="draft" :fields="cacheFields" :disabled="disabled" :capabilities="fieldCapabilities" :navigation-disabled="navigationDisabled" @change="change" @error="setError" @navigate="navigate" /></TabsContent>
        <TabsContent v-if="visitedTabs.has('features')" force-mount value="features" class="mt-0 space-y-3 data-[state=inactive]:hidden">
          <Collapsible v-for="feature in featureGroups" :key="feature.key" :default-open="feature.key === initialFeature" class="group rounded-lg border border-border bg-card" @update:open="open => { if (open) openedFeatures.add(feature.key) }">
            <div class="relative flex min-h-12 items-center justify-between gap-2 px-3 py-3 text-sm font-medium">
              <CollapsibleTrigger type="button" :aria-label="`${feature.title}设置`" :disabled="disabled" class="absolute inset-0 cursor-pointer rounded-lg outline-none focus-visible:ring-2 focus-visible:ring-primary-accent disabled:opacity-50" />
              <span class="pointer-events-none flex min-w-0 flex-1 items-center gap-1"><span class="min-w-0">{{ feature.title }}</span><span class="pointer-events-auto relative z-10"><FieldHelp :label="feature.title" :text="feature.help" /></span></span>
              <span class="chip pointer-events-none shrink-0" :class="!isCustom(`${feature.key}.enabled`) && read(`${feature.key}.enabled`) ? 'chip-ok' : ''">{{ isCustom(`${feature.key}.enabled`) ? '自定义配置' : read(`${feature.key}.enabled`) ? '已启用' : '未启用' }}</span><ChevronDown class="pointer-events-none size-4 shrink-0 text-muted-foreground group-data-[state=open]:rotate-180" />
            </div>
            <p class="m-0 px-3 pb-3 text-xs leading-relaxed text-muted-foreground">{{ feature.hint }}</p>
            <CollapsibleContent v-if="openedFeatures.has(feature.key)" force-mount class="space-y-4 border-t border-border p-3 data-[state=closed]:hidden">
              <div v-if="isCustom(`${feature.key}.enabled`)" class="flex flex-wrap items-center gap-1 text-xs text-muted-foreground"><span>自定义配置</span><FieldHelp :label="`${feature.title}配置状态`" :text="capability(`${feature.key}.enabled`)?.reason || customReason" /><Button type="button" variant="ghost" class="ml-auto min-h-11 text-xs" :disabled="disabled || navigationDisabled" @click="navigate(capability(`${feature.key}.enabled`)?.target || feature.key)">完整参数<ChevronRight class="size-3.5" /></Button></div>
              <div v-else class="flex min-h-11 items-center gap-3"><Switch :id="`oxi-enable-${feature.key}`" :aria-label="`启用${feature.title}`" :model-value="Boolean(read(`${feature.key}.enabled`))" :disabled="disabled" @update:model-value="change(`${feature.key}.enabled`, $event)" /><Label :for="`oxi-enable-${feature.key}`">启用{{ feature.title }}</Label></div>
              <OxidnsFields v-show="feature.key === 'rule_update' || (read(`${feature.key}.enabled`) && !isCustom(`${feature.key}.enabled`))" :settings="draft" :fields="featureFields(feature)" :disabled="disabled" :capabilities="fieldCapabilities" :navigation-disabled="navigationDisabled" @change="change" @error="setError" @navigate="navigate" />
            </CollapsibleContent>
          </Collapsible>
        </TabsContent>
        <TabsContent v-if="visitedTabs.has('access')" force-mount value="access" class="mt-0 space-y-4 data-[state=inactive]:hidden">
          <h3 class="text-sm font-semibold">管理 API / WebUI</h3>
          <OxidnsFields :settings="draft" :fields="apiFields" :disabled="disabled" :capabilities="fieldCapabilities" :navigation-disabled="navigationDisabled" @change="change" @error="setError" @navigate="navigate" />
          <section v-if="read('api.enabled')" ref="apiAuthSection" class="space-y-3 rounded-lg border border-border bg-card p-3" aria-labelledby="oxi-api-auth-title">
            <div class="flex flex-wrap items-center gap-1">
              <h4 id="oxi-api-auth-title" class="text-sm font-medium">访问认证（可选）</h4>
              <FieldHelp label="访问认证" :text="optionHelp('API 监听局域网地址，或需要限制谁能打开 WebUI 时设置。', '用户名和密码需同时填写，两项都留空则不验证身份。DNS 面板和规则更新会自动使用这组凭据，无需另行配置。')" />
              <span class="chip ml-auto shrink-0" :class="apiAuthSet && 'chip-ok'">{{ apiAuthSet ? '已设置' : '未设置' }}</span>
            </div>
            <p class="mt-0 text-xs leading-relaxed" :class="apiAuthWarning ? 'text-warning-accent' : 'text-muted-foreground'" role="status">{{ apiAuthNote }}</p>
            <OxidnsFields :settings="draft" :fields="apiAuthFields" :disabled="disabled" :capabilities="fieldCapabilities" :navigation-disabled="navigationDisabled" @change="change" @error="setError" @navigate="navigate" />
            <p v-if="errors['api.auth'] && !apiAuthCustom" :role="apiAuthIncomplete ? undefined : 'alert'" class="m-0 text-xs" :class="apiAuthIncomplete ? 'text-muted-foreground' : 'text-destructive-accent'">{{ errors['api.auth'] }}</p>
          </section>
          <OxidnsFields :settings="draft" :fields="apiExtraFields" :disabled="disabled" :capabilities="fieldCapabilities" :navigation-disabled="navigationDisabled" @change="change" @error="setError" @navigate="navigate" />
          <h3 class="border-t border-border pt-4 text-sm font-semibold">日志</h3>
          <OxidnsFields :settings="draft" :fields="logFields" :disabled="disabled" :capabilities="fieldCapabilities" :navigation-disabled="navigationDisabled" @change="change" @error="setError" @navigate="navigate" />
        </TabsContent>
        <TabsContent v-if="visitedTabs.has('advanced')" force-mount value="advanced" class="mt-0 space-y-4 data-[state=inactive]:hidden">
          <OxidnsFields :settings="draft" :fields="positionFields" :disabled="disabled" :capabilities="fieldCapabilities" :navigation-disabled="navigationDisabled" @change="change" @error="setError" @navigate="navigate" />
          <div v-if="customMatches.length && read('custom_match_position') === 'tail' && !isCustom('custom_match_position')" role="status" class="flex flex-wrap items-center gap-2 rounded-lg bg-warning-soft p-3 text-xs">
            <p class="m-0 min-w-0 flex-1 basis-64">当前先执行分流规则，已被域名分流规则处理的请求不会再执行下方匹配。需要覆盖分流结果时，请先执行自定义匹配。</p>
            <Button type="button" variant="outline" class="min-h-11 text-xs" :disabled="disabled" @click="change('custom_match_position', 'head')">先执行自定义匹配</Button>
          </div>
          <div v-if="isCustom('custom_matches')" class="flex items-center gap-1 text-xs text-muted-foreground"><span>自定义匹配使用手工结构</span><FieldHelp label="自定义匹配配置状态" :text="capability('custom_matches')?.reason || customReason" /><Button type="button" variant="ghost" class="ml-auto min-h-11 text-xs" :disabled="disabled || navigationDisabled" @click="navigate(capability('custom_matches')?.target || 'custom_matches')">完整参数<ChevronRight class="size-3.5" /></Button></div>
          <OxidnsMatchEditor v-else :model-value="customMatches" :targets="customMatchTargets" :target-tags="customMatchTargetTags" :disabled="disabled" :allow-disable="false" @update:model-value="change('custom_matches', $event)" @error="setError('custom_matches', $event ? `自定义匹配：${$event}` : '')" />
          <div class="flex flex-wrap items-center gap-2 border-t border-border pt-3"><span class="text-xs text-muted-foreground">其他插件、网络出站和扩展字段</span><Button type="button" variant="outline" class="ml-auto min-h-11 text-xs" :disabled="disabled || navigationDisabled" @click="emit('full')">完整参数<ChevronRight class="size-3.5" /></Button></div>
        </TabsContent>
      </div>
    </Tabs>
    <div class="flex shrink-0 items-center gap-1 border-t border-border px-3 py-1.5 text-xs text-muted-foreground"><span>编辑当前配置，最后统一保存</span><FieldHelp label="常用设置同步" :text="optionHelp('希望通过表单修改现有配置，无需反复切换编辑入口。', '这里展示实际配置值，修改会同步到完整参数和 YAML。原生插件、未展示参数及附加文件均保留；自定义结构请到完整参数编辑。')" /><Button type="button" variant="ghost" class="ml-auto min-h-11 shrink-0 text-xs" :disabled="disabled || navigationDisabled" @click="emit('full')">全部功能<ChevronRight class="size-3.5" /></Button></div>
  </section>
</template>
<script setup lang="ts">
import { reactive, ref, watch, computed, nextTick, onUnmounted } from 'vue'
import { ChevronDown, ChevronRight } from '@lucide/vue'
import { apiAuthFields, apiExtraFields, apiFields, applyVerifiedOxidnsTemplate, logFields, cacheFields, coreFields as sourceCoreFields, featureGroups, upstreamFields as sourceUpstreamFields, type OxidnsField } from '@/lib/oxidns'
import { optionCopy, optionHelp } from '@/lib/optionCopy'
import type { OxidnsMatchTarget } from '@/lib/oxidnsMatchActions'
import OxidnsFields, { type OxidnsFieldCapability } from './OxidnsFields.vue'
import OxidnsMatchEditor from './OxidnsMatchEditor.vue'
import FieldHelp from '@/components/common/FieldHelp.vue'
import { Button } from '@/components/ui/button'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

const props = withDefaults(defineProps<{ values: Record<string, any>; capabilities: Record<string, OxidnsFieldCapability>; customMatchTargets?: OxidnsMatchTarget[]; customMatchTargetTags?: string[]; disabled?: boolean; navigationDisabled?: boolean; initialFeature?: string }>(), { disabled: false, navigationDisabled: false, customMatchTargets: () => [] })
const emit = defineEmits<{ change: [changes: Record<string, unknown>]; error: [message: string]; navigate: [target: string]; full: [] }>()
const clone = <T,>(value: T): T => value === undefined ? value : JSON.parse(JSON.stringify(value))
const draft = reactive<Record<string, any>>(clone(props.values))
const errors = reactive<Record<string, string>>({})
const tab = ref(props.initialFeature === 'api' ? 'access' : featureGroups.some(feature => feature.key === props.initialFeature) ? 'features' : 'core')
// Build each tab (and feature section) the first time it is shown, then keep it
// mounted so drafts and field errors survive switching away.
const visitedTabs = reactive(new Set([tab.value]))
watch(tab, value => visitedTabs.add(value))
const openedFeatures = reactive(new Set(props.initialFeature ? [props.initialFeature] : []))
const customReason = '当前配置使用自定义结构，请通过完整参数编辑，现有内容会保留。'
const coreFields = sourceCoreFields
const upstreamDescriptions: Record<string, ReturnType<typeof optionCopy>> = {
  local_dns: optionCopy('为已选直连 DNS 的域名查询网站地址。', '需要设置当前配置中直连域名查询所使用的服务器。', '逐条添加正在使用的地址，展开可调整连接参数，也可切换 YAML 编辑完整上游对象。这里编辑现有配置，不会因清空列表自动补入默认地址。'),
  remote_dns: optionCopy('设置代理分流的查询上游，临时地址由它提供。', '需要调整代理分流所用上游，Fake-IP 须由外部上游提供。', '逐条填写当前上游。域名地址可用引导 DNS 或指定连接 IP，避免循环解析；标为代理 DNS 不会自动让连接走代理。'),
}
const upstreamFields = sourceUpstreamFields.map(field => upstreamDescriptions[field.path] ? { ...field, ...upstreamDescriptions[field.path], emptyHint: '尚未配置上游，点击添加上游填写地址。' } : field)
const tabs = [{ value: 'core', label: '核心设置' }, { value: 'upstreams', label: 'DNS 上游' }, { value: 'cache', label: '缓存' }, { value: 'features', label: '特色功能' }, { value: 'access', label: 'API / 日志' }, { value: 'advanced', label: '高级' }]
const hostFields: OxidnsField[] = [{ path: 'custom_hosts', label: '自定义 Hosts', type: 'lines', wide: true, placeholder: 'full:example.com 192.0.2.10 2001:db8::10', ...optionCopy('为指定域名固定返回地址，支持双栈查询。', '希望手动指定某个域名的查询结果，而非交给上游解析。', '每条填写域名规则和一个或多个 IP，支持 IPv4、IPv6。可逐条编辑，也可切换 YAML 批量填写，地址应按希望的匹配范围设置。') }]
const positionFields: OxidnsField[] = [{ path: 'custom_match_position', label: '自定义匹配执行顺序', ...optionCopy('选择先执行自定义匹配，还是先执行分流规则。', '添加自定义匹配后，需要调整执行顺序时设置。', '此处只调整与策略规则页所设域名分流的先后。先执行的一方若结束请求，就不再执行另一方。自定义匹配始终早于动态域名集和默认转发。'), options: [{ value: 'head', label: '先执行自定义匹配' }, { value: 'tail', label: '先执行分流规则' }] }]
const read = (path: string): unknown => path.split('.').reduce<unknown>((value, key) =>
  value !== null && typeof value === 'object' ? (value as Record<string, unknown>)[key] : undefined, draft)
const customMatches = computed(() => {
  const value = read('custom_matches')
  return Array.isArray(value) ? value.filter((item): item is Record<string, unknown> =>
    item !== null && typeof item === 'object' && !Array.isArray(item)) : []
})
// The backend accepts API argument edits in the same batch that enables the API,
// so don't keep them locked while the projection still reflects the disabled state.
const fieldCapabilities = computed(() => {
  const capabilities = props.capabilities
  if (!read('api.enabled') || capabilities['api.enabled']?.editable === false) return capabilities
  return Object.fromEntries(Object.entries(capabilities).map(([key, value]) =>
    [key, key.startsWith('api.') && key !== 'api.enabled' && value.editable === false && value.reason?.startsWith('请先启用 API') ? { ...value, editable: true, reason: undefined } : value]))
})
function capability(path: string): OxidnsFieldCapability | undefined {
  const parts = path.split('.')
  while (parts.length) { const found = fieldCapabilities.value[parts.join('.')]; if (found) return found; parts.pop() }
}
const isCustom = (path: string) => capability(path)?.editable === false
const ruleDownloadFields = new Set(['rule_update.args.timeout', 'rule_update.args.outbound', 'rule_update.args.socks5'])
const featureFields = (feature: typeof featureGroups[number]) => feature.key === 'rule_update'
  ? feature.fields.filter(field => ruleDownloadFields.has(field.path) || (read('rule_update.enabled') && !isCustom('rule_update.enabled')))
  : feature.fields
function write(path: string, value: unknown) {
  const parts = path.split('.'); const key = parts.pop()!
  const target = parts.reduce((object, part) => (object[part] ??= {}), draft)
  if (value === undefined) delete target[key]
  else target[key] = clone(value)
}
function change(path: string, value: unknown) {
  if (props.disabled || isCustom(path)) return
  write(path, value)
  if (path.startsWith('api.')) checkApiAuth()
  reportErrors()
  emit('change', { [path]: value === undefined ? null : clone(value) })
  // Turning the API on is when credentials matter; bring the optional block into view.
  if (path === 'api.enabled' && value === true) nextTick(() => apiAuthSection.value?.scrollIntoView?.({ block: 'nearest' }))
}
const apiAuthSection = ref<HTMLElement | null>(null)
const apiText = (path: string) => { const value = read(path); return typeof value === 'string' ? value : '' }
const apiAuthSet = computed(() => Boolean(apiText('api.username') && apiText('api.password')))
// The Agent connects to the API on the core host itself, so only these hosts keep it private.
function loopbackListen(listen: string) {
  const host = (listen.startsWith('[') ? listen.slice(1, listen.indexOf(']')) : listen.split(':').length === 2 ? listen.split(':')[0] : listen).toLowerCase()
  return host === 'localhost' || host === '::1' || /^127\.\d{1,3}\.\d{1,3}\.\d{1,3}$/.test(host)
}
const apiAuthWarning = computed(() => !apiAuthSet.value && !loopbackListen(apiText('api.listen').trim()))
const apiAuthNote = computed(() => apiAuthSet.value ? '已设置账号：DNS 面板和规则更新会自动使用这组凭据。'
  : apiAuthWarning.value ? 'API 监听在本机以外的地址且未设置账号，局域网设备可直接调用接口、查看查询记录，建议设置用户名和密码。'
    : '当前仅本机可访问 API，可以不设置账号。改为局域网监听或启用 WebUI 时，建议设置用户名和密码。')
// Mirrors the backend pair rule and the literal-only credentials the Agent can forward.
const apiAuthCustom = computed(() => isCustom('api.username') || isCustom('api.password'))
const apiAuthIncomplete = computed(() => Boolean(apiText('api.username')) !== Boolean(apiText('api.password')))
function checkApiAuth() {
  const username = apiText('api.username'), password = apiText('api.password')
  const message = apiAuthIncomplete.value ? (username ? '还需填写密码；不需要认证时请清空用户名' : '还需填写用户名；不需要认证时请清空密码')
    : username.includes(':') ? '用户名不能包含英文冒号'
      : [username, password].some(value => value.includes('${')) ? '用户名和密码不能包含 ${，DNS 面板无法读取环境变量引用'
        : [username, password].some(value => /[\u0000-\u001f\u007f-\u009f]/.test(value)) ? '用户名和密码不能包含控制字符' : ''
  errors['api.auth'] = message
}
const cacheErrorDependencies: Record<string, string[]> = {
  cache_size: ['cache_enabled'],
  cache_lazy_ttl: ['cache_enabled'],
  cache_dump_file: ['cache_enabled', 'cache_dump_enabled'],
  cache_dump_interval: ['cache_enabled', 'cache_dump_enabled'],
}
function activeError() {
  return Object.entries(errors).find(([path, message]) => message && !isCustom(path)
    && !(cacheErrorDependencies[path] || []).some(dependency => !read(dependency))
    && !featureGroups.some(feature => path.startsWith(`${feature.key}.`) && !path.endsWith('.enabled') && !ruleDownloadFields.has(path) && !read(`${feature.key}.enabled`))
    && !(path.startsWith('api.') && path !== 'api.enabled' && !read('api.enabled'))
    && !(path === 'api.auth' && apiAuthCustom.value))?.[1] || ''
}
function reportErrors() { emit('error', activeError()) }
function setError(path: string, message: string) { errors[path] = message; reportErrors() }
const navigate = (target: string) => emit('navigate', target)
const performance = applyVerifiedOxidnsTemplate({})
const availablePerformance = computed(() => Object.fromEntries(Object.entries(performance).filter(([path]) => !isCustom(path))))
const canApplyPerformance = computed(() => Object.keys(availablePerformance.value).length > 0)
function applyPerformance() {
  if (props.disabled || !canApplyPerformance.value) return
  for (const [path, value] of Object.entries(availablePerformance.value)) write(path, value)
  emit('change', clone(availablePerformance.value))
}
// Parent replaces this projection once its patch queue is drained. Reconcile the
// current draft even when the server returns the same values as before (for
// example, restoring TLS after a coalesced off/on). Keep incomplete input intact.
watch(() => props.values, value => {
  const sync = (before: any, after: any, path = '') => {
    if (JSON.stringify(before) === JSON.stringify(after)) return
    if (Object.entries(errors).some(([key, message]) => message && (path === key || path.startsWith(`${key}.`)))) return
    if (before && after && typeof before === 'object' && typeof after === 'object' && !Array.isArray(before) && !Array.isArray(after)) {
      for (const key of new Set([...Object.keys(before), ...Object.keys(after)])) sync(before[key], after[key], path ? `${path}.${key}` : key)
    } else if (path) write(path, after)
  }
  sync(draft, value)
  checkApiAuth()
  reportErrors()
}, { deep: true })
checkApiAuth()
if (activeError()) reportErrors()
onUnmounted(() => emit('error', ''))
</script>
