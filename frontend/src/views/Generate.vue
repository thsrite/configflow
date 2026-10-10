<template>
  <div class="min-w-0 [overflow-wrap:anywhere]">
    <PageHeader title="配置生成">
      <template #actions>
        <div id="generate-header-actions" class="contents" />
      </template>
    </PageHeader>

    <!-- ===== 生成工作台：目标切换 + 编译步骤 + 代码面板 ===== -->
    <GenerateStudio :targets="studioTargets" @copy="copyUrl" />

    <!-- ===== 自定义基础配置 ===== -->
    <Dialog v-model:open="customConfigDialogVisible">
      <DialogContent
        class="h-[min(88dvh,900px)] max-h-[calc(100dvh-2rem)] max-w-[calc(100vw-2rem)] grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden [overflow-wrap:anywhere] sm:w-[90vw] sm:max-w-[1400px] max-sm:h-[calc(100dvh-1rem)] max-sm:max-h-[calc(100dvh-1rem)] max-sm:w-[calc(100vw-1rem)] max-sm:max-w-[calc(100vw-1rem)] max-sm:p-4"
      >
        <DialogHeader>
          <DialogTitle>{{ getCustomConfigDialogTitle() }}</DialogTitle>
          <DialogDescription class="[overflow-wrap:anywhere]">
            {{ getCustomConfigDialogDesc() }}
            留空并保存可恢复默认基础配置。{{ currentConfigType === 'surge' || currentConfigType === 'loon' ? '请使用 INI 格式' : '请使用 YAML 格式' }}。
          </DialogDescription>
        </DialogHeader>

        <YamlEditor
          v-model="customConfigContent"
          class="h-full min-h-0"
          :placeholder="getCustomConfigPlaceholder()"
        />

        <DialogFooter>
          <Button variant="outline" @click="customConfigDialogVisible = false">取消</Button>
          <Button :disabled="savingCustomConfig" @click="saveCustomConfig">
            <Loader2 v-if="savingCustomConfig" class="size-4 animate-spin" />
            保存
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- ===== 配置预览 ===== -->
    <Dialog v-model:open="previewDialogVisible">
      <DialogContent
        class="h-[min(88dvh,900px)] max-h-[calc(100dvh-2rem)] max-w-[calc(100vw-2rem)] grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden [overflow-wrap:anywhere] sm:w-[90vw] sm:max-w-[1400px] max-sm:h-[calc(100dvh-1rem)] max-sm:max-h-[calc(100dvh-1rem)] max-sm:w-[calc(100vw-1rem)] max-sm:max-w-[calc(100vw-1rem)] max-sm:p-4"
      >
        <DialogHeader>
          <DialogTitle>{{ getPreviewDialogTitle() }}</DialogTitle>
          <DialogDescription>查看生成结果，确认后可复制完整内容。此处不能直接修改。</DialogDescription>
        </DialogHeader>

        <YamlEditor v-model="previewContent" class="h-full min-h-0" :read-only="true" />

        <DialogFooter>
          <Button variant="outline" @click="previewDialogVisible = false">关闭</Button>
          <Button @click="copyPreviewContent">
            <Copy class="size-4" />
            复制到剪贴板
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- ===== MosDNS 设置 ===== -->
    <Dialog v-model:open="mosdnsSettingsDialogVisible">
      <DialogContent class="max-w-[760px] [overflow-wrap:anywhere]">
        <DialogHeader>
          <DialogTitle>MosDNS 设置</DialogTitle>
          <DialogDescription>设置当前配置空间的域名解析规则、DNS 服务器、缓存和日志，用于生成 MosDNS 配置。</DialogDescription>
        </DialogHeader>

        <Tabs v-model="mosdnsActiveTab" class="min-w-0">
          <TabsList class="w-full justify-start overflow-x-auto dark:bg-background/50">
            <TabsTrigger value="rules" class="text-xs">规则配置</TabsTrigger>
            <TabsTrigger value="cache" class="text-xs">缓存</TabsTrigger>
            <TabsTrigger value="dns" class="text-xs">DNS 服务器</TabsTrigger>
            <TabsTrigger value="default" class="text-xs">默认转发</TabsTrigger>
            <TabsTrigger value="hosts" class="text-xs">自定义 Host</TabsTrigger>
            <TabsTrigger value="log" class="text-xs">日志</TabsTrigger>
            <TabsTrigger value="api" class="text-xs">API</TabsTrigger>
          </TabsList>

          <!-- 规则配置 -->
          <TabsContent value="rules" class="cf-focus-gutter max-h-[56dvh] overflow-y-auto">
            <div class="flex flex-col gap-4">
              <InfoNote>
                <p>选择哪些规则和规则集使用国内 DNS，哪些使用国外 DNS。</p>
                <p>只有选中的规则和规则集才会用于生成 MosDNS 配置。</p>
              </InfoNote>

              <LabeledDivider label="直连规则配置" />

              <FormField label="直连规则集" hint="使用国内 DNS；已选为代理规则集的项目不能重复选择。">
                <MultiSelect
                  v-model="mosdnsDirectRulesets"
                  :options="directRulesetOptions"
                  placeholder="选择使用国内 DNS 的规则集"
                />
              </FormField>

              <FormField label="直连规则" hint="使用国内 DNS；已选为代理规则的项目不能重复选择。">
                <MultiSelect
                  v-model="mosdnsDirectRules"
                  :options="directRuleOptions"
                  placeholder="选择使用国内 DNS 的规则"
                />
              </FormField>

              <LabeledDivider label="代理规则配置" />

              <FormField label="代理规则集" hint="使用国外 DNS；已选为直连规则集的项目不能重复选择。">
                <MultiSelect
                  v-model="mosdnsProxyRulesets"
                  :options="proxyRulesetOptions"
                  placeholder="选择使用国外 DNS 的规则集"
                />
              </FormField>

              <FormField label="代理规则" hint="使用国外 DNS；已选为直连规则的项目不能重复选择。">
                <MultiSelect
                  v-model="mosdnsProxyRules"
                  :options="proxyRuleOptions"
                  placeholder="选择使用国外 DNS 的规则"
                />
              </FormField>

              <LabeledDivider label="自定义 Match" />

              <FormField label="插入位置" hint="选择自定义 match 在自动生成的规则匹配之前或之后执行。">
                <Select v-model="mosdnsCustomMatchPosition">
                  <SelectTrigger class="w-full bg-background/50">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="head">优先匹配（在规则匹配之前执行）</SelectItem>
                    <SelectItem value="tail">尾部匹配（在规则匹配之后执行）</SelectItem>
                  </SelectContent>
                </Select>
              </FormField>

              <div class="flex flex-col gap-2">
                <div class="flex flex-wrap items-center gap-2">
                  <Label class="flex-1">自定义 Match</Label>
                  <Button variant="outline" size="sm" class="border-border/60 bg-background/40" @click="addMosdnsCustomMatch">
                    <Plus class="size-3.5" />
                    添加匹配项
                  </Button>
                </div>

                <p
                  v-if="mosdnsCustomMatches.length === 0"
                  class="m-0 rounded-lg border border-border/50 bg-background/40 px-3 py-3 text-[12px] text-muted-foreground"
                >
                  尚未添加自定义匹配项，点击「添加匹配项」设置条件和动作。
                </p>

                <div
                  v-for="(item, index) in mosdnsCustomMatches"
                  :key="item.id"
                  class="flex flex-col gap-3 rounded-lg border border-border/50 bg-background/40 p-3"
                >
                  <div class="flex flex-wrap items-center gap-2">
                    <span class="text-[12.5px] font-semibold text-foreground">匹配项 {{ index + 1 }}</span>
                    <Switch v-model="item.enabled" />
                    <span class="text-[11.5px] text-muted-foreground">
                      {{ item.enabled ? '启用' : '禁用' }}
                    </span>
                    <div class="ml-auto flex items-center gap-0.5">
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        title="上移"
                        aria-label="上移"
                        :disabled="index === 0"
                        @click="moveMosdnsCustomMatch(index, 'up')"
                      >
                        <ChevronUp class="size-4" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        title="下移"
                        aria-label="下移"
                        :disabled="index === mosdnsCustomMatches.length - 1"
                        @click="moveMosdnsCustomMatch(index, 'down')"
                      >
                        <ChevronDown class="size-4" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        class="text-destructive-accent hover:bg-destructive-soft"
                        title="删除"
                        aria-label="删除匹配项"
                        @click="removeMosdnsCustomMatch(item.id)"
                      >
                        <Trash2 class="size-4" />
                      </Button>
                    </div>
                  </div>

                  <FormField label="匹配条件" hint="支持 MosDNS sequence 的 matches 格式，自动忽略空行。">
                    <Textarea
                      v-model="item.matchesText"
                      class="bg-background/50 font-mono text-[12px]"
                      :rows="3"
                      placeholder="每行一个 match 表达式，例如 qname $自定义规则集"
                    />
                  </FormField>

                  <FormField label="执行动作">
                    <Input
                      v-model="item.exec"
                      class="bg-background/50 font-mono"
                      placeholder="例如 goto china_dns 或 $proxy_dns"
                    />
                  </FormField>
                </div>
              </div>
            </div>
          </TabsContent>

          <!-- 缓存 -->
          <TabsContent value="cache" class="cf-focus-gutter max-h-[56dvh] overflow-y-auto">
            <div class="flex flex-col gap-4">
              <InfoNote>
                <p>开启缓存后，可复用 DNS 查询结果，加快重复查询。</p>
                <p>关闭后，生成的配置将不再保存或使用这些缓存结果。</p>
              </InfoNote>

              <FormField hint="关闭后，国内 DNS 查询将不再使用此缓存。">
                <div class="flex items-center gap-2.5">
                  <Switch id="cache-enabled" v-model="mosdnsCacheEnabled" />
                  <Label for="cache-enabled" class="text-[13px] text-muted-foreground">启用缓存</Label>
                </div>
              </FormField>

              <template v-if="mosdnsCacheEnabled">
                <FormField label="size" html-for="cache-size" hint="缓存条目数量（建议 10240 起）。">
                  <Input
                    id="cache-size"
                    v-model.number="mosdnsCacheSize"
                    type="number"
                    :min="1"
                    :max="2000000"
                    :step="256"
                    class="w-48 bg-background/50"
                  />
                </FormField>

                <FormField label="lazy_cache_ttl" html-for="cache-ttl" hint="缓存过期后的延迟删除时间（秒）。">
                  <Input
                    id="cache-ttl"
                    v-model.number="mosdnsCacheLazyTtl"
                    type="number"
                    :min="0"
                    :max="31536000"
                    :step="60"
                    class="w-48 bg-background/50"
                  />
                </FormField>

                <FormField>
                  <div class="flex items-center gap-2.5">
                    <Switch id="cache-dump" v-model="mosdnsCacheDumpEnabled" />
                    <Label for="cache-dump" class="text-[13px] text-muted-foreground">将缓存保存到文件</Label>
                  </div>
                </FormField>

                <template v-if="mosdnsCacheDumpEnabled">
                  <FormField
                    label="dump_file"
                    html-for="cache-dump-file"
                    hint="缓存文件的保存位置，相对于 MosDNS 配置目录。"
                  >
                    <Input
                      id="cache-dump-file"
                      v-model="mosdnsCacheDumpFile"
                      class="bg-background/50 font-mono"
                      placeholder="./cache.dump"
                    />
                  </FormField>

                  <FormField label="dump_interval" html-for="cache-dump-interval" hint="缓存保存间隔（秒）。">
                    <Input
                      id="cache-dump-interval"
                      v-model.number="mosdnsCacheDumpInterval"
                      type="number"
                      :min="1"
                      :max="86400"
                      :step="10"
                      class="w-48 bg-background/50"
                    />
                  </FormField>
                </template>
              </template>
            </div>
          </TabsContent>

          <!-- DNS 服务器 -->
          <TabsContent value="dns" class="cf-focus-gutter max-h-[56dvh] overflow-y-auto">
            <div class="flex flex-col gap-5">
              <InfoNote>
                <p>配置国内和国外的 DNS 服务器地址。</p>
                <p>支持 UDP、TCP、DoH、DoT 等协议格式，每个条目可使用简单模式或 YAML 模式。</p>
              </InfoNote>

              <section v-for="group in dnsGroups" :key="group.kind" class="flex flex-col gap-2">
                <div class="flex flex-wrap items-center gap-2">
                  <Label class="flex-1">{{ group.label }}</Label>
                  <Button
                    variant="outline"
                    size="sm"
                    class="border-border/60 bg-background/40"
                    @click="addDnsEntry(group.kind)"
                  >
                    <Plus class="size-3.5" />
                    添加条目
                  </Button>
                </div>

                <p
                  v-if="group.entries.length === 0"
                  class="m-0 rounded-lg border border-border/50 bg-background/40 px-3 py-3 text-[12px] text-muted-foreground"
                >
                  {{ group.emptyText }}
                </p>

                <div :ref="group.listRef" class="flex flex-col gap-2">
                  <div
                    v-for="(entry, index) in group.entries"
                    :key="entry.id"
                    class="flex flex-col gap-3 rounded-lg border border-border/50 bg-background/40 p-3"
                  >
                    <div class="flex flex-wrap items-center gap-2">
                      <GripVertical
                        class="drag-handle size-3.5 shrink-0 cursor-grab text-muted-foreground"
                        aria-hidden="true"
                      />
                      <span class="text-[12.5px] font-semibold text-foreground">条目 {{ index + 1 }}</span>
                      <div class="ml-auto flex min-w-0 flex-wrap items-center gap-1">
                        <Button
                          variant="ghost"
                          size="sm"
                          class="h-auto min-h-8 whitespace-normal"
                          @click="toggleDnsEntryMode(group.kind, entry.id)"
                        >
                          {{ entry.mode === 'simple' ? '切换到 YAML' : '切换到简单模式' }}
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          class="text-destructive-accent hover:bg-destructive-soft"
                          title="删除"
                          aria-label="删除条目"
                          @click="removeDnsEntry(group.kind, entry.id)"
                        >
                          <Trash2 class="size-4" />
                        </Button>
                      </div>
                    </div>

                    <template v-if="entry.mode === 'simple'">
                      <FormField label="地址">
                        <Input
                          v-model="entry.addr"
                          class="bg-background/50 font-mono text-[12px]"
                          :placeholder="group.addrPlaceholder"
                        />
                      </FormField>
                      <FormField
                        label="Bootstrap"
                        :hint="!isDomainAddr(entry.addr) && entry.addr ? '仅域名地址或 DoH/DoT 地址需要 Bootstrap' : ''"
                      >
                        <Input
                          v-model="entry.bootstrap"
                          class="bg-background/50 font-mono text-[12px]"
                          placeholder="223.5.5.5（可选）"
                          :disabled="!isDomainAddr(entry.addr)"
                        />
                      </FormField>
                      <label class="flex cursor-pointer items-center gap-2 text-[13px] text-muted-foreground">
                        <Checkbox v-model="entry.enable_pipeline" />
                        启用 Pipeline
                      </label>
                    </template>

                    <template v-else>
                      <Textarea
                        v-model="entry.yaml_config"
                        class="bg-background/50 font-mono text-[12px]"
                        :class="entry.yaml_error && 'border-destructive-accent/60'"
                        :rows="4"
                        placeholder="- addr: 192.168.1.1:53&#10;  bootstrap: 223.5.5.5&#10;  enable_pipeline: false"
                        @input="validateYaml(entry)"
                      />
                      <p v-if="entry.yaml_error" class="m-0 text-[12px] text-destructive-accent">
                        {{ entry.yaml_error }}
                      </p>
                      <p
                        v-else-if="entry.yaml_config && entry.yaml_config.trim()"
                        class="m-0 text-[12px] text-success-accent"
                      >
                        YAML 语法正确
                      </p>
                    </template>
                  </div>
                </div>

                <p class="m-0 text-[12px] text-muted-foreground">{{ group.hint }}</p>
              </section>
            </div>
          </TabsContent>

          <!-- 默认转发 -->
          <TabsContent value="default" class="cf-focus-gutter max-h-[56dvh] overflow-y-auto">
            <div class="flex flex-col gap-4">
              <InfoNote>
                <p>当所有规则都不匹配时，使用的默认 DNS 服务器。</p>
                <p>可选择「国外 DNS」，减少 DNS 污染的影响。</p>
              </InfoNote>

              <RadioGroup v-model="mosdnsDefaultForward" class="flex flex-col gap-2">
                <label
                  v-for="option in DEFAULT_FORWARD_OPTIONS"
                  :key="option.value"
                  :class="[
                    'flex cursor-pointer items-start gap-2.5 rounded-lg border px-3.5 py-3 transition-colors',
                    mosdnsDefaultForward === option.value
                      ? 'border-primary-accent bg-primary-soft dark:border-primary-accent/40 dark:bg-primary-soft/50'
                      : 'border-input bg-card hover:border-border-strong dark:border-border/50 dark:bg-background/40'
                  ]"
                >
                  <RadioGroupItem :value="option.value" class="mt-0.5" />
                  <span class="min-w-0">
                    <span class="block text-[13px] font-medium text-foreground">{{ option.label }}</span>
                    <span class="mt-0.5 block text-[12px] text-muted-foreground">{{ option.desc }}</span>
                  </span>
                </label>
              </RadioGroup>
            </div>
          </TabsContent>

          <!-- 自定义 Host -->
          <TabsContent value="hosts" class="cf-focus-gutter max-h-[56dvh] overflow-y-auto">
            <div class="flex flex-col gap-4">
              <InfoNote>
                <p>为指定域名设置固定 IP，优先于其他规则使用。</p>
                <p>格式：每行一个映射，域名在前，IP 地址在后，用空格分隔。</p>
              </InfoNote>

              <FormField
                label="Hosts 记录"
                html-for="mosdns-hosts"
                hint="自定义 Host 记录会在所有规则之前优先匹配。"
              >
                <Textarea
                  id="mosdns-hosts"
                  v-model="mosdnsCustomHosts"
                  class="min-h-[220px] bg-background/50 font-mono text-[12px]"
                  :rows="10"
                  placeholder="localhost 127.0.0.1&#10;myserver.local 192.168.1.100&#10;dns.google 8.8.8.8"
                />
              </FormField>
            </div>
          </TabsContent>

          <!-- 日志 -->
          <TabsContent value="log" class="cf-focus-gutter max-h-[56dvh] overflow-y-auto">
            <div class="flex flex-col gap-4">
              <InfoNote>
                <p>配置 MosDNS 日志输出级别和文件路径。</p>
                <p>可以控制日志详细程度，帮助排查问题。</p>
              </InfoNote>

              <FormField hint="关闭后不再记录日志，出现问题时将缺少排查依据。">
                <div class="flex items-center gap-2.5">
                  <Switch id="mosdns-log" v-model="mosdnsLogEnabled" />
                  <Label for="mosdns-log" class="text-[13px] text-muted-foreground">启用日志</Label>
                </div>
              </FormField>

              <template v-if="mosdnsLogEnabled">
                <FormField label="日志级别" hint="推荐使用 Info 级别，调试时可使用 Debug 级别。">
                  <Select v-model="mosdnsLogLevel">
                    <SelectTrigger class="w-full bg-background/50">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem v-for="level in LOG_LEVELS" :key="level.value" :value="level.value">
                        <span class="flex flex-col leading-snug">
                          <span class="text-[13px] font-medium">{{ level.label }}</span>
                          <span class="text-[11.5px] text-muted-foreground">{{ level.desc }}</span>
                        </span>
                      </SelectItem>
                    </SelectContent>
                  </Select>
                </FormField>

                <FormField
                  label="日志文件路径"
                  html-for="mosdns-log-file"
                  hint="日志文件的保存路径，相对于 MosDNS 配置文件目录。"
                >
                  <Input
                    id="mosdns-log-file"
                    v-model="mosdnsLogFile"
                    class="bg-background/50 font-mono"
                    placeholder="./mosdns.log"
                  />
                </FormField>
              </template>
            </div>
          </TabsContent>

          <!-- API -->
          <TabsContent value="api" class="cf-focus-gutter max-h-[56dvh] overflow-y-auto">
            <div class="flex flex-col gap-4">
              <InfoNote>
                <p>开启 API 后，可通过 API 查询 MosDNS 运行状态和统计信息。</p>
                <p>关闭后将无法通过 API 监控和管理服务。</p>
              </InfoNote>

              <FormField hint="需要通过其他工具查看状态或管理 MosDNS 时开启。">
                <div class="flex items-center gap-2.5">
                  <Switch id="mosdns-api" v-model="mosdnsApiEnabled" />
                  <Label for="mosdns-api" class="text-[13px] text-muted-foreground">启用 API</Label>
                </div>
              </FormField>

              <FormField
                v-if="mosdnsApiEnabled"
                label="API 监听地址"
                html-for="mosdns-api-addr"
                hint="格式 IP:端口，例如 0.0.0.0:8338 或 127.0.0.1:8338。"
              >
                <Input
                  id="mosdns-api-addr"
                  v-model="mosdnsApiAddress"
                  class="bg-background/50 font-mono"
                  placeholder="0.0.0.0:8338"
                />
              </FormField>
            </div>
          </TabsContent>
        </Tabs>

        <DialogFooter>
          <Button variant="outline" @click="mosdnsSettingsDialogVisible = false">取消</Button>
          <Button :disabled="savingMosdnsSettings" @click="saveMosdnsSettings">
            <Loader2 v-if="savingMosdnsSettings" class="size-4 animate-spin" />
            保存
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- ===== Surge Smart 模式 ===== -->
    <Dialog v-model:open="surgeSmartDialogVisible">
      <DialogContent class="max-w-[680px] [overflow-wrap:anywhere]">
        <DialogHeader>
          <DialogTitle>Surge Smart 模式</DialogTitle>
          <DialogDescription class="[overflow-wrap:anywhere]">
            选择要使用 Smart 模式的策略组，并填写 policy-priority 设置优先级。
            仅影响 Surge 配置；Mihomo 中仍保留原类型（如 url-test）。
          </DialogDescription>
        </DialogHeader>

        <div class="cf-focus-gutter flex max-h-[52dvh] flex-col gap-2 overflow-y-auto">
          <div
            v-for="(item, index) in surgeSmartGroups"
            :key="index"
            class="flex min-w-0 flex-wrap items-center gap-2"
          >
            <Select v-model="item.group_id">
              <SelectTrigger class="min-w-0 flex-[1_1_12rem] bg-background/50">
                <SelectValue placeholder="选择策略组" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem
                  v-for="g in proxyGroupOptions"
                  :key="g.id"
                  :value="g.id"
                  :disabled="g.id !== item.group_id && surgeSmartGroups.some(sg => sg.group_id === g.id)"
                >
                  {{ g.name }}
                </SelectItem>
              </SelectContent>
            </Select>
            <Input
              v-model="item.policy_priority"
              class="min-w-0 flex-[1.5_1_12rem] bg-background/50 font-mono text-[12px]"
              placeholder="policy-priority，如 香港:0;美国:1"
            />
            <Button
              variant="ghost"
              size="icon-sm"
              class="shrink-0 text-destructive-accent hover:bg-destructive-soft"
              title="移除"
              aria-label="移除该策略组"
              @click="removeSurgeSmartGroup(index)"
            >
              <Trash2 class="size-4" />
            </Button>
          </div>

          <Button
            variant="outline"
            size="sm"
            class="self-start border-border/60 bg-background/40"
            @click="addSurgeSmartGroup"
          >
            <Plus class="size-3.5" />
            添加策略组
          </Button>
        </div>

        <DialogFooter>
          <Button variant="outline" @click="surgeSmartDialogVisible = false">取消</Button>
          <Button :disabled="savingSurgeSmartGroups" @click="saveSurgeSmartGroups">
            <Loader2 v-if="savingSurgeSmartGroups" class="size-4 animate-spin" />
            保存
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

  </div>
