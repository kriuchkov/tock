#!/usr/bin/env python3
"""Shift all dates in test_data so the latest date in each source becomes today - N days.

Usage: python3 scripts/refresh_test_data.py [--days N]  (default: 1)
"""

import argparse
import datetime
import re
from pathlib import Path

ROOT = Path(__file__).parent.parent
TXT = ROOT / "test_data/test_tock_data.txt"
CFG = ROOT / "test_data/test_config.yaml"
NOTES = ROOT / "test_data/.tock/notes"
TW_DIR = ROOT / "test_data/timewarrior"

parser = argparse.ArgumentParser()
parser.add_argument("--days", type=int, default=1, help="Target offset from today (default: 1)")
TARGET = datetime.date.today() - datetime.timedelta(days=parser.parse_args().days)


def make_shift_ymd(delta: datetime.timedelta):
    def shift(m: re.Match) -> str:
        return (datetime.date.fromisoformat(m.group(0)) + delta).strftime("%Y-%m-%d")
    return shift


def make_shift_tw_ts(delta: datetime.timedelta):
    def shift(m: re.Match) -> str:
        ts = m.group(0)
        d = datetime.date(int(ts[:4]), int(ts[4:6]), int(ts[6:8])) + delta
        return d.strftime("%Y%m%d")
    return shift


# --- File backend + config ---
txt = TXT.read_text()
txt_dates = re.findall(r"\d{4}-\d{2}-\d{2}", txt)
if txt_dates:
    latest_txt = max(txt_dates)
    delta_txt = TARGET - datetime.date.fromisoformat(latest_txt)
    TXT.write_text(re.sub(r"\d{4}-\d{2}-\d{2}", make_shift_ymd(delta_txt), txt))
    CFG.write_text(re.sub(r"\d{4}-\d{2}-\d{2}", make_shift_ymd(delta_txt), CFG.read_text()))
    print(f"[txt]   shifted by {delta_txt.days} days: {latest_txt} -> {TARGET}")

    # --- Notes directories ---
    if NOTES.is_dir():
        for d in sorted(NOTES.iterdir()):
            if re.match(r"\d{4}-\d{2}-\d{2}", d.name):
                d.rename(d.parent / (datetime.date.fromisoformat(d.name) + delta_txt).strftime("%Y-%m-%d"))

# --- Timewarrior ---
if TW_DIR.is_dir():
    tw_files = list(TW_DIR.glob("*.data"))
    tw_contents = {p: p.read_text() for p in tw_files}
    all_tw_dates = [
        f"{ts[:4]}-{ts[4:6]}-{ts[6:8]}"
        for content in tw_contents.values()
        for ts in re.findall(r"\d{8}(?=T\d{6}Z)", content)
    ]

    if all_tw_dates:
        latest_tw = max(all_tw_dates)
        delta_tw = TARGET - datetime.date.fromisoformat(latest_tw)
        shift_ts = make_shift_tw_ts(delta_tw)

        new_files: dict[str, list[str]] = {}
        for content in tw_contents.values():
            for line in content.splitlines(keepends=True):
                shifted_line = re.sub(r"\d{8}(?=T\d{6}Z)", shift_ts, line)
                m = re.search(r"(\d{8})T\d{6}Z", shifted_line)
                month_key = f"{m.group(1)[:4]}-{m.group(1)[4:6]}" if m else "unknown"
                new_files.setdefault(month_key, []).append(shifted_line)

        for p in tw_files:
            p.unlink()
        for month_key, lines in new_files.items():
            (TW_DIR / f"{month_key}.data").write_text("".join(lines))

        print(f"[tw]    shifted by {delta_tw.days} days: {latest_tw} -> {TARGET}")
