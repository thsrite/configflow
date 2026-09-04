<template>
  <div>
    <ScopeBanner scope="profile" :profile-name="cfProfileName" />

    <PageHeader
      eyebrow="Profile"
      title="配置生成"
      description="Mihomo、Surge 与 MosDNS 配置的生成与预览，订阅 URL 可直接给客户端使用。"
    />

    <!-- ===== 三个生成目标 ===== -->
    <div class="mb-4 grid grid-cols-3 gap-3 max-[1100px]:grid-cols-1">
      <Motion
        v-for="(target, index) in targets"
        :key="target.key"
        v-bind="listItem(index)"
        class="hairline edge-light relative flex flex-col gap-3.5 overflow-hidden rounded-xl border border-border/35 bg-card/55 p-5 backdrop-blur-xl transition-all duration-300 hover:shadow-glow-soft max-md:p-4"
      >
        <header class="flex items-center gap-2.5">
          <span
            class="relative grid size-9 shrink-0 place-items-center rounded-lg border border-border/50 bg-background/50 text-primary-accent"
          >
            <span
              class="absolute inset-0 rounded-lg bg-linear-to-br from-primary/20 to-accent-2/10"
              aria-hidden="true"
            />
            <component :is="target.icon" class="relative size-4.5" :stroke-width="2" aria-hidden="true" />
          </span>
          <div class="min-w-0">
            <p class="m-0 text-[14px] font-semibold text-foreground">{{ target.title }}</p>
            <p class="mt-0.5 mb-0 text-[12px] text-muted-foreground">{{ target.desc }}</p>
          </div>
        </header>

        <!-- 订阅 URL：只读展示 + 一键复制 -->
        <div class="flex flex-col gap-1.5">
          <div class="flex items-center gap-1.5">
            <Input
              :model-value="target.urlDisplay"
              readonly
              class="h-9 bg-background/50 font-mono text-[11.5px]"
              placeholder="配置 URL"
              :aria-label="`${target.title} 配置 URL`"
            />
            <Button
              variant="outline"
              size="icon"
              class="size-9 shrink-0 border-border/60 bg-background/40"
              :title="`复制 ${target.title} 配置 URL`"
              :aria-label="`复制 ${target.title} 配置 URL`"
              @click="copyUrl(target.url, target.title)"
            >
              <Copy class="size-4" />
            </Button>
          </div>
          <p class="m-0 text-[11.5px] text-muted-foreground">复制后可在客户端中直接订阅此 URL。</p>
        </div>

        <div class="mt-auto flex flex-wrap items-center gap-1.5">
          <Button
            v-for="action in target.actions"
            :key="action.label"
            :variant="action.primary ? 'default' : 'outline'"
            size="sm"
            :class="action.primary ? 'shadow-glow' : 'border-border/60 bg-background/40'"
            :disabled="action.loading"
            @click="action.run"
          >
            <Loader2 v-if="action.loading" class="size-3.5 animate-spin" />
            <component v-else :is="action.icon" class="size-3.5" />
            {{ action.label }}
          </Button>
        </div>
      </Motion>
    </div>

    <!-- ===== 服务配置 / 配置管理 ===== -->
    <div class="grid grid-cols-2 gap-3 max-[1100px]:grid-cols-1">
      <SectionCard title="服务配置" :icon="Settings">
        <div class="flex flex-col gap-4">
          <FormField
            label="服务域名"
            html-for="server-domain"
            hint="用于规则仓库内容 URL、MosDNS 规则转换接口、Agent 安装脚本与配置订阅 URL。"
          >
            <div class="flex items-center gap-1.5">
              <Input
                id="server-domain"
                v-model="serverDomain"
                class="bg-background/50 font-mono"
                placeholder="http://example.com:5001"
                @blur="onServerDomainBlur"
              />
              <Button
                variant="outline"
                size="icon"
                class="shrink-0 border-border/60 bg-background/40"
                title="恢复默认"
                aria-label="恢复默认服务域名"
                @click="resetServerDomain"
              >
                <RefreshCw class="size-4" />
              </Button>
            </div>
          </FormField>

          <FormField
            label="Sub-Store"
            html-for="sub-store-url"
            hint="Sub-Store 后端 API 地址，用于订阅解析和节点格式转换。Docker 部署默认 http://sub-store:3001，留空使用环境变量或默认值。"
          >
            <Input
              id="sub-store-url"
              v-model="subStoreUrl"
              class="bg-background/50 font-mono"
              placeholder="http://127.0.0.1:3001"
              @blur="onSubStoreUrlBlur"
            />
          </FormField>

          <FormField hint="开启后将在资源分组下显示「订阅聚合」，可组合订阅和节点。">
            <div class="flex items-center gap-2.5">
              <Switch
                id="agg-enabled"
                v-model="subscriptionAggregationEnabled"
                @update:model-value="value => onSubscriptionAggregationChange(Boolean(value))"
              />
              <Label for="agg-enabled" class="text-[13px] text-muted-foreground">订阅聚合</Label>
            </div>
          </FormField>

          <FormField
            label="令牌"
            html-for="config-token"
            hint="配置令牌后，外部访问配置 URL 需要添加 ?token=xxx 参数；留空表示不启用令牌保护。"
          >
            <div class="flex items-center gap-1.5">
              <Input
                id="config-token"
                v-model="configToken"
                class="bg-background/50 font-mono"
                placeholder="可手动输入或点击生成"
                @blur="onTokenBlur"
              />
              <Button
                variant="outline"
                size="icon"
                class="shrink-0 border-border/60 bg-background/40"
                title="生成随机令牌"
                aria-label="生成随机令牌"
                @click="generateToken"
              >
                <RefreshCw class="size-4" />
              </Button>
            </div>
          </FormField>
        </div>
      </SectionCard>

      <SectionCard title="配置管理" :icon="Archive">
        <div class="flex flex-col gap-4">
          <div class="grid grid-cols-2 gap-2 max-[480px]:grid-cols-1">
            <Button variant="outline" class="border-border/60 bg-background/40" @click="exportConfig">
              <Upload class="size-4" />
              导出
            </Button>
            <Button
              variant="outline"
              class="border-border/60 bg-background/40"
              @click="exportConfigDesensitized"
            >
              <ShieldCheck class="size-4" />
              脱敏导出
            </Button>
            <Button variant="outline" class="border-border/60 bg-background/40" @click="pickImportFile">
              <Download class="size-4" />
              导入
            </Button>
            <Button variant="outline" class="border-border/60 bg-background/40" @click="handleBackup">
              <CloudUpload class="size-4" />
              备份
            </Button>
            <input
              ref="importInput"
              type="file"
              accept=".json"
              hidden
              @change="onImportFileChange"
            />
          </div>

          <p class="m-0 text-[12px] leading-relaxed text-muted-foreground">
            导出保存所有订阅、节点、规则和策略组设置；脱敏导出会隐藏敏感信息；导入将覆盖当前所有设置；备份会把配置上传到远程存储（如 WebDAV）。
          </p>

          <!-- 重置会清空全部数据，与常规操作分区并降低视觉权重，避免误触 -->
          <div
            class="mt-auto flex flex-wrap items-center gap-3 rounded-lg border border-destructive-accent/25 bg-destructive-soft/30 p-3"
          >
            <div class="min-w-0 flex-1">
              <p class="m-0 text-[13px] font-semibold text-destructive-accent">重置配置</p>
              <p class="mt-0.5 mb-0 text-[12px] text-muted-foreground">
                恢复默认配置并清空所有数据，不可撤销。
              </p>
            </div>
            <Button
              variant="outline"
              size="sm"
              class="shrink-0 border-destructive-accent/40 bg-transparent text-destructive-accent"
              @click="resetConfig"
            >
              <RotateCcw class="size-3.5" />
              重置
            </Button>
          </div>
        </div>
      </SectionCard>
    </div>

    <!-- ===== 自定义基础配置 ===== -->
    <Dialog v-model:open="customConfigDialogVisible">
      <DialogContent class="glass-strong hairline max-w-[900px] border-border/50">
        <DialogHeader>
          <DialogTitle>{{ getCustomConfigDialogTitle() }}</DialogTitle>
          <DialogDescription>
            {{ getCustomConfigDialogDesc() }}
            留空则使用默认基础配置，{{ currentConfigType === 'surge' ? '支持 INI 格式语法' : '支持 YAML 语法高亮' }}。
          </DialogDescription>
        </DialogHeader>

        <YamlEditor v-model="customConfigContent" :placeholder="getCustomConfigPlaceholder()" />

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
      <DialogContent class="glass-strong hairline max-w-[1000px] border-border/50">
        <DialogHeader>
          <DialogTitle>{{ getPreviewDialogTitle() }}</DialogTitle>
          <DialogDescription>只读预览，可整段复制到剪贴板。</DialogDescription>
        </DialogHeader>

        <YamlEditor v-model="previewContent" :read-only="true" />

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
      <DialogContent class="glass-strong hairline max-w-[760px] border-border/50">
        <DialogHeader>
          <DialogTitle>MosDNS 设置</DialogTitle>
          <DialogDescription>规则分流、缓存、DNS 服务器与日志等生成参数。</DialogDescription>
        </DialogHeader>

        <Tabs v-model="mosdnsActiveTab" class="min-w-0">
          <TabsList class="w-full justify-start overflow-x-auto bg-background/50">
            <TabsTrigger value="rules" class="text-xs">规则配置</TabsTrigger>
            <TabsTrigger value="cache" class="text-xs">缓存</TabsTrigger>
            <TabsTrigger value="dns" class="text-xs">DNS 服务器</TabsTrigger>
            <TabsTrigger value="default" class="text-xs">默认转发</TabsTrigger>
            <TabsTrigger value="hosts" class="text-xs">自定义 Host</TabsTrigger>
            <TabsTrigger value="log" class="text-xs">日志</TabsTrigger>
            <TabsTrigger value="api" class="text-xs">API</TabsTrigger>
          </TabsList>

          <!-- 规则配置 -->
          <TabsContent value="rules" class="max-h-[56dvh] overflow-y-auto pr-1">
            <div class="flex flex-col gap-4">
              <InfoNote>
                <p>选择哪些规则 / 规则集使用直连 DNS（国内），哪些使用代理 DNS（国外）。</p>
                <p>只有被选择的规则和规则集会包含在 MosDNS 配置中。</p>
              </InfoNote>

              <LabeledDivider label="直连规则配置" />

              <FormField label="直连规则集" hint="这些规则集将使用国内 DNS（与代理规则集互斥）。">
                <MultiSelect
                  v-model="mosdnsDirectRulesets"
                  :options="directRulesetOptions"
                  placeholder="选择使用国内 DNS 的规则集"
                />
              </FormField>

              <FormField label="直连规则" hint="这些单条规则将使用国内 DNS（与代理规则互斥）。">
                <MultiSelect
                  v-model="mosdnsDirectRules"
                  :options="directRuleOptions"
                  placeholder="选择使用国内 DNS 的规则"
                />
              </FormField>

              <LabeledDivider label="代理规则配置" />

              <FormField label="代理规则集" hint="这些规则集将使用国外 DNS（与直连规则集互斥）。">
                <MultiSelect
                  v-model="mosdnsProxyRulesets"
                  :options="proxyRulesetOptions"
                  placeholder="选择使用国外 DNS 的规则集"
                />
              </FormField>

              <FormField label="代理规则" hint="这些单条规则将使用国外 DNS（与直连规则互斥）。">
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
                  <SelectContent class="glass-strong">
                    <SelectItem value="head">优先匹配（在规则匹配之前执行）</SelectItem>
                    <SelectItem value="tail">尾部匹配（在规则匹配之后执行）</SelectItem>
                  </SelectContent>
                </Select>
              </FormField>

              <div class="flex flex-col gap-2">
                <div class="flex items-center gap-2">
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
                  暂无自定义 match，可点击上方按钮添加。
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
          <TabsContent value="cache" class="max-h-[56dvh] overflow-y-auto pr-1">
            <div class="flex flex-col gap-4">
              <InfoNote>
                <p>配置 MosDNS 缓存（lazy cache），用于加速重复解析。</p>
                <p>关闭后会从生成配置中移除 cache 插件，并且不再在国内 DNS 序列中执行缓存。</p>
              </InfoNote>

              <FormField hint="关闭后会完全移除 tag: lazy_cache 相关配置。">
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
                    <Label for="cache-dump" class="text-[13px] text-muted-foreground">持久化缓存</Label>
                  </div>
                </FormField>

                <template v-if="mosdnsCacheDumpEnabled">
                  <FormField
                    label="dump_file"
                    html-for="cache-dump-file"
                    hint="缓存持久化文件路径（相对于 MosDNS 配置目录）。"
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
          <TabsContent value="dns" class="max-h-[56dvh] overflow-y-auto pr-1">
            <div class="flex flex-col gap-5">
              <InfoNote>
                <p>配置国内和国外的 DNS 服务器地址。</p>
                <p>支持 UDP、TCP、DoH、DoT 等协议格式，每个条目可使用简单模式或 YAML 模式。</p>
              </InfoNote>

              <section v-for="group in dnsGroups" :key="group.kind" class="flex flex-col gap-2">
                <div class="flex items-center gap-2">
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
                    <div class="flex items-center gap-2">
                      <GripVertical
                        class="drag-handle size-3.5 shrink-0 cursor-grab text-muted-foreground"
                        aria-hidden="true"
                      />
                      <span class="text-[12.5px] font-semibold text-foreground">条目 {{ index + 1 }}</span>
                      <div class="ml-auto flex items-center gap-1">
                        <Button
                          variant="ghost"
                          size="sm"
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
          <TabsContent value="default" class="max-h-[56dvh] overflow-y-auto pr-1">
            <div class="flex flex-col gap-4">
              <InfoNote>
                <p>当所有规则都不匹配时，使用的默认 DNS 服务器。</p>
                <p>推荐使用「国外 DNS」以避免污染。</p>
              </InfoNote>

              <RadioGroup v-model="mosdnsDefaultForward" class="flex flex-col gap-2">
                <label
                  v-for="option in DEFAULT_FORWARD_OPTIONS"
                  :key="option.value"
                  :class="[
                    'flex cursor-pointer items-start gap-2.5 rounded-lg border px-3.5 py-3 transition-colors',
                    mosdnsDefaultForward === option.value
                      ? 'border-primary-accent/40 bg-primary-soft/50'
                      : 'border-border/50 bg-background/40 hover:border-border-strong'
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
          <TabsContent value="hosts" class="max-h-[56dvh] overflow-y-auto pr-1">
            <div class="flex flex-col gap-4">
              <InfoNote>
                <p>配置自定义域名解析，优先级最高。</p>
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
          <TabsContent value="log" class="max-h-[56dvh] overflow-y-auto pr-1">
            <div class="flex flex-col gap-4">
              <InfoNote>
                <p>配置 MosDNS 日志输出级别和文件路径。</p>
                <p>可以控制日志详细程度，帮助排查问题。</p>
              </InfoNote>

              <FormField hint="关闭日志可以提高性能，但不利于问题排查。">
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
                    <SelectContent class="glass-strong">
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
          <TabsContent value="api" class="max-h-[56dvh] overflow-y-auto pr-1">
            <div class="flex flex-col gap-4">
              <InfoNote>
                <p>配置 MosDNS API 接口，用于监控和管理 MosDNS 服务。</p>
                <p>API 接口可以查询 MosDNS 运行状态和统计信息。</p>
              </InfoNote>

              <FormField hint="关闭 API 可以减少资源占用，但无法通过 API 查询状态。">
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
      <DialogContent class="glass-strong hairline max-w-[680px] border-border/50">
        <DialogHeader>
          <DialogTitle>Surge Smart 模式</DialogTitle>
          <DialogDescription>
            选择要在 Surge 中以 Smart 模式输出的策略组并配置 policy-priority。
            同一策略组在 Mihomo 中仍输出原类型（如 url-test），仅 Surge 配置受影响。
          </DialogDescription>
        </DialogHeader>

        <div class="flex max-h-[52dvh] flex-col gap-2 overflow-y-auto pr-1">
          <div
            v-for="(item, index) in surgeSmartGroups"
            :key="index"
            class="flex items-center gap-2"
          >
            <Select v-model="item.group_id">
              <SelectTrigger class="flex-1 bg-background/50">
                <SelectValue placeholder="选择策略组" />
              </SelectTrigger>
              <SelectContent class="glass-strong">
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
              class="flex-[1.5] bg-background/50 font-mono text-[12px]"
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
            添加
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

    <!-- ===== 配置备份 ===== -->
    <Dialog v-model:open="backupDialogVisible">
      <DialogContent class="glass-strong hairline max-w-[620px] border-border/50">
        <DialogHeader>
          <DialogTitle>配置备份</DialogTitle>
          <DialogDescription>
            配置 WebDAV 远程备份，自动将配置文件上传到远程存储。支持坚果云、Nextcloud 等服务。
          </DialogDescription>
        </DialogHeader>

        <div class="flex max-h-[56dvh] flex-col gap-4 overflow-y-auto pr-1">
          <FormField
            label="WebDAV 地址"
            html-for="webdav-url"
            hint="例如坚果云：https://dav.jianguoyun.com/dav/"
          >
            <Input
              id="webdav-url"
              v-model="backupForm.webdav_url"
              class="bg-background/50 font-mono"
              placeholder="https://dav.jianguoyun.com/dav/"
            />
          </FormField>

          <FormField label="用户名" html-for="webdav-user">
            <Input
              id="webdav-user"
              v-model="backupForm.webdav_username"
              autocomplete="username"
              class="bg-background/50"
              placeholder="WebDAV 用户名 / 邮箱"
            />
          </FormField>

          <FormField label="密码" html-for="webdav-pass" hint="坚果云需要使用应用密码，不是登录密码。">
            <Input
              id="webdav-pass"
              v-model="backupForm.webdav_password"
              type="password"
              autocomplete="current-password"
              class="bg-background/50"
              placeholder="WebDAV 密码 / 应用密码"
            />
          </FormField>

          <FormField label="备份路径" html-for="webdav-path" hint="远程存储路径，默认为 /config-flow-backup/">
            <Input
              id="webdav-path"
              v-model="backupForm.webdav_path"
              class="bg-background/50 font-mono"
              placeholder="/config-flow-backup/"
            />
          </FormField>

          <FormField hint="开启后每次配置变更时自动备份。">
            <div class="flex items-center gap-2.5">
              <Switch id="auto-backup" v-model="backupForm.auto_backup" />
              <Label for="auto-backup" class="text-[13px] text-muted-foreground">自动备份</Label>
            </div>
          </FormField>
        </div>

        <DialogFooter class="sm:justify-between">
          <div class="flex items-center gap-2">
            <Button
              variant="outline"
              class="border-border/60 bg-background/40"
              :disabled="testingConnection"
              @click="testWebDAVConnection"
            >
              <Loader2 v-if="testingConnection" class="size-4 animate-spin" />
              测试连接
            </Button>
            <Button variant="outline" class="border-border/60 bg-background/40" :disabled="backingUp" @click="backupNow">
              <Loader2 v-if="backingUp" class="size-4 animate-spin" />
              立即备份
            </Button>
          </div>
          <div class="flex items-center gap-2">
            <Button variant="outline" @click="backupDialogVisible = false">取消</Button>
            <Button :disabled="savingBackup" @click="saveBackupConfig">
              <Loader2 v-if="savingBackup" class="size-4 animate-spin" />
              保存配置
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>