</template>

<script setup lang="ts">
import PageHeader from '@/components/common/PageHeader.vue'
import GenerateStudio from '@/components/generate/GenerateStudio.vue'
import { useProfileStore } from '@/stores/profile'
import { ref, onMounted, computed, watch, nextTick } from 'vue'
import {
  ChevronDown,
  ChevronUp,
  Copy,
  Download,
  Eye,
  FileCode2,
  GripVertical,
  Loader2,
  Network,
  Pencil,
  Plus,
  Settings,
  Shield,
  Smartphone,
  Trash2,
  Upload
} from '@lucide/vue'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import FormField from '@/components/common/FormField.vue'
import InfoNote from '@/components/common/InfoNote.vue'
import LabeledDivider from '@/components/common/LabeledDivider.vue'
import MultiSelect from '@/components/common/MultiSelect.vue'
import { notify } from '@/lib/feedback'
import { generateApi, customConfigApi, ruleApi, ruleSetApi, proxyGroupApi, serverDomainApi, configTokenApi } from '@/api'
import YamlEditor from '@/components/YamlEditor.vue'
import api from '@/api'
import type { RuleSet } from '@/types'
import { activeProfileId } from '@/profileContext'
import * as yaml from 'js-yaml'
import Sortable from 'sortablejs'

