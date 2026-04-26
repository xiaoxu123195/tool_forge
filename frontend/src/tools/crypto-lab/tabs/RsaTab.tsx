import { useState } from 'react'
import { Key } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { DataField, ErrorBanner } from '../ui'
import { fromBytes, toBytes, type DataEncoding } from '../lib/encoding'
import {
  pemInfo,
  rsaGenerateKeyPair,
  rsaOaepDecrypt,
  rsaOaepEncrypt,
  rsaPssSign,
  rsaPssVerify,
  type RsaHash,
} from '../lib/rsa'

type Op = 'encrypt' | 'decrypt' | 'sign' | 'verify'

export function RsaTab() {
  const [op, setOp] = useState<Op>('encrypt')
  const [hash, setHash] = useState<RsaHash>('SHA-256')
  const [publicPem, setPublicPem] = useState('')
  const [privatePem, setPrivatePem] = useState('')

  const [input, setInput] = useState('hello rsa')
  const [inputEnc, setInputEnc] = useState<DataEncoding>('utf8')
  const [signature, setSignature] = useState('')
  const [signatureEnc, setSignatureEnc] = useState<DataEncoding>('base64')
  const [output, setOutput] = useState('')
  const [outputEnc, setOutputEnc] = useState<DataEncoding>('base64')

  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [verifyResult, setVerifyResult] = useState<null | boolean>(null)

  const [bits, setBits] = useState<2048 | 3072 | 4096>(2048)

  const genKey = async () => {
    setError('')
    setBusy(true)
    try {
      const kp = await rsaGenerateKeyPair(bits)
      setPublicPem(kp.publicKeyPem)
      setPrivatePem(kp.privateKeyPem)
    } catch (e: any) {
      setError(e?.message ?? String(e))
    } finally {
      setBusy(false)
    }
  }

  const execute = async () => {
    setError('')
    setVerifyResult(null)
    setBusy(true)
    try {
      if (op === 'encrypt') {
        const plain = toBytes(input, inputEnc)
        const ct = await rsaOaepEncrypt(publicPem, hash, plain)
        setOutput(fromBytes(ct, outputEnc))
      } else if (op === 'decrypt') {
        const ct = toBytes(input, inputEnc)
        const pt = await rsaOaepDecrypt(privatePem, hash, ct)
        setOutput(fromBytes(pt, outputEnc))
      } else if (op === 'sign') {
        const data = toBytes(input, inputEnc)
        const sig = await rsaPssSign(privatePem, hash, data)
        setOutput(fromBytes(sig, outputEnc))
      } else if (op === 'verify') {
        const data = toBytes(input, inputEnc)
        const sig = toBytes(signature, signatureEnc)
        const ok = await rsaPssVerify(publicPem, hash, data, sig)
        setVerifyResult(ok)
        setOutput(ok ? '✅ 签名有效' : '❌ 签名无效')
      }
    } catch (e: any) {
      setError(e?.message ?? String(e))
    } finally {
      setBusy(false)
    }
  }

  const pubInfo = safePemInfo(publicPem)
  const privInfo = safePemInfo(privatePem)

  return (
    <div className="flex flex-col gap-3">
      {/* 操作 + 哈希 */}
      <div className="flex flex-wrap items-center gap-2">
        <OpSelect value={op} onChange={setOp} />
        <span className="text-xs text-muted-foreground">哈希</span>
        <select
          value={hash}
          onChange={(e) => setHash(e.target.value as RsaHash)}
          className="rounded-md border border-border bg-background px-2 py-1 text-xs outline-none"
        >
          <option value="SHA-256">SHA-256</option>
          <option value="SHA-384">SHA-384</option>
          <option value="SHA-512">SHA-512</option>
        </select>
        <span className="text-[10px] text-muted-foreground">
          {op === 'encrypt' || op === 'decrypt'
            ? 'RSA-OAEP'
            : 'RSA-PSS（saltLength=hash 长度）'}
        </span>

        <div className="ml-auto flex items-center gap-1">
          <select
            value={bits}
            onChange={(e) => setBits(parseInt(e.target.value, 10) as 2048 | 3072 | 4096)}
            className="rounded-md border border-border bg-background px-2 py-1 text-xs outline-none"
          >
            <option value={2048}>2048</option>
            <option value={3072}>3072</option>
            <option value={4096}>4096</option>
          </select>
          <Button size="sm" variant="ghost" onClick={genKey} disabled={busy}>
            <Key className="h-3.5 w-3.5" />
            生成密钥对
          </Button>
        </div>
      </div>

      <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
        <PemField
          label="公钥 PEM"
          value={publicPem}
          onChange={setPublicPem}
          info={pubInfo}
          needed={op === 'encrypt' || op === 'verify'}
        />
        <PemField
          label="私钥 PEM"
          value={privatePem}
          onChange={setPrivatePem}
          info={privInfo}
          needed={op === 'decrypt' || op === 'sign'}
        />
      </div>

      <DataField
        label={
          op === 'encrypt'
            ? '明文输入'
            : op === 'decrypt'
              ? '密文输入'
              : op === 'sign'
                ? '待签数据'
                : '原始数据'
        }
        value={input}
        onChange={setInput}
        enc={inputEnc}
        onEnc={setInputEnc}
        rows={4}
      />

      {op === 'verify' && (
        <DataField
          label="签名"
          value={signature}
          onChange={setSignature}
          enc={signatureEnc}
          onEnc={setSignatureEnc}
          rows={2}
        />
      )}

      <div className="flex items-center justify-end">
        <Button size="sm" onClick={execute} disabled={busy}>
          执行
        </Button>
      </div>

      <ErrorBanner error={error} />

      {verifyResult != null ? (
        <div
          className={`rounded-md border px-3 py-2 text-sm ${
            verifyResult
              ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300'
              : 'border-red-500/30 bg-red-500/10 text-red-700 dark:text-red-300'
          }`}
        >
          {output}
        </div>
      ) : (
        <DataField
          label="输出"
          value={output}
          onChange={() => {}}
          readOnly
          enc={outputEnc}
          onEnc={setOutputEnc}
          rows={4}
        />
      )}
    </div>
  )
}

