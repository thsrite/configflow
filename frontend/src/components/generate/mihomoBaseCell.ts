/** Shared looks for the base settings sheet: values read as plain text until hovered or focused. */
export const CELL_INPUT = 'h-11 w-full min-w-0 rounded-md border border-transparent bg-transparent px-1.5 font-mono text-[13.5px] text-foreground outline-none transition-colors placeholder:font-sans placeholder:text-muted-foreground hover:border-border-strong focus-visible:border-primary-accent focus-visible:bg-background focus-visible:ring-2 focus-visible:ring-primary-soft disabled:cursor-not-allowed disabled:opacity-60 disabled:hover:border-transparent aria-[invalid=true]:border-destructive-accent sm:h-8'
export const CELL_TRIGGER = 'flex h-11 w-full min-w-0 cursor-pointer items-center gap-1 rounded-md border border-transparent px-1.5 text-left text-[13.5px] outline-none transition-colors hover:border-border-strong focus-visible:ring-2 focus-visible:ring-primary-accent disabled:cursor-not-allowed disabled:opacity-60 disabled:hover:border-transparent data-[state=open]:border-primary-accent data-[state=open]:bg-background sm:h-8'
// min-h-11 marks the row as the switch's touch target on a phone.
export const CELL_SWITCH = 'flex min-h-11 min-w-0 items-center gap-2 px-1.5 text-[13.5px] sm:min-h-8'
// Never wider than a narrow phone's screen.
export const CELL_POPOVER = 'grid w-80 max-w-[calc(100vw-2rem)] gap-3'

/** 「混合代理端口（mixed-port）」→「混合代理端口」; the full name stays in the help text. */
export const shortLabel = (label: string) => label.replace(/（[^（）]*）$/, '')
export const yamlKey = (label: string) => /（([^（）]*)）$/.exec(label)?.[1] ?? ''
export const cellHelp = (label: string, help?: string) => {
  const key = yamlKey(label)
  return help ? (key ? `YAML 键：${key}\n${help}` : help) : ''
}