// DNS 条目接口定义

const cfProfileStore = useProfileStore()
const profileId = activeProfileId.value
const profileOptions = { headers: { 'X-ConfigFlow-Profile': profileId } }
interface DnsEntry {
  id: string
  mode: 'simple' | 'yaml'
  addr: string
  bootstrap: string
  enable_pipeline: boolean
  yaml_config: string
  yaml_error?: string  // YAML 验证错误信息
}

interface MosdnsCustomMatchItem {
  id: string
  enabled: boolean
  exec: string
  matchesText: string
}

// 生成唯一 ID
const generateId = (): string => {
  return `dns-${Date.now()}-${Math.random().toString(36).substr(2, 9)}`
}

const generateMatchId = (): string => {
  return `mosdns-match-${Date.now()}-${Math.random().toString(36).substr(2, 9)}`
}

// 判断地址是否需要 Bootstrap
// DoH/DoT/DoQ 等加密 DNS 协议即使使用 IP 地址也需要 Bootstrap
const isDomainAddr = (addr: string): boolean => {
  if (!addr || !addr.trim()) {
    return false
  }

  const trimmedAddr = addr.trim()

  // 如果地址包含 http://, https://, tls://, quic:// 等协议前缀
  // 说明是 DoH/DoT/DoQ 等加密 DNS，需要 Bootstrap
  if (/^(https?|tls|quic):\/\//.test(trimmedAddr)) {
    return true
  }

  // 提取地址中的主机部分（去除协议、端口、路径等）
  let host = trimmedAddr

  // 去除协议前缀
  host = host.replace(/^(https?|tls|quic):\/\//, '')

  // 去除路径部分
  host = host.split('/')[0]

  // 去除端口号
  host = host.split(':')[0]

  // 检查是否为 IPv4 地址（如 1.1.1.1）
  const ipv4Regex = /^(\d{1,3}\.){3}\d{1,3}$/
  if (ipv4Regex.test(host)) {
    return false
  }

  // 检查是否为 IPv6 地址（简化判断，包含多个冒号或方括号）
  if (host.includes('[') || (host.match(/:/g) || []).length > 1) {
    return false
  }

  // 其他情况视为域名（包含字母或以点分隔的多个部分）
  return /[a-zA-Z]/.test(host)
}

// YAML 验证函数
const validateYaml = (entry: DnsEntry): void => {
  if (entry.mode !== 'yaml' || !entry.yaml_config || !entry.yaml_config.trim()) {
    entry.yaml_error = undefined
    return
  }

  try {
    // 尝试解析 YAML
    const parsed = yaml.load(entry.yaml_config)

    // 验证格式：必须是对象（单个DNS条目）或数组（多个DNS条目）
    if (typeof parsed === 'object' && parsed !== null) {
      // 如果是数组，检查第一个元素
      if (Array.isArray(parsed)) {
        if (parsed.length > 0) {
          const firstItem = parsed[0]
          if (typeof firstItem !== 'object' || !firstItem.addr) {
            entry.yaml_error = '请为 YAML 列表中的 DNS 条目填写 addr 地址字段'
            return
          }
        }
      } else {
        // 如果是对象，必须有 addr 字段
        if (!parsed.addr) {
          entry.yaml_error = '请在 YAML 中填写 addr 地址字段'
          return
        }
      }
    } else {
      entry.yaml_error = '请使用 YAML 对象填写单个 DNS 条目，或使用列表填写多个条目'
      return
    }

    entry.yaml_error = undefined
  } catch (error: any) {
    // 提供更有帮助的错误信息
    let errorMsg = error.message || 'YAML 语法错误'

    // 检查常见错误：缺少缩进
    const lines = entry.yaml_config.trim().split('\n')
    if (lines.length > 1 && lines[0].trim().startsWith('-')) {
      // 检查后续行是否缺少缩进
      for (let i = 1; i < lines.length; i++) {
        const line = lines[i]
        if (line.trim() && !line.trim().startsWith('-') && !line.startsWith(' ') && !line.startsWith('\t')) {
          errorMsg = 'YAML 格式错误：第 ' + (i + 1) + ' 行缺少缩进（应该以 2 个空格开头）'
          break
        }
      }
    }

    entry.yaml_error = errorMsg
  }
}

// 文本 → DNS 条目数组
const parseDnsText = (text: string): DnsEntry[] => {
  if (!text || !text.trim()) {
    return []
  }

  const entries: DnsEntry[] = []
  const lines = text.trim().split('\n')
  let currentYamlLines: string[] = []
  let inYamlBlock = false

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]
    const trimmedLine = line.trim()

    // 跳过空行
    if (!trimmedLine) {
      continue
    }

    // 检测 YAML 格式（以 - 开头）
    if (trimmedLine.startsWith('-')) {
      inYamlBlock = true
      currentYamlLines = [line]

      // 收集 YAML 块的所有行（缩进的行属于同一块）
      for (let j = i + 1; j < lines.length; j++) {
        const nextLine = lines[j]
        const nextTrimmed = nextLine.trim()

        // 如果遇到新的 - 开头或非缩进行，停止收集
        if (nextTrimmed.startsWith('-') || (nextTrimmed && !nextLine.startsWith(' ') && !nextLine.startsWith('\t'))) {
          break
        }

        // 如果是空行或缩进行，加入当前块
        if (!nextTrimmed || nextLine.startsWith(' ') || nextLine.startsWith('\t')) {
          currentYamlLines.push(nextLine)
          i = j
        }
      }

      // 创建 YAML 模式条目
      entries.push({
        id: generateId(),
        mode: 'yaml',
        addr: '',
        bootstrap: '',
        enable_pipeline: false,
        yaml_config: currentYamlLines.join('\n')
      })
    } else {
      // 简单格式：addr bootstrap=xxx enable_pipeline=true
      const parts = trimmedLine.split(/\s+/)
      const addr = parts[0] || ''
      let bootstrap = ''
      let enable_pipeline = false

      // 解析参数
      for (let j = 1; j < parts.length; j++) {
        const part = parts[j]
        if (part.startsWith('bootstrap=')) {
          bootstrap = part.substring('bootstrap='.length)
        } else if (part === 'enable_pipeline=true' || part === 'enable_pipeline:true') {
          enable_pipeline = true
        } else if (part === 'enable_pipeline=false' || part === 'enable_pipeline:false') {
          enable_pipeline = false
        }
      }

      entries.push({
        id: generateId(),
        mode: 'simple',
        addr,
        bootstrap,
        enable_pipeline,
        yaml_config: ''
      })
    }
  }

  return entries
}

