<template>
  <div class="min-w-0 [overflow-wrap:anywhere]">
    <PageHeader v-if="!agentSettings" title="配置生成" />

    <!-- ===== 生成工作台：目标切换 + 编译步骤 + 代码面板 ===== -->
    <GenerateStudio v-if="!agentSettings" :targets="studioTargets" :profile-id="profileId" :loon-file-name="`${loonConfigName}.lcf`" @copy="copyUrl" />

    <LinuxGatewaySettings v-model:open="linuxGatewayDialogVisible" :profile-id="profileId" />

    <!-- ===== 自定义基础配置 ===== -->
    <SettingsWorkspace
      v-model:open="customConfigDialogVisible"
      :title="getCustomConfigDialogTitle()"
      :description="customConfigDescription"
      :back-label="settingsBackLabel"
      :dirty="customConfigDirty"
      :busy="savingCustomConfig || ipv6NetworkBusy"
      :save-disabled="currentConfigType === 'mihomo' && ipv6Validation.invalid"
      size="wide"
      save-label="保存草稿"
      @save="saveCustomConfig()"
    >
      <div class="flex min-h-0 min-w-0 flex-1 flex-col overflow-y-auto">
      <MihomoSyncInterval v-if="currentConfigType === 'mihomo'" v-model="mihomoSyncInterval" :inert="savingCustomConfig || ipv6NetworkBusy" />
      <MihomoIPv6Settings v-if="currentConfigType === 'mihomo'" v-model="customConfigContent" :open="customConfigDialogVisible"
        :profile-id="profileId" :preferred-agent-id="agentSettings?.agentId" :default-config="mihomoDefaultConfig"
        :dirty="customConfigDirty" :disabled="savingCustomConfig" @busy="ipv6NetworkBusy = $event" @validation="ipv6Validation = $event"
        :refresh-key="ipv6Published" />
      <div :inert="savingCustomConfig || ipv6NetworkBusy" class="flex min-h-80 min-w-0 flex-1 flex-col gap-3 p-4 md:p-5">
        <p v-if="settingsContextLabel" class="m-0 shrink-0 break-words text-xs text-muted-foreground">{{ settingsContextLabel }}</p>
        <YamlEditor
          v-model="customConfigContent"
          class="h-full min-h-0"
          :placeholder="getCustomConfigPlaceholder()"
        />
      </div>
      </div>
      <template v-if="currentConfigType !== 'loon'" #footer="{ disabled }">
        <SettingsPublishBar
          :open="customConfigDialogVisible"
          :profile-id="profileId"
          :preferred-agent-id="agentSettings?.agentId"
          :initial-targets="agentSettings?.targets"
          :service-type="currentConfigType"
          :disabled="disabled"
          :save="() => saveCustomConfig(false)"
          @save-draft="saveCustomConfig()"
          @published="ipv6Published++"
        />
      </template>
    </SettingsWorkspace>

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

    <OxidnsSettings
      v-model:open="oxidnsSettingsDialogVisible"
      :profile-id="profileId"
      :initial-tab="oxidnsInitialTab"
      :preferred-agent-id="agentSettings?.agentId"
      :initial-targets="agentSettings?.targets"
      :context-label="settingsContextLabel"
      :back-label="settingsBackLabel"
    />

    <!-- ===== MosDNS 设置 ===== -->
    <SettingsWorkspace
      v-model:open="mosdnsSettingsDialogVisible"
      title="MosDNS 设置"
      description="设置当前配置空间的域名解析规则、DNS 服务器、缓存和日志，用于生成 MosDNS 配置。"
      :back-label="settingsBackLabel"
      :dirty="mosdnsSettingsDirty"
      :busy="savingMosdnsSettings"
      size="form"
      @save="saveMosdnsSettings()"
    >
        <p v-if="settingsContextLabel" class="m-0 shrink-0 break-words px-4 pt-3 text-xs text-muted-foreground md:px-5">{{ settingsContextLabel }}</p>
        <Tabs v-model="mosdnsActiveTab" :inert="savingMosdnsSettings" class="min-h-0 min-w-0 flex-1 gap-0 overflow-hidden">
          <TabsList variant="strip" class="h-auto min-h-11 w-full shrink-0 justify-start gap-1 overflow-x-auto rounded-none border-b border-border bg-card px-4 py-2 md:px-5">
            <TabsTrigger value="rules" class="min-h-9 shrink-0 text-xs max-md:min-h-11">规则配置</TabsTrigger>
            <TabsTrigger value="cache" class="min-h-9 shrink-0 text-xs max-md:min-h-11">缓存</TabsTrigger>
            <TabsTrigger value="dns" class="min-h-9 shrink-0 text-xs max-md:min-h-11">DNS 服务器</TabsTrigger>
            <TabsTrigger value="default" class="min-h-9 shrink-0 text-xs max-md:min-h-11">默认转发</TabsTrigger>
            <TabsTrigger value="hosts" class="min-h-9 shrink-0 text-xs max-md:min-h-11">自定义 Host</TabsTrigger>
            <TabsTrigger value="log" class="min-h-9 shrink-0 text-xs max-md:min-h-11">日志</TabsTrigger>
            <TabsTrigger value="api" class="min-h-9 shrink-0 text-xs max-md:min-h-11">API</TabsTrigger>
          </TabsList>

          <!-- 规则配置 -->
          <TabsContent value="rules" class="min-h-0 overflow-y-auto px-4 py-4 md:px-5">
            <div class="flex flex-col gap-4">
              <InfoNote>
                <p>选择哪些规则和规则集使用国内 DNS，哪些使用国外 DNS。</p>
                <p>只有选中的规则和规则集才会用于生成 MosDNS 配置。</p>
              </InfoNote>
              <InfoNote v-if="oxidnsDnsDifference" data-oxidns-dns-difference>
                <p>{{ oxidnsDnsDifference }}</p>
              </InfoNote>

              <LabeledDivider label="直连规则配置" />

              <FormField label="直连规则集" hint="整组网站或 IP 命中后，使用国内 DNS 查询。" :help="optionHelp('希望为整组网站或 IP 指定国内 DNS 时选择。', '与规则一起按既定顺序参与分流，命中后使用国内上游。同一规则集不能同时归入代理分组；需要国外 DNS 的项目应选到另一组。')">
                <MultiSelect
                  v-model="mosdnsDirectRulesets"
                  :options="directRulesetOptions"
                  placeholder="选择使用国内 DNS 的规则集"
                />
              </FormField>

              <FormField label="直连规则" hint="单条网站或 IP 规则命中后，使用国内 DNS。" :help="optionHelp('只需为个别网站或 IP 指定国内 DNS，而非整组规则。', '所选规则命中后使用国内上游，与规则集按既定顺序共同参与分流。同一规则不能同时归入代理分组，选择前先确定希望使用的查询方式。')">
                <MultiSelect
                  v-model="mosdnsDirectRules"
                  :options="directRuleOptions"
                  placeholder="选择使用国内 DNS 的规则"
                />
              </FormField>

              <LabeledDivider label="代理规则配置" />

              <FormField label="代理规则集" hint="整组网站或 IP 命中后，使用国外 DNS 查询。" :help="optionHelp('希望为整组网站或 IP 指定国外 DNS 时选择。', '所选规则集命中后使用国外上游，并按当前配置的备用机制处理超时。同一规则集不能同时归入直连分组；查询方式不等于自动设置网络代理。')">
                <MultiSelect
                  v-model="mosdnsProxyRulesets"
                  :options="proxyRulesetOptions"
                  placeholder="选择使用国外 DNS 的规则集"
                />
              </FormField>

              <FormField label="代理规则" hint="单条网站或 IP 规则命中后，使用国外 DNS。" :help="optionHelp('希望为个别网站或 IP 指定国外 DNS 时选择。', '命中后使用国外上游，与规则集按既定顺序参与分流。同一规则不能同时归入直连分组；仅选择 DNS 不会替设备启用网络代理。')">
                <MultiSelect
                  v-model="mosdnsProxyRules"
                  :options="proxyRuleOptions"
                  placeholder="选择使用国外 DNS 的规则"
                />
              </FormField>

              <LabeledDivider label="自定义 Match" />

              <FormField label="插入位置" hint="选择自定义匹配先执行，或让自动规则优先。" :help="optionHelp('已经添加自定义匹配，需要决定它和自动规则的先后。', '放在自动规则之前，可先尝试自己的条件和动作；放在之后，则先让自动规则处理。顺序会影响命中结果，应按希望的优先级选择。')">
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

                  <FormField label="匹配条件" hint="每行填写一个条件，空行会被自动忽略。" :help="optionHelp('需要按域名、类型等条件自定义 MosDNS 处理流程。', '使用 MosDNS sequence 的 matches 格式，每行一个表达式，空行自动忽略。应按核心支持的语法填写，条件决定何时执行对应动作。')">
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
          <TabsContent value="cache" class="min-h-0 overflow-y-auto px-4 py-4 md:px-5">
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
                <FormField label="缓存条目数（size）" html-for="cache-size" hint="保存更多查询结果，同时占用更多设备内存。" :help="optionHelp('已开启 MosDNS 缓存，希望减少重复向上游查询。', '设置最多保存的查询条目数，建议从 10240 起。数量不是字节数，增大能容纳更多域名结果，但也会增加内存占用，应结合设备资源调整。')">
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

                <FormField label="过期缓存保留秒数（lazy_cache_ttl）" html-for="cache-ttl" hint="延后删除过期缓存，按秒设置额外保留时间。" :help="optionHelp('MosDNS 缓存已开启，希望在结果过期后继续保留一段时间。', '这是缓存过期后的延迟删除时长，单位为秒。与正常有效期分开设置，不等于让上游返回的 DNS 记录在客户端永久有效。')">
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
                    label="缓存文件（dump_file）"
                    html-for="cache-dump-file"
                    hint="填写缓存保存位置，相对路径从配置目录开始。"
                    :help="optionHelp('已开启将缓存保存到文件，需要指定快照位置时填写。', '文件位于 MosDNS 运行设备，使用相对路径时，以配置文件所在目录为起点。填写可写入的位置，如 ./cache.dump，供重启后恢复缓存。')"
                  >
                    <Input
                      id="cache-dump-file"
                      v-model="mosdnsCacheDumpFile"
                      class="bg-background/50 font-mono"
                      placeholder="./cache.dump"
                    />
                  </FormField>

                  <FormField label="保存间隔秒数（dump_interval）" html-for="cache-dump-interval" hint="控制缓存保存频率，越频繁则磁盘写入越多。" :help="optionHelp('缓存已开启，且选择了将缓存保存到文件时生效。', '按此秒数定期保存缓存快照。缩短可减少停止前尚未保存的缓存，但会增加磁盘写入；不改变 DNS 查询结果本身的有效期。')">
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
          <TabsContent value="dns" class="min-h-0 overflow-y-auto px-4 py-4 md:px-5">
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
                        label="引导 DNS（Bootstrap）"
                        hint="帮助找到上游服务器，普通 IP 地址无需填写。"
                        :help="optionHelp('上游使用域名或加密 DNS 地址，且此输入框可用时填写。', '先通过此 DNS 找到上游地址，避免解析上游时依赖自身形成循环。普通 IP 地址不使用此项；是否写入配置以当前上游格式为准。')"
                      >
                        <Input
                          v-model="entry.bootstrap"
                          class="bg-background/50 font-mono text-[12px]"
                          placeholder="223.5.5.5（可选）"
                          :disabled="!isDomainAddr(entry.addr)"
                        />
                      </FormField>
                      <FormField label="连接流水线（Pipeline）" hint="上游支持时，复用同一连接发送多个查询。" :help="optionHelp('上游支持 TCP / DoT 的多查询复用，希望减少连接开销。', '允许在一条连接上同时发送多个 DNS 查询。并非所有上游都兼容，使用前应确认支持；UDP 查询不会因此变成 TCP 查询。')">
                      <label class="flex cursor-pointer items-center gap-2 text-[13px] text-muted-foreground">
                        <Checkbox v-model="entry.enable_pipeline" />
                        启用连接复用
                      </label>
                      </FormField>
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
          <TabsContent value="default" class="min-h-0 overflow-y-auto px-4 py-4 md:px-5">
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
          <TabsContent value="hosts" class="min-h-0 overflow-y-auto px-4 py-4 md:px-5">
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
          <TabsContent value="log" class="min-h-0 overflow-y-auto px-4 py-4 md:px-5">
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
          <TabsContent value="api" class="min-h-0 overflow-y-auto px-4 py-4 md:px-5">
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
                :help="optionHelp('已启用 MosDNS API，需指定从哪里访问管理接口。', '填写 IP:端口。127.0.0.1:8338 仅本机访问，0.0.0.0:8338 接收所有 IPv4 网卡的连接；其他设备还需能访问此端口。')"
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
        <template #footer="{ disabled }">
          <SettingsPublishBar
            :open="mosdnsSettingsDialogVisible"
            :profile-id="profileId"
            :preferred-agent-id="agentSettings?.agentId"
            :initial-targets="agentSettings?.targets"
            service-type="mosdns"
            :disabled="disabled"
            :save="() => saveMosdnsSettings(false)"
            @save-draft="saveMosdnsSettings()"
          />
        </template>
    </SettingsWorkspace>

    <!-- ===== Surge Smart 模式 ===== -->
    <SettingsWorkspace
      v-model:open="surgeSmartDialogVisible"
      title="Surge Smart 模式"
      description="选择要使用 Smart 模式的策略组，并填写 policy-priority 设置优先级。仅影响 Surge 配置；Mihomo 中仍保留原类型（如 url-test）。"
      :dirty="surgeSmartDirty"
      :busy="savingSurgeSmartGroups"
      size="compact"
      @save="saveSurgeSmartGroups()"
    >
        <div :inert="savingSurgeSmartGroups" class="flex min-h-0 min-w-0 flex-1 flex-col gap-3 overflow-y-auto p-4 md:p-5">
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
        <template #footer="{ disabled }">
          <SettingsPublishBar
            :open="surgeSmartDialogVisible"
            :profile-id="profileId"
            service-type="surge"
            :disabled="disabled"
            :save="() => saveSurgeSmartGroups(false)"
            @save-draft="saveSurgeSmartGroups()"
          />
        </template>
    </SettingsWorkspace>

  </div>
