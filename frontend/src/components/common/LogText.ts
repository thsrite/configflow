import { h, type FunctionalComponent } from 'vue'
import { markLogTokens, type LogToken } from '@/lib/agentLogs'
import './logText.css'

/**
 * 日志正文：按 tokenizeLogMessage 的片段着色，并标出搜索词。
 * 用函数式组件，长列表里每行不多一个组件实例；颜色在 logText.css 里，
 * 系统日志页与 Mihomo / Agent 日志（AgentLogLines）共用。
 */
const LogText: FunctionalComponent<{ tokens: LogToken[]; highlight?: string }> = props =>
  markLogTokens(props.tokens, props.highlight || '').map(token => {
    const kind = token.kind ? `log-tk-${token.kind}` : undefined
    if (token.hit) return h('mark', { class: ['log-hit', kind] }, token.text)
    return kind ? h('span', { class: kind }, token.text) : token.text
  })

LogText.props = ['tokens', 'highlight']

export default LogText