// DNS 条目数组 → 文本
const dnsEntriesToText = (entries: DnsEntry[]): string => {
  if (!entries || entries.length === 0) {
    return ''
  }

  return entries.map(entry => {
    if (entry.mode === 'yaml') {
      return entry.yaml_config
    } else {
      // 简单模式转换为文本
      const parts = [entry.addr]
      // 只有域名才添加 bootstrap
      if (entry.bootstrap && isDomainAddr(entry.addr)) {
        parts.push(`bootstrap=${entry.bootstrap}`)
      }
      if (entry.enable_pipeline) {
        parts.push('enable_pipeline=true')
      }
      return parts.join(' ')
    }
  }).join('\n')
}

const addMosdnsCustomMatch = () => {
  mosdnsCustomMatches.value.push({
    id: generateMatchId(),
    enabled: true,
    exec: '',
    matchesText: ''
  })
}

const removeMosdnsCustomMatch = (id: string) => {
  mosdnsCustomMatches.value = mosdnsCustomMatches.value.filter(item => item.id !== id)
}

const moveMosdnsCustomMatch = (index: number, direction: 'up' | 'down') => {
  const targetIndex = direction === 'up' ? index - 1 : index + 1
  if (targetIndex < 0 || targetIndex >= mosdnsCustomMatches.value.length) {
    return
  }

  const list = [...mosdnsCustomMatches.value]
  const [item] = list.splice(index, 1)
  list.splice(targetIndex, 0, item)
  mosdnsCustomMatches.value = list
}

const mihomoLoading = ref(false)
const surgeLoading = ref(false)
const loonLoading = ref(false)
const mosdnsLoading = ref(false)

const mihomoPreviewLoading = ref(false)
const surgePreviewLoading = ref(false)
const loonPreviewLoading = ref(false)
const mosdnsPreviewLoading = ref(false)

type ConfigType = 'mihomo' | 'surge' | 'loon' | 'mosdns'

const customConfigDialogVisible = ref(false)
const customConfigContent = ref('')
const savingCustomConfig = ref(false)
const currentConfigType = ref<ConfigType>('mihomo')

const previewDialogVisible = ref(false)
const previewContent = ref('')
const currentPreviewType = ref<ConfigType>('mihomo')

const mosdnsSettingsDialogVisible = ref(false)
const mosdnsActiveTab = ref('rules')
const mosdnsCustomConfig = ref('')
const mosdnsDirectRulesets = ref<string[]>([])
const mosdnsProxyRulesets = ref<string[]>([])
const mosdnsDirectRules = ref<string[]>([])
const mosdnsProxyRules = ref<string[]>([])
const mosdnsCustomMatches = ref<MosdnsCustomMatchItem[]>([])
const mosdnsCustomMatchPosition = ref<'head' | 'tail'>('tail')
const mosdnsLocalDns = ref('')
const mosdnsRemoteDns = ref('')
const mosdnsFallbackDns = ref('')
// DNS 条目数组（新的数据结构）
const mosdnsLocalDnsEntries = ref<DnsEntry[]>([])
const mosdnsRemoteDnsEntries = ref<DnsEntry[]>([])
const mosdnsFallbackDnsEntries = ref<DnsEntry[]>([])
// DNS 列表容器的 ref
const localDnsListRef = ref<HTMLElement | null>(null)
const remoteDnsListRef = ref<HTMLElement | null>(null)
const fallbackDnsListRef = ref<HTMLElement | null>(null)
const mosdnsDefaultForward = ref('forward_remote')
const mosdnsCustomHosts = ref('')
const mosdnsLogEnabled = ref(true)
const mosdnsLogLevel = ref('info')
const mosdnsLogFile = ref('./mosdns.log')
const mosdnsApiEnabled = ref(true)
const mosdnsApiAddress = ref('0.0.0.0:8338')
const mosdnsCacheEnabled = ref(true)
const mosdnsCacheSize = ref(10240)
const mosdnsCacheLazyTtl = ref(21600)
const mosdnsCacheDumpEnabled = ref(true)
const mosdnsCacheDumpFile = ref('./cache.dump')
const mosdnsCacheDumpInterval = ref(300)
const availableRuleSets = ref<RuleSet[]>([])
const availableRules = ref<any[]>([])
const savingMosdnsSettings = ref(false)

