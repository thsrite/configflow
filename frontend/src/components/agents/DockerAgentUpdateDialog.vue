<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Copy } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import FormField from '@/components/common/FormField.vue'
import InfoNote from '@/components/common/InfoNote.vue'
import { notify } from '@/lib/feedback'
import { copyText } from '@/lib/clipboard'
import { systemApi } from '@/api'
import type { Agent } from '@/types'

defineProps<{ agent: Agent }>()
const emit = defineEmits<{ close: []; refresh: [] }>()
const method = ref('compose')
const service = ref('agent')
// Default to the Agent image paired with this panel (`latest` stays on the last
// Python-era Agent until the maintainer moves it); the server falls back to the
// major-version tag for builds without a paired image.
const fallbackImage = 'thsrite/config-flow-agent:3'
const image = ref(fallbackImage)
onMounted(async () => {
  try {
    const paired = (await systemApi.getVersion()).data?.agent_image
    if (typeof paired === 'string' && paired && image.value === fallbackImage) image.value = paired
  } catch {
    // Keep the major-version default; the field stays editable.
  }
})
// Restrict generated commands to one argument; never interpolate arbitrary shell text.
const validService = computed(() => /^[a-zA-Z0-9][a-zA-Z0-9_.-]*$/.test(service.value.trim()))
const validImage = computed(() => /^[a-zA-Z0-9][a-zA-Z0-9._:/@-]*$/.test(image.value.trim()))
const composeCommand = computed(() => validService.value
  ? `docker compose pull '${service.value.trim()}' &&\ndocker compose up -d --no-deps '${service.value.trim()}'`
  : '')
const pullCommand = computed(() => validImage.value ? `docker pull '${image.value.trim()}'` : '')

async function copyCommand(command: string) {
  if (!command) return
  try {
    await copyText(command)
    notify.success('命令已复制，请到 Docker 宿主机执行')
  } catch {
    notify.error('复制失败，请手动选择并复制命令')
  }
}
</script>

<template>
  <Dialog :open="true" @update:open="value => { if (!value) emit('close') }">
    <DialogContent class="max-w-[720px]">
      <DialogHeader>
        <DialogTitle>Docker Agent 更新步骤</DialogTitle>
        <DialogDescription>
          {{ agent.name }} · {{ agent.host }}。请在该 Agent 所在的 Docker 宿主机执行以下步骤，网页仅提供操作指南。
        </DialogDescription>
      </DialogHeader>

      <div class="flex flex-col gap-4 text-sm leading-relaxed">
        <InfoNote>更新会短暂中断该容器中的 Agent 和核心服务。请等待当前配置发布完成，并保留原环境变量、网络、端口和数据挂载。</InfoNote>
        <InfoNote v-if="agent.service_type === 'oxidns'">已开启 DNS 高可用时，先更新未持有虚拟 IP 的容器，确认其核心和高可用状态正常后再处理另一台，不要同时重建。保留所选网络、固定地址、权限、宿主机身份挂载和全部数据目录。Host 模式需保留独立高可用容器运行；普通容器不会因更新镜像而自动获得高可用支持。</InfoNote>
        <FormField label="原安装方式">
          <RadioGroup v-model="method" class="flex flex-wrap gap-5" aria-label="原安装方式">
            <label class="flex cursor-pointer items-center gap-2"><RadioGroupItem value="compose" />Docker Compose</label>
            <label class="flex cursor-pointer items-center gap-2"><RadioGroupItem value="run" />Docker Run / 容器管理器</label>
          </RadioGroup>
        </FormField>

        <template v-if="method === 'compose'">
          <ol class="m-0 list-decimal space-y-3 pl-5">
            <li>进入原 Compose 文件所在目录，沿用原项目名、环境文件和启动参数。备份 Compose 文件及挂载的数据；若镜像固定了旧版本标签或摘要，先修改为目标版本。</li>
            <li>
              确认 Agent 的服务名。可运行 <code>docker compose config --services</code> 查看，它不是容器名称。
              <FormField class="mt-2" label="Compose 服务名" html-for="docker-update-service" hint="安装页面生成的默认服务名为 agent；自行修改过的请填写实际名称。">
                <Input id="docker-update-service" v-model="service" autocomplete="off" spellcheck="false" :aria-invalid="!validService" />
              </FormField>
              <p v-if="!validService" role="alert" class="mt-1 text-destructive">请填写单个服务名，仅使用字母、数字、点、下划线或连字符，且以字母或数字开头。</p>
            </li>
            <li>
              拉取镜像并重建该服务，保留原挂载。若原命令使用 <code>-f</code>、<code>-p</code> 或 <code>--env-file</code>，请在下方每个 <code>docker compose</code> 后补上相同参数。
              <div v-if="composeCommand" class="mt-2 rounded-md border border-border bg-muted/40 p-3">
                <pre class="m-0 overflow-x-auto whitespace-pre-wrap break-all text-xs"><code>{{ composeCommand }}</code></pre>
                <Button variant="outline" size="sm" class="mt-2" @click="copyCommand(composeCommand)"><Copy class="size-3.5" />复制 Compose 命令</Button>
              </div>
            </li>
          </ol>
        </template>

        <template v-else>
          <ol class="m-0 list-decimal space-y-3 pl-5">
            <li>找到原 <code>docker run</code> 命令，或在容器管理器中查看现有容器设置。记录容器名、环境变量、网络、端口、重启策略、权限及全部挂载，并备份挂载数据。</li>
            <li>
              拉取目标镜像。下方为官方默认镜像；使用自建镜像、私有仓库或固定版本时，请改为自己的目标镜像。
              <FormField class="mt-2" label="目标镜像" html-for="docker-update-image">
                <Input id="docker-update-image" v-model="image" autocomplete="off" spellcheck="false" :aria-invalid="!validImage" />
              </FormField>
              <p v-if="!validImage" role="alert" class="mt-1 text-destructive">请填写一个镜像引用，不要包含空格、引号或其他 Shell 字符。</p>
              <div v-if="pullCommand" class="mt-2 rounded-md border border-border bg-muted/40 p-3">
                <pre class="m-0 overflow-x-auto whitespace-pre-wrap break-all text-xs"><code>{{ pullCommand }}</code></pre>
                <Button variant="outline" size="sm" class="mt-2" @click="copyCommand(pullCommand)"><Copy class="size-3.5" />复制拉取命令</Button>
              </div>
            </li>
            <li>准备好原启动命令后，停止旧容器并改名保留，再以原容器名、原参数和目标镜像运行新容器。容器管理器用户可使用“重新创建”功能并核对所有设置。必须复用原宿主机目录或原数据卷；匿名卷需按原卷名显式挂载。</li>
          </ol>
          <InfoNote>完整的重建命令取决于原容器配置，请沿用原启动命令。不要重新生成安装命令覆盖旧配置，也不要删除数据卷。更新失败时，停止新容器后可恢复旧容器；数据若有变化，应同时恢复备份。</InfoNote>
        </template>

        <p class="m-0">完成后检查容器日志，等待 Agent 重新上线，再点击“刷新 Agent 列表”核对版本和核心服务状态。若版本未变化，请确认所用镜像已发布新版。</p>
        <a class="w-fit text-primary-accent underline underline-offset-4" href="https://docs.docker.com/reference/cli/docker/compose/up/" target="_blank" rel="noopener noreferrer">Docker 官方重建容器说明</a>
      </div>

      <DialogFooter>
        <Button variant="outline" @click="emit('close')">关闭</Button>
        <Button @click="emit('refresh'); emit('close')">刷新 Agent 列表</Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>
