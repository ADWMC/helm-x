#!/usr/bin/env python3
"""contrast.py —— WCAG 2.1 对比度审计（OKLCH / HEX 双输入，支持前景 alpha 合成）。

用法：
    python tools/contrast.py                      # 审计内置的现行主题 + 候选主题
    python tools/contrast.py --check "#1f2328" "#f7f8fa"
    python tools/contrast.py --fg oklch:93%,0.004,60 --bg oklch:12%,0.006,60

判定（WCAG 2.1 AA）：
    正文 < 18pt           ≥ 4.5 : 1
    大字 ≥ 18pt / 粗体 14pt ≥ 3.0 : 1
    非文字 UI（边框/图标）  ≥ 3.0 : 1
退出码：0 全部达标；1 有未达标项（供 CI / go generate 断言用）。
"""

from __future__ import annotations

import argparse
import math
import re
import sys


# ── 色彩空间 ────────────────────────────────────────────────────────────────

def oklch_to_srgb(L: float, C: float, h_deg: float) -> tuple[float, float, float]:
    """OKLCH → sRGB（0–1，已做 gamma），越界裁剪。"""
    h = math.radians(h_deg)
    a, b = C * math.cos(h), C * math.sin(h)

    l_ = L + 0.3963377774 * a + 0.2158037573 * b
    m_ = L - 0.1055613458 * a - 0.0638541728 * b
    s_ = L - 0.0894841775 * a - 1.2914855480 * b
    l, m, s = l_**3, m_**3, s_**3

    r = +4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s
    g = -1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s
    bl = -0.0041960863 * l - 0.7034186147 * m + 1.7076147010 * s

    def gamma(x: float) -> float:
        x = min(1.0, max(0.0, x))
        return 12.92 * x if x <= 0.0031308 else 1.055 * x ** (1 / 2.4) - 0.055

    return gamma(r), gamma(g), gamma(bl)


def parse_color(s: str) -> tuple[float, float, float]:
    """支持 #rrggbb / #rgb / oklch:L,C,H（L、C 可带 %）。"""
    s = s.strip()
    if s.startswith("oklch:"):
        body = s[len("oklch:"):]
        parts = [p.strip() for p in body.split(",")]
        if len(parts) != 3:
            raise ValueError(f"oklch 需要三段: {s}")
        L = float(parts[0].rstrip("%")) / (100 if parts[0].endswith("%") else 1)
        C = float(parts[1].rstrip("%")) / (100 if parts[1].endswith("%") else 1)
        H = float(parts[2].rstrip("deg"))
        return oklch_to_srgb(L, C, H)
    m = re.fullmatch(r"#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})", s)
    if not m:
        raise ValueError(f"无法解析颜色: {s}")
    h = m.group(1)
    if len(h) == 3:
        h = "".join(c * 2 for c in h)
    return tuple(int(h[i:i + 2], 16) / 255 for i in (0, 2, 4))  # type: ignore[return-value]


def relative_luminance(rgb: tuple[float, float, float]) -> float:
    def lin(c: float) -> float:
        return c / 12.92 if c <= 0.04045 else ((c + 0.055) / 1.055) ** 2.4
    r, g, b = (lin(c) for c in rgb)
    return 0.2126 * r + 0.7152 * g + 0.0722 * b


def blend(fg: tuple[float, float, float], bg: tuple[float, float, float], alpha: float):
    """把带 alpha 的前景合成到背景上（结果仍是显示色）。"""
    if alpha >= 1.0:
        return fg
    return tuple(alpha * f + (1 - alpha) * b for f, b in zip(fg, bg))


def contrast(fg: str, bg: str, alpha: float = 1.0) -> float:
    b = parse_color(bg)
    f = blend(parse_color(fg), b, alpha)
    l1, l2 = relative_luminance(f), relative_luminance(b)
    hi, lo = max(l1, l2), min(l1, l2)
    return (hi + 0.05) / (lo + 0.05)


def verdict(ratio: float, floor: float) -> str:
    return "PASS" if ratio >= floor else ("NEAR" if ratio >= floor * 0.9 else "FAIL")


# ── 审计用例 ────────────────────────────────────────────────────────────────
# (标签, 前景, 背景, alpha, 门槛)
# 现行主题取值见 frontend/src/style.css 的 @plugin "daisyui/theme" 块。

