<template>
  <div class="overflow-x-auto overflow-y-hidden [scrollbar-width:thin]">
    <div
      ref="root"
      class="relative px-5 pt-5 pb-4"
      :style="{ minWidth: `${minWidth}px` }"
      @pointerleave="setFocus(null)"
    >
      <canvas ref="canvas" class="pointer-events-none absolute inset-0 size-full" aria-hidden="true" />

      <div
        :class="cn('relative grid', !gapX && 'gap-x-11 max-sm:gap-x-7')"
        :style="{
          gridTemplateColumns: columnsTemplate || `repeat(${columns.length}, minmax(0, 1fr))`,
          columnGap: gapX ? `${gapX}px` : undefined,
          minHeight: `${height}px`
        }"
      >
        <div
          v-for="(column, ci) in columns"
          :key="column.key"
          class="relative flex min-w-0 flex-col justify-around gap-2.5 pt-6"
        >
          <div class="absolute top-0 left-0 font-mono text-[10px] tracking-[0.14em] text-muted-foreground uppercase">
            {{ String(ci + 1).padStart(2, '0') }} {{ column.title }}
          </div>

          <component
            :is="node.to ? 'button' : 'div'"
            v-for="node in column.nodes"
            :key="node.id"
            :type="node.to ? 'button' : undefined"
            :data-fid="node.id"
            :class="cn(
              'flow-node relative min-w-0 rounded-xl border border-border-strong bg-card/90 text-left backdrop-blur-sm transition-[border-color,box-shadow,opacity,transform] duration-300',
              node.fit ? 'w-fit max-w-full self-start' : 'w-full',
              node.to && 'cursor-pointer hover:-translate-y-px hover:border-primary-accent/45 focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none',
              hits[node.id] && 'is-hit',
              lit && !lit.has(node.id) && 'opacity-30',
              node.muted && 'opacity-60'
            )"
            :aria-label="node.to ? `${node.title}，查看详情` : undefined"
            @pointerenter="setFocus(node.id)"
            @focus="setFocus(node.id)"
            @blur="setFocus(null)"
            @click="node.to && router.push(node.to)"
          >
            <slot name="node" :node="node">
              <span class="flex items-center gap-2.5 px-2.5 py-2">
                <span class="grid size-7 shrink-0 place-items-center rounded-lg bg-secondary text-muted-foreground">
                  <component :is="node.icon" v-if="node.icon" class="size-3.5" :stroke-width="2" />
                </span>
                <span class="min-w-0 flex-1">
                  <span class="block truncate text-[12.5px] font-semibold text-foreground">{{ node.title }}</span>
                  <span v-if="node.meta" class="block truncate font-mono text-[10.5px] text-muted-foreground">
                    {{ node.meta }}
                  </span>
                </span>
                <span
                  v-if="node.status"
                  :class="cn(
                    'size-[7px] shrink-0 rounded-full',
                    node.status === 'online'
                      ? 'bg-success-accent shadow-[0_0_10px_var(--success-accent)]'
                      : 'bg-muted-foreground/60'
                  )"
                  :title="node.status === 'online' ? '在线' : '离线'"
                />
              </span>
            </slot>
          </component>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, watch, type Component } from 'vue'
import { useRouter } from 'vue-router'
import { cn } from '@/lib/utils'
import { useThemeStore } from '@/stores/theme'
import { usePreferences } from '@/stores/preferences'

export interface FlowNode {
  id: string
  title: string
  meta?: string
  icon?: Component
  /** 点击后跳转的路由 */
  to?: string
  status?: 'online' | 'offline'
  /** 弱化显示（例如已停用） */
  muted?: boolean
  /** 供自定义插槽区分渲染样式 */
  kind?: string
  /** 按内容宽度显示，不撑满整列 */
  fit?: boolean
  [key: string]: unknown
}

export interface FlowColumn {
  key: string
  title: string
  nodes: FlowNode[]
}

export type FlowTone = 'primary' | 'kraft' | 'success' | 'info'

export interface FlowEdge {
  from: string
  to: string
  /** 粒子发射频率权重，约 0.2 ~ 2 */
  weight?: number
  tone?: FlowTone
  /** 链路不通（离线 / 停用）：虚线且不发射粒子 */
  dead?: boolean
}

const props = withDefaults(
  defineProps<{
    columns: FlowColumn[]
    edges: FlowEdge[]
    height?: number
    minWidth?: number
    /** 粒子密度倍率 */
    density?: number
    /** 自定义列宽，如 '1fr 1.2fr' */
    columnsTemplate?: string
    /** 列间距（px），连线越长粒子越有「流动感」 */
    gapX?: number
  }>(),
  { height: 380, minWidth: 980, density: 1 }
)