function OpSelect({ value, onChange }: { value: Op; onChange: (v: Op) => void }) {
  const list: Array<{ v: Op; label: string }> = [
    { v: 'encrypt', label: '加密' },
    { v: 'decrypt', label: '解密' },
    { v: 'sign', label: '签名' },
    { v: 'verify', label: '验签' },
  ]
  return (
    <div className="inline-flex items-center rounded-md border border-border bg-background p-0.5">
      {list.map((o) => (
        <button
          key={o.v}
          onClick={() => onChange(o.v)}
          className={
            'rounded-sm px-3 py-1 text-xs font-medium ' +
            (value === o.v
              ? 'bg-info text-white'
              : 'text-muted-foreground hover:text-foreground')
          }
        >
          {o.label}
        </button>
      ))}
    </div>
  )
}

function PemField({
  label,
  value,
  onChange,
  info,
  needed,
}: {
  label: string
  value: string
  onChange: (v: string) => void
  info: { label: string; sizeBytes: number } | null
  needed: boolean
}) {
  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-center justify-between">
        <span className="text-xs text-muted-foreground">
          {label}
          {needed && <span className="ml-1 text-amber-500">*</span>}
        </span>
        {info ? (
          <span className="font-mono text-[10px] text-emerald-600 dark:text-emerald-400">
            {info.label} · {info.sizeBytes}B
          </span>
        ) : value ? (
          <span className="font-mono text-[10px] text-red-500">非合法 PEM</span>
        ) : (
          <span className="font-mono text-[10px] text-muted-foreground">（空）</span>
        )}
      </div>
      <textarea
        value={value}
        onChange={(e) => onChange(e.target.value)}
        rows={5}
        placeholder="-----BEGIN PUBLIC KEY-----..."
        spellCheck={false}
        className="rounded-md border border-border bg-background px-3 py-2 font-mono text-[11px] outline-none focus:border-primary/50"
      />
    </div>
  )
}

function safePemInfo(pem: string) {
  if (!pem.trim()) return null
  try {
    return pemInfo(pem)
  } catch {
    return null
  }
}
