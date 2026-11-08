<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { onBeforeRouteLeave, onBeforeRouteUpdate, useRoute, useRouter } from 'vue-router'
import { ArrowLeft, ArrowRight, Check, CircleCheck, Eye, EyeOff, LoaderCircle, Network, Router as RouterIcon } from '@lucide/vue'
import { setupApi, type SetupDomainRule, type SetupGoal, type SetupIndex, type SetupSession } from '@/api/setup'
import { SETUP_GOALS, SETUP_STEPS, abandonSetup, setupControllerSecretWarning, setupError, setupLocalDnsWarning, setupRuleConflicts, setupRuleOptions, useLibraryNamesake } from '@/lib/setupFlow'
import { confirm } from '@/lib/feedback'
import { stripTrailingPunctuation } from '@/lib/text'
import { createDeploymentId } from '@/composables/useAgentDeployments'
import { useProfileStore } from '@/stores/profile'
import { setActiveProfileId } from '@/profileContext'
import PageHeader from '@/components/common/PageHeader.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import FormField from '@/components/common/FormField.vue'
import FieldHelp from '@/components/common/FieldHelp.vue'
import SetupSources from '@/components/setup/SetupSources.vue'
import SetupInstallation from '@/components/setup/SetupInstallation.vue'
import GatewayClientInstructions from '@/components/agents/GatewayClientInstructions.vue'
import Routers from '@/views/Routers.vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Checkbox } from '@/components/ui/checkbox'
import { Switch } from '@/components/ui/switch'
import Segmented from '@/components/common/Segmented.vue'
import MultiSelect from '@/components/common/MultiSelect.vue'

const router = useRouter()
const route = useRoute()
const profileStore = useProfileStore()
const options = ref<SetupIndex>({ sessions: [], suggested: false, subscriptions: [], nodes: [] })
const session = ref<SetupSession>()
const goal = ref<SetupGoal>('router')
const name = ref('我的分流配置')
const dnsFallback = ref<'mihomo' | 'direct'>('mihomo')
const trafficFallback = ref<'proxy' | 'direct'>('proxy')
const defaultLocalDns = 'https://223.5.5.5/dns-query'
const localDns = ref(defaultLocalDns)
const queryRecorder = ref(true)
const directRules = ref(['builtin:direct'])
const proxyRules = ref(['builtin:proxy'])
// Kept in step with IP_TAKEOVER_RULES in backend/common/setup_presets.py.
const ipTakeoverRules = ['builtin:telegram', 'builtin:googlefcm']
const ipTakeover = computed(() => proxyRules.value.some(id => ipTakeoverRules.includes(id)))
const localDnsWarning = computed(() => setupLocalDnsWarning(localDns.value))
// Typed by the user only; a saved password is never sent back to the page.
const controllerSecret = ref('')
const showControllerSecret = ref(false)
const controllerSecretWarning = computed(() => setupControllerSecretWarning(controllerSecret.value, !session.value?.controller_secret_saved))
const controllerHint = computed(() => {
  const address = session.value?.controller_address || '0.0.0.0:9090'
  const exposure = /^(0\.0\.0\.0|\[::\]|:)/.test(address) ? `控制面板监听 ${address}，局域网内的设备都能打开，靠这个密码保护。` : `控制面板监听 ${address}。`
  return session.value?.controller_secret_saved ? `${exposure}已设置密码；留空保留原密码，填写则更换。` : `${exposure}8–64 个字母、数字或符号，不含空格。`
})
const domainRules = computed(() => setupRuleOptions(options.value.domain_rules || []))
const ruleConflicts = computed(() => setupRuleConflicts(options.value.domain_rules || [], { direct: directRules.value, proxy: proxyRules.value }))
function useNamesake(rule: SetupDomainRule) {
  const next = useLibraryNamesake({ direct: directRules.value, proxy: proxyRules.value }, rule)
  directRules.value = next.direct; proxyRules.value = next.proxy
}
const directOptions = computed(() => domainRules.value.filter(item => !proxyRules.value.includes(item.value)))
const proxyOptions = computed(() => domainRules.value.filter(item => !directRules.value.includes(item.value)))
function setDomainRules(value = options.value.recommended_domain_rules ?? { direct: ['builtin:direct'], proxy: ['builtin:proxy'] }) {
  const canonical = (id: string) => options.value.domain_rules?.find(item => item.aliases?.includes(id))?.id ?? id
  directRules.value = [...new Set(value.direct.map(canonical))]
  proxyRules.value = [...new Set(value.proxy.map(canonical))]
}
const busy = ref(false)
const childBusy = ref(false)
const loading = ref(true)
const ready = ref(false)
const error = ref('')
const routerMode = ref<'manual' | 'managed'>('manual')
const makeDefault = ref(false)
const routerId = ref('')
const ackRoutes = ref(false)
const ackDNS = ref(false)
const createId = ref(createDeploymentId().replace(/-/g, ''))
const previewRouting = ref(false)
const clientChecking = ref(false)
const clientReady = ref(false)
const clientError = ref('')
let clientCheckEpoch = 0
let freshlyCheckedClient = ''
let disposed = false
const icons = { router: RouterIcon, gateway: Network }
const step = computed(() => previewRouting.value ? 4 : session.value?.step ?? 0)
const canPreviewRouting = computed(() => session.value?.goal === 'router' && session.value.prepared && session.value.max_step === 3)
const routeInstructions = computed(() => previewRouting.value ? undefined : session.value?.route_instructions)
// A forwarded rule set the server could not download leaves its ranges out of the table.
const missingRuleSets = computed(() => routeInstructions.value?.incomplete ? routeInstructions.value.missing_rule_sets || [] : [])
const routesIncomplete = computed(() => !!routeInstructions.value?.incomplete)
const working = computed(() => busy.value || childBusy.value || clientChecking.value)
const pair = computed(() => session.value?.installation)
// Opened for this wizard's profile without switching the global one. Its back link
// carries the unsaved manual choice and ticks, which a remount would otherwise reset.
const clientDevicesLink = computed(() => ({ path: '/client-devices', query: { profile: session.value?.profile_id,
  from: router.resolve({ path: '/setup', query: { session: session.value?.id, ack: [ackRoutes.value && 'routes', ackDNS.value && 'dns'].filter(Boolean).join(',') } }).fullPath } }))