const router = useRouter()
const { theme } = useThemeStore()
const { prefs } = usePreferences()
const root = ref<HTMLElement | null>(null)
const canvas = ref<HTMLCanvasElement | null>(null)

/* ---------- 悬停链路追踪：沿边上下游展开 ---------- */
const focus = ref<string | null>(null)
const lit = computed<Set<string> | null>(() => {
  const id = focus.value
  if (!id) return null
  const set = new Set([id])
  for (const forward of [true, false]) {
    const seen = new Set([id])
    let grew = true
    while (grew) {
      grew = false
      for (const e of props.edges) {
        const [a, b] = forward ? [e.from, e.to] : [e.to, e.from]
        if (seen.has(a) && !seen.has(b)) {
          seen.add(b)
          set.add(b)
          grew = true
        }
      }
    }
  }
  return set
})
const setFocus = (id: string | null) => {
  focus.value = id
}

/** 粒子抵达时目标节点短暂发光 */
const hits = reactive<Record<string, boolean>>({})
const hitAt = new Map<string, number>()
const hitTimers = new Map<string, number>()

/* ---------- 几何 ---------- */
type Point = [number, number]
interface Geo {
  edge: FlowEdge
  p: [Point, Point, Point, Point]
  len: number
  acc: number
}
interface Particle {
  g: Geo
  t: number
  v: number
  size: number
  boost: number
}

let geos: Geo[] = []
let particles: Particle[] = []
let W = 0
let H = 0
let palette: Record<FlowTone | 'muted', string> = {
  primary: '#d97757',
  kraft: '#dcae86',
  success: '#a3b97f',
  info: '#8fb4dc',
  muted: '#a9a598'
}
let additive = true

const readPalette = () => {
  const cs = getComputedStyle(document.documentElement)
  const v = (name: string, fallback: string) => cs.getPropertyValue(name).trim() || fallback
  palette = {
    primary: v('--primary-accent', palette.primary),
    kraft: v('--accent-2', palette.kraft),
    success: v('--success-accent', palette.success),
    info: v('--info-accent', palette.info),
    muted: v('--muted-foreground', palette.muted)
  }
  additive = theme.value === 'dark'
}

const layout = () => {
  const el = root.value
  const cv = canvas.value
  if (!el || !cv) return
  const R = el.getBoundingClientRect()
  if (!R.width) return
  const dpr = Math.min(window.devicePixelRatio || 1, 2)
  W = R.width
  H = R.height
  cv.width = Math.round(W * dpr)
  cv.height = Math.round(H * dpr)
  cv.getContext('2d')?.setTransform(dpr, 0, 0, dpr, 0, 0)

  const rectOf = (id: string) =>
    el.querySelector<HTMLElement>(`[data-fid="${CSS.escape(id)}"]`)?.getBoundingClientRect()
  const prev = new Map(geos.map(g => [`${g.edge.from}>${g.edge.to}`, g]))
  const next: Geo[] = []
  for (const edge of props.edges) {
    const A = rectOf(edge.from)
    const B = rectOf(edge.to)
    if (!A || !B) continue
    const p0: Point = [A.right - R.left, A.top + A.height / 2 - R.top]
    const p3: Point = [B.left - R.left, B.top + B.height / 2 - R.top]
    const dx = (p3[0] - p0[0]) * 0.55
    const old = prev.get(`${edge.from}>${edge.to}`)
    next.push({
      edge,
      p: [p0, [p0[0] + dx, p0[1]], [p3[0] - dx, p3[1]], p3],
      len: Math.max(60, Math.hypot(p3[0] - p0[0], p3[1] - p0[1]) * 1.1),
      acc: old?.acc ?? Math.random()
    })
  }
  // 边被替换后旧粒子失去几何，直接丢弃
  const alive = new Set(next)
  const remap = new Map(next.map(g => [`${g.edge.from}>${g.edge.to}`, g]))
  particles = particles
    .map(pt => ({ ...pt, g: remap.get(`${pt.g.edge.from}>${pt.g.edge.to}`) as Geo }))
    .filter(pt => pt.g && alive.has(pt.g))
  geos = next
}

const bezier = (p: Geo['p'], t: number): Point => {
  const u = 1 - t
  const a = u * u * u
  const b = 3 * u * u * t
  const c = 3 * u * t * t
  const d = t * t * t
  return [
    a * p[0][0] + b * p[1][0] + c * p[2][0] + d * p[3][0],
    a * p[0][1] + b * p[1][1] + c * p[2][1] + d * p[3][1]
  ]
}

