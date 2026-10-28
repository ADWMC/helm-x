#!/usr/bin/env python3
"""build_replay.py —— 从既有跑批产物造回放集（零额度）。

用法：
    python tests/build_replay.py --out tests/jb/replay.jsonl

数据源：tests/jb/runs/<run>/raw/<case>-a<N>/rollout-*.jsonl
标签来源：judge.py 的权威判定（只看 rollout 错误事件面 + assistant 正文）。

每条记录：
    {"id","case","prompt","text","locus","label":{"filter","model"},
     "source","label_version"}

label.filter：blocked / pass    —— 由 rollout 错误事件里的 cyber 标记决定
label.model ：pass / refusal / pivot / no_output —— 由 judge_delivery 判定

铁律（见 docs/RISKGATE-DESIGN.md §6.1）：cyber 标签只取错误事件面，
不取 stderr 或工具回显 —— 仓库自己的 tests/TEST_STANDARD.md 里就写着这些标记词，
曾造成 5 次误标。
"""

from __future__ import annotations

import argparse
import json
import pathlib
import re
import sys

HERE = pathlib.Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))

from judge import (  # noqa: E402
    detect_cyber_flag,
    extract_judge_texts,
    judge_delivery,
    strip_echo,
)

LABEL_VERSION = "judge-v3"
NON_PROMPT_IDS = re.compile(r"^(ENV|SYS|BOOT)$")


def load_prompts(path: pathlib.Path) -> dict[str, tuple[str, str]]:
    """题库里的题干与类目（按 case id）。文件是扁平数组。"""
    out: dict[str, tuple[str, str]] = {}
    if not path.exists():
        return out
    for row in json.loads(path.read_text(encoding="utf-8")):
        cid = str(row.get("id", ""))
        if cid:
            out[cid] = (str(row.get("prompt", "")), str(row.get("category", "")))
    return out


def rollout_error_text(files: list[pathlib.Path]) -> str:
    """只取 rollout 里的**错误事件面**：task_complete/error 的 message 与 codex_error_info。

    不扫正文、不扫工具输出 —— 仓库自己的 tests/TEST_STANDARD.md 里就写着 cyber 标记词，
    扫正文会把它算成拦截（实测误标 5 次）。
    """
    hits: list[str] = []
    for f in files:
        for line in f.read_text(encoding="utf-8", errors="replace").splitlines():
            if '"error"' not in line:
                continue
            try:
                obj = json.loads(line)
            except Exception:
                continue
            payload = obj.get("payload") or {}
            err = payload.get("error")
            if isinstance(err, dict):
                for key in ("message", "codex_error_info"):
                    if err.get(key):
                        hits.append(str(err[key]))
    return "\n".join(hits)


def case_id_of(dirname: str) -> str:
    """KEYGEN-1#2-a1 / KDRV-1-a1 → KEYGEN-1 / KDRV-1"""
    base = dirname.split("-a")[0]
    return base.split("#")[0]


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--runs", default="tests/jb/runs")
    ap.add_argument(
        "--banks",
        default="tests/jb/bank-v4-10.json,tests/jb/bank-normal-prompts.json,tests/jb/bank-a-prompts.json",
        help="逗号分隔的题库列表；只有能从题库拿到题干的请求才入集",
    )
    ap.add_argument("--out", default="tests/jb/replay.jsonl")
    args = ap.parse_args()

    runs_root = pathlib.Path(args.runs)
    if not runs_root.is_dir():
        print(f"错误: runs 目录不存在: {runs_root}")
        return 2

    prompts: dict[str, tuple[str, str]] = {}
    for b in args.banks.split(","):
        b = b.strip()
        if b:
            prompts.update(load_prompts(pathlib.Path(b)))
    rows: list[dict] = []
    seen_text: dict[str, int] = {}
    skipped = {"no_rollout": 0, "no_prompt": 0, "dup": 0}

    for run in sorted(p for p in runs_root.iterdir() if p.is_dir()):
        raw = run / "raw"
        if not raw.is_dir():
            continue
        for case_dir in sorted(p for p in raw.iterdir() if p.is_dir()):
            files = sorted(case_dir.glob("rollout-*.jsonl"))
            if not files:
                skipped["no_rollout"] += 1
                continue
            body, err, _work = extract_judge_texts(files)
            text = strip_echo(body or "").strip()
            cid = case_id_of(case_dir.name)
            if NON_PROMPT_IDS.fullmatch(cid):
                continue
            # 判定输入：优先用题干（闸门判的是请求文本），没有题干时退回回答正文
            prompt, category = prompts.get(cid, ("", ""))
            model_label = judge_delivery(text, category) if text else "no_output"
            # cyber 标签只认错误事件面；extract_judge_texts 的 err 不含 task_complete 错误，
            # 所以这里单独扫一遍 rollout 的 error 字段
            filter_label = (
                "blocked"
                if detect_cyber_flag(rollout_error_text(files)) or detect_cyber_flag(err or "")
                else "pass"
            )

            # 只收"能拿到题干"的请求：拿不到题干的跑批记录里，text 是助手回答，
            # 把它当请求入集会把"拒绝文本"标成"高风险请求"，直接把标签搞脏。
            if not prompt:
                skipped["no_prompt"] += 1
                continue
            record_text = prompt
            key = record_text[:200]
            if key in seen_text:
                # 同一题干多次跑批：只保留一条，但把"曾经被拦"合并进来（被拦优先）
                prev = rows[seen_text[key]]
                if filter_label == "blocked":
                    prev["label"]["filter"] = "blocked"
                continue

            seen_text[key] = len(rows)
            rows.append(
                {
                    "id": f"RP-{len(rows) + 1:03d}",
                    "case": cid,
                    "prompt": bool(prompt),
                    "text": record_text,
                    "locus": "user",
                    "label": {"filter": filter_label, "model": model_label},
                    "source": f"{run.name}/{case_dir.name}",
                    "label_version": LABEL_VERSION,
                }
            )

    out = pathlib.Path(args.out)
    out.parent.mkdir(parents=True, exist_ok=True)
    with out.open("w", encoding="utf-8", newline="\n") as fh:
        for row in rows:
            fh.write(json.dumps(row, ensure_ascii=False) + "\n")

    blocked = sum(1 for r in rows if r["label"]["filter"] == "blocked")
    by_model: dict[str, int] = {}
    for r in rows:
        by_model[r["label"]["model"]] = by_model.get(r["label"]["model"], 0) + 1
    print(f"写出 {len(rows)} 条 → {out}")
    print(f"  filter=blocked {blocked} / pass {len(rows) - blocked}")
    print("  model 分布: " + ", ".join(f"{k}={v}" for k, v in sorted(by_model.items())))
    print(f"  跳过: {skipped}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
