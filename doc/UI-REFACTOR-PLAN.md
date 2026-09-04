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
- [ ] P0-b 主题层：theme.css 科技感升级（glow / grid / aurora / gradient border token），启用 preflight
- [ ] P0-c 基础设施：`useToast` / `useConfirm` 替代 ElMessage/ElMessageBox；motion 预设；共享原语 PageShell / Toolbar / DataTable / EmptyState / StatTile / SectionCard
- [ ] P1 应用壳：App.vue + AppRail + MobileTabBar + PageHeader，动画导航指示器、⌘K 命令面板、路由过渡
- [ ] P2 Dashboard（bento + count-up + 图表主题）
- [ ] P3 Login（分栏 hero + aurora）
- [ ] P4 Profiles / Subscriptions / Nodes / SubscriptionAggregation
- [ ] P5 RuleLibrary / Rules / ProxyGroups
- [ ] P6 Generate / Agents / Logs
- [ ] P7 移除 element-plus 依赖、全量构建、Chrome 实机截图核对、开 PR

## 改造手法（每页统一）
1. 保留 `<script setup>` 业务逻辑，只重写 `<template>`，删除 `<style scoped>`
2. `el-message` → `toast`；`el-message-box` → `confirm()`；`el-table` → shadcn Table；
   `el-dialog` → Dialog/Sheet；`el-form` → Label+Input+Field；`el-tag` → Badge；
   `el-select` → Select；`el-switch` → Switch；`el-tabs` → Tabs；`el-tooltip` → Tooltip
3. 列表页统一：PageHeader（标题+说明+主操作）→ Toolbar（搜索/筛选/批量）→ 内容（表格或卡片网格）→ EmptyState
4. 进场动画：容器 stagger 40ms，卡片 y:8→0 / opacity 0→1，spring