// 服务域名配置
const serverDomain = ref(localStorage.getItem('serverDomain') || window.location.origin)

// 配置令牌
const configToken = ref('')

// Surge Smart 模式相关
const surgeSmartDialogVisible = ref(false)
const surgeSmartGroups = ref<Array<{group_id: string, policy_priority: string}>>([])
const proxyGroupOptions = ref<Array<{id: string, name: string}>>([])
const savingSurgeSmartGroups = ref(false)

const showSurgeSmartDialog = async () => {
  try {
    // 并行加载策略组列表和当前 smart_groups 配置
    const [groupsRes, surgeRes] = await Promise.all([
      proxyGroupApi.getAll(profileId),
      customConfigApi.getSurge(profileId)
    ])
    const allGroups = groupsRes.data || []
    const groupMap = new Map(allGroups.map((g: any) => [g.id, g]))
    // 检查策略组是否引用了其他策略组（含跟随链）
    const hasStrategyRef = (g: any): boolean => {
      if (g.include_groups?.length > 0) return true
      if (g.follow_group) {
        const followed = groupMap.get(g.follow_group)
        if (followed) return hasStrategyRef(followed)
      }
      return false
    }
    proxyGroupOptions.value = allGroups
      .filter((g: any) => g.type !== 'chain' && !hasStrategyRef(g))
      .map((g: any) => ({ id: g.id, name: g.name }))
    surgeSmartGroups.value = (surgeRes.data.smart_groups || []).map((sg: any) => ({
      group_id: sg.group_id || '',
      policy_priority: sg.policy_priority || ''
    }))
    surgeSmartDialogVisible.value = true
  } catch (error) {
    notify.error('加载 Smart 配置失败')
  }
}

const addSurgeSmartGroup = () => {
  surgeSmartGroups.value.push({ group_id: '', policy_priority: '' })
}

const removeSurgeSmartGroup = (index: number) => {
  surgeSmartGroups.value.splice(index, 1)
}

const saveSurgeSmartGroups = async () => {
  try {
    savingSurgeSmartGroups.value = true
    // 过滤掉未选择策略组的空行
    const validGroups = surgeSmartGroups.value.filter(g => g.group_id)
    await customConfigApi.saveSurge({ smart_groups: validGroups }, profileId)
    notify.success('Smart 配置已保存')
    surgeSmartDialogVisible.value = false
  } catch (error) {
    notify.error('保存 Surge Smart 设置失败')
  } finally {
    savingSurgeSmartGroups.value = false
  }
}

// 处理按钮点击
const handleSurgeCustomConfig = () => {
  showCustomConfigDialog('surge')
}

const handleSurgePreview = () => {
  previewConfig('surge')
}

// 配置令牌相关函数
const loadConfigToken = async () => {
  try {
    const response = await configTokenApi.get()
    configToken.value = response.data.config_token || ''
  } catch (error: any) {
    console.error('加载配置令牌失败:', error)
  }
}

// 计算配置 URL - 使用 serverDomain 代替固定的 window.location.origin
const baseUrl = computed(() => {
  return serverDomain.value.replace(/\/+$/, '')
})

