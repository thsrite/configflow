# ConfigFlow 全站 UI 重构方案（feat/ui-tech-refactor）

需求：忽略原有排版布局，全站 12 个页面重构，要美观 / 直观 / 有科技感，使用现成组件库与动效，自动完成。

## 设计方向
「Control Room / 控制台」深色科技风：
- 基底：近黑蓝紫冷调，分层玻璃表面（`bg-card/60 + backdrop-blur`）
- 强调：靛蓝主色 + 青色副色，双色渐变发光（glow）用于激活态、焦点环、图表
- 氛围层：页面底部固定 aurora 光斑 + 细网格（`--tech-grid`），不干扰内容可读性
- 边框：渐变发丝边（gradient hairline）替代纯色描边
- 数据：数字统一等宽 tabular-nums，KPI 用 count-up 动效
- 布局：bento grid（总览）/ 工具条 + 数据表（列表页）/ 分栏（编辑页）

## 技术栈决定
| 项 | 选择 | 理由 |
|---|---|---|
| 组件库 | shadcn-vue (reka-ui) 全量 32 组件 | 已在用，无样式黑箱，可深度定制 |
| 动效 | `motion-v`（Motion for Vue） + `tw-animate-css` | 声明式 layout/enter/exit 动画，支持 stagger 与 spring |
| 提示 | `vue-sonner` + shadcn `AlertDialog` | 替代 ElMessage / ElMessageBox |
| 样式 | Tailwind v4 + preflight（Element Plus 移除后启用） | 去掉 cascade layer 冲突约束 |
| 图表 | echarts（保留），换科技感主题 | 已在用 |

**关键破坏性动作**：彻底移除 element-plus。移除后 theme.css 里的 cascade-layer 约束失效条件消失，可启用 preflight，样式基线统一。

## 分阶段交付（每阶段可独立验收：`npx vite build` 通过 + 视觉核对）

- [x] P0-a 依赖与组件：装 motion-v / vue-sonner，补齐 shadcn 组件到 32 个
- [x] P0-b 主题层：theme.css 科技感升级（glow / grid / aurora / gradient border token），启用 preflight
- [x] P0-c 基础设施：`useToast` / `useConfirm` 替代 ElMessage/ElMessageBox；motion 预设；共享原语 PageShell / Toolbar / DataTable / EmptyState / StatTile / SectionCard
- [x] P1 应用壳：App.vue + AppRail + MobileTabBar + PageHeader，动画导航指示器、⌘K 命令面板、路由过渡
- [x] P2 Dashboard（bento + count-up + 图表主题）
- [x] P3 Login（分栏 hero + aurora）
- [x] P4 资源与系统页：Profiles ✅ / Logs ✅ / Nodes ✅ / SubscriptionAggregation ✅ / Subscriptions（仅需科技感重塑，已是 shadcn）
- [x] P5 RuleLibrary / Rules / ProxyGroups
- [x] P6 Generate / Agents / Logs
- [x] P7 移除 element-plus 依赖、全量构建、Chrome 实机截图核对、开 PR

## 改造手法（每页统一）
1. 保留 `<script setup>` 业务逻辑，只重写 `<template>`，删除 `<style scoped>`
2. `el-message` → `toast`；`el-message-box` → `confirm()`；`el-table` → shadcn Table；
   `el-dialog` → Dialog/Sheet；`el-form` → Label+Input+Field；`el-tag` → Badge；
   `el-select` → Select；`el-switch` → Switch；`el-tabs` → Tabs；`el-tooltip` → Tooltip
3. 列表页统一：PageHeader（标题+说明+主操作）→ Toolbar（搜索/筛选/批量）→ 内容（表格或卡片网格）→ EmptyState
4. 进场动画：容器 stagger 40ms，卡片 y:8→0 / opacity 0→1，spring


## 完成情况

全部 14 个页面 / 壳层已重构完成，element-plus 已从依赖中移除，Tailwind preflight 已启用。

- 壳层：App.vue（玻璃顶栏 · ⌘K 命令面板 · rail 滑动指示器 · 路由过渡）、AppRail、
  MobileTabBar、MobileGroupNav、CommandPalette
- 页面：数据统计 / 登录 / 配置空间 / 日志 / 节点库 / 订阅聚合 / 订阅来源 /
  策略组 / 策略规则 / 规则库 / 配置生成 / Agent
- 共享原语（components/common/）：PageHeader · SectionCard · StatTile · Toolbar ·
  EmptyState · StatusDot · AnimatedNumber · LoadingRows · ViewToggle ·
  DataTableShell · MultiSelect · GroupField · FormField · InfoNote · LabeledDivider
- 反馈层（lib/feedback.ts）：notify · confirm · confirmDanger · choose（三选一）· prompt

### 验证证据
- `npx vite build` 通过（每个页面提交前均跑过）
- Chrome 实机核对 10 个路由：全部正常渲染，`list_console_messages` 无 error / warn
- 深浅主题各截图核对一次；390×844 移动端布局核对一次
- `pytest backend` 410 passed / 1 failed；该 1 例
  （test_sensitive_config_exports 的递归深度用例）在 main 上同样失败，属既有问题
- test_agent_profile_dropdown 的两条断言随实现更新：原本守「原生 select + 显式
  文字色」，是为绕开 el-select popper 在深色主题/iOS 下的着色问题；改用 shadcn
  Select 后弹层是自有 DOM、由 token 着色，断言改为守「用 shadcn Select 且绑定正确」

## 改造手法备忘（供后续维护）
1. 模板整体替换时不能用 `split('</template>')`——插槽 `<template #actions>` 会先命中，
   要按「第一处顶层 `  </div>\n</template>\n`」定位
2. Vue 模板会压缩标签间空白，行内分段着色要用 flex gap 而不是空格
3. 页面自带 viewMode 的，值统一为 'list' | 'card'，与 ViewToggle 对齐
4. **TabsContent 上不要直接加 `flex`**：它会覆盖 `[hidden]` 的 `display:none`，
   导致所有面板同时占位、每个只分到 1/N 高度。外层只负责滚动，内层 div 负责布局
5. preflight 启用前的两个坑已随 preflight 一并消失，仅作记录：
   未启用时 `<button>` 需显式 `border-0 bg-transparent p-0`；
   `border-dashed` 必须先 `border-0` 再单边，否则四边都以 UA 默认宽度显示
