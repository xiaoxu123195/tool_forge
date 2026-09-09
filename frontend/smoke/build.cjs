// 把 probe.tsx 打成能在 Node 里跑的单文件。
// 所有 wailsjs 导入都被指到 stub.cjs;@ 别名手工解析(esbuild 不读 vite 配置)。
const path = require('path')
const fs = require('fs')
const esbuild = require('esbuild')

const SRC = path.resolve(__dirname, '../src')

function guessExt(p) {
  for (const e of ['', '.tsx', '.ts', '.jsx', '.js']) {
    if (fs.existsSync(p + e) && fs.statSync(p + e).isFile()) return e
  }
  for (const e of ['/index.tsx', '/index.ts']) {
    if (fs.existsSync(p + e)) return e
  }
  return ''
}

esbuild
  .build({
    entryPoints: [path.join(__dirname, 'probe.tsx')],
    bundle: true,
    platform: 'node',
    format: 'cjs',
    jsx: 'automatic',
    outfile: path.join(__dirname, '.out.cjs'),
    logLevel: 'error',
    loader: { '.tsx': 'tsx', '.ts': 'ts', '.json': 'json', '.css': 'empty' },
    plugins: [
      {
        name: 'smoke-stub',
        setup(b) {
          b.onResolve({ filter: /wailsjs/ }, () => ({ path: path.join(__dirname, 'stub.cjs') }))
          b.onResolve({ filter: /^@\// }, (a) => {
            const p = path.join(SRC, a.path.slice(2))
            return { path: p + guessExt(p) }
          })
        },
      },
    ],
  })
  .catch(() => process.exit(1))