</template>

<script setup lang="ts">
import PageHeader from '@/components/common/PageHeader.vue'
import OxidnsSettings from '@/components/generate/OxidnsSettings.vue'
import LinuxGatewaySettings from '@/components/generate/LinuxGatewaySettings.vue'
import GenerateStudio from '@/components/generate/GenerateStudio.vue'
import SettingsWorkspace from '@/components/generate/SettingsWorkspace.vue'
import MihomoSyncInterval from '@/components/generate/MihomoSyncInterval.vue'
import MihomoIPv6Settings from '@/components/generate/MihomoIPv6Settings.vue'
import { isRefreshInterval } from '@/lib/subscriptionRefresh'
import SettingsPublishBar from '@/components/generate/SettingsPublishBar.vue'
import { useProfileStore } from '@/stores/profile'
import { ref, onMounted, onBeforeUnmount, computed, watch, nextTick, type Ref } from 'vue'
import {
  ChevronDown,
  ChevronUp,
  Copy,
  Download,
  Eye,
  FileCode2,
  GripVertical,
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
import { copyText } from '@/lib/clipboard'
import { optionHelp } from '@/lib/optionCopy'
import { isDnsRuleType } from '@/lib/oxidns'
import { isBlockRoute, isUpstreamValue } from '@/lib/ruleDns'
import router from '@/router'
import { generateApi, customConfigApi, ruleApi, ruleSetApi, proxyGroupApi, serverDomainApi, configTokenApi, type PublishGuard } from '@/api'
import YamlEditor from '@/components/YamlEditor.vue'
import api from '@/api'
import type { RuleDnsSnapshot, RuleSet } from '@/types'
import { activeProfileId } from '@/profileContext'
import type { AgentSettingsContext } from '@/lib/agentSettings'
import * as yaml from 'js-yaml'
import Sortable from 'sortablejs'

// DNS 条目接口定义

// Agent editors keep the bound space for their entire lifetime, independently
// of the global space switcher. A new entry mounts a new editor session.
const props = defineProps<{ agentSettings?: AgentSettingsContext }>()
const emit = defineEmits<{ ready: []; close: [] }>()
const cfProfileStore = useProfileStore()
const profileId = props.agentSettings?.profileId ?? activeProfileId.value
const linuxGatewayDialogVisible = ref(false)
const profileOptions = { headers: { 'X-ConfigFlow-Profile': profileId } }
const settingsBackLabel = props.agentSettings ? '返回 Agent 列表' : '返回生成工作台'
const settingsContextLabel = computed(() => {
  const context = props.agentSettings
  if (!context) return ''
  const service = { mihomo: 'Mihomo', mosdns: 'MosDNS', oxidns: 'OxiDNS', surge: 'Surge' }[context.serviceType]
  return `设备「${context.agentName}」 · 配置空间「${context.profileName}」。同空间的 ${service} 设备共用此配置；保存草稿不会下发。`
})
let pageActive = true
onBeforeUnmount(() => { pageActive = false })

// A completed save must not publish from an editor session the user has left.
const trackEditorSession = (open: Ref<boolean>, busy: Ref<boolean>) => {
  let session = 0
  watch(open, () => {
    session += 1
    busy.value = false
  }, { flush: 'sync' })
  return {
    capture: () => session,
    isCurrent: (expected: number) => pageActive && open.value && expected === session
  }
}
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
          if (firstItem === null || typeof firstItem !== 'object' || !('addr' in firstItem) || !firstItem.addr) {
            entry.yaml_error = '请为 YAML 列表中的 DNS 条目填写 addr 地址字段'
            return
          }
        }
      } else {
        // 如果是对象，必须有 addr 字段
        if (!('addr' in parsed) || !parsed.addr) {
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
const oxidnsLoading = ref(false)
const oxidnsSettingsDialogVisible = ref(false)
// 「打开完整参数」深链（/generate?oxidns=config）只作用于这一次打开。
const oxidnsInitialTab = ref<'config'>()
watch(oxidnsSettingsDialogVisible, open => { if (!open) oxidnsInitialTab.value = undefined })

const mihomoPreviewLoading = ref(false)
const surgePreviewLoading = ref(false)
const loonPreviewLoading = ref(false)
const mosdnsPreviewLoading = ref(false)

type ConfigType = 'mihomo' | 'surge' | 'loon' | 'mosdns'

const customConfigDialogVisible = ref(false)
const customConfigContent = ref('')
const mihomoDefaultConfig = ref('')
const ipv6NetworkBusy = ref(false)
const ipv6Validation = ref({ invalid: false, dirty: false })
// A confirmed Mihomo publish changes what the IPv6 steps read from the device.
const ipv6Published = ref(0)
const customConfigSavedContent = ref('')
const mihomoSyncInterval = ref<number | string>(3600)
const mihomoSavedSyncInterval = ref(3600)
const customConfigDirty = computed(() => customConfigContent.value !== customConfigSavedContent.value
  || (currentConfigType.value === 'mihomo' && (mihomoSyncInterval.value !== mihomoSavedSyncInterval.value || ipv6Validation.value.dirty)))
const savingCustomConfig = ref(false)
const currentConfigType = ref<ConfigType>('mihomo')
const customConfigSession = trackEditorSession(customConfigDialogVisible, savingCustomConfig)

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
const mosdnsSettingsSession = trackEditorSession(mosdnsSettingsDialogVisible, savingMosdnsSettings)
const mosdnsSavedSnapshot = ref('')
// Compare editable drafts, not tab selection, validation messages or generated row IDs.
const mosdnsDraftSnapshot = computed(() => JSON.stringify({
  customConfig: mosdnsCustomConfig.value,
  directRulesets: mosdnsDirectRulesets.value,
  proxyRulesets: mosdnsProxyRulesets.value,
  directRules: mosdnsDirectRules.value,
  proxyRules: mosdnsProxyRules.value,
  customMatches: mosdnsCustomMatches.value.map(({ enabled, exec, matchesText }) => ({ enabled, exec, matchesText })),
  customMatchPosition: mosdnsCustomMatchPosition.value,
  dns: [mosdnsLocalDnsEntries.value, mosdnsRemoteDnsEntries.value, mosdnsFallbackDnsEntries.value]
    .map(entries => entries.map(entry => entry.mode === 'yaml'
      ? { yaml: entry.yaml_config }
      : { addr: entry.addr, bootstrap: entry.bootstrap, enable_pipeline: entry.enable_pipeline })),
  defaultForward: mosdnsDefaultForward.value,
  customHosts: mosdnsCustomHosts.value,
  logEnabled: mosdnsLogEnabled.value,
  logLevel: mosdnsLogLevel.value,
  logFile: mosdnsLogFile.value,
  apiEnabled: mosdnsApiEnabled.value,
  apiAddress: mosdnsApiAddress.value,
  cacheEnabled: mosdnsCacheEnabled.value,
  cacheSize: mosdnsCacheSize.value,
  cacheLazyTtl: mosdnsCacheLazyTtl.value,
  cacheDumpEnabled: mosdnsCacheDumpEnabled.value,
  cacheDumpFile: mosdnsCacheDumpFile.value,
  cacheDumpInterval: mosdnsCacheDumpInterval.value
}))
const mosdnsSettingsDirty = computed(() => mosdnsDraftSnapshot.value !== mosdnsSavedSnapshot.value)

// 服务域名配置
const serverDomain = ref(localStorage.getItem('serverDomain') || window.location.origin)

// 配置令牌
const configToken = ref('')

// Surge Smart 模式相关
const surgeSmartDialogVisible = ref(false)
const surgeSmartGroups = ref<Array<{group_id: string, policy_priority: string}>>([])
const proxyGroupOptions = ref<Array<{id: string, name: string}>>([])
const savingSurgeSmartGroups = ref(false)
const surgeSmartSession = trackEditorSession(surgeSmartDialogVisible, savingSurgeSmartGroups)
const surgeSmartSavedSnapshot = ref('')
const surgeSmartDraftSnapshot = computed(() => JSON.stringify(surgeSmartGroups.value))
const surgeSmartDirty = computed(() => surgeSmartDraftSnapshot.value !== surgeSmartSavedSnapshot.value)

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
    surgeSmartSavedSnapshot.value = surgeSmartDraftSnapshot.value
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

const saveSurgeSmartGroups = async (close = true): Promise<PublishGuard | null> => {
  if (!pageActive || !surgeSmartDialogVisible.value || savingSurgeSmartGroups.value) return null
  const session = surgeSmartSession.capture()
  const snapshot = surgeSmartDraftSnapshot.value
  const groups: Array<{ group_id: string, policy_priority: string }> = JSON.parse(snapshot)
  try {
    savingSurgeSmartGroups.value = true
    // 过滤掉未选择策略组的空行
    const validGroups = groups.filter(g => g.group_id)
    const response = await customConfigApi.saveSurge({ smart_groups: validGroups }, profileId)
    if (!surgeSmartSession.isCurrent(session)) return null
    surgeSmartSavedSnapshot.value = snapshot
    notify.success('Smart 配置草稿已保存')
    if (close) surgeSmartDialogVisible.value = false
    return response.data.publish_guard ?? null
  } catch (error) {
    if (surgeSmartSession.isCurrent(session)) notify.error('保存 Surge Smart 设置失败')
    return null
  } finally {
    if (surgeSmartSession.isCurrent(session)) savingSurgeSmartGroups.value = false
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
const oxidnsUrl = computed(() => {
  const url = `${baseUrl.value}/api/config/${encodeURIComponent(profileId)}/oxidns`
  return configToken.value ? `${url}?token=${encodeURIComponent(configToken.value)}` : url
})
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
      { label: '基础配置', group: 'settings' as const, icon: Pencil, run: () => showCustomConfigDialog('mihomo') },
      { label: 'Linux 网关', group: 'settings' as const, icon: Network, run: () => { linuxGatewayDialogVisible.value = true } },
      { label: '预览', icon: Eye, loading: mihomoPreviewLoading.value, run: () => previewConfig('mihomo') },
      { label: '下载', group: 'export' as const, icon: Download, primary: true, loading: mihomoLoading.value, run: generateMihomo }
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
      { label: '基础配置', group: 'settings' as const, icon: Pencil, run: handleSurgeCustomConfig },
      { label: 'Smart', group: 'settings' as const, icon: Settings, run: showSurgeSmartDialog },
      { label: '预览', icon: Eye, loading: surgePreviewLoading.value, run: handleSurgePreview },
      { label: '下载', group: 'export' as const, icon: Download, primary: true, loading: surgeLoading.value, run: generateSurge }
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
      { label: '基础配置', group: 'settings' as const, icon: Pencil, run: () => showCustomConfigDialog('loon') },
      { label: '预览', icon: Eye, loading: loonPreviewLoading.value, run: () => previewConfig('loon') },
      { label: '导入 Loon', group: 'export' as const, icon: Upload, run: importToLoon },
      { label: '下载', group: 'export' as const, icon: Download, primary: true, loading: loonLoading.value, run: generateLoon }
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
      { label: '设置', group: 'settings' as const, icon: Settings, run: showMosdnsSettingsDialog },
      { label: '预览', icon: Eye, loading: mosdnsPreviewLoading.value, run: () => previewConfig('mosdns') },
      { label: '下载', group: 'export' as const, icon: Download, primary: true, loading: mosdnsLoading.value, run: generateMosdns }
    ]
  },
  {
    key: 'oxidns',
    title: 'OxiDNS',
    desc: '生成 OxiDNS 配置与完整规则包',
    icon: Network,
    url: oxidnsUrl.value,
    urlDisplay: oxidnsUrl.value,
    actions: [
      { label: '设置', group: 'settings' as const, icon: Settings, run: () => { oxidnsSettingsDialogVisible.value = true } },
      { label: '下载', group: 'export' as const, icon: Download, primary: true, loading: oxidnsLoading.value, run: generateOxidns }
    ]
  }
])

