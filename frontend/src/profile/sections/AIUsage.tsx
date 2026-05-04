import { useEffect, useState } from 'react'
import { ListAIProviders } from '../../../wailsjs/go/main/App'
import { UsagePane } from '@/tools/ai-chat/UsagePane'
import type { Provider } from '@/tools/ai-chat/types'

export function AIUsageSection() {
  const [providers, setProviders] = useState<Provider[]>([])
  useEffect(() => {
    void (async () => {
      const list = ((await ListAIProviders()) ?? []) as unknown as Provider[]
      setProviders(list)
    })()
  }, [])
  return (
    <div className="-m-6 flex h-[calc(100%+3rem)] min-w-0 flex-col">
      <UsagePane providers={providers} />
    </div>
  )
}
