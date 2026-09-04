/**
 * Element Plus 图标名 → lucide 组件的映射。
 *
 * navigation.ts 与各视图仍以字符串声明图标（未改造页面依赖全局注册的 EP 图标），
 * 已改造为 shadcn 的组件通过本表取到对应 lucide 组件，两套图标可并存过渡。
 */
import {
  ArrowDown,
  ArrowRight,
  ArrowUp,
  ChevronRight,
  Download,
  FileText,
  FolderOpen,
  GripVertical,
  IdCard,
  Layers,
  LayoutDashboard,
  LayoutGrid,
  Link2,
  LogOut,
  Moon,
  Network,
  RefreshCw,
  ScrollText,
  Send,
  Server,
  Settings,
  Share2,
  Sun,
  User,
  type LucideIcon
} from '@lucide/vue'

const MAP: Record<string, LucideIcon> = {
  DataAnalysis: LayoutDashboard,
  Files: Layers,
  Link: Link2,
  Connection: Network,
  Share: Share2,
  FolderOpened: FolderOpen,
  Grid: LayoutGrid,
  Document: FileText,
  Download,
  Setting: Settings,
  Monitor: Server,
  Tickets: ScrollText,
  Postcard: IdCard,
  Refresh: RefreshCw,
  Promotion: Send,
  ArrowRight,
  ArrowUp,
  ArrowDown,
  ChevronRight,
  Rank: GripVertical,
  Sunny: Sun,
  Moon,
  User,
  SwitchButton: LogOut
}

/** 取不到时回落到通用图标，避免导航项因图标缺失而不渲染 */
export const iconOf = (name: string): LucideIcon => MAP[name] ?? Layers
