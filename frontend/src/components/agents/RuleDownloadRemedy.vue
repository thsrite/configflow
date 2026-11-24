<template>
  <div class="space-y-3 rounded-lg border border-border bg-muted/40 p-3" aria-label="下载失败处理">
    <div class="space-y-1">
      <p class="m-0 text-[13px] font-semibold">{{ github ? '服务器无法访问 GitHub' : '服务器无法下载规则' }}</p>
      <p class="m-0 text-xs leading-relaxed text-muted-foreground">
        规则由 ConfigFlow 服务器下载后再发给设备。{{ github ? '任选一项填写' : '填写可用的代理' }}，保存后会重新发布。
      </p>
    </div>

    <LoadingRows v-if="loading" :rows="2" />
    <div v-else-if="loadError" class="flex flex-wrap items-center gap-2 text-xs">
      <p role="alert" class="m-0 text-destructive-accent">{{ loadError }}</p>
      <Button variant="outline" size="sm" @click="load"><RefreshCw class="size-3.5" />重新读取</Button>
    </div>
    <template v-else>
      <FormField
        v-if="github"
        label="GitHub 代理域名"
        html-for="remedy-github-proxy"
        hint="原地址失败后改用镜像下载，例如 https://mirror.example.com。"
        :help="optionHelp('服务器无法直连 GitHub，但能访问 GitHub 镜像时使用。', '下载 GitHub 文件时先试原地址，失败后在地址前加上此前缀重试。生成配置中的 GitHub 链接也会使用此设置；也可在规则库中修改。')"
      >
        <Input
          id="remedy-github-proxy"
          v-model="githubProxy"
          autocomplete="off"
          :spellcheck="false"
          class="bg-background font-mono"
          placeholder="https://mirror.example.com"
        />
      </FormField>
      <FormField
        label="规则下载代理"
        html-for="remedy-rule-proxy"
        hint="服务器通过此 HTTP 代理下载规则，留空直接连接。"
        :help="optionHelp('局域网内已有可用代理（如另一台 Mihomo）时使用。', '支持含账号密码的 HTTP/HTTPS 代理，原地址和 GitHub 代理域名都经过它下载；只影响服务器，不修改设备代理。也可在系统设置中修改。')"
      >
        <div class="flex items-center gap-1.5">
          <Input
            id="remedy-rule-proxy"
            v-model="ruleProxy"
            :type="showRuleProxy ? 'text' : 'password'"
            autocomplete="off"
            :spellcheck="false"
            class="min-w-0 bg-background font-mono"
            placeholder="http://192.0.2.3:7890"
          />
          <Button
            variant="outline"
            size="icon"
            class="shrink-0"
            :aria-label="showRuleProxy ? '隐藏规则下载代理' : '显示规则下载代理'"
            @click="showRuleProxy = !showRuleProxy"
          >
            <EyeOff v-if="showRuleProxy" class="size-4" />
            <Eye v-else class="size-4" />
          </Button>
        </div>
      </FormField>
      <p v-if="saveError" role="alert" class="m-0 text-xs text-destructive-accent">{{ saveError }}</p>
      <Button class="min-h-11" :disabled="busy || saving || !changed" @click="saveAndRetry">
        <LoaderCircle v-if="saving" class="size-4 animate-spin" />
        {{ saving ? '正在保存…' : '保存并重试发布' }}
      </Button>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Eye, EyeOff, LoaderCircle, RefreshCw } from '@lucide/vue'
import { githubProxyDomainApi, ruleFetchProxyApi } from '@/api'
import FormField from '@/components/common/FormField.vue'
import LoadingRows from '@/components/common/LoadingRows.vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { optionHelp } from '@/lib/optionCopy'

defineProps<{ github: boolean; busy?: boolean }>()
const emit = defineEmits<{ (e: 'retry'): void }>()

const loading = ref(true)
const loadError = ref('')
const saving = ref(false)
const saveError = ref('')
const showRuleProxy = ref(false)
const githubProxy = ref('')
const ruleProxy = ref('')
const saved = ref({ github: '', rule: '' })

/* 只保存改动过的项；未读到当前值时不允许保存，避免把已有设置清空 */
const changed = computed(() =>
  githubProxy.value.trim() !== saved.value.github || ruleProxy.value.trim() !== saved.value.rule
)

const load = async () => {
  loading.value = true
  loadError.value = ''
  try {
    const [github, rule] = await Promise.all([githubProxyDomainApi.get(), ruleFetchProxyApi.get()])
    saved.value = { github: github.data.proxy_domains || '', rule: rule.data.rule_fetch_proxy || '' }
    githubProxy.value = saved.value.github
    ruleProxy.value = saved.value.rule
  } catch {
    loadError.value = '读取当前下载设置失败，请重新读取。'
  } finally {
    loading.value = false
  }
}

const saveAndRetry = async () => {
  saving.value = true
  saveError.value = ''
  try {
    const github = githubProxy.value.trim()
    const rule = ruleProxy.value.trim()
    if (rule !== saved.value.rule) {
      try {
        const { data } = await ruleFetchProxyApi.update({ rule_fetch_proxy: rule })
        saved.value.rule = data.rule_fetch_proxy
        ruleProxy.value = data.rule_fetch_proxy
      } catch {
        saveError.value = '规则下载代理无效，请填写 HTTP/HTTPS 代理地址，或留空直接连接。'
        return
      }
    }
    if (github !== saved.value.github) {
      try {
        const { data } = await githubProxyDomainApi.update(github)
        saved.value.github = data.proxy_domains
        githubProxy.value = data.proxy_domains
      } catch {
        saveError.value = '保存 GitHub 代理域名失败，请稍后重试。'
        return
      }
    }
    emit('retry')
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>