const mihomoUrl = computed(() => {
  const url = `${baseUrl.value}/api/config/${encodeURIComponent(profileId)}/mihomo`
  return configToken.value ? `${url}?token=${encodeURIComponent(configToken.value)}` : url
})
const surgeUrl = computed(() => {
  const url = `${baseUrl.value}/api/config/${encodeURIComponent(profileId)}/surge`
  return configToken.value ? `${url}?token=${encodeURIComponent(configToken.value)}` : url
})
// Loon 以链接末段（文件名）作为导入后的配置名称，与后端 loon_config_name 保持一致
const loonConfigName = computed(() => {
  const name = cfProfileStore.activeProfile.value?.name || ''
  const cleaned = name.replace(/[\\/:*?"<>|\x00-\x1f]+/g, ' ').trim().replace(/^\.+|\.+$/g, '')
  return cleaned || profileId || 'loon'
})
const loonUrl = computed(() => {
  const url = `${baseUrl.value}/api/config/${encodeURIComponent(profileId)}/loon/${encodeURIComponent(loonConfigName.value)}.lcf`
  return configToken.value ? `${url}?token=${encodeURIComponent(configToken.value)}` : url
})
const mosdnsUrl = computed(() => {
  const url = `${baseUrl.value}/api/config/${encodeURIComponent(profileId)}/mosdns`
  return configToken.value ? `${url}?token=${encodeURIComponent(configToken.value)}` : url
})

// URL显示
const mihomoUrlDisplay = computed(() => mihomoUrl.value)
const surgeUrlDisplay = computed(() => surgeUrl.value)
const loonUrlDisplay = computed(() => loonUrl.value)
const mosdnsUrlDisplay = computed(() => mosdnsUrl.value)

/* ---------- 新 UI 的派生数据 ---------- */

/** 各生成目标的展示与操作，避免在模板里重复几乎相同的卡片 */
const targets = computed(() => [
  {
    key: 'mihomo',
    title: 'Mihomo',
    desc: '生成 Mihomo / Clash Meta 的 YAML 配置',
    icon: FileCode2,
    url: mihomoUrl.value,
    urlDisplay: mihomoUrlDisplay.value,
    actions: [
      { label: '基础配置', icon: Pencil, run: () => showCustomConfigDialog('mihomo') },
      { label: '预览', icon: Eye, loading: mihomoPreviewLoading.value, run: () => previewConfig('mihomo') },
      { label: '下载', icon: Download, primary: true, loading: mihomoLoading.value, run: generateMihomo }
    ]
  },
  {
    key: 'surge',
    title: 'Surge',
    desc: '生成 Surge 的 .conf 配置（INI 格式）',
    icon: Shield,
    url: surgeUrl.value,
    urlDisplay: surgeUrlDisplay.value,
    actions: [
      { label: '基础配置', icon: Pencil, run: handleSurgeCustomConfig },
      { label: 'Smart', icon: Settings, run: showSurgeSmartDialog },
      { label: '预览', icon: Eye, loading: surgePreviewLoading.value, run: handleSurgePreview },
      { label: '下载', icon: Download, primary: true, loading: surgeLoading.value, run: generateSurge }
    ]
  },
  {
    key: 'loon',
    title: 'Loon',
    desc: '生成 Loon 的 .lcf 配置，iOS 上可一键导入',
    icon: Smartphone,
    url: loonUrl.value,
    urlDisplay: loonUrlDisplay.value,
    actions: [
      { label: '基础配置', icon: Pencil, run: () => showCustomConfigDialog('loon') },
      { label: '预览', icon: Eye, loading: loonPreviewLoading.value, run: () => previewConfig('loon') },
      { label: '导入 Loon', icon: Upload, run: importToLoon },
      { label: '下载', icon: Download, primary: true, loading: loonLoading.value, run: generateLoon }
    ]
  },
  {
    key: 'mosdns',
    title: 'MosDNS',
    desc: '生成 MosDNS 的 YAML 配置',
    icon: Network,
    url: mosdnsUrl.value,
    urlDisplay: mosdnsUrlDisplay.value,
    actions: [
      { label: '设置', icon: Settings, run: showMosdnsSettingsDialog },
      { label: '预览', icon: Eye, loading: mosdnsPreviewLoading.value, run: () => previewConfig('mosdns') },
      { label: '下载', icon: Download, primary: true, loading: mosdnsLoading.value, run: generateMosdns }
    ]
  }
])

/* 工作台里的代码面板就是预览，原「预览」按钮不再单列 */
const studioTargets = computed(() =>
  targets.value.map(t => ({
    ...t,
    key: t.key as 'mihomo' | 'surge' | 'loon' | 'mosdns',
    actions: t.actions.filter(action => action.label !== '预览')
  }))
)

/* 直连与代理互斥：把已被对面选走的项从本列表里剔除（已选中的自己保留），
 * 取代原先「显示为禁用项」的做法，选择面板里不再出现点不动的行。 */
const rulesetOptionsExcept = (taken: string[], mine: string[]) =>
  availableRuleSets.value
    .filter(rs => !taken.includes(rs.id) || mine.includes(rs.id))
    .map(rs => ({ value: rs.id, label: rs.name }))

const ruleOptionsExcept = (taken: string[], mine: string[]) =>
  availableRules.value
    .filter(rule => !taken.includes(rule.id) || mine.includes(rule.id))
    .map(rule => ({
      value: rule.id,
      label: `${rule.rule_type}: ${rule.value} → ${rule.policy}`
    }))

const directRulesetOptions = computed(() =>
  rulesetOptionsExcept(mosdnsProxyRulesets.value, mosdnsDirectRulesets.value)
)
const proxyRulesetOptions = computed(() =>
  rulesetOptionsExcept(mosdnsDirectRulesets.value, mosdnsProxyRulesets.value)
)
const directRuleOptions = computed(() =>
  ruleOptionsExcept(mosdnsProxyRules.value, mosdnsDirectRules.value)
)
const proxyRuleOptions = computed(() =>
  ruleOptionsExcept(mosdnsDirectRules.value, mosdnsProxyRules.value)
)

/** 三组 DNS 条目共用一套渲染，listRef 作为函数 ref 回填各自的容器元素供 Sortable 使用 */
const dnsGroups = computed(() => [
  {
    kind: 'local' as const,
    label: '国内 DNS',
    entries: mosdnsLocalDnsEntries.value,
    listRef: (el: unknown) => (localDnsListRef.value = el as HTMLElement | null),
    emptyText: '尚未设置国内 DNS，点击「添加条目」填写服务器地址',
    addrPlaceholder: 'https://dns.alidns.com/dns-query 或 223.5.5.5',
    hint: '直连规则使用的 DNS 服务器'
  },
  {
    kind: 'remote' as const,
    label: '国外 DNS',
    entries: mosdnsRemoteDnsEntries.value,
    listRef: (el: unknown) => (remoteDnsListRef.value = el as HTMLElement | null),
    emptyText: '尚未设置国外 DNS，点击「添加条目」填写服务器地址',
    addrPlaceholder: 'https://1.1.1.1/dns-query 或 1.1.1.1',
    hint: '代理规则使用的主 DNS 服务器'
  },
  {
    kind: 'fallback' as const,
    label: 'Fallback DNS',
    entries: mosdnsFallbackDnsEntries.value,
    listRef: (el: unknown) => (fallbackDnsListRef.value = el as HTMLElement | null),
    emptyText: '尚未设置备用 DNS。可点击「添加条目」填写地址；留空使用国内 DNS',
    addrPlaceholder: 'https://dns.alidns.com/dns-query 或 223.5.5.5',
    hint: '国外 DNS 超时时使用此备用服务器；留空使用国内 DNS'
  }
])

const DEFAULT_FORWARD_OPTIONS = [
  {
    value: 'forward_remote',
    label: '国外 DNS（推荐）',
    desc: '使用国外 DNS 服务器，减少 DNS 污染的影响'
  },
  { value: 'forward_local', label: '国内 DNS', desc: '使用国内 DNS 服务器处理未匹配规则的查询' }
]

const LOG_LEVELS = [
  { value: 'debug', label: 'Debug（调试）', desc: '最详细的日志，包含所有调试信息' },
  { value: 'info', label: 'Info（信息）', desc: '一般信息日志，包含重要操作记录' },
  { value: 'warn', label: 'Warn（警告）', desc: '仅记录警告和错误信息' },
  { value: 'error', label: 'Error（错误）', desc: '仅记录错误信息' }
]

// 复制 URL 到剪贴板
const copyUrl = (url: string, configType: string) => {
  // 检查 Clipboard API 是否可用
  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(url).then(() => {
      notify.success(`${configType} 订阅链接已复制，请添加到客户端`)
    }).catch(() => {
      fallbackCopyUrl(url, configType)
    })
  } else {
    // 降级到传统方法
    fallbackCopyUrl(url, configType)
  }
}

// 降级复制方法
const fallbackCopyUrl = (text: string, configType: string) => {
  const textarea = document.createElement('textarea')
  textarea.value = text
  textarea.style.position = 'fixed'
  textarea.style.opacity = '0'
  document.body.appendChild(textarea)
  textarea.select()
  try {
    document.execCommand('copy')
    notify.success(`${configType} 订阅链接已复制，请添加到客户端`)
  } catch (err) {
    notify.error('复制失败，请手动复制')
  }
  document.body.removeChild(textarea)
}

const generateMihomo = async () => {
  try {
    mihomoLoading.value = true
    const response = await generateApi.mihomo(profileId)

    // 创建下载链接
    const blob = new Blob([response.data], { type: 'application/x-yaml' })
    const url = window.URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = 'mihomo.yaml'
    link.click()
    window.URL.revokeObjectURL(url)

    notify.success('Mihomo 配置已生成')
  } catch (error) {
    notify.error('生成 Mihomo 配置失败')
  } finally {
    mihomoLoading.value = false
  }
}

const generateSurge = async () => {
  try {
    surgeLoading.value = true
    const response = await generateApi.surge(profileId)

    // 创建下载链接
    const blob = new Blob([response.data], { type: 'text/plain' })
    const url = window.URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = 'surge.conf'
    link.click()
    window.URL.revokeObjectURL(url)

    notify.success('Surge 配置已生成')
  } catch (error) {
    notify.error('生成 Surge 配置失败')
  } finally {
    surgeLoading.value = false
  }
}

const generateLoon = async () => {
  try {
    loonLoading.value = true
    const response = await generateApi.loon(profileId)

    // 创建下载链接
    const blob = new Blob([response.data], { type: 'text/plain' })
    const url = window.URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `${loonConfigName.value}.lcf`
    link.click()
    window.URL.revokeObjectURL(url)

    notify.success('Loon 配置已生成')
  } catch (error) {
    notify.error('生成 Loon 配置失败')
  } finally {
    loonLoading.value = false
  }
}

// 通过 Loon URL Scheme 导入远程配置：https://nsloon.app/docs/Scheme/
// 需在用户点击中同步跳转，iOS Safari 才会唤起 App；未安装 Loon 时 Safari 会提示无法打开
const importToLoon = () => {
  window.location.href = `loon://import?sub=${encodeURIComponent(loonUrl.value)}`
}

const generateMosdns = async () => {
  try {
    mosdnsLoading.value = true
    const response = await generateApi.mosdns(profileId)

    // 创建下载链接
    const blob = new Blob([response.data], { type: 'application/zip' })
    const url = window.URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = 'mosdns-config.zip'
    link.click()
    window.URL.revokeObjectURL(url)

    notify.success('MosDNS 配置已生成')
  } catch (error: any) {
    let message = '生成 MosDNS 配置失败'
    if (error?.code === 'ECONNABORTED' || error?.code === 'ETIMEDOUT') {
      message = '生成 MosDNS 配置超时，请稍后重试'
    } else {
      try {
        // 下载接口的 JSON 错误也会被 Axios 按 Blob 接收。
        const data = error?.response?.data
        const details = data instanceof Blob ? JSON.parse(await data.text()) : data
        if (typeof details?.message === 'string' && details.message.trim()) {
          message = `生成 MosDNS 配置失败：${details.message.trim()}`
        }
      } catch {
        // 网关可能返回 HTML 或无效 JSON，保留可读的通用错误提示。
      }
    }
    notify.error(message)
  } finally {
    mosdnsLoading.value = false
  }
}

const getCustomConfigDialogTitle = () => {
  const titles = {
    mihomo: '自定义 Mihomo 基础配置',
    surge: '自定义 Surge 基础配置',
    loon: '自定义 Loon 基础配置',
    mosdns: '自定义 MosDNS 基础配置'
  }
  return titles[currentConfigType.value]
}

const getCustomConfigDialogDesc = () => {
  const descs = {
    mihomo: '设置 mixed-port、dns、tun 等基础选项。生成时会合并当前配置空间中的节点、策略组和规则（proxies、proxy-groups、rules）。',
    surge: '编辑 [General] 等基础设置；[Proxy]、[Proxy Group]、[Rule] 将由当前配置空间的设置生成。规则格式为 TYPE,VALUE,POLICY，如 DOMAIN-SUFFIX,google.com,Proxy。',
    loon: '编辑 [General]、[Rewrite]、[Plugin]、[MITM] 等设置；[Proxy]、[Remote Proxy]、[Remote Filter]、[Proxy Group]、[Rule]、[Remote Rule] 将由当前配置空间的设置生成。',
    mosdns: '设置 log、data_providers、plugins、servers 等选项。请按依赖关系排列插件，确保被使用的插件先初始化。'
  }
  return descs[currentConfigType.value]
}

const getCustomConfigPlaceholder = () => {
  const placeholders = {
    mihomo: '输入自定义 YAML 配置，例如：\nmixed-port: 7890\nallow-lan: true\nmode: rule\nlog-level: info\nexternal-controller: 127.0.0.1:9090\ndns:\n  enable: true\n  listen: 0.0.0.0:53\n  enhanced-mode: fake-ip',
    surge: '输入自定义配置，例如：\n[General]\nloglevel = notify\ninternet-test-url = http://www.gstatic.com/generate_204\nproxy-test-url = http://www.gstatic.com/generate_204\nskip-proxy = 127.0.0.1, 192.168.0.0/16, 10.0.0.0/8\n\n# 代理将自动生成在 [Proxy] 部分\n# 策略组将自动生成在 [Proxy Group] 部分\n# 规则将自动生成在 [Rule] 部分',
    loon: '输入自定义配置，例如：\n[General]\nip-mode = dual\ndns-server = system, 223.5.5.5\nproxy-test-url = http://www.gstatic.com/generate_204\nskip-proxy = 192.168.0.0/16, 10.0.0.0/8, localhost, *.local\n\n[Plugin]\n# 插件链接, tag=名称, enabled=true\n\n# 节点、订阅、策略组和规则将自动生成',
    mosdns: '输入自定义 YAML 配置，例如：\nlog:\n  level: info\n  file: ./mosdns.log\n\nservers:\n  - addr: 127.0.0.1:53\n    protocol: udp\n\n# data_providers 和 plugins 将自动生成'
  }
  return placeholders[currentConfigType.value]
}

const getPreviewDialogTitle = () => {
  const titles = {
    mihomo: '预览 Mihomo 配置',
    surge: '预览 Surge 配置',
    loon: '预览 Loon 配置',
    mosdns: '预览 MosDNS 配置'
  }
  return titles[currentPreviewType.value]
}

const showCustomConfigDialog = async (type: ConfigType) => {
  currentConfigType.value = type
  try {
    const apiMap = {
      mihomo: customConfigApi.getMihomo,
      surge: customConfigApi.getSurge,
      loon: customConfigApi.getLoon,
      mosdns: customConfigApi.getMosdns
    }
    const response = await apiMap[type](profileId)
    customConfigContent.value = response.data.config || ''
    customConfigDialogVisible.value = true
  } catch (error) {
    notify.error('加载自定义配置失败')
  }
}

const saveCustomConfig = async () => {
  try {
    savingCustomConfig.value = true
    const apiMap = {
      mihomo: customConfigApi.saveMihomo,
      surge: customConfigApi.saveSurge,
      loon: customConfigApi.saveLoon,
      mosdns: customConfigApi.saveMosdns
    }
    await apiMap[currentConfigType.value]({ config: customConfigContent.value }, profileId)
    notify.success('自定义配置已保存')
    customConfigDialogVisible.value = false
  } catch (error) {
    notify.error('保存基础配置失败')
  } finally {
    savingCustomConfig.value = false
  }
}

const previewConfig = async (type: ConfigType) => {
  currentPreviewType.value = type
  const loadingMap = {
    mihomo: mihomoPreviewLoading,
    surge: surgePreviewLoading,
    loon: loonPreviewLoading,
    mosdns: mosdnsPreviewLoading
  }

  try {
    loadingMap[type].value = true

    const apiMap = {
      mihomo: generateApi.previewMihomo,
      surge: generateApi.previewSurge,
      loon: generateApi.previewLoon,
      mosdns: generateApi.previewMosdns
    }
    const response = await apiMap[type](profileId)
    previewContent.value = response.data.content || response.data
    previewDialogVisible.value = true
  } catch (error) {
    notify.error('生成预览失败')
  } finally {
    loadingMap[type].value = false
  }
}

const copyPreviewContent = () => {
  // 检查 Clipboard API 是否可用
  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(previewContent.value).then(() => {
      notify.success('已复制到剪贴板')
    }).catch(() => {
      fallbackCopyPreview()
    })
  } else {
    // 降级到传统方法
    fallbackCopyPreview()
  }
}

