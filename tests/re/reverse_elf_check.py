#!/usr/bin/env python3
"""Reverse a byte-wise ELF check pipeline from a .rodata target."""

from __future__ import annotations

import argparse
import string
import sys


MASK = 0xFF


def parse_ops(spec: str) -> list[tuple[str, int]]:
    if not spec:
        return []
    result = []
    for item in spec.split(","):
        parts = item.strip().lower().split(":", 1)
        if len(parts) != 2 or parts[0] not in {"xor", "add", "sub", "rol", "ror"}:
            raise ValueError(f"invalid operation {item!r}; expected xor/add/sub/rol/ror:VALUE")
        value = int(parts[1], 0)
        if parts[0] in {"rol", "ror"}:
            value %= 8
        result.append((parts[0], value & MASK))
    return result


def undo_round(data: bytes, ops: list[tuple[str, int]]) -> bytes:
    output = bytearray()
    for byte in data:
        value = byte
        for op, arg in reversed(ops):
            if op == "xor":
                value ^= arg
            elif op == "add":
                value = (value - arg) & MASK
            elif op == "sub":
                value = (value + arg) & MASK
            elif op == "rol":
                value = ((value >> arg) | (value << (8 - arg))) & MASK if arg else value
            elif op == "ror":
                value = ((value << arg) | (value >> (8 - arg))) & MASK if arg else value
        output.append(value)
    return bytes(output)


def read_rodata(path: str, offset: int, length: int) -> bytes:
    try:
        from elftools.elf.elffile import ELFFile
    except ImportError as exc:
        raise RuntimeError("pyelftools is required: python -m pip install pyelftools") from exc

    with open(path, "rb") as stream:
        elf = ELFFile(stream)
        section = elf.get_section_by_name(".rodata")
        if section is None:
            raise ValueError("ELF has no .rodata section")
        content = section.data()
    if offset < 0 or length <= 0 or offset + length > len(content):
        raise ValueError(f"range is outside .rodata (size 0x{len(content):x})")
    return content[offset : offset + length]


def printable(data: bytes) -> str:
    return "".join(chr(byte) if chr(byte) in string.printable and byte not in b"\r\n\t\x0b\x0c" else "." for byte in data)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("elf", help="path to the ELF file")
    parser.add_argument("--offset", required=True, type=lambda x: int(x, 0), help="target offset within .rodata")
    parser.add_argument("--length", required=True, type=lambda x: int(x, 0), help="target byte length")
    parser.add_argument("--round1", default="", help="round 1 forward ops, e.g. xor:0x12,add:3")
    parser.add_argument("--round2", default="", help="round 2 forward ops, e.g. rol:1,sub:7")
    args = parser.parse_args()

    try:
        target = read_rodata(args.elf, args.offset, args.length)
        round1 = parse_ops(args.round1)
        round2 = parse_ops(args.round2)
        value = bytes(byte ^ 0x5A for byte in target)
        value = undo_round(value, round2)
        value = undo_round(value, round1)
    except (OSError, ValueError, RuntimeError) as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 2

    print(f"target  : {target.hex()}")
    print(f"input   : {value.hex()}")
    print(f"ascii   : {printable(value)}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
