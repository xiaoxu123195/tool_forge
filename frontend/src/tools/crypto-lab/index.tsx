import { useState } from 'react'
import { ToolShell } from '@/components/tool/ToolShell'
import { cn } from '@/lib/utils'
import { meta } from './meta'
import { AesTab } from './tabs/AesTab'
import { RsaTab } from './tabs/RsaTab'
import { Sm2Tab } from './tabs/Sm2Tab'
import { Sm4Tab } from './tabs/Sm4Tab'

type Tab = 'aes' | 'rsa' | 'sm2' | 'sm4'

export default function CryptoLabTool() {
  const [tab, setTab] = useState<Tab>('aes')

  return (
    <ToolShell
      title={meta.title}
      description={meta.description}
      actions={
        <>
          <TabButton label="AES / ChaCha" active={tab === 'aes'} onClick={() => setTab('aes')} />
          <TabButton label="RSA" active={tab === 'rsa'} onClick={() => setTab('rsa')} />
          <TabButton label="SM2" active={tab === 'sm2'} onClick={() => setTab('sm2')} />
          <TabButton label="SM4" active={tab === 'sm4'} onClick={() => setTab('sm4')} />
        </>
      }
    >
      <div className="mx-auto max-w-4xl">
        {tab === 'aes' && <AesTab />}
        {tab === 'rsa' && <RsaTab />}
        {tab === 'sm2' && <Sm2Tab />}
        {tab === 'sm4' && <Sm4Tab />}
      </div>
    </ToolShell>
  )
}

function TabButton({
  label,
  active,
  onClick,
}: {
  label: string
  active: boolean
  onClick: () => void
}) {
  return (
    <button
      onClick={onClick}
      className={cn(
        'inline-flex h-8 items-center gap-1.5 rounded-md px-3 text-xs font-medium transition-colors',
        active
          ? 'bg-info/15 text-info'
          : 'text-muted-foreground hover:bg-accent hover:text-accent-foreground',
      )}
    >
      {label}
    </button>
  )
}
