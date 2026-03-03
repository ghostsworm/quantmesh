#!/usr/bin/env python3
"""
补全 i18n locale 文件中缺失的键。
以 zh-CN 为参考，将缺失键从 zh-CN 或 en-US 复制到各 locale。
- zh-TW: 使用 zh-CN 的值
- 其他: 优先使用 en-US，若 en-US 无则用 zh-CN
"""

import json
import sys
from pathlib import Path


def get_by_path(obj: dict, path: str):
    """按 path (如 aiInterpret.analyzing) 获取值"""
    parts = path.split(".")
    cur = obj
    for p in parts:
        cur = cur.get(p)
        if cur is None:
            return None
    return cur


def set_by_path(obj: dict, path: str, value):
    """按 path 设置值，自动创建中间对象"""
    parts = path.split(".")
    cur = obj
    for i, p in enumerate(parts[:-1]):
        if p not in cur:
            cur[p] = {}
        cur = cur[p]
    cur[parts[-1]] = value


def extract_keys(obj: dict, prefix: str = "") -> set[str]:
    keys = set()
    for k, v in obj.items():
        path = f"{prefix}.{k}" if prefix else k
        if isinstance(v, dict) and v:
            keys.update(extract_keys(v, path))
        elif not isinstance(v, (dict, list)):
            keys.add(path)
    return keys


def load_locale(path: Path) -> dict | None:
    try:
        with open(path, encoding="utf-8") as f:
            return json.load(f)
    except (json.JSONDecodeError, OSError) as e:
        print(f"Warning: Failed to load {path}: {e}", file=sys.stderr)
        return None


def main():
    locales_dir = Path("webui/src/i18n/locales")
    if not locales_dir.is_dir():
        print(f"Error: {locales_dir} not found", file=sys.stderr)
        sys.exit(1)

    zh_cn = load_locale(locales_dir / "zh-CN.json")
    en_us = load_locale(locales_dir / "en-US.json")
    if zh_cn is None or en_us is None:
        sys.exit(1)

    ref_keys = extract_keys(zh_cn)
    en_us_keys = extract_keys(en_us)

    for fp in sorted(locales_dir.glob("*.json")):
        name = fp.stem
        if name == "zh-CN":
            continue
        data = load_locale(fp)
        if data is None:
            continue
        have = extract_keys(data)
        missing = ref_keys - have
        if not missing:
            print(f"{name}: OK")
            continue

        # 选择来源: zh-TW 用 zh-CN，其他优先 en-US
        source = zh_cn if name == "zh-TW" else en_us
        fallback = zh_cn

        added = 0
        for key in sorted(missing):
            val = get_by_path(source, key)
            if val is None:
                val = get_by_path(fallback, key)
            if val is not None:
                set_by_path(data, key, val)
                added += 1

        with open(fp, "w", encoding="utf-8") as f:
            json.dump(data, f, ensure_ascii=False, indent=2)
        print(f"{name}: 补全 {added}/{len(missing)} 个键")


if __name__ == "__main__":
    main()
