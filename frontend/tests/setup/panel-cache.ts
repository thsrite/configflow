import { afterEach } from 'vitest'
import { clearPanelCache } from '@/lib/mihomoPanelCache'

// The Mihomo panel cache is module state; a test must not open on data another test left behind.
afterEach(clearPanelCache)