# floor=0.0 表示「信息项」：只报数值不判达标（装饰分隔线不适用 1.4.11 的 3:1）。
CURRENT_DARK = [
    ("body 93% on canvas 12%", "oklch:93%,0.004,60", "oklch:12%,0.006,60", 1.0, 4.5),
    ("body 93% on card 22%", "oklch:93%,0.004,60", "oklch:22%,0.008,60", 1.0, 4.5),
    ("meta /60 on canvas 12%", "oklch:93%,0.004,60", "oklch:12%,0.006,60", 0.60, 4.5),
    ("meta /30 on canvas 12% (App.vue 等)", "oklch:93%,0.004,60", "oklch:12%,0.006,60", 0.30, 4.5),
    ("meta /45 on canvas 12%", "oklch:93%,0.004,60", "oklch:12%,0.006,60", 0.45, 4.5),
    ("control border hairline 7% white", "#ffffff", "oklch:12%,0.006,60", 0.07, 3.0),
    ("divider 28% vs canvas 12%", "oklch:28%,0.009,60", "oklch:12%,0.006,60", 1.0, 0.0),
    ("card 22% vs canvas 12% (separation)", "oklch:22%,0.008,60", "oklch:12%,0.006,60", 1.0, 0.0),
    ("primary 72% on canvas 12%", "oklch:72%,0.12,165", "oklch:12%,0.006,60", 1.0, 4.5),
    ("warning 80% on canvas 12%", "oklch:80%,0.11,80", "oklch:12%,0.006,60", 1.0, 4.5),
    ("error 70% on canvas 12%", "oklch:70%,0.14,25", "oklch:12%,0.006,60", 1.0, 4.5),
]

CANDIDATE_LIGHT = [
    ("body #1f2328 on canvas #f6f7f9", "#1f2328", "#f6f7f9", 1.0, 4.5),
    ("body #1f2328 on panel #ffffff", "#1f2328", "#ffffff", 1.0, 4.5),
    ("secondary #57606a on canvas", "#57606a", "#f6f7f9", 1.0, 4.5),
    ("secondary #57606a on panel", "#57606a", "#ffffff", 1.0, 4.5),
    ("muted #6e7781 on panel", "#6e7781", "#ffffff", 1.0, 4.5),
    ("control border #7d8590 on panel", "#7d8590", "#ffffff", 1.0, 3.0),
    ("divider #e3e7ec on panel", "#e3e7ec", "#ffffff", 1.0, 0.0),
    ("panel vs canvas (separation)", "#ffffff", "#f6f7f9", 1.0, 0.0),
    ("accent #1a5fb4 on panel", "#1a5fb4", "#ffffff", 1.0, 4.5),
    ("success #1a7f37 on panel", "#1a7f37", "#ffffff", 1.0, 4.5),
    ("warning #8a5a00 on panel", "#8a5a00", "#ffffff", 1.0, 4.5),
    ("danger #b42318 on panel", "#b42318", "#ffffff", 1.0, 4.5),
    ("selection #eaf1fb + body", "#1f2328", "#eaf1fb", 1.0, 4.5),
]

CANDIDATE_DARK = [
    ("body #e6e9ee on canvas #16181d", "#e6e9ee", "#16181d", 1.0, 4.5),
    ("body #e6e9ee on panel #1f232b", "#e6e9ee", "#1f232b", 1.0, 4.5),
    ("secondary #a7b0bd on panel", "#a7b0bd", "#1f232b", 1.0, 4.5),
    ("muted #8b95a3 on panel", "#8b95a3", "#1f232b", 1.0, 4.5),
    ("control border #6b7482 on panel", "#6b7482", "#1f232b", 1.0, 3.0),
    ("divider #2c313b on canvas", "#2c313b", "#16181d", 1.0, 0.0),
    ("panel vs canvas (separation)", "#1f232b", "#16181d", 1.0, 0.0),
    ("accent #79b8ff on panel", "#79b8ff", "#1f232b", 1.0, 4.5),
    ("success #57d38c on panel", "#57d38c", "#1f232b", 1.0, 4.5),
    ("warning #e5b567 on panel", "#e5b567", "#1f232b", 1.0, 4.5),
    ("danger #ff8a80 on panel", "#ff8a80", "#1f232b", 1.0, 4.5),
]


def audit(title: str, rows) -> int:
    print(f"\n=== {title} ===")
    print(f"{'case':<42}{'ratio':>7}  {'floor':>5}  verdict")
    bad = 0
    for label, fg, bg, alpha, floor in rows:
        r = contrast(fg, bg, alpha)
        if floor <= 0:
            v = "INFO"
        else:
            v = verdict(r, floor)
            if v != "PASS":
                bad += 1
        print(f"{label:<42}{r:>7.2f}  {floor:>5.1f}  {v}")
    return bad


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--check", nargs=2, metavar=("FG", "BG"))
    ap.add_argument("--fg")
    ap.add_argument("--bg")
    ap.add_argument("--theme", choices=["all", "current", "light", "dark"], default="all",
                    help="只审计某一套主题（CI 用 light/dark 断言达标）")
    a = ap.parse_args()

    if a.check:
        print(f"{contrast(a.check[0], a.check[1]):.2f}:1")
        return 0
    if a.fg and a.bg:
        print(f"{contrast(a.fg, a.bg):.2f}:1")
        return 0

    bad = 0
    if a.theme in ("all", "current"):
        bad += audit("现行深色主题（frontend/src/style.css）", CURRENT_DARK)
    if a.theme in ("all", "light"):
        bad += audit("候选浅色（JetBrains 风，浅色为默认）", CANDIDATE_LIGHT)
    if a.theme in ("all", "dark"):
        bad += audit("候选深色（可选，≥AA）", CANDIDATE_DARK)
    print(f"\n未达标 {bad} 项")
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main())
