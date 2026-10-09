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
    // 图片按 dataurl 收:挂整个设置页会把「关于」那一栏的 logo 引进来。
    // 设成 empty 的话 import 得到 undefined,<img src={undefined}> 在 jsdom 里会报警告
    loader: {
      '.tsx': 'tsx',
      '.ts': 'ts',
      '.json': 'json',
      '.css': 'empty',
      '.png': 'dataurl',
      '.jpg': 'dataurl',
      '.svg': 'dataurl',
    },
    plugins: [
      {
        name: 'smoke-stub',
        setup(b) {
          b.onResolve({ filter: /wailsjs/ }, () => ({ path: path.join(__dirname, 'stub.cjs') }))
          // noVNC 要真的 VNC 服务端,而且模块顶层有 await(打不进 Node 的单文件):换成假的
          b.onResolve({ filter: /^@novnc\/novnc$/ }, () => ({ path: path.join(__dirname, 'novnc-stub.cjs') }))
          b.onResolve({ filter: /^@\// }, (a) => {
            const p = path.join(SRC, a.path.slice(2))
            return { path: p + guessExt(p) }
          })
        },
      },
    ],
  })
  .catch(() => process.exit(1))
