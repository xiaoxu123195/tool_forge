import { useState } from 'react'
import { Key } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { DataField, ErrorBanner } from '../ui'
import { fromBytes, toBytes, type DataEncoding } from '../lib/encoding'
import {
  sm2Decrypt,
  sm2Encrypt,
  sm2GenerateKeyPair,
  sm2Sign,
  sm2Verify,
} from '../lib/sm'

type Op = 'encrypt' | 'decrypt' | 'sign' | 'verify'

export function Sm2Tab() {
  const [op, setOp] = useState<Op>('encrypt')
  const [cipherMode, setCipherMode] = useState<'C1C3C2' | 'C1C2C3'>('C1C3C2')
  const [publicKey, setPublicKey] = useState('')
  const [privateKey, setPrivateKey] = useState('')
  const [input, setInput] = useState('你好 国密')
  const [inputEnc, setInputEnc] = useState<DataEncoding>('utf8')
  const [signature, setSignature] = useState('')
  const [output, setOutput] = useState('')
  const [outputEnc, setOutputEnc] = useState<DataEncoding>('hex')
  const [error, setError] = useState('')
  const [verify, setVerify] = useState<null | boolean>(null)
  const [userId, setUserId] = useState('1234567812345678')

  const genKey = () => {
    try {
      const kp = sm2GenerateKeyPair()
      setPublicKey(kp.publicKey)
      setPrivateKey(kp.privateKey)
      setError('')
    } catch (e: any) {
      setError(e?.message ?? String(e))
    }
  }

  const execute = () => {
    setError('')
    setVerify(null)
    try {
      if (op === 'encrypt') {
        const ct = sm2Encrypt(publicKey, toBytes(input, inputEnc), cipherMode)
        setOutput(fromBytes(ct, outputEnc))
      } else if (op === 'decrypt') {
        const pt = sm2Decrypt(privateKey, toBytes(input, inputEnc), cipherMode)
        setOutput(fromBytes(pt, outputEnc))
      } else if (op === 'sign') {
        const sig = sm2Sign(privateKey, toBytes(input, inputEnc), {
          publicKey,
          userId,
          hash: true,
          der: false,
        })
        setOutput(sig)
      } else if (op === 'verify') {
        const ok = sm2Verify(publicKey, toBytes(input, inputEnc), signature, {
          userId,
          hash: true,
          der: false,
        })
        setVerify(ok)
        setOutput(ok ? '✅ 签名有效' : '❌ 签名无效')
      }
    } catch (e: any) {
      setError(e?.message ?? String(e))
    }
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-2">
        <OpSelect value={op} onChange={setOp} />
        {(op === 'encrypt' || op === 'decrypt') && (
          <>
            <span className="text-xs text-muted-foreground">密文格式</span>
            <select
              value={cipherMode}
              onChange={(e) => setCipherMode(e.target.value as any)}
              className="rounded-md border border-border bg-background px-2 py-1 text-xs outline-none"
            >
              <option value="C1C3C2">C1C3C2（国密推荐）</option>
              <option value="C1C2C3">C1C2C3</option>
            </select>
          </>
        )}
        {(op === 'sign' || op === 'verify') && (
          <>
            <span className="text-xs text-muted-foreground">UserID</span>
            <input
              value={userId}
              onChange={(e) => setUserId(e.target.value)}
              className="w-44 rounded-md border border-border bg-background px-2 py-1 font-mono text-xs outline-none"
            />
          </>
        )}
        <Button size="sm" variant="ghost" onClick={genKey} className="ml-auto">
          <Key className="h-3.5 w-3.5" />
          生成密钥对
        </Button>
      </div>

      <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
        <HexField
          label="公钥（130 hex，含 04 前缀；也可接受 128 hex）"
          value={publicKey}
          onChange={setPublicKey}
          required={op === 'encrypt' || op === 'verify'}
        />
        <HexField
          label="私钥（64 hex）"
          value={privateKey}
          onChange={setPrivateKey}
          required={op === 'decrypt' || op === 'sign'}
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
        <div className="flex flex-col gap-1">
          <span className="text-xs text-muted-foreground">签名（hex）</span>
          <textarea
            value={signature}
            onChange={(e) => setSignature(e.target.value)}
            rows={2}
            spellCheck={false}
            className="rounded-md border border-border bg-background px-3 py-2 font-mono text-xs outline-none focus:border-primary/50"
          />
        </div>
      )}

      <div className="flex items-center justify-end">
        <Button size="sm" onClick={execute}>
          执行
        </Button>
      </div>

      <ErrorBanner error={error} />

      {verify != null ? (
        <div
          className={`rounded-md border px-3 py-2 text-sm ${
            verify
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
          enc={op === 'sign' ? 'hex' : outputEnc}
          onEnc={setOutputEnc}
          rows={4}
          allowEnc={op === 'sign' ? ['hex'] : ['utf8', 'hex', 'base64']}
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

function HexField({
  label,
  value,
  onChange,
  required,
}: {
  label: string
  value: string
  onChange: (v: string) => void
  required?: boolean
}) {
  return (
    <div className="flex flex-col gap-1">
      <span className="text-xs text-muted-foreground">
        {label}
        {required && <span className="ml-1 text-amber-500">*</span>}
      </span>
      <input
        value={value}
        onChange={(e) => onChange(e.target.value.replace(/\s+/g, ''))}
        spellCheck={false}
        className="rounded-md border border-border bg-background px-3 py-2 font-mono text-[11px] outline-none focus:border-primary/50"
      />
    </div>
  )
}