<script setup lang="ts">
import PageHeader from '@/components/common/PageHeader.vue'
import ScopeBanner from '@/components/shell/ScopeBanner.vue'
import { useProfileStore } from '@/stores/profile'
import { ref, onMounted, onUnmounted, computed, watch, nextTick } from 'vue'
import { Motion } from 'motion-v'
import {
  Archive,
  ChevronDown,
  ChevronUp,
  CloudUpload,
  Copy,
  Download,
  Eye,
  FileCode2,
  GripVertical,
  Loader2,
  Network,
  Pencil,
  Plus,
  RefreshCw,
  RotateCcw,
  Settings,
  Shield,
  ShieldCheck,
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
import SectionCard from '@/components/common/SectionCard.vue'
import { confirm, confirmDanger, notify } from '@/lib/feedback'
import { listItem } from '@/lib/motion'
import { generateApi, configApi, customConfigApi, subscriptionApi, nodeApi, ruleApi, ruleSetApi, proxyGroupApi, agentApi, serverDomainApi, configTokenApi, subStoreUrlApi } from '@/api'
import YamlEditor from '@/components/YamlEditor.vue'
import api from '@/api'
import type { RuleSet } from '@/types'
import { activeProfileId } from '@/profileContext'
import * as yaml from 'js-yaml'
import Sortable from 'sortablejs'

// DNS 条目接口定义

const cfProfileStore = useProfileStore()
const cfProfileName = computed(
  () => cfProfileStore.activeProfile.value?.name || cfProfileStore.activeProfileId.value
)
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
            entry.yaml_error = 'YAML 格式错误：数组项必须包含 addr 字段'
            return
          }
        }
      } else {
        // 如果是对象，必须有 addr 字段
        if (!parsed.addr) {
          entry.yaml_error = 'YAML 格式错误：必须包含 addr 字段'
          return
        }
      }
    } else {
      entry.yaml_error = 'YAML 格式错误：必须是对象或数组'
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
const mosdnsLoading = ref(false)

const mihomoPreviewLoading = ref(false)
const surgePreviewLoading = ref(false)
const mosdnsPreviewLoading = ref(false)

const customConfigDialogVisible = ref(false)
const customConfigContent = ref('')
const savingCustomConfig = ref(false)
const currentConfigType = ref<'mihomo' | 'surge' | 'mosdns'>('mihomo')

const previewDialogVisible = ref(false)
const previewContent = ref('')
const currentPreviewType = ref<'mihomo' | 'surge' | 'mosdns'>('mihomo')

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

// 备份配置
const backupDialogVisible = ref(false)
const backupForm = ref({
  webdav_url: '',
  webdav_username: '',
  webdav_password: '',
  webdav_path: '/config-flow-backup/',
  auto_backup: false
})
const testingConnection = ref(false)
const backingUp = ref(false)
const savingBackup = ref(false)

// 服务域名配置
const serverDomain = ref(localStorage.getItem('serverDomain') || window.location.origin)

// 配置令牌
const configToken = ref('')

// Sub-Store URL
const subStoreUrl = ref('')

// 订阅聚合开关
const subscriptionAggregationEnabled = ref(false)

// Surge Smart 模式相关
const surgeSmartDialogVisible = ref(false)
const surgeSmartGroups = ref<Array<{group_id: string, policy_priority: string}>>([])
const proxyGroupOptions = ref<Array<{id: string, name: string}>>([])
const savingSurgeSmartGroups = ref(false)

const showSurgeSmartDialog = async () => {
  try {
    // 并行加载策略组列表和当前 smart_groups 配置
    const [groupsRes, surgeRes] = await Promise.all([
      proxyGroupApi.getAll(),
      customConfigApi.getSurge()
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
      .filter((g: any) => !hasStrategyRef(g))
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
    await customConfigApi.saveSurge({ smart_groups: validGroups })
    notify.success('Smart 配置已保存')
    surgeSmartDialogVisible.value = false
  } catch (error) {
    notify.error('保存失败')
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

const handleBackup = () => {
  showBackupDialog()
}

// 重置服务域名为当前浏览器地址
const resetServerDomain = async () => {
  const ok = await confirm('是否将服务域名重置为当前浏览器地址？', { title: '重置服务域名' })
  if (!ok) return

  try {
    const newDomain = window.location.origin

    await serverDomainApi.update({
      new_domain: newDomain
    })

    serverDomain.value = newDomain
    localStorage.setItem('serverDomain', newDomain)

    notify.success('服务域名已重置为当前地址')
  } catch (error) {
    console.error('重置服务域名失败:', error)
    notify.error('重置失败')
  }
}

// 输入框失去焦点时保存
const onServerDomainBlur = async () => {
  if (!serverDomain.value) {
    return
  }

  // 保存到 localStorage
  localStorage.setItem('serverDomain', serverDomain.value)

  try {
    await serverDomainApi.update({
      new_domain: serverDomain.value
    })

    notify.success(`服务域名已更新为：${serverDomain.value}`)
  } catch (error: any) {
    console.error('更新服务域名失败:', error)
    notify.error('更新失败')
  }
}

// 监听服务域名变化并保存到 localStorage
const saveServerDomain = () => {
  localStorage.setItem('serverDomain', serverDomain.value)
}

// 订阅聚合开关变化处理
const onSubscriptionAggregationChange = async (value: boolean) => {
  try {
    // 保存到 localStorage
    localStorage.setItem('subscriptionAggregationEnabled', value.toString())

    // 保存到后端
    await api.post('/settings/subscription-aggregation', {
      enabled: value
    })

    notify.success(value ? '订阅聚合已开启' : '订阅聚合已关闭')

    // 触发自定义事件，通知其他组件更新
    window.dispatchEvent(new CustomEvent('subscription-aggregation-changed', {
      detail: { enabled: value }
    }))
  } catch (error: any) {
    console.error('更新订阅聚合开关失败:', error)
    notify.error('更新失败')
    // 失败时恢复原值
    subscriptionAggregationEnabled.value = !value
  }
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

const generateToken = async () => {
  try {
    const response = await configTokenApi.update({ generate: true })
    configToken.value = response.data.config_token
    notify.success('令牌已生成并保存')
  } catch (error: any) {
    console.error('生成令牌失败:', error)
    notify.error('生成令牌失败')
  }
}

const saveToken = async () => {
  try {
    if (!configToken.value || configToken.value.trim() === '') {
      notify.warning('令牌不能为空，如需清空请点击清除按钮')
      return
    }
    await configTokenApi.update({ token: configToken.value })
    notify.success('令牌已保存')
  } catch (error: any) {
    console.error('保存令牌失败:', error)
    notify.error('保存令牌失败')
  }
}

const onTokenBlur = async () => {
  // 如果输入框为空，不保存
  if (!configToken.value || configToken.value.trim() === '') {
    return
  }

  try {
    await configTokenApi.update({ token: configToken.value })
    notify.success('令牌已保存')
  } catch (error: any) {
    console.error('保存令牌失败:', error)
    notify.error('保存令牌失败')
  }
}

const onClearToken = async () => {
  const ok = await confirm('确定要清除配置令牌吗？清除后配置 URL 将不再需要令牌验证。', {
    title: '清除令牌',
    confirmText: '清除'
  })
  if (!ok) return

  try {
    await configTokenApi.delete()
    configToken.value = ''
    notify.success('令牌已清除')
  } catch (error) {
    console.error('清除令牌失败:', error)
    notify.error('清除令牌失败')
  }
}

// Sub-Store URL 相关函数
const onSubStoreUrlBlur = async () => {
  try {
    await subStoreUrlApi.update({
      sub_store_url: subStoreUrl.value
    })

    notify.success('Sub-Store URL 已保存')
  } catch (error: any) {
    console.error('保存 Sub-Store URL 失败:', error)
    notify.error('保存失败')
  }
}

const loadSubStoreUrl = async () => {
  try {
    const response = await subStoreUrlApi.get()
    subStoreUrl.value = response.data.sub_store_url || ''
  } catch (error: any) {
    console.error('加载 Sub-Store URL 失败:', error)
  }
}

// 计算配置 URL - 使用 serverDomain 代替固定的 window.location.origin
const baseUrl = computed(() => {
  return serverDomain.value
})

const mihomoUrl = computed(() => {
  const url = `${baseUrl.value}/api/config/${encodeURIComponent(activeProfileId.value)}/mihomo`
  return configToken.value ? `${url}?token=${configToken.value}` : url
})
const surgeUrl = computed(() => {
  const url = `${baseUrl.value}/api/config/${encodeURIComponent(activeProfileId.value)}/surge`
  return configToken.value ? `${url}?token=${configToken.value}` : url
})
const mosdnsUrl = computed(() => {
  const url = `${baseUrl.value}/api/config/${encodeURIComponent(activeProfileId.value)}/mosdns`
  return configToken.value ? `${url}?token=${configToken.value}` : url
})

// URL显示
const mihomoUrlDisplay = computed(() => mihomoUrl.value)
const surgeUrlDisplay = computed(() => surgeUrl.value)
const mosdnsUrlDisplay = computed(() => mosdnsUrl.value)

/* ---------- 新 UI 的派生数据 ---------- */

/** 三个生成目标的展示与操作，避免在模板里重复三段几乎相同的卡片 */
const targets = computed(() => [
  {
    key: 'mihomo',
    title: 'Mihomo',
    desc: '生成 Mihomo / Clash Meta 的 YAML 配置',
    icon: FileCode2,
    url: mihomoUrl.value,
    urlDisplay: mihomoUrlDisplay.value,
    actions: [
      { label: '基础', icon: Pencil, run: () => showCustomConfigDialog('mihomo') },
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
      { label: '基础', icon: Pencil, run: handleSurgeCustomConfig },
      { label: 'Smart', icon: Settings, run: showSurgeSmartDialog },
      { label: '预览', icon: Eye, loading: surgePreviewLoading.value, run: handleSurgePreview },
      { label: '下载', icon: Download, primary: true, loading: surgeLoading.value, run: generateSurge }
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
    emptyText: '暂无 DNS 条目，点击上方按钮添加',
    addrPlaceholder: 'https://dns.alidns.com/dns-query 或 223.5.5.5',
    hint: '直连规则使用的 DNS 服务器'
  },
  {
    kind: 'remote' as const,
    label: '国外 DNS',
    entries: mosdnsRemoteDnsEntries.value,
    listRef: (el: unknown) => (remoteDnsListRef.value = el as HTMLElement | null),
    emptyText: '暂无 DNS 条目，点击上方按钮添加',
    addrPlaceholder: 'https://1.1.1.1/dns-query 或 1.1.1.1',
    hint: '代理规则使用的主 DNS 服务器'
  },
  {
    kind: 'fallback' as const,
    label: 'Fallback DNS',
    entries: mosdnsFallbackDnsEntries.value,
    listRef: (el: unknown) => (fallbackDnsListRef.value = el as HTMLElement | null),
    emptyText: '暂无 DNS 条目，点击上方按钮添加（留空则使用国内 DNS）',
    addrPlaceholder: 'https://dns.alidns.com/dns-query 或 223.5.5.5',
    hint: '当国外 DNS 超时时使用的备用 DNS 服务器，留空则复用国内 DNS 配置'
  }
])

const DEFAULT_FORWARD_OPTIONS = [
  {
    value: 'forward_remote',
    label: '国外 DNS（推荐）',
    desc: '使用国外 DNS 服务器，避免 DNS 污染'
  },
  { value: 'forward_local', label: '国内 DNS', desc: '使用国内 DNS 服务器，解析速度更快' }
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
      notify.success(`${configType} 配置 URL 已复制到剪贴板`)
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
    notify.success(`${configType} 配置 URL 已复制到剪贴板`)
  } catch (err) {
    notify.error('复制失败，请手动复制')
  }
  document.body.removeChild(textarea)
}

const generateMihomo = async () => {
  try {
    mihomoLoading.value = true
    const response = await generateApi.mihomo()

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
    notify.error('生成失败')
  } finally {
    mihomoLoading.value = false
  }
}

const generateSurge = async () => {
  try {
    surgeLoading.value = true
    const response = await generateApi.surge()

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
    notify.error('生成失败')
  } finally {
    surgeLoading.value = false
  }
}

const generateMosdns = async () => {
  try {
    mosdnsLoading.value = true
    const response = await generateApi.mosdns()

    // 创建下载链接
    const blob = new Blob([response.data], { type: 'application/zip' })
    const url = window.URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = 'mosdns-config.zip'
    link.click()
    window.URL.revokeObjectURL(url)

    notify.success('MosDNS 配置已生成')
  } catch (error) {
    notify.error('生成失败')
  } finally {
    mosdnsLoading.value = false
  }
}

const exportConfig = async () => {
  try {
    const response = await configApi.export()

    const blob = new Blob([response.data], { type: 'application/json' })
    const url = window.URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = 'config.json'
    link.click()
    window.URL.revokeObjectURL(url)

    notify.success('配置已导出')
  } catch (error) {
    notify.error('导出失败')
  }
}

const exportConfigDesensitized = async () => {
  try {
    const response = await configApi.exportDesensitized()

    const blob = new Blob([response.data], { type: 'application/json' })
    const url = window.URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = 'config_desensitized.json'
    link.click()
    window.URL.revokeObjectURL(url)

    notify.success('脱敏配置已导出')
  } catch (error) {
    notify.error('导出失败')
  }
}

const importInput = ref<HTMLInputElement | null>(null)

const pickImportFile = () => importInput.value?.click()

const onImportFileChange = async (event: Event) => {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return

  try {
    const config = JSON.parse(await file.text())
    await configApi.import(config)
    notify.success('配置导入成功，请刷新页面')
  } catch (error) {
    console.error('导入配置失败:', error)
    notify.error('导入失败，请检查文件格式')
  } finally {
    input.value = ''
  }
}

const resetConfig = async () => {
  const ok = await confirmDanger(
    '重置配置将清空所有订阅、节点、规则和策略组设置，恢复为默认配置。此操作不可撤销。',
    { title: '重置配置', confirmText: '确认重置' }
  )
  if (!ok) return

  try {
    await api.post('/config/reset')
    notify.success('配置已重置为默认值，请刷新页面')

    // 刷新统计
    setTimeout(() => {
      // 刷新页面以加载新配置
      window.location.reload()
    }, 500)
  } catch (error) {
    if (error !== 'cancel') {
      notify.error('重置失败')
    }
  }
}

const getCustomConfigDialogTitle = () => {
  const titles = {
    mihomo: '自定义 Mihomo 基础配置',
    surge: '自定义 Surge 基础配置',
    mosdns: '自定义 MosDNS 基础配置'
  }
  return titles[currentConfigType.value]
}

const getCustomConfigDialogDesc = () => {
  const descs = {
    mihomo: '在此编辑 Mihomo 的基础配置（如 mixed-port、dns、tun 等），生成配置时会自动合并 proxies、proxy-groups、rules 等配置。使用 YAML 格式。',
    surge: '在此编辑 Surge 的基础配置。使用 INI 风格的配置格式，包含 [General]、[Proxy]、[Proxy Group]、[Rule] 等部分。规则格式：TYPE,VALUE,POLICY（如 DOMAIN-SUFFIX,google.com,Proxy）',
    mosdns: '在此编辑 MosDNS 的基础配置。使用 YAML 格式，主要包含 log、data_providers、plugins、servers 等部分。注意插件初始化顺序。'
  }
  return descs[currentConfigType.value]
}

const getCustomConfigPlaceholder = () => {
  const placeholders = {
    mihomo: '输入自定义 YAML 配置，例如：\nmixed-port: 7890\nallow-lan: true\nmode: rule\nlog-level: info\nexternal-controller: 127.0.0.1:9090\ndns:\n  enable: true\n  listen: 0.0.0.0:53\n  enhanced-mode: fake-ip',
    surge: '输入自定义配置，例如：\n[General]\nloglevel = notify\ninternet-test-url = http://www.gstatic.com/generate_204\nproxy-test-url = http://www.gstatic.com/generate_204\nskip-proxy = 127.0.0.1, 192.168.0.0/16, 10.0.0.0/8\n\n# 代理将自动生成在 [Proxy] 部分\n# 策略组将自动生成在 [Proxy Group] 部分\n# 规则将自动生成在 [Rule] 部分',
    mosdns: '输入自定义 YAML 配置，例如：\nlog:\n  level: info\n  file: ./mosdns.log\n\nservers:\n  - addr: 127.0.0.1:53\n    protocol: udp\n\n# data_providers 和 plugins 将自动生成'
  }
  return placeholders[currentConfigType.value]
}

const getPreviewDialogTitle = () => {
  const titles = {
    mihomo: '预览 Mihomo 配置',
    surge: '预览 Surge 配置',
    mosdns: '预览 MosDNS 配置'
  }
  return titles[currentPreviewType.value]
}

const showCustomConfigDialog = async (type: 'mihomo' | 'surge' | 'mosdns') => {
  currentConfigType.value = type
  try {
    const apiMap = {
      mihomo: customConfigApi.getMihomo,
      surge: customConfigApi.getSurge,
      mosdns: customConfigApi.getMosdns
    }
    const response = await apiMap[type]()
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
      mosdns: customConfigApi.saveMosdns
    }
    await apiMap[currentConfigType.value]({ config: customConfigContent.value })
    notify.success('自定义配置已保存')
    customConfigDialogVisible.value = false
  } catch (error) {
    notify.error('保存失败')
  } finally {
    savingCustomConfig.value = false
  }
}

const previewConfig = async (type: 'mihomo' | 'surge' | 'mosdns') => {
  currentPreviewType.value = type
  const loadingMap = {
    mihomo: mihomoPreviewLoading,
    surge: surgePreviewLoading,
    mosdns: mosdnsPreviewLoading
  }

  try {
    loadingMap[type].value = true

    const apiMap = {
      mihomo: generateApi.previewMihomo,
      surge: generateApi.previewSurge,
      mosdns: generateApi.previewMosdns
    }
    const response = await apiMap[type]()
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
    notify.error('复制失败')
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
        notify.warning('YAML 解析失败，已清空字段')
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
    const response = await ruleSetApi.getAll()
    availableRuleSets.value = response.data
  } catch (error) {
    console.error('加载规则集列表失败', error)
  }
}

const loadRules = async () => {
  try {
    const response = await ruleApi.getAll()
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
    const customConfigResponse = await customConfigApi.getMosdns()
    mosdnsCustomConfig.value = customConfigResponse.data.config || ''

    // 加载规则集配置
    const rulesetResponse = await api.get('/mosdns/rulesets')
    mosdnsDirectRulesets.value = rulesetResponse.data.direct_rulesets || []
    mosdnsProxyRulesets.value = rulesetResponse.data.proxy_rulesets || []
    mosdnsDirectRules.value = rulesetResponse.data.direct_rules || []
    mosdnsProxyRules.value = rulesetResponse.data.proxy_rules || []

    // 加载自定义 match 配置
    const customMatchResponse = await api.get('/mosdns/custom-matches')
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
    const dnsResponse = await api.get('/mosdns/dns-servers')
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
    const logResponse = await api.get('/mosdns/log-settings')
    mosdnsLogEnabled.value = logResponse.data.log_enabled !== undefined ? logResponse.data.log_enabled : true
    mosdnsLogLevel.value = logResponse.data.log_level || 'info'
    mosdnsLogFile.value = logResponse.data.log_file || './mosdns.log'

    // 加载 API 配置
    const apiResponse = await api.get('/mosdns/api-settings')
    mosdnsApiEnabled.value = apiResponse.data.api_enabled !== undefined ? apiResponse.data.api_enabled : true
    mosdnsApiAddress.value = apiResponse.data.api_address || '0.0.0.0:8338'

    // 加载缓存配置
    const cacheResponse = await api.get('/mosdns/cache-settings')
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
    notify.error('加载设置失败')
  }
}

const saveMosdnsSettings = async () => {
  try {
    savingMosdnsSettings.value = true

    // 保存自定义配置
    await customConfigApi.saveMosdns({ config: mosdnsCustomConfig.value })

    // 保存规则集和规则配置
    await api.post('/mosdns/rulesets', {
      direct_rulesets: mosdnsDirectRulesets.value,
      proxy_rulesets: mosdnsProxyRulesets.value,
      direct_rules: mosdnsDirectRules.value,
      proxy_rules: mosdnsProxyRules.value
    })

    const customMatchPayload = mosdnsCustomMatches.value.map(item => ({
      id: item.id,
      enabled: item.enabled,
      exec: item.exec,
      matches: item.matchesText
    }))

    await api.post('/mosdns/custom-matches', {
      custom_matches: customMatchPayload,
      position: mosdnsCustomMatchPosition.value
    })

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
    })

    // 同步更新文本字段（保持兼容性）
    mosdnsLocalDns.value = localDnsText
    mosdnsRemoteDns.value = remoteDnsText
    mosdnsFallbackDns.value = fallbackDnsText

    // 保存日志配置
    await api.post('/mosdns/log-settings', {
      log_enabled: mosdnsLogEnabled.value,
      log_level: mosdnsLogLevel.value,
      log_file: mosdnsLogFile.value
    })

    // 保存 API 配置
    await api.post('/mosdns/api-settings', {
      api_enabled: mosdnsApiEnabled.value,
      api_address: mosdnsApiAddress.value
    })

    // 保存缓存配置
    await api.post('/mosdns/cache-settings', {
      cache_enabled: mosdnsCacheEnabled.value,
      cache_size: mosdnsCacheSize.value,
      cache_lazy_ttl: mosdnsCacheLazyTtl.value,
      cache_dump_enabled: mosdnsCacheDumpEnabled.value,
      cache_dump_file: mosdnsCacheDumpFile.value,
      cache_dump_interval: mosdnsCacheDumpInterval.value
    })

    notify.success('MosDNS 设置已保存')
    mosdnsSettingsDialogVisible.value = false
  } catch (error) {
    console.error('保存 MosDNS 设置失败', error)
    notify.error('保存失败')
  } finally {
    savingMosdnsSettings.value = false
  }
}

// 备份相关方法
const showBackupDialog = async () => {
  try {
    // 加载备份配置
    const response = await api.get('/backup/config')
    if (response.data) {
      backupForm.value = {
        webdav_url: response.data.webdav_url || '',
        webdav_username: response.data.webdav_username || '',
        webdav_password: response.data.webdav_password || '',
        webdav_path: response.data.webdav_path || '/config-flow-backup/',
        auto_backup: response.data.auto_backup || false
      }
    }
    backupDialogVisible.value = true
  } catch (error) {
    console.error('加载备份配置失败', error)
    backupDialogVisible.value = true
  }
}

const testWebDAVConnection = async () => {
  if (!backupForm.value.webdav_url) {
    notify.warning('请输入 WebDAV 地址')
    return
  }
  if (!backupForm.value.webdav_username) {
    notify.warning('请输入用户名')
    return
  }
  if (!backupForm.value.webdav_password) {
    notify.warning('请输入密码')
    return
  }

  try {
    testingConnection.value = true
    await api.post('/backup/test', {
      webdav_url: backupForm.value.webdav_url,
      webdav_username: backupForm.value.webdav_username,
      webdav_password: backupForm.value.webdav_password,
      webdav_path: backupForm.value.webdav_path
    })
    notify.success('连接测试成功')
  } catch (error: any) {
    console.error('测试连接失败', error)
    const errorMsg = error.response?.data?.message || '连接测试失败，请检查配置'
    notify.error(errorMsg)
  } finally {
    testingConnection.value = false
  }
}

const backupNow = async () => {
  if (!backupForm.value.webdav_url) {
    notify.warning('请输入 WebDAV 地址')
    return
  }
  if (!backupForm.value.webdav_username) {
    notify.warning('请输入用户名')
    return
  }
  if (!backupForm.value.webdav_password) {
    notify.warning('请输入密码')
    return
  }

  try {
    backingUp.value = true
    await api.post('/backup/now', {
      webdav_url: backupForm.value.webdav_url,
      webdav_username: backupForm.value.webdav_username,
      webdav_password: backupForm.value.webdav_password,
      webdav_path: backupForm.value.webdav_path
    })
    notify.success('备份成功')
  } catch (error: any) {
    console.error('备份失败', error)
    const errorMsg = error.response?.data?.message || '备份失败，请检查配置'
    notify.error(errorMsg)
  } finally {
    backingUp.value = false
  }
}

const saveBackupConfig = async () => {
  try {
    savingBackup.value = true
    await api.post('/backup/config', backupForm.value)
    notify.success('备份配置已保存')
    backupDialogVisible.value = false
  } catch (error: any) {
    console.error('保存备份配置失败', error)
    notify.error('保存失败')
  } finally {
    savingBackup.value = false
  }
}

// 监听服务域名变化并自动保存
watch(serverDomain, (newValue) => {
  if (newValue) {
    localStorage.setItem('serverDomain', newValue)
  }
})

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
      // 后端没有配置，使用 localStorage 或当前地址
      const localDomain = localStorage.getItem('serverDomain') || window.location.origin
      serverDomain.value = localDomain
    }
  } catch (error) {
    console.error('加载服务域名失败:', error)
    // 加载失败，使用 localStorage 或当前地址
    const fallbackDomain = localStorage.getItem('serverDomain') || window.location.origin
    serverDomain.value = fallbackDomain
  }

  // 加载配置令牌
  await loadConfigToken()

  // 加载 Sub-Store URL
  await loadSubStoreUrl()

  // 从后端加载订阅聚合开关配置
  try {
    const response = await api.get('/settings/subscription-aggregation')
    const enabled = response.data.enabled || false
    subscriptionAggregationEnabled.value = enabled
    localStorage.setItem('subscriptionAggregationEnabled', enabled.toString())
  } catch (error) {
    console.error('加载订阅聚合开关失败:', error)
    // 加载失败，使用 localStorage
    const localEnabled = localStorage.getItem('subscriptionAggregationEnabled') === 'true'
    subscriptionAggregationEnabled.value = localEnabled
  }

})

onUnmounted(() => {
})
</script>