function restoreManualStep(ack: unknown) {
  const current = session.value
  if (typeof ack !== 'string' || current?.goal !== 'router' || current.step !== 4 || current.completed) return
  const acks = ack.split(',')
  routerMode.value = 'manual'; ackRoutes.value = acks.includes('routes'); ackDNS.value = acks.includes('dns')
}
const routerAgents = computed(() => pair.value ? { mihomo: pair.value.mihomo.id, oxidns: pair.value.oxidns.id } : undefined)
const steps = computed(() => SETUP_STEPS.map((title, i) => i === 4 ? goal.value === 'router' ? '接入主路由' : '客户端设置' : title))
const help = [
  '两种方案都安装 Mihomo + OxiDNS。区别在于主路由按静态路由转交流量，或客户端直接将 Linux 作为网关。向导创建独立配置空间，详细选择条件见每个选项旁的问号。',
  '粘贴 HTTP(S) 订阅链接，或选择已有订阅和节点。读取失败可在这里重试，不会重复创建同一个订阅。',
  '先选择域名分流规则，再设置未命中时的默认行为。Mihomo DNS 的监听和上游由向导自动配置，无需手填。直连 DNS 使用本页填写的地址，后续安装和发布沿用这些设置。',
  '沿用前面保存的规则、直连 DNS 和漏网处理。主机 IP 可选，留空由现有 Agent 自动识别，无需单独执行检测命令或填写网卡与网段。安装后在本页发布和检查两项核心，再按所选方案接入主路由或设置客户端。',
  '主路由方案需要静态路由和 DNS 设置；网关方案需要调整测试客户端的网关与 DNS。iKuai / RouterOS 可通过现有路由器功能预览并发布；其他品牌按提供的地址和网段手动配置。',
  '配置发布、主机转发与客户端上网是不同的检查范围。设备和路由设置完成后，请在实际使用的手机或电脑验证域名解析、国内网站和需要代理的网站。'
]

