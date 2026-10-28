#!/usr/bin/env python3
"""Reverse byte-wise rounds from a static ELF target in .rodata.

Forward operations are supplied in execution order, for example:
  --round xor:0xA7 --round rol:3 --round add:0x31 --round xor:0xC3 --round xor:0x5A

The script reads bytes from .rodata and applies the inverse operations in reverse order.
It never loads or executes the ELF.
"""

from __future__ import annotations

import argparse
import sys
from pathlib import Path

from elftools.elf.elffile import ELFFile


MASK = 0xFF


def rol8(x: int, n: int) -> int:
    n &= 7
    return ((x << n) | (x >> (8 - n))) & MASK if n else x & MASK


def ror8(x: int, n: int) -> int:
    n &= 7
    return ((x >> n) | (x << (8 - n))) & MASK if n else x & MASK


def parse_int(value: str) -> int:
    return int(value, 0)


def parse_round(spec: str) -> tuple[str, int]:
    try:
        name, raw = spec.split(":", 1)
    except ValueError as exc:
        raise ValueError(f"bad --round {spec!r}; use op:value") from exc
    name = name.lower()
    if name not in {"xor", "add", "sub", "rol", "ror"}:
        raise ValueError(f"unsupported operation {name!r}")
    return name, parse_int(raw)


def invert_byte(value: int, op: tuple[str, int]) -> int:
    name, arg = op
    if name == "xor":
        return value ^ (arg & MASK)
    if name == "add":
        return (value - arg) & MASK
    if name == "sub":
        return (value + arg) & MASK
    if name == "rol":
        return ror8(value, arg)
    if name == "ror":
        return rol8(value, arg)
    raise AssertionError(name)


def invert(data: bytes, ops: list[tuple[str, int]]) -> bytes:
    out = bytearray(data)
    for op in reversed(ops):
        for i, value in enumerate(out):
            out[i] = invert_byte(value, op)
    return bytes(out)


def read_rodata(path: Path, offset: int, length: int) -> bytes:
    with path.open("rb") as stream:
        elf = ELFFile(stream)
        section = elf.get_section_by_name(".rodata")
        if section is None:
            raise ValueError("ELF has no .rodata section")
        blob = section.data()
        if offset < 0 or length < 0 or offset + length > len(blob):
            raise ValueError(
                f".rodata range 0x{offset:x}+{length} exceeds section size 0x{len(blob):x}"
            )
        return blob[offset : offset + length]


def find_rodata(path: Path, needle: bytes) -> tuple[int, bytes]:
    with path.open("rb") as stream:
        elf = ELFFile(stream)
        section = elf.get_section_by_name(".rodata")
        if section is None:
            raise ValueError("ELF has no .rodata section")
        blob = section.data()
        pos = blob.find(needle)
        if pos < 0:
            raise ValueError(f"target {needle!r} was not found in .rodata")
        return pos, needle


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("elf", type=Path)
    source = parser.add_mutually_exclusive_group(required=True)
    source.add_argument("--target-hex", help="target bytes, e.g. 8f21a0")
    source.add_argument("--target-string", help="target bytes as UTF-8 text")
    source.add_argument("--rodata-offset", type=parse_int, help="section-relative .rodata offset")
    source.add_argument("--find-string", help="find this UTF-8 sequence inside .rodata")
    parser.add_argument("--length", type=parse_int, help="target length for --rodata-offset")
    parser.add_argument(
        "--round",
        action="append",
        required=True,
        metavar="OP:VALUE",
        help="forward round; repeat in execution order (xor/add/sub/rol/ror)",
    )
    args = parser.parse_args()

    ops = [parse_round(item) for item in args.round]
    if args.rodata_offset is not None:
        if args.length is None:
            parser.error("--length is required with --rodata-offset")
        target = read_rodata(args.elf, args.rodata_offset, args.length)
        location = f".rodata+0x{args.rodata_offset:x}"
    elif args.target_hex is not None:
        try:
            target = bytes.fromhex(args.target_hex)
        except ValueError as exc:
            parser.error(f"invalid --target-hex: {exc}")
        location = "command line"
    elif args.target_string is not None:
        target = args.target_string.encode()
        location = "command line"
    else:
        offset, target = find_rodata(args.elf, args.find_string.encode())
        location = f".rodata+0x{offset:x}"

    original = invert(target, ops)
    print(f"target source : {location}")
    print(f"target bytes  : {target.hex(' ')}")
    print(f"recovered hex : {original.hex(' ')}")
    print(f"recovered text: {original!r}")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (OSError, ValueError) as exc:
        print(f"error: {exc}", file=sys.stderr)
        raise SystemExit(2)