const edgeLit = (e: FlowEdge) => !lit.value || (lit.value.has(e.from) && lit.value.has(e.to))

const spawn = (g: Geo, t = 0, boost = 1) => {
  particles.push({ g, t, v: ((150 + Math.random() * 110) * boost) / g.len, size: 1.2 + Math.random() * 1.6 * boost, boost })
}

/** 外部触发：向某个节点集中发射一串粒子（例如推送、拉取完成） */
const burst = (to: string, count = 12) => {
  if (!canRender() || !animationEnabled()) return
  for (const g of geos) {
    if (g.edge.to !== to || g.edge.dead) continue
    for (let i = 0; i < count; i++) spawn(g, -i * 0.06, 1.8)
  }
  requestDraw()
}
defineExpose({ burst })

const markHit = (id: string, now: number) => {
  if (now - (hitAt.get(id) || 0) < 380) return
  hitAt.set(id, now)
  hits[id] = true
  window.clearTimeout(hitTimers.get(id))
  hitTimers.set(id, window.setTimeout(() => {
    hitTimers.delete(id)
    hits[id] = false
  }, 260))
}

let visible = true
let mounted = false
let reducedMotion = false
const canRender = () => mounted && visible && !document.hidden
const animationEnabled = () => prefs.value.motion && !reducedMotion

const draw = (dt: number, animate: boolean) => {
  const ctx = canvas.value?.getContext('2d')
  if (!ctx || !W) return
  ctx.clearRect(0, 0, W, H)
  for (const g of geos) {
    const on = edgeLit(g.edge)
    ctx.beginPath()
    ctx.moveTo(...g.p[0])
    ctx.bezierCurveTo(...g.p[1], ...g.p[2], ...g.p[3])
    ctx.setLineDash(g.edge.dead ? [3, 5] : [])
    ctx.lineWidth = on && lit.value ? 1.6 : 1
    ctx.strokeStyle = g.edge.dead ? palette.muted : palette[g.edge.tone || 'primary']
    ctx.globalAlpha = g.edge.dead ? 0.35 : on ? (lit.value ? 0.55 : 0.22) : 0.05
    ctx.stroke()
    if (animate && !g.edge.dead) {
      g.acc += dt * (g.edge.weight ?? 1) * props.density * prefs.value.density * 2.2
      while (g.acc > 1) {
        g.acc -= 1
        spawn(g)
      }
    }
  }
  ctx.setLineDash([])

  if (!animate) {
    // 减少动效：每条边固定三个点表达方向
    for (const g of geos) {
      if (g.edge.dead) continue
      ctx.fillStyle = palette[g.edge.tone || 'primary']
      ctx.globalAlpha = edgeLit(g.edge) ? 0.8 : 0.1
      for (const t of [0.25, 0.5, 0.75]) {
        const [x, y] = bezier(g.p, t)
        ctx.beginPath()
        ctx.arc(x, y, 2, 0, Math.PI * 2)
        ctx.fill()
      }
    }
    ctx.globalAlpha = 1
    return
  }

  ctx.globalCompositeOperation = additive ? 'lighter' : 'source-over'
  const now = performance.now()
  for (let i = particles.length - 1; i >= 0; i--) {
    const pt = particles[i]
    pt.t += pt.v * dt
    if (pt.t >= 1) {
      particles.splice(i, 1)
      markHit(pt.g.edge.to, now)
      continue
    }
    if (pt.t < 0) continue
    const on = edgeLit(pt.g.edge)
    const color = palette[pt.g.edge.tone || 'primary']
    const head = bezier(pt.g.p, pt.t)
    const tail = bezier(pt.g.p, Math.max(0, pt.t - 0.07 * pt.boost))
    const grad = ctx.createLinearGradient(tail[0], tail[1], head[0], head[1])
    grad.addColorStop(0, 'transparent')
    grad.addColorStop(1, color)
    ctx.globalAlpha = on ? 1 : 0.08
    ctx.strokeStyle = grad
    ctx.lineWidth = pt.size
    ctx.lineCap = 'round'
    ctx.beginPath()
    ctx.moveTo(...tail)
    ctx.lineTo(...head)
    ctx.stroke()
    ctx.globalAlpha = on ? 0.22 : 0.03
    ctx.fillStyle = color
    ctx.beginPath()
    ctx.arc(head[0], head[1], pt.size * 3.2, 0, Math.PI * 2)
    ctx.fill()
    ctx.globalAlpha = on ? 1 : 0.1
    ctx.fillStyle = additive ? '#fff' : color
    ctx.beginPath()
    ctx.arc(head[0], head[1], pt.size * 0.7, 0, Math.PI * 2)
    ctx.fill()
  }
  ctx.globalAlpha = 1
  ctx.globalCompositeOperation = 'source-over'
}