// 降级复制预览内容
const fallbackCopyPreview = () => {
  const textarea = document.createElement('textarea')
  textarea.value = previewContent.value
  textarea.style.position = 'fixed'
  textarea.style.opacity = '0'
  document.body.appendChild(textarea)
  textarea.select()
  try {
    document.execCommand('copy')
    notify.success('已复制到剪贴板')
  } catch (err) {
    notify.error('复制失败，请手动复制预览内容')
  }
  document.body.removeChild(textarea)
}

// ===== DNS 条目管理函数 =====

// 添加 DNS 条目
const addDnsEntry = (type: 'local' | 'remote' | 'fallback') => {
  const newEntry: DnsEntry = {
    id: generateId(),
    mode: 'simple',
    addr: '',
    bootstrap: '',
    enable_pipeline: false,
    yaml_config: ''
  }

  if (type === 'local') {
    mosdnsLocalDnsEntries.value.push(newEntry)
  } else if (type === 'remote') {
    mosdnsRemoteDnsEntries.value.push(newEntry)
  } else {
    mosdnsFallbackDnsEntries.value.push(newEntry)
  }
}

// 删除 DNS 条目
const removeDnsEntry = (type: 'local' | 'remote' | 'fallback', id: string) => {
  if (type === 'local') {
    mosdnsLocalDnsEntries.value = mosdnsLocalDnsEntries.value.filter(entry => entry.id !== id)
  } else if (type === 'remote') {
    mosdnsRemoteDnsEntries.value = mosdnsRemoteDnsEntries.value.filter(entry => entry.id !== id)
  } else {
    mosdnsFallbackDnsEntries.value = mosdnsFallbackDnsEntries.value.filter(entry => entry.id !== id)
  }
}

// 切换 DNS 条目模式
const toggleDnsEntryMode = (type: 'local' | 'remote' | 'fallback', id: string) => {
  const entries = type === 'local' ? mosdnsLocalDnsEntries.value
                : type === 'remote' ? mosdnsRemoteDnsEntries.value
                : mosdnsFallbackDnsEntries.value

  const entry = entries.find(e => e.id === id)
  if (entry) {
    if (entry.mode === 'simple') {
      // 切换到 YAML 模式，生成初始 YAML 配置
      const yamlParts = [`- addr: ${entry.addr || ''}`]
      // 只有域名才添加 bootstrap
      if (entry.bootstrap && isDomainAddr(entry.addr)) {
        yamlParts.push(`  bootstrap: ${entry.bootstrap}`)
      }
      if (entry.enable_pipeline) {
        yamlParts.push(`  enable_pipeline: true`)
      }
      entry.yaml_config = yamlParts.join('\n')
      entry.mode = 'yaml'
    } else {
      // 切换到简单模式，尝试解析 YAML
      try {
        const lines = entry.yaml_config.trim().split('\n')
        let addr = ''
        let bootstrap = ''
        let enable_pipeline = false

        for (const line of lines) {
          const trimmed = line.trim()
          if (trimmed.startsWith('addr:') || trimmed.startsWith('- addr:')) {
            addr = trimmed.replace(/^-?\s*addr:\s*/, '')
          } else if (trimmed.startsWith('bootstrap:')) {
            bootstrap = trimmed.replace(/^bootstrap:\s*/, '')
          } else if (trimmed.includes('enable_pipeline') && trimmed.includes('true')) {
            enable_pipeline = true
          }
        }

        entry.addr = addr
        // 如果地址不是域名，清空 bootstrap
        entry.bootstrap = isDomainAddr(addr) ? bootstrap : ''
        entry.enable_pipeline = enable_pipeline
        entry.mode = 'simple'
      } catch (error) {
        notify.warning('无法读取 YAML，地址等字段已清空，请重新填写')
        entry.addr = ''
        entry.bootstrap = ''
        entry.enable_pipeline = false
        entry.mode = 'simple'
      }
    }
  }
}

// 初始化 DNS 条目拖拽功能
const initDnsSortable = () => {
  nextTick(() => {
    // 初始化国内 DNS 拖拽
    if (localDnsListRef.value) {
      Sortable.create(localDnsListRef.value, {
        animation: 150,
        handle: '.dns-entry-card',
        ghostClass: 'sortable-ghost',
        onEnd: (evt) => {
          const oldIndex = evt.oldIndex
          const newIndex = evt.newIndex
          if (oldIndex !== undefined && newIndex !== undefined && oldIndex !== newIndex) {
            const item = mosdnsLocalDnsEntries.value.splice(oldIndex, 1)[0]
            mosdnsLocalDnsEntries.value.splice(newIndex, 0, item)
          }
        }
      })
    }

    // 初始化国外 DNS 拖拽
    if (remoteDnsListRef.value) {
      Sortable.create(remoteDnsListRef.value, {
        animation: 150,
        handle: '.dns-entry-card',
        ghostClass: 'sortable-ghost',
        onEnd: (evt) => {
          const oldIndex = evt.oldIndex
          const newIndex = evt.newIndex
          if (oldIndex !== undefined && newIndex !== undefined && oldIndex !== newIndex) {
            const item = mosdnsRemoteDnsEntries.value.splice(oldIndex, 1)[0]
            mosdnsRemoteDnsEntries.value.splice(newIndex, 0, item)
          }
        }
      })
    }

    // 初始化 Fallback DNS 拖拽
    if (fallbackDnsListRef.value) {
      Sortable.create(fallbackDnsListRef.value, {
        animation: 150,
        handle: '.dns-entry-card',
        ghostClass: 'sortable-ghost',
        onEnd: (evt) => {
          const oldIndex = evt.oldIndex
          const newIndex = evt.newIndex
          if (oldIndex !== undefined && newIndex !== undefined && oldIndex !== newIndex) {
            const item = mosdnsFallbackDnsEntries.value.splice(oldIndex, 1)[0]
            mosdnsFallbackDnsEntries.value.splice(newIndex, 0, item)
          }
        }
      })
    }
  })
}

