import { useState } from 'react'
import { ToolShell } from '@/components/tool/ToolShell'
import { cn } from '@/lib/utils'
import { meta } from './meta'
import { SslTab } from './tabs/SslTab'
import { DnsTab } from './tabs/DnsTab'
import { WhoisTab } from './tabs/WhoisTab'
import { PortTab } from './tabs/PortTab'

type Tab = 'ssl' | 'dns' | 'whois' | 'port'

export default function NetTools() {
  const [tab, setTab] = useState<Tab>('ssl')
  return (
    <ToolShell
      title={meta.title}
      description={meta.description}
      actions={
        <>
          <TabButton label="SSL 证书" active={tab === 'ssl'} onClick={() => setTab('ssl')} />
          <TabButton label="DNS" active={tab === 'dns'} onClick={() => setTab('dns')} />
          <TabButton label="WHOIS" active={tab === 'whois'} onClick={() => setTab('whois')} />
          <TabButton label="端口" active={tab === 'port'} onClick={() => setTab('port')} />
        </>
      }
    >
      <div className="mx-auto max-w-4xl">
        {tab === 'ssl' && <SslTab />}
        {tab === 'dns' && <DnsTab />}
        {tab === 'whois' && <WhoisTab />}
        {tab === 'port' && <PortTab />}
      </div>
    </ToolShell>
  )
}

function TabButton({ label, active, onClick }: { label: string; active: boolean; onClick: () => void }) {
  return (
    <button
      onClick={onClick}
      className={cn(
        'inline-flex h-8 items-center gap-1.5 rounded-md px-3 text-xs font-medium transition-colors',
        active
          ? 'bg-info/15 text-info'
          : 'text-muted-foreground hover:bg-accent hover:text-accent-foreground'
      )}
    >
      {label}
    </button>
  )
}
