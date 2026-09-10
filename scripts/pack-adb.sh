#!/usr/bin/env bash
# 把 adb 打成内置载荷,放进 backend/tools/adbx/bundled/。
#
# 用法:scripts/pack-adb.sh <platform-tools 目录>
#   例:scripts/pack-adb.sh ~/.toolforge/platform-tools
#
# 只取三个文件:adb.exe 和它要在同目录找到的两个 DLL。实测这三个就能跑,
# 整包里的 fastboot / sqlite3 / mke2fs / etc1tool 我们一个都不用 ——
# 带上只是让每个用户多下十几 MB。
set -euo pipefail

src="${1:-}"
if [ -z "$src" ] || [ ! -d "$src" ]; then
  echo "用法: $0 <platform-tools 目录>" >&2
  exit 1
fi

root="$(cd "$(dirname "$0")/.." && pwd)"
out="$root/backend/tools/adbx/bundled/platform-tools.tar.gz"

files=(adb.exe AdbWinApi.dll AdbWinUsbApi.dll)
for f in "${files[@]}"; do
  if [ ! -f "$src/$f" ]; then
    echo "缺少 $src/$f" >&2
    exit 1
  fi
done

# 平铺,不带目录层级 —— adb.exe 是在自己旁边找那两个 DLL 的
tar -C "$src" -cf - "${files[@]}" | gzip -9 > "$out"

echo "已生成 $out"
ls -l "$out" | awk '{printf "  %.2f MB\n", $5/1024/1024}'
echo "版本:"
"$src/adb.exe" version 2>/dev/null | head -2 | sed 's/^/  /'