const loadRuleSets = async () => {
  try {
    const response = await ruleSetApi.getAll(profileId)
    availableRuleSets.value = response.data
  } catch (error) {
    console.error('加载规则集列表失败', error)
  }
}

const loadRules = async () => {
  try {
    const response = await ruleApi.getAll(profileId)
    // 筛选出单条规则（itemType 为 'rule'）
    availableRules.value = response.data.filter((item: any) => item.itemType === 'rule')
  } catch (error) {
    console.error('加载规则列表失败', error)
  }
}

const getMosdnsCustomConfigPlaceholder = () => {
  return '输入自定义 YAML 配置，例如：\nlog:\n  level: info\n  file: ./mosdns.log\n\nservers:\n  - addr: 127.0.0.1:5335\n    protocol: udp\n\n# data_providers 和 plugins 将自动生成'
}

const showMosdnsSettingsDialog = async () => {
  try {
    // 加载规则集列表和规则列表
    await Promise.all([loadRuleSets(), loadRules()])

    // 加载自定义配置
    const customConfigResponse = await customConfigApi.getMosdns(profileId)
    mosdnsCustomConfig.value = customConfigResponse.data.config || ''

    // 加载规则集配置
    const rulesetResponse = await api.get('/mosdns/rulesets', profileOptions)
    mosdnsDirectRulesets.value = rulesetResponse.data.direct_rulesets || []
    mosdnsProxyRulesets.value = rulesetResponse.data.proxy_rulesets || []
    mosdnsDirectRules.value = rulesetResponse.data.direct_rules || []
    mosdnsProxyRules.value = rulesetResponse.data.proxy_rules || []

    // 加载自定义 match 配置
    const customMatchResponse = await api.get('/mosdns/custom-matches', profileOptions)
    const fetchedMatches = Array.isArray(customMatchResponse.data?.custom_matches)
      ? customMatchResponse.data.custom_matches
      : []
    const fetchedPosition = customMatchResponse.data?.position
    mosdnsCustomMatchPosition.value = fetchedPosition === 'head' ? 'head' : 'tail'
    mosdnsCustomMatches.value = fetchedMatches.map((item: any) => ({
      id: item.id || generateMatchId(),
      enabled: item.enabled !== undefined ? Boolean(item.enabled) : true,
      exec: item.exec || '',
      matchesText: Array.isArray(item.matches)
        ? item.matches.join('\n')
        : (item.matches || '')
    }))

    // 加载 DNS 服务器配置
    const dnsResponse = await api.get('/mosdns/dns-servers', profileOptions)
    mosdnsLocalDns.value = dnsResponse.data.local_dns || ''
    mosdnsRemoteDns.value = dnsResponse.data.remote_dns || ''
    mosdnsFallbackDns.value = dnsResponse.data.fallback_dns || ''
    // 解析 DNS 文本为条目数组
    mosdnsLocalDnsEntries.value = parseDnsText(mosdnsLocalDns.value)
    mosdnsRemoteDnsEntries.value = parseDnsText(mosdnsRemoteDns.value)
    mosdnsFallbackDnsEntries.value = parseDnsText(mosdnsFallbackDns.value)
    mosdnsDefaultForward.value = dnsResponse.data.default_forward || 'forward_remote'
    mosdnsCustomHosts.value = dnsResponse.data.custom_hosts || ''

    // 加载日志配置
    const logResponse = await api.get('/mosdns/log-settings', profileOptions)
    mosdnsLogEnabled.value = logResponse.data.log_enabled !== undefined ? logResponse.data.log_enabled : true
    mosdnsLogLevel.value = logResponse.data.log_level || 'info'
    mosdnsLogFile.value = logResponse.data.log_file || './mosdns.log'

    // 加载 API 配置
    const apiResponse = await api.get('/mosdns/api-settings', profileOptions)
    mosdnsApiEnabled.value = apiResponse.data.api_enabled !== undefined ? apiResponse.data.api_enabled : true
    mosdnsApiAddress.value = apiResponse.data.api_address || '0.0.0.0:8338'

    // 加载缓存配置
    const cacheResponse = await api.get('/mosdns/cache-settings', profileOptions)
    mosdnsCacheEnabled.value = cacheResponse.data.cache_enabled !== undefined ? Boolean(cacheResponse.data.cache_enabled) : true
    mosdnsCacheSize.value = Number(cacheResponse.data.cache_size ?? 10240)
    mosdnsCacheLazyTtl.value = Number(cacheResponse.data.cache_lazy_ttl ?? 21600)
    mosdnsCacheDumpEnabled.value = cacheResponse.data.cache_dump_enabled !== undefined ? Boolean(cacheResponse.data.cache_dump_enabled) : true
    mosdnsCacheDumpFile.value = cacheResponse.data.cache_dump_file ?? './cache.dump'
    mosdnsCacheDumpInterval.value = Number(cacheResponse.data.cache_dump_interval ?? 300)

    // 重置到第一个 tab
    mosdnsActiveTab.value = 'rules'

    // 显示对话框
    mosdnsSettingsDialogVisible.value = true

    // 初始化拖拽功能
    initDnsSortable()
  } catch (error) {
    console.error('加载 MosDNS 设置失败', error)
    notify.error('加载 MosDNS 设置失败')
  }
}

const saveMosdnsSettings = async () => {
  try {
    savingMosdnsSettings.value = true

    // 保存自定义配置
    await customConfigApi.saveMosdns({ config: mosdnsCustomConfig.value }, profileId)

    // 保存规则集和规则配置
    await api.post('/mosdns/rulesets', {
      direct_rulesets: mosdnsDirectRulesets.value,
      proxy_rulesets: mosdnsProxyRulesets.value,
      direct_rules: mosdnsDirectRules.value,
      proxy_rules: mosdnsProxyRules.value
    }, profileOptions)

    const customMatchPayload = mosdnsCustomMatches.value.map(item => ({
      id: item.id,
      enabled: item.enabled,
      exec: item.exec,
      matches: item.matchesText
    }))

    await api.post('/mosdns/custom-matches', {
      custom_matches: customMatchPayload,
      position: mosdnsCustomMatchPosition.value
    }, profileOptions)

    // 将 DNS 条目数组转换为文本
    const localDnsText = dnsEntriesToText(mosdnsLocalDnsEntries.value)
    const remoteDnsText = dnsEntriesToText(mosdnsRemoteDnsEntries.value)
    const fallbackDnsText = dnsEntriesToText(mosdnsFallbackDnsEntries.value)

    // 保存 DNS 服务器配置
    await api.post('/mosdns/dns-servers', {
      local_dns: localDnsText,
      remote_dns: remoteDnsText,
      fallback_dns: fallbackDnsText,
      default_forward: mosdnsDefaultForward.value,
      custom_hosts: mosdnsCustomHosts.value
    }, profileOptions)

    // 同步更新文本字段（保持兼容性）
    mosdnsLocalDns.value = localDnsText
    mosdnsRemoteDns.value = remoteDnsText
    mosdnsFallbackDns.value = fallbackDnsText

    // 保存日志配置
    await api.post('/mosdns/log-settings', {
      log_enabled: mosdnsLogEnabled.value,
      log_level: mosdnsLogLevel.value,
      log_file: mosdnsLogFile.value
    }, profileOptions)

    // 保存 API 配置
    await api.post('/mosdns/api-settings', {
      api_enabled: mosdnsApiEnabled.value,
      api_address: mosdnsApiAddress.value
    }, profileOptions)

    // 保存缓存配置
    await api.post('/mosdns/cache-settings', {
      cache_enabled: mosdnsCacheEnabled.value,
      cache_size: mosdnsCacheSize.value,
      cache_lazy_ttl: mosdnsCacheLazyTtl.value,
      cache_dump_enabled: mosdnsCacheDumpEnabled.value,
      cache_dump_file: mosdnsCacheDumpFile.value,
      cache_dump_interval: mosdnsCacheDumpInterval.value
    }, profileOptions)

    notify.success('MosDNS 设置已保存')
    mosdnsSettingsDialogVisible.value = false
  } catch (error) {
    console.error('保存 MosDNS 设置失败', error)
    notify.error('保存 MosDNS 设置失败')
  } finally {
    savingMosdnsSettings.value = false
  }
}

onMounted(async () => {
  // 从后端加载服务域名配置
  try {
    const response = await serverDomainApi.get()
    const backendDomain = response.data.server_domain

    // 如果后端有配置，优先使用后端的配置
    if (backendDomain && backendDomain.trim()) {
      serverDomain.value = backendDomain
      localStorage.setItem('serverDomain', backendDomain)
    } else {
      serverDomain.value = window.location.origin
      localStorage.setItem('serverDomain', serverDomain.value)
    }
  } catch (error) {
    console.error('加载服务域名失败:', error)
    // 加载失败，使用 localStorage 或当前地址
    const fallbackDomain = localStorage.getItem('serverDomain') || window.location.origin
    serverDomain.value = fallbackDomain
  }

  // 加载配置令牌
  await loadConfigToken()
})

</script>