let frame: number | null = null
let last: number | null = null
let dirty = true
let layoutDirty = true
let paletteDirty = true

// All invalidations share one frame. Static/hidden maps have no idle RAF loop.
const scheduleDraw = () => {
  if (!canRender() || frame !== null || (!dirty && !animationEnabled())) return
  const requested = requestAnimationFrame(now => {
    if (frame !== requested) return
    frame = null
    if (!canRender()) return
    if (paletteDirty) {
      readPalette()
      paletteDirty = false
    }
    if (layoutDirty) {
      layout()
      layoutDirty = false
    }
    const animate = animationEnabled()
    const dt = animate && last !== null ? Math.min(0.05, (now - last) / 1000) : 0
    last = animate ? now : null
    dirty = false
    draw(dt, animate)
    scheduleDraw()
  })
  frame = requested
}

const requestDraw = (geometry = false, colors = false) => {
  dirty = true
  layoutDirty ||= geometry
  paletteDirty ||= colors
  scheduleDraw()
}

const cancelDraw = () => {
  if (frame !== null) cancelAnimationFrame(frame)
  frame = null
  last = null
}

const clearTransientEffects = () => {
  particles = []
  geos.forEach(g => { g.acc = 0 })
  hitTimers.forEach(timer => window.clearTimeout(timer))
  hitTimers.clear()
  hitAt.clear()
  Object.keys(hits).forEach(id => { delete hits[id] })
}

const updateRendering = () => {
  if (!mounted) return
  cancelDraw()
  if (!canRender() || !animationEnabled()) clearTransientEffects()
  // Re-measure on resume: hidden layouts may have changed without a resize event.
  requestDraw(true)
}

let resizeObserver: ResizeObserver | null = null
let intersection: IntersectionObserver | null = null
let motionQuery: MediaQueryList | null = null
const onMotionChange = (event: MediaQueryListEvent) => {
  reducedMotion = event.matches
  updateRendering()
}

onMounted(() => {
  mounted = true
  motionQuery = typeof window.matchMedia === 'function' ? window.matchMedia('(prefers-reduced-motion: reduce)') : null
  reducedMotion = motionQuery?.matches ?? false
  motionQuery?.addEventListener?.('change', onMotionChange)
  document.addEventListener('visibilitychange', updateRendering)
  // 观察器不可用的环境（旧浏览器、测试环境）退化为：不随尺寸重排、始终视为可见
  if (typeof ResizeObserver === 'function') {
    resizeObserver = new ResizeObserver(() => requestDraw(true))
    if (root.value) resizeObserver.observe(root.value)
  }
  if (typeof IntersectionObserver === 'function') {
    intersection = new IntersectionObserver(([entry]) => {
      if (!entry || visible === entry.isIntersecting) return
      visible = entry.isIntersecting
      updateRendering()
    })
    if (root.value) intersection.observe(root.value)
  }
  requestDraw()
})

onUnmounted(() => {
  mounted = false
  cancelDraw()
  resizeObserver?.disconnect()
  intersection?.disconnect()
  motionQuery?.removeEventListener?.('change', onMotionChange)
  document.removeEventListener('visibilitychange', updateRendering)
  clearTransientEffects()
})

// 主题切换后 CSS 变量已更新，下一帧再读取
watch([theme, () => prefs.value.accent], () => requestDraw(false, true), { flush: 'post' })
watch(() => prefs.value.motion, updateRendering, { flush: 'post' })
watch(() => prefs.value.density, () => requestDraw(), { flush: 'post' })
watch(focus, () => requestDraw(), { flush: 'post' })
// 节点或边变化后等 DOM 更新完再重算几何
watch(
  () => [props.columns, props.edges, props.height, props.minWidth, props.columnsTemplate, props.gapX],
  () => requestDraw(true),
  { deep: true, flush: 'post' }
)
watch(() => props.density, () => requestDraw(), { flush: 'post' })
</script>

<style scoped>
.flow-node.is-hit {
  border-color: oklch(from var(--primary-accent) l c h / 55%);
  box-shadow:
    0 0 0 1px oklch(from var(--primary-accent) l c h / 18%),
    0 0 24px -6px var(--primary-accent);
}
</style>
