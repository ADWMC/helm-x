#!/usr/bin/env python3
"""Recover input for the described 32-bit PE check.

Forward check (for byte index i) is assumed to be:

    b[i] = (input[i] ^ 0x13) + i
    every 4-byte little-endian DWORD = rol32(DWORD, 3)

The solver applies the inverse operations and verifies the result by running
the forward transform again.  Paste the complete data-section table with
--table-hex, or put its hex bytes in --table-file.  The bundled default is
the eight-byte table prefix supplied with the challenge.
"""

from __future__ import annotations

import argparse
import string
import sys
from pathlib import Path


MASK8 = 0xFF
MASK32 = 0xFFFFFFFF
DEFAULT_TABLE_HEX = "3F 2A 6D 55 C4 2B 1E 0F"


def rol32(value: int, count: int) -> int:
    count &= 31
    if count == 0:
        return value & MASK32
    return ((value << count) | (value >> (32 - count))) & MASK32


def ror32(value: int, count: int) -> int:
    count &= 31
    if count == 0:
        return value & MASK32
    return ((value >> count) | (value << (32 - count))) & MASK32


def parse_hex(raw: str) -> bytes:
    """Accept `3f2a...`, whitespace, commas, and an optional 0x prefix."""
    cleaned = raw.replace(",", " ").replace("0x", "").replace("0X", "")
    try:
        value = bytes.fromhex(cleaned)
    except ValueError as exc:
        raise ValueError(f"invalid table hex: {exc}") from exc
    if not value:
        raise ValueError("table is empty")
    return value


def forward(data: bytes, index_base: int = 0, rotate: int = 3) -> bytes:
    mixed = bytearray(
        ((byte ^ 0x13) + index_base + index) & MASK8
        for index, byte in enumerate(data)
    )
    if len(mixed) % 4:
        raise ValueError("table length must be a multiple of 4 for DWORD rotation")
    for offset in range(0, len(mixed), 4):
        dword = int.from_bytes(mixed[offset : offset + 4], "little")
        mixed[offset : offset + 4] = rol32(dword, rotate).to_bytes(4, "little")
    return bytes(mixed)


def recover(table: bytes, index_base: int = 0, rotate: int = 3) -> bytes:
    if len(table) % 4:
        raise ValueError("table length must be a multiple of 4 for DWORD rotation")
    unmixed = bytearray(table)
    for offset in range(0, len(unmixed), 4):
        dword = int.from_bytes(unmixed[offset : offset + 4], "little")
        unmixed[offset : offset + 4] = ror32(dword, rotate).to_bytes(4, "little")
    return bytes(
        ((byte - index_base - index) & MASK8) ^ 0x13
        for index, byte in enumerate(unmixed)
    )


def printable(data: bytes) -> str:
    return "".join(
        chr(byte) if chr(byte) in string.printable and byte not in b"\r\n\t\x0b\x0c" else "."
        for byte in data
    )


def load_table(args: argparse.Namespace) -> bytes:
    if args.table_hex is not None and args.table_file is not None:
        raise ValueError("use only one of --table-hex and --table-file")
    if args.table_file is not None:
        return parse_hex(Path(args.table_file).read_text(encoding="ascii"))
    return parse_hex(args.table_hex if args.table_hex is not None else DEFAULT_TABLE_HEX)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    source = parser.add_mutually_exclusive_group()
    source.add_argument("--table-hex", help="complete table, e.g. '3f 2a 6d 55 ...'")
    source.add_argument("--table-file", help="text file containing the complete table as hex")
    parser.add_argument("--index-base", type=lambda value: int(value, 0), default=0)
    parser.add_argument("--rotate", type=lambda value: int(value, 0), default=3)
    args = parser.parse_args()

    table = load_table(args)
    recovered = recover(table, args.index_base, args.rotate)
    if forward(recovered, args.index_base, args.rotate) != table:
        raise AssertionError("forward verification failed")

    print(f"table bytes  : {table.hex(' ')}")
    print(f"input hex    : {recovered.hex(' ')}")
    print(f"input ascii  : {printable(recovered)}")
    print(f"input length : {len(recovered)} bytes")
    if args.table_hex is None and args.table_file is None:
        print("scope        : default table is an 8-byte prefix; this is only the matching input prefix")
    print("verify       : ok")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (OSError, ValueError, AssertionError) as exc:
        print(f"error: {exc}", file=sys.stderr)
        raise SystemExit(2)
