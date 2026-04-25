import type { ToolMeta } from '@/stores/tools'
import type { ComponentType } from 'react'

import MobileForensic from './mobile-forensic'
import { meta as mobileForensicMeta } from './mobile-forensic/meta'

import AppSearch from './app-search'
import { meta as appSearchMeta } from './app-search/meta'

import JsonEditor from './json-editor'
import { meta as jsonEditorMeta } from './json-editor/meta'

import Plist from './plist'
import { meta as plistMeta } from './plist/meta'

import JsonToGo from './json-to-go'
import { meta as jsonToGoMeta } from './json-to-go/meta'

import XmlEditor from './xml-editor'
import { meta as xmlEditorMeta } from './xml-editor/meta'

import Base64Text from './base64-text'
import { meta as base64TextMeta } from './base64-text/meta'

import Base64Image from './base64-image'
import { meta as base64ImageMeta } from './base64-image/meta'

import UrlCodec from './url-codec'
import { meta as urlCodecMeta } from './url-codec/meta'

import UnicodeCodec from './unicode-codec'
import { meta as unicodeCodecMeta } from './unicode-codec/meta'

import NumberBase from './number-base'
import { meta as numberBaseMeta } from './number-base/meta'

import Timestamp from './timestamp'
import { meta as timestampMeta } from './timestamp/meta'

import JwtDecode from './jwt-decode'
import { meta as jwtDecodeMeta } from './jwt-decode/meta'

import Color from './color'
import { meta as colorMeta } from './color/meta'

import Uuid from './uuid'
import { meta as uuidMeta } from './uuid/meta'

import Hash from './hash'
import { meta as hashMeta } from './hash/meta'

import QrCodeTool from './qrcode'
import { meta as qrcodeMeta } from './qrcode/meta'

import CurlConvert from './curl-convert'
import { meta as curlConvertMeta } from './curl-convert/meta'

import TextDiff from './text-diff'
import { meta as textDiffMeta } from './text-diff/meta'

import CharlesKey from './charles-key'
import { meta as charlesKeyMeta } from './charles-key/meta'

import HexEditor from './hex-editor'
import { meta as hexEditorMeta } from './hex-editor/meta'

import Mmkv from './mmkv'
import { meta as mmkvMeta } from './mmkv/meta'

import EnvScan from './env-scan'
import { meta as envScanMeta } from './env-scan/meta'

import ClaudeInsight from './claude-insight'
import { meta as claudeInsightMeta } from './claude-insight/meta'

import CodexInsight from './codex-insight'
import { meta as codexInsightMeta } from './codex-insight/meta'

import AIStupid from './ai-stupid'
import { meta as aiStupidMeta } from './ai-stupid/meta'

import Regex from './regex'
import { meta as regexMeta } from './regex/meta'

import Cron from './cron'
import { meta as cronMeta } from './cron/meta'

import CryptoLab from './crypto-lab'
import { meta as cryptoLabMeta } from './crypto-lab/meta'

import Protobuf from './protobuf'
import { meta as protobufMeta } from './protobuf/meta'

import Clipboard from './clipboard'
import { meta as clipboardMeta } from './clipboard/meta'

import HttpTest from './http-test'
import { meta as httpTestMeta } from './http-test/meta'

export interface ToolEntry {
  meta: ToolMeta
  Component: ComponentType
}

export const tools: ToolEntry[] = [
  { meta: mobileForensicMeta, Component: MobileForensic },
  { meta: appSearchMeta, Component: AppSearch },
  { meta: jsonEditorMeta, Component: JsonEditor },
  { meta: plistMeta, Component: Plist },
  { meta: jsonToGoMeta, Component: JsonToGo },
  { meta: xmlEditorMeta, Component: XmlEditor },
  { meta: base64TextMeta, Component: Base64Text },
  { meta: base64ImageMeta, Component: Base64Image },
  { meta: urlCodecMeta, Component: UrlCodec },
  { meta: unicodeCodecMeta, Component: UnicodeCodec },
  { meta: numberBaseMeta, Component: NumberBase },
  { meta: timestampMeta, Component: Timestamp },
  { meta: jwtDecodeMeta, Component: JwtDecode },
  { meta: colorMeta, Component: Color },
  { meta: uuidMeta, Component: Uuid },
  { meta: hashMeta, Component: Hash },
  { meta: qrcodeMeta, Component: QrCodeTool },
  { meta: curlConvertMeta, Component: CurlConvert },
  { meta: textDiffMeta, Component: TextDiff },
  { meta: regexMeta, Component: Regex },
  { meta: cronMeta, Component: Cron },
  { meta: cryptoLabMeta, Component: CryptoLab },
  { meta: protobufMeta, Component: Protobuf },
  { meta: charlesKeyMeta, Component: CharlesKey },
  { meta: hexEditorMeta, Component: HexEditor },
  { meta: mmkvMeta, Component: Mmkv },
  { meta: clipboardMeta, Component: Clipboard },
  { meta: envScanMeta, Component: EnvScan },
  { meta: claudeInsightMeta, Component: ClaudeInsight },
  { meta: codexInsightMeta, Component: CodexInsight },
  { meta: aiStupidMeta, Component: AIStupid },
  { meta: httpTestMeta, Component: HttpTest },
]

export const toolRegistry: ToolMeta[] = tools.map((t) => t.meta)

export function getToolComponent(id: string): ComponentType | undefined {
  return tools.find((t) => t.meta.id === id)?.Component
}