async function accept(value: SetupSession) {
  if (disposed) return
  if (!SETUP_GOALS.some(item => item.id === value.goal)) throw new Error('试用方案已调整，请新建一次配置，选择主路由分流或 Linux 网关')
  // Start from the suggestion once per wizard; later responses keep the choice.
  if (value.id !== session.value?.id) makeDefault.value = value.default_profile?.suggested ?? false
  session.value = value; goal.value = value.goal; name.value = value.name
  previewRouting.value = false
  dnsFallback.value = value.dns_fallback || 'mihomo'
  trafficFallback.value = value.traffic_fallback || 'proxy'
  localDns.value = value.local_dns ?? defaultLocalDns
  // A setup prepared before this switch existed wrote the recorder off.
  queryRecorder.value = value.query_recorder ?? !value.prepared
  setDomainRules(value.domain_rules)
  routerMode.value = value.router_mode || 'manual'; routerId.value = value.router_id || ''
  await router.replace({ path: '/setup', query: { session: value.id } })
}
async function run(action: () => Promise<void>) {
  if (working.value) return
  busy.value = true; error.value = ''
  try { await action() }
  catch (failure) { if (!disposed) error.value = setupError(failure) }
  finally { if (!disposed) busy.value = false }
}
async function resume(id: string) {
  await run(async () => { const { data } = await setupApi.get(id); await accept(data) })
}
async function start() {
  await run(async () => {
    const { data } = await setupApi.create({ id: createId.value, name: name.value, goal: goal.value })
    await accept(data); await profileStore.refreshProfiles()
  })
}
async function move(index: number) {
  const current = session.value
  if (!current) return
  if (index === 4 && canPreviewRouting.value) { previewRouting.value = true; return }
  if (previewRouting.value) {
    previewRouting.value = false
    if (index === current.step) return
  }
  await run(async () => { const { data } = await setupApi.progress(current.id, { step: index }); await accept(data) })
}
async function prepare() {
  const current = session.value
  if (!current) return
  await run(async () => {
    const { data } = await setupApi.prepare(current.id, { dns_fallback: dnsFallback.value, traffic_fallback: trafficFallback.value,
      local_dns: localDns.value, domain_rules: { direct: directRules.value, proxy: proxyRules.value }, query_recorder: queryRecorder.value,
      ...(controllerSecret.value ? { controller_secret: controllerSecret.value } : {}) })
    controllerSecret.value = ''; showControllerSecret.value = false
    await accept(data); await profileStore.refreshProfiles()
  })
}
async function checkInstallation() {
  const current = session.value
  if (!current) return
  await run(async () => {
    const { data } = await setupApi.check(current.id)
    if (data.goal === 'gateway' && data.step === 4) freshlyCheckedClient = data.id
    await accept(data)
  })
}
async function selectRouter(id: string) {
  const current = session.value
  if (!current) return
  routerId.value = id
  try {
    const { data } = await setupApi.progress(current.id, { router_id: id, router_mode: 'managed' })
    if (!disposed && session.value?.id === current.id) session.value = { ...session.value, router_id: data.router_id, router_mode: data.router_mode }
  } catch (failure) { if (!disposed) error.value = setupError(failure) }
}
async function finish() {
  const current = session.value
  if (!current || previewRouting.value || (current.goal === 'gateway' && !clientReady.value)) return
  await run(async () => {
    const { data } = await setupApi.finish(current.id, {
      ...(current.goal === 'router'
        ? { router_mode: routerMode.value, router_id: routerId.value, ack_routes: ackRoutes.value, ack_dns: ackDNS.value } : {}),
      ...(current.default_profile && !current.default_profile.is_default ? { make_default: makeDefault.value } : {}) })
    await accept(data)
    if (data.default_profile?.is_default) await profileStore.refreshProfiles()
  })
}
async function checkClientReadiness() {
  const current = session.value
  if (!current || current.goal !== 'gateway' || step.value !== 4) return
  const epoch = ++clientCheckEpoch
  clientChecking.value = true; clientReady.value = false; clientError.value = ''
  try {
    const { data } = await setupApi.check(current.id)
    if (disposed || epoch !== clientCheckEpoch || session.value?.id !== current.id || step.value !== 4) return
    session.value = data; clientReady.value = true
  } catch (failure) {
    if (!disposed && epoch === clientCheckEpoch) clientError.value = setupError(failure)
  } finally { if (!disposed && epoch === clientCheckEpoch) clientChecking.value = false }
}
// A re-check can add ranges; the confirmation was for the earlier table.
watch(() => routeInstructions.value?.routes.map(item => item.cidr).join(' '), (now, before) => {
  if (before !== undefined && now !== before) ackRoutes.value = false
})
watch(() => session.value?.goal === 'gateway' && step.value === 4 ? session.value.id : '', id => {
  clientCheckEpoch++; clientReady.value = false; clientChecking.value = false; clientError.value = ''
  if (id && freshlyCheckedClient === id) { freshlyCheckedClient = ''; clientReady.value = true }
  else if (id) void checkClientReadiness()
})
async function manage(target: '/agents' | '/routers' | '/profiles' | '/subscriptions' | '/client-devices') {
  if (!session.value) return
  await profileStore.refreshProfiles(); setActiveProfileId(session.value.profile_id); await router.push(target)
}
async function abandonCurrent() {
  if (!session.value || working.value) return
  busy.value = true; error.value = ''
  try {
    const id = session.value.id
    const result = await abandonSetup(id)
    if (!result || disposed) return
    options.value.sessions = result.delete_profile
      ? options.value.sessions.filter(item => item.id !== id)
      : options.value.sessions.map(item => item.id === id ? { ...item, abandoned: true } : item)
    if (result.delete_profile) await profileStore.refreshProfiles()
    newSetup()
  } catch (failure) { if (!disposed) error.value = setupError(failure) }
  finally { busy.value = false }
}
function newSetup() {
  session.value = undefined; error.value = ''; childBusy.value = false; ackRoutes.value = false; ackDNS.value = false
  previewRouting.value = false
  createId.value = createDeploymentId().replace(/-/g, ''); void router.replace('/setup')
}
async function load() {
  loading.value = true; error.value = ''
  try {
    const { data } = await setupApi.index()
    if (disposed) return
    options.value = { ...data, sessions: data.sessions.filter(item => SETUP_GOALS.some(goal => goal.id === item.goal)) }
    ready.value = true
    // accept() rewrites the query, so read the return state first.
    const ack = route.query.ack
    if (typeof route.query.session === 'string') { await resume(route.query.session); restoreManualStep(ack) }
  } catch (failure) { if (!disposed) error.value = setupError(failure) }
  finally { if (!disposed) loading.value = false }
}
watch(() => route.query.session, id => {
  if (!ready.value || loading.value || working.value) return
  if (typeof id === 'string' && id !== session.value?.id) void resume(id)
  else if (!id) session.value = undefined
})
onMounted(load)
onBeforeRouteUpdate(to => !working.value || to.query.session === session.value?.id)
onBeforeRouteLeave(async () => !working.value || await confirm('当前操作仍在处理中，离开后可从向导记录继续。现在离开吗？', { title: '离开配置向导', confirmText: '离开' }))
onUnmounted(() => { disposed = true })
</script>

