import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { AlertCircle, CheckCircle2, Cpu, RefreshCw, Settings } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { CheckForensic } from '../../../wailsjs/go/main/App'
import { useForensicStore } from '@/stores/forensic'
import type { forensic } from '../../../wailsjs/go/models'

interface Props {
  onReady: (info: forensic.Info) => void
  /**
   * 一键换回内置引擎。
   *
   * Android 上必须给这条路:内置引擎根本不需要 go-forensic,
   * 要是没装就把人堵在这一页、还只给"去配置"一个出口,那才是真的没路走。
   */
  onUseBuiltin?: () => void
}

export function SetupGuide({ onReady, onUseBuiltin }: Props) {
  const binaryPath = useForensicStore((s) => s.binaryPath)
  const cache = useForensicStore((s) => s.checkCache)
  const setCheckCache = useForensicStore((s) => s.setCheckCache)

  const [info, setInfo] = useState<forensic.Info | null>(null)
  const [checking, setChecking] = useState(false)

  const applyInfo = (i: forensic.Info) => {
    setInfo(i)
    if (i.found) onReady(i)
  }

  const doCheck = async (force: boolean) => {
    // 使用缓存：当路径未变且已检测过，且非强制刷新
    if (!force && cache && cache.forPath === binaryPath) {
      applyInfo({
        found: cache.found,
        path: cache.resolvedPath,
        version: cache.version,
        error: cache.error || undefined,
      } as forensic.Info)
      return
    }
    setChecking(true)
    const result = await CheckForensic(binaryPath)
    setCheckCache({
      forPath: binaryPath,
      found: result.found,
      resolvedPath: result.path,
      version: result.version,
      error: result.error ?? '',
      at: Date.now(),
    })
    applyInfo(result)
    setChecking(false)
  }

  useEffect(() => {
    doCheck(false)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return (
    <div className="space-y-4">
      <div className="rounded-lg border border-amber-500/40 bg-amber-500/5 p-5">
        <h2 className="text-base font-semibold">这次的选择需要 go-forensic</h2>
        <p className="mt-1 text-sm text-muted-foreground">
          iOS 取证、以及 Android 选了{' '}
          <code className="rounded bg-muted px-1 py-0.5 text-xs">go-forensic</code>{' '}
          引擎时，要用到这个外部可执行文件；请在设置中指定它的位置。
          Android 的<strong className="font-medium text-foreground">内置引擎不需要它</strong>。
        </p>

        <div className="mt-5 rounded-md border border-dashed border-border p-4">
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            {checking ? (
              <>
                <RefreshCw className="h-3.5 w-3.5 animate-spin" />
                <span>正在检测…</span>
              </>
            ) : info?.found ? (
              <>
                <CheckCircle2 className="h-3.5 w-3.5 text-emerald-500" />
                <span>已找到 · {info.path}</span>
              </>
            ) : (
              <>
                <AlertCircle className="h-3.5 w-3.5 text-destructive" />
                <span>{info?.error || '未找到 go-forensic'}</span>
              </>
            )}
          </div>
        </div>

        <div className="mt-5 flex flex-wrap gap-2">
          {onUseBuiltin && (
            <Button size="sm" onClick={onUseBuiltin}>
              <Cpu className="h-3.5 w-3.5" />
              改用内置引擎
            </Button>
          )}
          <Button asChild size="sm" variant={onUseBuiltin ? 'outline' : 'default'}>
            <Link to="/profile">
              <Settings className="h-3.5 w-3.5" />
              去配置
            </Link>
          </Button>
          <Button variant="outline" size="sm" onClick={() => doCheck(true)} disabled={checking}>
            <RefreshCw className={checking ? 'h-3.5 w-3.5 animate-spin' : 'h-3.5 w-3.5'} />
            重新检测
          </Button>
        </div>

        <details className="mt-5 text-xs text-muted-foreground">
          <summary className="cursor-pointer select-none">如何获取 go-forensic？</summary>
          <div className="mt-2 space-y-1.5 pl-2">
            <p>方案 A：本地有 Go 环境，可直接安装到 PATH</p>
            <pre className="rounded bg-muted p-2 font-mono text-[11px]">go install gitlab.forensix.cn/GoldenEyes/mobile-forensic/fastplugindev/go-forensic@latest</pre>
            <p>方案 B：直接指定已编译好的 exe 路径（推荐）</p>
            <p>在 Profile → 外部工具中填入 <code>go-forensic.exe</code> 的完整路径即可。</p>
          </div>
        </details>
      </div>
    </div>
  )
}