/* 工作台里的代码面板就是预览，原「预览」按钮不再单列 */
const studioTargets = computed(() =>
  targets.value.map(t => ({
    ...t,
    key: t.key as 'mihomo' | 'surge' | 'loon' | 'mosdns' | 'oxidns',
    actions: t.actions.filter(action => action.label !== '预览')
  }))
)

/* 直连与代理互斥：把已被对面选走的项从本列表里剔除（已选中的自己保留），
 * 取代原先「显示为禁用项」的做法，选择面板里不再出现点不动的行。 */
const rulesetOptionsExcept = (taken: string[], mine: string[]) =>
  availableRuleSets.value
    .filter(rs => !taken.includes(rs.id) || mine.includes(rs.id))
    .map(rs => ({ value: rs.id, label: rs.name }))

/* 目前只能把域名和 IP-CIDR 类规则转换为 MosDNS 规则；已选中的其他类型保留并标注，便于移除 */
const ruleOptionsExcept = (taken: string[], mine: string[]) =>
  availableRules.value
    .filter(rule => (!taken.includes(rule.id) || mine.includes(rule.id))
      && (isDnsRuleType(rule.rule_type) || mine.includes(rule.id)))
    .map(rule => ({
      value: rule.id,
      label: `${rule.rule_type}: ${rule.value} → ${rule.policy}`
        + (isDnsRuleType(rule.rule_type) ? '' : '（暂不能转换为 MosDNS 规则，不会生效）')
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

const copyUrl = async (url: string, configType: string) => {
  try {
    await copyText(url)
    notify.success(`${configType} 订阅链接已复制，请添加到客户端`)
  } catch {
    notify.error('复制失败，请手动复制')
  }
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

const generateOxidns = async () => {
  try {
    oxidnsLoading.value = true
    const response = await generateApi.oxidns(profileId)

    // 创建下载链接
    const blob = new Blob([response.data], { type: 'application/zip' })
    const url = window.URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = 'oxidns-config.zip'
    link.click()
    window.URL.revokeObjectURL(url)

    notify.success('OxiDNS 配置已生成')
  } catch (error: any) {
    let message = '生成 OxiDNS 配置失败'
    if (error?.code === 'ECONNABORTED' || error?.code === 'ETIMEDOUT') {
      message = '生成 OxiDNS 配置超时，请稍后重试'
    } else {
      try {
        // 下载接口的 JSON 错误也会被 Axios 按 Blob 接收。
        const data = error?.response?.data
        const details = data instanceof Blob ? JSON.parse(await data.text()) : data
        if (typeof details?.message === 'string' && details.message.trim()) {
          message = `生成 OxiDNS 配置失败：${details.message.trim()}`
        }
      } catch {
        // 网关可能返回 HTML 或无效 JSON，保留可读的通用错误提示。
      }
    }
    notify.error(message)
  } finally {
    oxidnsLoading.value = false
  }
}

const getCustomConfigDialogTitle = () => {
  const titles = {
    mihomo: 'Mihomo 基础配置',
    surge: 'Surge 基础配置',
    loon: 'Loon 基础配置',
    mosdns: 'MosDNS 基础配置'
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

const customConfigDescription = computed(() => {
  const format = currentConfigType.value === 'surge' || currentConfigType.value === 'loon' ? 'INI' : 'YAML'
  return `${getCustomConfigDialogDesc()} 留空并保存可恢复默认基础配置。请使用 ${format} 格式。`
})

const getCustomConfigPlaceholder = () => {
  const placeholders = {
    mihomo: '输入自定义 YAML 配置，例如：\nmixed-port: 7890\nallow-lan: true\nmode: rule\nlog-level: info\nexternal-controller: 127.0.0.1:9090\ndns:\n  enable: true\n  listen: 0.0.0.0:53\n  enhanced-mode: fake-ip',
    surge: '输入自定义配置，例如：\n[General]\nloglevel = notify\ninternet-test-url = https://www.gstatic.com/generate_204\nproxy-test-url = https://www.gstatic.com/generate_204\nskip-proxy = 127.0.0.1, 192.168.0.0/16, 10.0.0.0/8\n\n# 代理将自动生成在 [Proxy] 部分\n# 策略组将自动生成在 [Proxy Group] 部分\n# 规则将自动生成在 [Rule] 部分',
    loon: '输入自定义配置，例如：\n[General]\nip-mode = dual\ndns-server = system, 223.5.5.5\nproxy-test-url = https://www.gstatic.com/generate_204\nskip-proxy = 192.168.0.0/16, 10.0.0.0/8, localhost, *.local\n\n[Plugin]\n# 插件链接, tag=名称, enabled=true\n\n# 节点、订阅、策略组和规则将自动生成',
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
    if (!pageActive) return
    customConfigContent.value = response.data.config || ''
    mihomoDefaultConfig.value = response.data.default_config || ''
    customConfigSavedContent.value = customConfigContent.value
    if (type === 'mihomo') mihomoSyncInterval.value = mihomoSavedSyncInterval.value = response.data.provider_update_interval ?? 3600
    customConfigDialogVisible.value = true
  } catch (error) {
    if (pageActive) notify.error('加载自定义配置失败')
  }
}

const saveCustomConfig = async (close = currentConfigType.value !== 'mihomo'): Promise<PublishGuard | null> => {
  if (!pageActive || !customConfigDialogVisible.value || savingCustomConfig.value || ipv6NetworkBusy.value) return null
  const session = customConfigSession.capture()
  const type = currentConfigType.value
  if (type === 'mihomo' && ipv6Validation.value.invalid) return null
  const content = customConfigContent.value
  const interval = mihomoSyncInterval.value
  if (type === 'mihomo' && !isRefreshInterval(interval)) {
    notify.warning('订阅同步间隔需为 0 或 60 ~ 604800 秒的整数')
    return null
  }
  try {
    savingCustomConfig.value = true
    const apiMap = {
      mihomo: customConfigApi.saveMihomo,
      surge: customConfigApi.saveSurge,
      loon: customConfigApi.saveLoon,
      mosdns: customConfigApi.saveMosdns
    }
    const response = await apiMap[type]({ config: content, ...(type === 'mihomo' ? { provider_update_interval: interval } : {}) }, profileId)
    if (!customConfigSession.isCurrent(session) || type !== currentConfigType.value) return null
    customConfigSavedContent.value = content
    if (type === 'mihomo') mihomoSavedSyncInterval.value = interval as number
    notify.success('基础配置草稿已保存')
    if (close) customConfigDialogVisible.value = false
    return response.data.publish_guard ?? null
  } catch (error) {
    if (customConfigSession.isCurrent(session)) notify.error('保存基础配置失败')
    return null
  } finally {
    if (customConfigSession.isCurrent(session)) savingCustomConfig.value = false
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

const copyPreviewContent = async () => {
  try {
    await copyText(previewContent.value)
    notify.success('已复制到剪贴板')
  } catch {
    notify.error('复制失败，请手动复制预览内容')
  }
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

// 规则在 OxiDNS 中的 DNS 处理（GET /rules/dns），用于提示与 MosDNS 自身分流不同的规则；
// 只用于提示，读取失败不影响 MosDNS 设置。
const mosdnsOxidnsDns = ref<RuleDnsSnapshot | null>(null)
const loadMosdnsOxidnsDns = async () => {
  try {
    const { data } = await ruleApi.getDns(profileId)
    if (pageActive) mosdnsOxidnsDns.value = data?.engine && data.rules ? data : null
  } catch (error) {
    console.error('加载规则 DNS 处理失败', error)
  }
}
/** MosDNS 只有直连 / 代理两种分流：OxiDNS 的拦截、指定上游及不同的分流按 MosDNS 草稿汇总 */
const oxidnsDnsDifference = computed(() => {
  const snapshot = mosdnsOxidnsDns.value
  if (!snapshot?.engine.oxidns_in_use) return ''
  const mosdns = new Map<string, 'direct' | 'proxy'>()
  for (const id of [...mosdnsDirectRules.value, ...mosdnsDirectRulesets.value]) mosdns.set(id, 'direct')
  for (const id of [...mosdnsProxyRules.value, ...mosdnsProxyRulesets.value]) mosdns.set(id, 'proxy')
  const known = new Set([...availableRules.value, ...availableRuleSets.value].map(item => item.id))
  const counts = { block: 0, upstream: 0, route: 0 }
  for (const id of new Set([...Object.keys(snapshot.rules), ...mosdns.keys()])) {
    // 省略的规则即 OxiDNS 默认上游；停用与不参与 DNS 的规则两边都不生成。
    const entry = snapshot.rules[id]
    if ((!entry && !known.has(id)) || entry?.state === 'unsupported' || entry?.state === 'disabled') continue
    const route = entry?.route || null
    if (isBlockRoute(route)) counts.block++
    else if (isUpstreamValue(route)) counts.upstream++
    else if (route !== (mosdns.get(id) ?? null)) counts.route++
  }
  const parts = ([['拦截', counts.block], ['指定上游', counts.upstream], ['直连/代理', counts.route]] as const).filter(([, count]) => count)
  const total = counts.block + counts.upstream + counts.route
  return total
    ? `${total} 条规则在 OxiDNS 中的 DNS 处理与 MosDNS 不同（${parts.map(([label, count]) => `${label} ${count}`).join(' · ')}），MosDNS 按自身设置解析`
    : ''
})

const showMosdnsSettingsDialog = async () => {
  mosdnsOxidnsDns.value = null
  void loadMosdnsOxidnsDns()
  try {
    // 规则集列表、规则列表与 MosDNS 各项设置互不依赖，一次并行读取
    const [, , customConfigResponse, rulesetResponse, customMatchResponse, dnsResponse, logResponse, apiResponse, cacheResponse] = await Promise.all([
      loadRuleSets(),
      loadRules(),
      customConfigApi.getMosdns(profileId),
      api.get('/mosdns/rulesets', profileOptions),
      api.get('/mosdns/custom-matches', profileOptions),
      api.get('/mosdns/dns-servers', profileOptions),
      api.get('/mosdns/log-settings', profileOptions),
      api.get('/mosdns/api-settings', profileOptions),
      api.get('/mosdns/cache-settings', profileOptions)
    ])
    if (!pageActive) return

    // 加载自定义配置
    mosdnsCustomConfig.value = customConfigResponse.data.config || ''

    // 加载规则集配置
    mosdnsDirectRulesets.value = rulesetResponse.data.direct_rulesets || []
    mosdnsProxyRulesets.value = rulesetResponse.data.proxy_rulesets || []
    mosdnsDirectRules.value = rulesetResponse.data.direct_rules || []
    mosdnsProxyRules.value = rulesetResponse.data.proxy_rules || []

    // 加载自定义 match 配置
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
    mosdnsLogEnabled.value = logResponse.data.log_enabled !== undefined ? logResponse.data.log_enabled : true
    mosdnsLogLevel.value = logResponse.data.log_level || 'info'
    mosdnsLogFile.value = logResponse.data.log_file || './mosdns.log'

    // 加载 API 配置
    mosdnsApiEnabled.value = apiResponse.data.api_enabled !== undefined ? apiResponse.data.api_enabled : true
    mosdnsApiAddress.value = apiResponse.data.api_address || '0.0.0.0:8338'

    // 加载缓存配置
    mosdnsCacheEnabled.value = cacheResponse.data.cache_enabled !== undefined ? Boolean(cacheResponse.data.cache_enabled) : true
    mosdnsCacheSize.value = Number(cacheResponse.data.cache_size ?? 10240)
    mosdnsCacheLazyTtl.value = Number(cacheResponse.data.cache_lazy_ttl ?? 21600)
    mosdnsCacheDumpEnabled.value = cacheResponse.data.cache_dump_enabled !== undefined ? Boolean(cacheResponse.data.cache_dump_enabled) : true
    mosdnsCacheDumpFile.value = cacheResponse.data.cache_dump_file ?? './cache.dump'
    mosdnsCacheDumpInterval.value = Number(cacheResponse.data.cache_dump_interval ?? 300)

    // 重置到第一个 tab
    mosdnsActiveTab.value = 'rules'

    // Snapshot only once every editable field has finished loading.
    mosdnsSavedSnapshot.value = mosdnsDraftSnapshot.value
    mosdnsSettingsDialogVisible.value = true

    // 初始化拖拽功能
    initDnsSortable()
  } catch (error) {
    console.error('加载 MosDNS 设置失败', error)
    if (pageActive) notify.error('加载 MosDNS 设置失败')
  }
}

const saveMosdnsSettings = async (close = true): Promise<PublishGuard | null> => {
  if (!pageActive || !mosdnsSettingsDialogVisible.value || savingMosdnsSettings.value) return null
  const session = mosdnsSettingsSession.capture()
  const snapshot = mosdnsDraftSnapshot.value
  const draft = JSON.parse(snapshot)
  const customMatchPayload = mosdnsCustomMatches.value.map(item => ({
    id: item.id,
    enabled: item.enabled,
    exec: item.exec,
    matches: item.matchesText
  }))
  const localDnsText = dnsEntriesToText(mosdnsLocalDnsEntries.value)
  const remoteDnsText = dnsEntriesToText(mosdnsRemoteDnsEntries.value)
  const fallbackDnsText = dnsEntriesToText(mosdnsFallbackDnsEntries.value)
  try {
    savingMosdnsSettings.value = true

    const response = await api.post('/mosdns/settings', {
      custom_config: draft.customConfig,
      direct_rulesets: draft.directRulesets,
      proxy_rulesets: draft.proxyRulesets,
      direct_rules: draft.directRules,
      proxy_rules: draft.proxyRules,
      custom_matches: customMatchPayload,
      custom_match_position: draft.customMatchPosition,
      local_dns: localDnsText,
      remote_dns: remoteDnsText,
      fallback_dns: fallbackDnsText,
      default_forward: draft.defaultForward,
      custom_hosts: draft.customHosts,
      log_enabled: draft.logEnabled,
      log_level: draft.logLevel,
      log_file: draft.logFile,
      api_enabled: draft.apiEnabled,
      api_address: draft.apiAddress,
      cache_enabled: draft.cacheEnabled,
      cache_size: draft.cacheSize,
      cache_lazy_ttl: draft.cacheLazyTtl,
      cache_dump_enabled: draft.cacheDumpEnabled,
      cache_dump_file: draft.cacheDumpFile,
      cache_dump_interval: draft.cacheDumpInterval
    }, profileOptions)
    if (!mosdnsSettingsSession.isCurrent(session)) return null

    mosdnsLocalDns.value = localDnsText
    mosdnsRemoteDns.value = remoteDnsText
    mosdnsFallbackDns.value = fallbackDnsText
    mosdnsSavedSnapshot.value = snapshot
    notify.success('MosDNS 设置草稿已保存')
    if (close) mosdnsSettingsDialogVisible.value = false
    return response.data.publish_guard ?? null
  } catch (error) {
    if (mosdnsSettingsSession.isCurrent(session)) {
      console.error('保存 MosDNS 设置失败', error)
      notify.error('保存 MosDNS 设置失败')
    }
    return null
  } finally {
    if (mosdnsSettingsSession.isCurrent(session)) savingMosdnsSettings.value = false
  }
}

const agentEditorOpen = computed(() => customConfigDialogVisible.value || mosdnsSettingsDialogVisible.value || oxidnsSettingsDialogVisible.value)
watch(agentEditorOpen, (open, wasOpen) => {
  if (props.agentSettings && wasOpen && !open) emit('close')
})

/** 规则页「打开完整参数」的深链：/generate?oxidns=config 直接打开 OxiDNS 设置的完整参数 */
function openLinkedOxidnsSettings() {
  const query = router.currentRoute?.value?.query
  if (query?.oxidns !== 'config') return
  oxidnsInitialTab.value = 'config'
  oxidnsSettingsDialogVisible.value = true
  const { oxidns: _linked, ...rest } = query
  void router.replace({ query: rest })
}

onMounted(async () => {
  if (props.agentSettings) {
    const service = props.agentSettings.serviceType
    if (service === 'oxidns') oxidnsSettingsDialogVisible.value = true
    else if (service === 'mosdns') await showMosdnsSettingsDialog()
    else await showCustomConfigDialog(service)
    if (pageActive) {
      if (agentEditorOpen.value) emit('ready')
      else emit('close')
    }
    return
  }
  openLinkedOxidnsSettings()
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