<template>
  <div class="mx-auto max-w-5xl">
    <PageHeader title="开始使用"><template #actions><Button v-if="session && !session.completed" variant="ghost" class="min-h-11" :disabled="working" @click="abandonCurrent">放弃此向导</Button><Button variant="outline" class="min-h-11" :disabled="working" @click="session ? newSetup() : router.push('/dashboard')">{{ session ? '新建一次配置' : '返回总览' }}</Button></template></PageHeader>
    <p v-if="loading" role="status" class="py-12 text-center text-sm text-muted-foreground">正在读取向导进度…</p>
    <template v-else>
      <div v-if="error" role="alert" class="mb-4 rounded-lg border border-destructive-accent/30 bg-destructive-soft px-4 py-3 text-sm text-destructive-accent">{{ error }}<Button v-if="!ready" variant="ghost" class="ml-2" @click="load">重新加载</Button></div>
      <div class="grid grid-cols-[170px_minmax(0,1fr)] gap-5 max-md:grid-cols-1 max-md:gap-3">
        <nav aria-label="配置向导步骤" class="min-w-0"><ol class="m-0 flex list-none flex-col gap-1 p-0 max-md:flex-row max-md:overflow-x-auto">
          <li v-for="(label, index) in steps" :key="index" class="shrink-0"><button type="button" :disabled="working || !session || (index > session.max_step && !(index === 4 && canPreviewRouting))" :aria-current="step === index ? 'step' : undefined" class="flex min-h-11 w-full items-center gap-2.5 rounded-lg px-3 text-left text-[13px] disabled:cursor-default focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-accent" :class="step === index ? 'bg-primary-soft font-medium text-primary-accent' : index < (session?.step ?? 0) ? 'text-foreground' : 'text-muted-foreground'" @click="move(index)"><span class="grid size-6 shrink-0 place-items-center rounded-full border border-border-strong font-mono text-xs"><Check v-if="session?.completed || index < (session?.step ?? 0)" class="size-3.5" /><template v-else>{{ index + 1 }}</template></span>{{ label }}<span v-if="index === 4 && canPreviewRouting" class="ml-auto text-xs text-muted-foreground">{{ goal === 'router' ? '可先配置' : '预览' }}</span></button></li>
        </ol></nav>
        <SectionCard :title="steps[step]" :help="help[step]" class="min-w-0">
          <div v-if="session" class="mb-6 flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-border pb-4 text-xs text-muted-foreground"><span class="break-words font-medium text-foreground">{{ session.name }}</span><span>{{ SETUP_GOALS.find(item => item.id === session?.goal)?.title }}</span></div>
          <p v-if="session?.legacy_takeover && (step === 3 || step === 4)" role="status" class="mt-0 mb-5 rounded-lg border border-primary-accent/30 bg-primary-soft px-3 py-2.5 text-sm leading-relaxed text-foreground" data-legacy-takeover>此配置空间使用旧版 Linux 网关接管，保持不变。</p>
          <div v-if="session?.mihomo_changes?.length && step === 3" role="status" class="mt-0 mb-5 space-y-1 rounded-lg bg-muted px-3 py-2.5 text-sm leading-relaxed" data-mihomo-changes><p class="m-0 font-medium">已调整 Mihomo 基础配置的以下项，其余设置原样保留：</p><ul class="m-0 list-disc space-y-0.5 pl-5 text-xs text-muted-foreground"><li v-for="item in session.mihomo_changes" :key="item" class="break-words">{{ item }}</li></ul></div>
          <div v-if="step === 0" class="space-y-5">
            <FormField label="选择分流方式" help="两种方式都需要原生 Linux、管理员权限、固定局域网 IP 和可用代理节点。按能调整主路由的静态路由，还是能调整客户端网关来选择。详细条件见每个选项旁的问号。">
              <label v-for="item in SETUP_GOALS" :key="item.id" class="flex min-h-20 cursor-pointer items-center gap-3 rounded-lg border p-3" :class="goal === item.id ? 'border-primary-accent/50 bg-primary-soft' : 'border-border'">
                <input v-model="goal" type="radio" name="setup-goal" :value="item.id" :disabled="!!session" class="size-4 accent-primary" /><component :is="icons[item.id]" class="size-5 shrink-0 text-primary-accent" />
                <span class="min-w-0 flex-1"><span class="block text-sm font-medium">{{ item.title }}</span><span class="block text-xs leading-relaxed text-muted-foreground">{{ item.detail }}</span></span><FieldHelp :label="item.title" :text="item.help" />
              </label>
            </FormField>
            <FormField label="配置名称" html-for="setup-name" help="向导创建独立配置空间，订阅与节点保留在共享资源中。"><Input id="setup-name" v-model="name" :maxlength="64" :disabled="working || !!session" @keydown.enter="!session && start()" /></FormField>
            <Button v-if="!session" :disabled="working || !ready || !name.trim()" class="min-h-11" @click="start"><LoaderCircle v-if="working" class="size-4 animate-spin" />创建配置并继续<ArrowRight class="size-4" /></Button>
            <Button v-else class="min-h-11" @click="move(1)">继续当前配置<ArrowRight class="size-4" /></Button>
            <div v-if="!session && options.sessions.length" class="border-t border-border pt-4"><div class="mb-2 flex items-center gap-1"><h3 class="m-0 text-sm font-medium">最近的向导</h3><FieldHelp label="最近的向导" text="进度保存在服务端，刷新或更换浏览器后仍可继续。" /></div><button v-for="item in options.sessions.slice(0, 5)" :key="item.id" class="flex min-h-11 w-full items-center gap-2 border-b border-border/50 py-2 text-left text-sm" :disabled="working" @click="resume(item.id)"><span class="min-w-0 flex-1 truncate">{{ item.name }}</span><span class="text-xs text-muted-foreground">{{ item.completed ? '已完成' : item.abandoned ? '已放弃' : SETUP_STEPS[item.step] }}</span><ArrowRight class="size-3.5" /></button></div>
          </div>

          <SetupSources v-if="step === 1 && session" :key="session.id" :session="session" :options="options" @saved="accept" @busy="childBusy = $event" />
          <Button v-if="step === 1 && session?.prepared" class="mt-4" @click="move(2)">查看规则与 DNS<ArrowRight class="size-4" /></Button>
          <div v-if="step === 2 && session" class="space-y-5">
            <section class="space-y-3" aria-labelledby="setup-domain-title">
              <div class="flex flex-wrap items-center gap-1"><h3 id="setup-domain-title" class="m-0 text-sm font-semibold">分流规则</h3><FieldHelp label="分流规则" text="直接复用 ConfigFlow 原有初始模板的规则库、顺序及默认去向。支持 URL 和内容型的 Domain / Classical text / YAML 规则；Classical 中的域名与 IP 条件由现有转换流程处理。已有相同来源，或手写内容匹配的域名相同，会直接复用；规则库已有同名但内容不同的规则集时会提示改用。可取消或替换规则，同一规则集只能选择一种去向。两项核心引用相同规则；按模板顺序匹配，未命中时按下方设置处理。策略组沿用默认配置：代理规则各自使用对应的服务组（如 📲 Telegram、🤖 人工智能），默认跟随「✈️ 节点选择」，安装后可在 Mihomo 面板逐个切换；订阅中没有节点的地区组不会生成。" /><Button variant="ghost" size="sm" class="ml-auto min-h-11" :disabled="working || session?.completed" @click="setDomainRules()">恢复推荐</Button></div>
              <p class="text-xs text-muted-foreground">已选 <span class="num">{{ directRules.length }}</span> 组直连、<span class="num">{{ proxyRules.length }}</span> 组代理规则，可直接继续。</p>
              <div class="grid gap-3 sm:grid-cols-2">
                <FormField label="直连规则" html-for="setup-direct-rules" hint="使用直连 DNS" persistent-hint>
                  <MultiSelect v-model="directRules" trigger-id="setup-direct-rules" :options="directOptions" :disabled="working || session?.completed" :max-visible="2" placeholder="选择直连规则集" search-placeholder="搜索域名规则集…" class="min-h-11" />
                </FormField>
                <FormField label="代理规则" html-for="setup-proxy-rules" hint="交给 Mihomo DNS" persistent-hint>
                  <MultiSelect v-model="proxyRules" trigger-id="setup-proxy-rules" :options="proxyOptions" :disabled="working || session?.completed" :max-visible="2" placeholder="选择代理规则集" search-placeholder="搜索域名规则集…" class="min-h-11" />
                </FormField>
              </div>
              <div v-for="rule in ruleConflicts" :key="rule.id" role="status" class="flex flex-wrap items-center gap-x-3 gap-y-2 rounded-lg border border-warning-accent/30 bg-warning-soft px-3 py-2.5 text-xs leading-relaxed" data-rule-conflict>
                <p class="m-0 min-w-0 flex-1 basis-60">规则库已有同名的「{{ rule.conflict!.name }}」，但内容和内置版本不同。继续会在规则库另建一份内置版本。</p>
                <Button v-if="rule.conflict!.id" variant="outline" size="sm" class="min-h-11" :disabled="working || session?.completed" @click="useNamesake(rule)">改用规则库的「{{ rule.conflict!.name }}」</Button>
              </div>
              <p v-if="!directRules.length && !proxyRules.length" class="m-0 text-xs text-warning-accent" role="status">未选择域名规则，所有域名将使用下方的默认 DNS。</p>
              <p v-if="goal === 'gateway' && ipTakeover" class="m-0 text-xs leading-relaxed text-muted-foreground">Telegram、Google 推送（FCM）会直接连 IP，它们的 IP 网段也会交给 Mihomo。安装后可在「配置生成 → Linux 网关」的「IP 接管」里调整。</p>
            </section>
            <section class="space-y-3 border-t border-border pt-4" aria-label="DNS 上游">
              <FormField label="直连 DNS 上游" html-for="setup-local-dns" hint="IP 地址的 DoH / DoT，每行一个。" persistent-hint help="用于查询直连域名的真实 IP。默认地址可直接使用，例如 https://223.5.5.5/dns-query；也支持 tls://223.5.5.5:853，最多 16 个，无需 YAML 前缀。为避免 Linux DNS 接管回环，不支持普通 53 端口、本机地址或代理出口。后续安装自动带入，之后可在 OxiDNS 设置中修改。">
                <Textarea id="setup-local-dns" v-model="localDns" :disabled="working || session?.completed" :rows="2" :spellcheck="false" :aria-invalid="!!localDnsWarning" :aria-describedby="localDnsWarning ? 'setup-local-dns-warning' : undefined" class="min-h-16 font-mono text-xs" :placeholder="defaultLocalDns" />
                <p v-if="localDnsWarning" id="setup-local-dns-warning" role="alert" class="m-0 text-xs leading-relaxed text-destructive-accent">{{ localDnsWarning }}</p>
              </FormField>
              <div class="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs"><span class="font-medium">Mihomo DNS</span><span class="text-muted-foreground">自动配置，无需填写</span><FieldHelp label="Mihomo DNS" text="向导在现有 Mihomo 基础配置上只调整必要项：DNS 开启 fake-IP 并监听 127.0.0.1:1053，dns.nameserver 为空时补入 IP 地址的 DoH；主路由分流还会设为完整 TUN。端口、嗅探、控制面板及其他 DNS 设置原样保留，基础配置为空时使用初始模板。OxiDNS 的代理上游指向本机 127.0.0.1:1053，两项核心安装在同一台 Linux 上，无需填写设备 IP 或 DNS 地址；后续可在 Mihomo 基础配置中修改上游。DNS 解析与连接出口是两个设置，交给 Mihomo DNS 不代表连接一定走代理。" /></div>
            </section>
            <section class="border-t border-border pt-4" aria-label="控制面板密码">
              <FormField :label="session?.controller_secret_saved ? '控制面板密码' : '控制面板密码（必填）'" html-for="setup-controller-secret" :hint="controllerHint" persistent-hint help="写入 Mihomo 基础配置的 secret，打开 Mihomo 控制面板（external-controller）时需要输入。初始模板让控制面板监听 0.0.0.0:9090，同一局域网的设备都能访问，所以必须自己设置密码，不能使用模板默认的 123456。Agent 在本机用同一密码读取连接和流量统计。向导不会再显示已保存的密码；以后可在「配置生成 → Mihomo → 基础配置」的 secret 修改。">
                <div class="flex items-center gap-2">
                  <Input id="setup-controller-secret" v-model="controllerSecret" :type="showControllerSecret ? 'text' : 'password'" autocomplete="new-password" :spellcheck="false" :aria-required="!session?.controller_secret_saved" :disabled="working || session?.completed" :aria-invalid="!!controllerSecret && !!controllerSecretWarning" :aria-describedby="controllerSecret && controllerSecretWarning ? 'setup-controller-secret-warning' : undefined" class="min-h-11 min-w-0 font-mono" :placeholder="session?.controller_secret_saved ? '已设置，留空保留' : '设置控制面板密码'" />
                  <Button variant="outline" size="icon" class="size-11 shrink-0" :disabled="working || session?.completed" :aria-label="showControllerSecret ? '隐藏控制面板密码' : '显示控制面板密码'" @click="showControllerSecret = !showControllerSecret"><EyeOff v-if="showControllerSecret" class="size-4" /><Eye v-else class="size-4" /></Button>
                </div>
                <p v-if="controllerSecret && controllerSecretWarning" id="setup-controller-secret-warning" role="alert" class="m-0 text-xs leading-relaxed text-destructive-accent">{{ controllerSecretWarning }}</p>
              </FormField>
            </section>
            <section class="space-y-1 border-t border-border pt-3" aria-label="未匹配规则时">
              <div class="flex min-h-11 flex-wrap items-center justify-between gap-x-2">
                <div class="flex items-center gap-1 text-[13px] font-medium max-sm:text-xs"><span>未匹配域名</span><span class="cf-compact-help"><FieldHelp label="未匹配域名" text="OxiDNS 未匹配任何域名规则时，使用自动配置的 Mihomo DNS，或上方填写的直连 DNS。命中直连或代理规则的域名不受此选项影响。这里只选择 DNS 解析方式，与下面的连接出口互相独立。" /></span></div>
                <Segmented v-model="dnsFallback" :disabled="working || session?.completed" class="shrink-0 self-start sm:self-center" :options="[{value:'mihomo',label:'Mihomo DNS'},{value:'direct',label:'直连 DNS'}]" label="OxiDNS 未匹配域名的 DNS" />
              </div>
              <div class="flex min-h-11 flex-wrap items-center justify-between gap-x-2">
                <div class="flex items-center gap-1 text-[13px] font-medium max-sm:text-xs"><span>未匹配连接</span><span class="cf-compact-help"><FieldHelp label="未匹配连接" text="进入 Mihomo 的连接未匹配前面的规则时，由最后一条 MATCH 规则决定出口。走代理使用「🐟 漏网之鱼」策略组，默认跟随「✈️ 节点选择」；直连表示 DIRECT。只影响未匹配连接，实际流量是否进入 Mihomo 还取决于路由设置。" /></span></div>
                <Segmented v-model="trafficFallback" :disabled="working || session?.completed" class="shrink-0 self-start sm:self-center" :options="[{value:'proxy',label:'走代理'},{value:'direct',label:'直连'}]" label="Mihomo 未匹配连接的出口" />
              </div>
            </section>
            <section class="border-t border-border pt-3" aria-label="DNS 查询记录">
              <div class="flex min-h-11 flex-wrap items-center justify-between gap-x-2">
                <div class="flex items-center gap-1 text-[13px] font-medium max-sm:text-xs"><Label for="setup-query-recorder">记录 DNS 查询（OxiDNS 面板用）</Label><span class="cf-compact-help"><FieldHelp label="记录 DNS 查询" text="OxiDNS 面板的「查询记录」靠它查看每条查询的域名、结果和耗时，客户端设备页也从中找出最近查询过的设备。记录写入 OxiDNS 工作目录下 state/ 的 SQLite，默认保留 7 天并持续写盘；闪存或 SD 卡设备可关闭。之后可在 OxiDNS 设置中修改。" /></span></div>
                <Switch id="setup-query-recorder" v-model="queryRecorder" :disabled="working || session?.completed" class="shrink-0" />
              </div>
            </section>
            <div class="space-y-2 border-t border-border pt-4"><p class="text-xs text-muted-foreground">{{ session?.subscription_ids.length }} 个订阅 · {{ session?.node_ids.length }} 个已有节点</p><Button :disabled="working || session?.completed || !!localDnsWarning || !!controllerSecretWarning" class="min-h-11" @click="prepare"><LoaderCircle v-if="working" class="size-4 animate-spin" />生成配置并继续<ArrowRight class="size-4" /></Button></div>
          </div>
          <div v-if="step === 3 && session" class="space-y-4">
            <SetupInstallation :key="session.id" :profile-id="session.profile_id" :goal="session.goal" @busy="childBusy = $event" @completed="checkInstallation" />
          </div>
          <div v-if="step === 4 && session" class="flex flex-col gap-5">
            <p v-if="previewRouting" role="status" class="m-0 rounded-lg bg-info-soft px-3 py-2 text-sm text-info-accent">{{ goal === 'router' ? '安装尚未完成。可先填写主路由信息，安装后再发布设置。' : '安装尚未完成。可先查看客户端设置，实际地址在安装检查后显示。' }}</p>
            <div v-else-if="goal === 'router' || clientReady" class="flex items-center gap-2 text-sm"><CircleCheck class="size-4 text-success-accent" />{{ goal === 'router' && !session.legacy_takeover ? 'Mihomo、OxiDNS 已发布，IPv4 转发与 TUN 已检查' : 'Mihomo、OxiDNS 已发布，Linux 转发已检查' }}</div>
            <template v-if="goal === 'router'">
              <Segmented v-model="routerMode" :options="[{value:'manual',label:'手动配置主路由'},{value:'managed',label:'自动配置主路由'}]" label="主路由配置方式" />
              <template v-if="routerMode === 'manual'">
                <FormField label="1. 在主路由添加静态路由" help="目的网段是需要交给 Mihomo 的目标 IP 范围，由当前分流配置生成；下一跳就是安装 Mihomo 的 Linux 主机 IP。主路由收到发往这些网段的连接后，会转交 Linux。添加后检查没有指回主路由的循环路由。">
                  <p class="m-0 text-sm">登录主路由后台，在「静态路由」中按下表添加。客户端网关保持原来的主路由。</p>
                  <div v-if="routeInstructions" class="cf-table-wrap"><table class="cf-table"><thead><tr><th>目的网段</th><th>下一跳（Linux IP）</th></tr></thead><tbody><tr v-for="item in routeInstructions.routes" :key="item.cidr"><td class="font-mono">{{ item.cidr }}</td><td class="font-mono">{{ routeInstructions.gateway }}</td></tr></tbody></table></div>
                  <p v-else class="m-0 rounded-lg border border-dashed border-border px-3 py-3 text-xs text-muted-foreground">安装检查完成后，这里会列出可直接照填的网段和 Linux IP。</p>
                </FormField>
                <FormField label="2. 将客户端 DNS 改为 OxiDNS" help="也可先只手动修改一台测试设备的 DNS。DNS 地址就是安装 OxiDNS 的 Linux 主机 IP，使用标准 53 端口，路由器中通常只需填 IP。不要添加绕过 OxiDNS 的备用 DNS。">
                  <p class="m-0 text-sm">在主路由 DHCP 的「DNS 服务器」中填写下方地址，让测试设备重新连接网络后生效。</p>
                  <p v-if="routeInstructions" class="m-0 break-all font-mono text-sm">{{ routeInstructions.dns }}</p>
                  <p v-else class="m-0 text-xs text-muted-foreground">安装检查完成后自动显示 DNS 地址。</p>
                  <p v-if="!previewRouting" class="m-0 text-xs leading-relaxed text-muted-foreground" data-client-devices-link>只改一台测试设备时，可先在<RouterLink :to="clientDevicesLink" class="mx-0.5 inline-flex items-center text-primary-accent underline underline-offset-4 max-md:min-h-11">客户端设备</RouterLink>中给它命名，便于在 DNS 面板中认出，完成后可返回向导。</p>
                </FormField>
                <p v-if="routeInstructions?.routes.some(item => item.requires_source_exclusion)" class="m-0 text-sm text-warning-accent">真实 IP 网段需在主路由按来源排除 Linux 本机（策略路由），否则 Linux 直连这些地址时会被转回自己；只能按目的地址配置时，只添加 fake-IP 网段。</p>
                <p v-for="warning in routeInstructions?.warnings" :key="warning" class="m-0 text-sm text-warning-accent">{{ warning }}</p>
                <div v-if="routesIncomplete" role="status" class="flex flex-col items-start gap-2 rounded-lg border border-warning-accent/30 bg-warning-soft px-3 py-2.5 text-sm leading-relaxed" data-routes-incomplete><p class="m-0">规则集「{{ missingRuleSets.join('」「') || '主路由转发' }}」的 IP 段尚未下载，上表缺少这些网段，暂不能确认完成。请确认 ConfigFlow 服务器能访问规则地址后重新检查。</p><Button variant="outline" class="min-h-11" :disabled="working" @click="checkInstallation"><LoaderCircle v-if="working" class="size-4 animate-spin" />重新检查</Button></div>
                <label class="flex min-h-11 items-center gap-2 text-sm"><Checkbox v-model="ackRoutes" :disabled="previewRouting" />已配置主路由静态路由</label><label class="flex min-h-11 items-center gap-2 text-sm"><Checkbox v-model="ackDNS" :disabled="previewRouting" />已将测试客户端 DNS 指向 OxiDNS</label>
                <p class="m-0 text-xs text-muted-foreground">手动设置由你确认，向导无法自动核验这台主路由。</p>
              </template>
              <template v-else>
                <FormField label="连接主路由" help="自动配置支持 iKuai 4.x / RouterOS 7.1+；其他品牌使用手动配置。添加管理地址和账号后，ConfigFlow 可读取主路由设置。安装完成后自动关联本次设备；先探测，再打开差异报告，预览具体变更后确认发布。保存连接信息本身不会修改路由器。"><p class="m-0 text-sm">先添加主路由的管理地址和账号。安装完成后，由 ConfigFlow 配置静态路由和 DHCP DNS。</p></FormField>
                <ol aria-label="主路由自动配置步骤" class="m-0 flex list-none flex-wrap gap-x-4 gap-y-1 p-0 text-xs text-muted-foreground"><li>1 添加主路由</li><li>2 检查连接</li><li>3 查看改动并发布</li></ol>
                <Routers :key="previewRouting ? 'draft-' + session.id : 'installed-' + session.id" embedded :profile-id="session.profile_id" :setup-agents="routerAgents" :setup-draft-session="previewRouting ? session.id : undefined" @selected="selectRouter" @busy="childBusy = $event" />
              </template>
            </template>
            <template v-else>
              <p v-if="clientChecking" class="m-0 flex items-center gap-2 text-sm text-muted-foreground" role="status"><LoaderCircle class="size-4 animate-spin" />正在确认转发仍已启用且健康…</p>
              <div v-else-if="clientError" class="flex flex-col gap-3"><p class="m-0 text-sm text-destructive-accent" role="alert">{{ stripTrailingPunctuation(clientError) }}。请先返回安装与检查，暂时保持客户端原来的网关和 DNS。</p><Button variant="outline" @click="checkClientReadiness">重新检查转发</Button></div>
              <GatewayClientInstructions v-if="clientReady && routeInstructions" :address="routeInstructions.gateway" />
              <Button variant="outline" :disabled="working" @click="move(3)">管理网络与撤销转发</Button>
            </template>
            <label v-if="session.default_profile && !session.default_profile.is_default" class="flex items-start gap-2 text-sm"><Checkbox v-model="makeDefault" :disabled="working" class="mt-0.5" /><span><span class="font-medium">设为默认配置空间</span><span class="mt-1 block text-xs leading-relaxed text-muted-foreground">新打开的页面、未指定配置空间的配置链接和新注册的 Agent 都会使用它。现在的默认是「{{ session.default_profile.current.name }}」，已绑定的设备不受影响，之后也可以在配置空间里更改。「{{ session.default_profile.current.name }}」中的客户端设备名称和 DNS 策略不会带过来，需要时在客户端设备中重新设置。</span></span></label>
            <Button :disabled="previewRouting || working || (goal === 'gateway' && !clientReady) || (goal === 'router' && (routerMode === 'manual' ? !ackRoutes || !ackDNS || routesIncomplete : !routerId))" class="min-h-11" @click="finish"><LoaderCircle v-if="working" class="size-4 animate-spin" />{{ previewRouting ? '完成安装后继续' : goal === 'router' && routerMode === 'managed' ? '检查路由器并完成' : '确认设置并继续' }}<ArrowRight class="size-4" /></Button>
          </div>
          <div v-if="step === 5 && session" class="space-y-5">
            <div class="flex items-center gap-2"><CircleCheck class="size-5 text-success-accent" /><h3 class="m-0 text-base font-semibold">Mihomo + OxiDNS 已准备好</h3></div>
            <p class="m-0 text-sm">请在实际使用的手机或电脑测试解析、国内网站与代理网站；客户端连通性尚未由本向导确认。</p>
            <p v-if="session.router_verification === 'manual'" class="m-0 text-sm text-warning-accent">主路由为手动配置，尚未自动核验。</p>
            <p v-else-if="session.router_verification === 'verified'" class="m-0 text-sm text-success-accent">主路由的路由与 DHCP DNS 已核对同步。</p>
            <div v-if="session.default_profile?.is_default && session.previous_default" class="space-y-1 rounded-lg bg-muted px-3 py-2 text-sm">
              <p class="m-0">已设为默认配置空间，新打开的页面会直接进入它。</p>
              <p v-if="session.default_profile.leftovers?.profile" class="m-0 text-muted-foreground">原来的「{{ session.default_profile.leftovers.profile.name }}」没有设备在用，不需要的话可以在<button type="button" class="text-primary-accent underline-offset-2 hover:underline" @click="manage('/profiles')">配置空间</button>里删除。</p>
              <p v-if="session.default_profile.leftovers?.subscriptions.length" class="m-0 text-muted-foreground">示例订阅「{{ session.default_profile.leftovers.subscriptions.map(item => item.name).join('、') }}」只被原来的默认配置使用，不需要的话可以在<button type="button" class="text-primary-accent underline-offset-2 hover:underline" @click="manage('/subscriptions')">订阅来源</button>里删除。</p>
            </div>
            <div class="flex flex-wrap gap-2"><Button class="min-h-11" :disabled="working" @click="manage('/agents')">进入 Agent<ArrowRight class="size-4" /></Button><Button variant="outline" class="min-h-11" :disabled="working" @click="manage('/client-devices')">客户端设备</Button><Button v-if="goal === 'router'" variant="outline" class="min-h-11" @click="manage('/routers')">进入上游路由</Button></div>
          </div>
          <footer v-if="session && step > 0 && step < 5" class="mt-6 flex items-center gap-2 border-t border-border pt-4"><Button variant="ghost" :disabled="working" @click="move(previewRouting ? session.step : step - 1)"><ArrowLeft class="size-3.5" />{{ previewRouting ? '返回安装与检查' : '上一步' }}</Button><span class="ml-auto text-xs text-muted-foreground">{{ previewRouting ? '安装进度未改变' : '进度已保存' }}</span></footer>
        </SectionCard>
      </div>
    </template>
  </div>
</template>
