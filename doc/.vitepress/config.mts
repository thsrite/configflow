import { defineConfig } from 'vitepress'

export default defineConfig({
  lang: 'zh-CN',
  title: 'ConfigFlow 使用手册',
  description: '从安装、首次配置到日常维护，用日常话说明每个选择和按钮。',
  base: process.env.DOCS_BASE || '/',
  srcExclude: ['design/**', 'img/**',
    'UI-REFACTOR-PLAN.md', 'client-fixed-ip-validation.md', 'oxidns-coverage.md',
    'oxidns-device-policies.md', 'oxidns-validation-20261016.md'],
  cleanUrls: true,
  themeConfig: {
    nav: [
      { text: '文档首页', link: '/README' },
      { text: '项目主页', link: 'https://github.com/thsrite/configflow' },
    ],
    sidebar: [
      { text: '安装与首次使用', items: [
        { text: '文档概览', link: '/README' },
        { text: '安装管理面板', link: '/guide/install' },
        { text: 'v3.0.0 升级说明', link: '/guide/upgrade-v3' },
        { text: '首次使用向导', link: '/guide/first-run' },
        { text: '设备安装组合', link: '/guide/agent-install' },
        { text: 'RouterOS 容器安装', link: '/guide/routeros-install' },
        { text: 'OxiDNS 双机备用', link: '/guide/oxidns-ha' },
      ] },
      { text: '总览与运行面板', collapsed: true, items: [
        { text: '数据统计', link: '/manual/dashboard' },
        { text: 'OxiDNS 面板', link: '/manual/oxidns-panel' },
        { text: 'Mihomo 面板', link: '/manual/mihomo-panel' },
      ] },
      { text: '共享资源', collapsed: true, items: [
        { text: '订阅来源', link: '/manual/subscriptions' },
        { text: '节点库', link: '/manual/nodes' },
        { text: '订阅聚合', link: '/manual/subscription-aggregation' },
        { text: '规则库', link: '/manual/rule-library' },
      ] },
      { text: '当前配置', collapsed: true, items: [
        { text: '策略组', link: '/manual/proxy-groups' },
        { text: '策略规则', link: '/manual/rules' },
        { text: '客户端设备', link: '/manual/client-devices' },
        { text: '域名发现', link: '/manual/domain-discovery' },
        { text: '上游路由', link: '/manual/routers' },
        { text: '配置生成', link: '/manual/generate' },
        { text: '各软件的设置', link: '/manual/core-settings' },
      ] },
      { text: '系统与维护', collapsed: true, items: [
        { text: '系统设置', link: '/manual/system-settings' },
        { text: '连接 AI 工具（MCP）', link: '/manual/mcp' },
        { text: '配置空间', link: '/manual/profiles' },
        { text: 'Agent', link: '/manual/agents' },
        { text: '日志', link: '/manual/logs' },
      ] },
      { text: '遇到问题', collapsed: true, items: [
        { text: '常见问题与排查', link: '/guide/troubleshooting' },
        { text: '常见名词', link: '/guide/terms' },
      ] },
    ],
    outline: { level: [2, 3], label: '本页目录' },
    docFooter: { prev: '上一篇', next: '下一篇' },
    sidebarMenuLabel: '文档目录',
    returnToTopLabel: '回到顶部',
    darkModeSwitchLabel: '外观',
    lightModeSwitchTitle: '切换浅色',
    darkModeSwitchTitle: '切换深色',
    search: { provider: 'local', options: { locales: { root: { translations: {
      button: { buttonText: '搜索文档', buttonAriaLabel: '搜索文档' },
      modal: { displayDetails: '显示详细结果', resetButtonTitle: '清空搜索',
        backButtonTitle: '关闭搜索', noResultsText: '没有找到相关内容',
        footer: { selectText: '选择', navigateText: '切换', closeText: '关闭' } },
    } } } } },
  },
})
