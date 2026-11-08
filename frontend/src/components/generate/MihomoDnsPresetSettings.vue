<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import FieldHelp from '@/components/common/FieldHelp.vue'
import Segmented from '@/components/common/Segmented.vue'
import { DNS_LIST_COPY, dnsPresetHelp, followPolicyHelp, respectRulesHelp, type BaseFieldDef } from '@/lib/mihomoBaseCopy'
import { DNS_POLICY_KEY, readDnsPreset, type BaseIssue, type BasePath, type DnsPreset, type MihomoBaseView } from '@/lib/mihomoBaseSettings'
import MihomoBaseField from './MihomoBaseField.vue'

const props = defineProps<{ view: MihomoBaseView; issues: (field: string) => BaseIssue[]; disabled?: boolean }>()
const emit = defineEmits<{ change: [path: BasePath, value: unknown, kind: 'list' | 'scalar']; preset: [preset: Exclude<DnsPreset, 'custom'>]; draft: [field: string, error: string] }>()
const labels: Record<DnsPreset, string> = { domestic: '国内 DoH', split: '国内外分开解析', custom: '自定义' }
const descriptions: Record<DnsPreset, string> = {
  domestic: '与初始模板一致：全部使用国内 DoH，respect-rules 开启。',
  split: '国内和私有域名用国内 DoH，其他域名经代理用 Google、Cloudflare DoH；依赖 GeoSite 数据和可用节点。',
  custom: '当前写法与两种预设都不同，可在下方逐项编辑；每行一个上游。',
}
const state = computed(() => readDnsPreset(props.view))
const editing = ref(false)
watch(() => state.value.preset, value => { if (value === 'custom') editing.value = false })
const shown = computed(() => editing.value ? 'custom' : state.value.preset ?? '')
function choose(value: string) {
  if (value === 'custom') {
    editing.value = true
    return
  }
  editing.value = false
  if (value !== state.value.preset) emit('preset', value as Exclude<DnsPreset, 'custom'>)
}
const listKeys = computed(() => ['default-nameserver', 'nameserver', 'proxy-server-nameserver', 'direct-nameserver',
  ...(props.view.value(['dns', 'fallback']) !== undefined ? ['fallback'] : [])])
const listField = (key: string): BaseFieldDef => ({ path: ['dns', key], label: DNS_LIST_COPY[key].label, help: DNS_LIST_COPY[key].help, type: 'list', placeholder: 'https://223.5.5.5/dns-query' })
const respectField: BaseFieldDef = { path: ['dns', 'respect-rules'], label: 'DNS 查询遵循规则（respect-rules）', type: 'bool', coreDefault: false, help: respectRulesHelp }
const followField: BaseFieldDef = { path: ['dns', 'direct-nameserver-follow-policy'], label: '直连 DNS 遵循策略（direct-nameserver-follow-policy）', type: 'bool', coreDefault: false, help: followPolicyHelp }
const policyEntries = computed(() => {
  const policy = props.view.value(['dns', 'nameserver-policy'])
  return policy && typeof policy === 'object' && !Array.isArray(policy) ? Object.keys(policy) : []
})
const userPolicies = computed(() => policyEntries.value.filter(key => key !== DNS_POLICY_KEY))
</script>

<template>
  <div class="min-w-0 space-y-3" data-base-dns>
    <div class="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
      <div class="flex min-h-11 shrink-0 items-center gap-1">
        <span class="text-[13px] font-medium">DNS 方案</span>
        <FieldHelp label="DNS 方案" :text="dnsPresetHelp" />
      </div>
      <Segmented :model-value="shown" :options="(Object.keys(labels) as DnsPreset[]).map(value => ({ value, label: labels[value] }))" label="DNS 方案"
        class="max-w-full" wrap :disabled="disabled || !state.editable" @update:model-value="choose" />
    </div>
    <p v-if="state.preset" class="m-0 break-words text-xs leading-relaxed text-muted-foreground" data-base-dns-description>{{ descriptions[shown || state.preset] }}</p>
    <p v-if="!state.editable" class="m-0 break-words text-xs leading-relaxed text-warning-accent" role="alert">{{ state.reason }}</p>
    <p class="m-0 break-words text-xs leading-relaxed text-muted-foreground" data-base-dns-policy>
      <template v-if="userPolicies.length">nameserver-policy 中你自己的 {{ userPolicies.length }} 条（{{ userPolicies.slice(0, 3).join('、') }}{{ userPolicies.length > 3 ? ' 等' : '' }}）切换方案时保留并优先匹配，请在下方 YAML 中编辑。</template>
      <template v-else>nameserver-policy 没有自己写的条目；需要为个别域名指定 DNS 时请在下方 YAML 中添加。</template>
    </p>
    <div v-if="shown === 'custom' && state.editable" class="grid min-w-0 grid-cols-1 gap-4 sm:grid-cols-2" data-base-dns-custom>
      <MihomoBaseField v-for="key in listKeys" :key="key" :field="listField(key)" :state="view.field(['dns', key], 'list')" :issues="issues(`dns.${key}`)"
        :disabled="disabled" @change="emit('change', ['dns', key], $event, 'list')" />
      <MihomoBaseField :field="respectField" :state="view.field(['dns', 'respect-rules'])" :issues="issues('dns.respect-rules')" :disabled="disabled"
        @change="emit('change', ['dns', 'respect-rules'], $event, 'scalar')" />
      <MihomoBaseField :field="followField" :state="view.field(['dns', 'direct-nameserver-follow-policy'])" :issues="issues('dns.direct-nameserver-follow-policy')"
        :disabled="disabled" @change="emit('change', ['dns', 'direct-nameserver-follow-policy'], $event, 'scalar')" />
    </div>
    <template v-else>
      <p v-for="item in ['default-nameserver', 'nameserver', 'proxy-server-nameserver', 'direct-nameserver', 'nameserver-policy'].flatMap(key => issues(`dns.${key}`))"
        :key="item.message" class="m-0 break-words text-xs" :class="item.blocking ? 'text-destructive-accent' : 'text-warning-accent'" :role="item.blocking ? 'alert' : undefined" data-base-issue>
        {{ item.blocking ? item.message : `已保存的配置：${item.message}` }}
      </p>
    </template>
  </div>
</template>
