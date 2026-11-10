#!/usr/bin/env python3
"""Tests for fatextract.py: python3 -I firmware/fatextract_test.py

A small FAT32 volume is built in memory (BPB, FSInfo, two FATs, a root
directory with 8.3 and long names), so the cases that matter can be laid out
on purpose: a file across several clusters in a fragmented chain, a long name
(several pieces, matched without regard to case), the end-of-directory marker,
a looping chain, a chain that ends early, a bad-cluster marker, a cluster
number outside the volume, an unrelated long name right before the entry, an
MBR in front of the volume, and a volume that is not FAT32 at all.
"""

import io
import os
import struct
import subprocess
import sys
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import fatextract  # noqa: E402

BPS = 512  # bytes per sector
SPC = 1  # sectors per cluster: one 512-byte cluster keeps the images small
RESERVED = 32
FATS = 2
TOTAL_SECTORS = 4096  # about 2 MB


def short_entry(name83, attr, first, size):
    """A 32-byte 8.3 directory entry. name83 is 11 bytes, already padded."""
    e = bytearray(32)
    e[0:11] = name83
    e[11] = attr
    e[20:22] = struct.pack("<H", first >> 16)
    e[26:28] = struct.pack("<H", first & 0xFFFF)
    e[28:32] = struct.pack("<I", size)
    return bytes(e)


def lfn_entries(long_name, name83):
    """The VFAT long-name entries for long_name, last piece first."""
    chk = fatextract.short_checksum(name83 + b"\x00" * 21)
    units = long_name.encode("utf-16-le") + b"\x00\x00"
    pieces = []
    for i in range(0, len(units), 26):
        p = units[i : i + 26]
        p = p + b"\xff" * (26 - len(p))
        pieces.append(p)
    out = []
    n = len(pieces)
    for idx in range(n - 1, -1, -1):
        p = pieces[idx]
        e = bytearray(32)
        e[0] = (idx + 1) | (0x40 if idx == n - 1 else 0)
        e[1:11] = p[0:10]
        e[11] = 0x0F
        e[13] = chk
        e[14:26] = p[10:22]
        e[28:32] = p[22:26]
        out.append(bytes(e))
    return b"".join(out)


class Image:
    """A FAT32 volume with one directory cluster (cluster 2) and files whose
    cluster chains are given explicitly, so odd chains can be made."""

    def __init__(self, mbr=False):
        self.mbr = mbr
        self.base = 2048 * 512 if mbr else 0
        self.fat = {}  # cluster -> next value (raw 28-bit)
        self.data = {}  # cluster -> bytes
        self.dir = b""
        self.fat[2] = 0x0FFFFFFF  # root directory: one cluster

    def add_file(self, name83, long_name, content, chain, size=None):
        """Place content over the clusters of chain (in order), link them in
        the FAT, add a directory entry. chain may end in a special value
        (loop target, bad marker, out-of-range number) given as the last
        element with link_last."""
        size = len(content) if size is None else size
        for i, c in enumerate(chain):
            self.data[c] = content[i * BPS : (i + 1) * BPS].ljust(BPS, b"\x00")
            self.fat[c] = chain[i + 1] if i + 1 < len(chain) else 0x0FFFFFFF
        if long_name:
            self.dir += lfn_entries(long_name, name83)
        self.dir += short_entry(name83, 0x20, chain[0], size)

    def link(self, cluster, value):
        self.fat[cluster] = value

    def add_raw_dir(self, entries):
        self.dir += entries

    def build(self):
        vol = bytearray(TOTAL_SECTORS * BPS)
        bpb = bytearray(512)
        bpb[0:3] = b"\xeb\x58\x90"
        bpb[3:11] = b"MSWIN4.1"
        struct.pack_into("<H", bpb, 11, BPS)
        bpb[13] = SPC
        struct.pack_into("<H", bpb, 14, RESERVED)
        bpb[16] = FATS
        struct.pack_into("<H", bpb, 19, 0)
        bpb[21] = 0xF8
        struct.pack_into("<H", bpb, 22, 0)  # FAT16 size = 0
        struct.pack_into("<I", bpb, 32, TOTAL_SECTORS)
        sectors_per_fat = 16
        struct.pack_into("<I", bpb, 36, sectors_per_fat)
        struct.pack_into("<I", bpb, 44, 2)  # root cluster
        struct.pack_into("<H", bpb, 48, 1)  # FSInfo
        bpb[82:90] = b"FAT32   "
        bpb[510:512] = b"\x55\xaa"
        vol[0:512] = bpb
        fat = bytearray(sectors_per_fat * BPS)
        struct.pack_into("<I", fat, 0, 0x0FFFFFF8)
        struct.pack_into("<I", fat, 4, 0x0FFFFFFF)
        for c, v in self.fat.items():
            struct.pack_into("<I", fat, c * 4, v)
        for i in range(FATS):
            off = (RESERVED + i * sectors_per_fat) * BPS
            vol[off : off + len(fat)] = fat
        data_off = (RESERVED + FATS * sectors_per_fat) * BPS
        vol[data_off : data_off + BPS] = self.dir.ljust(BPS, b"\x00")[:BPS]
        for c, content in self.data.items():
            off = data_off + (c - 2) * BPS
            vol[off : off + BPS] = content
        if not self.mbr:
            return bytes(vol)
        mbr = bytearray(512)
        mbr[446 + 4] = 0x0C
        struct.pack_into("<I", mbr, 446 + 8, 2048)
        struct.pack_into("<I", mbr, 446 + 12, TOTAL_SECTORS)
        mbr[510:512] = b"\x55\xaa"
        return bytes(mbr) + b"\x00" * (2048 * 512 - 512) + bytes(vol)


class Case(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp()

    def tearDown(self):
        for f in os.listdir(self.tmp):
            os.remove(os.path.join(self.tmp, f))
        os.rmdir(self.tmp)

    def run_extract(self, img_bytes, name):
        img = os.path.join(self.tmp, "img.bin")
        with open(img, "wb") as f:
            f.write(img_bytes)
        out = os.path.join(self.tmp, "out.bin")
        r = subprocess.run(
            [sys.executable, "-I", os.path.join(HERE, "fatextract.py"), img, name, out],
            capture_output=True,
            text=True,
        )
        return r, out

    def test_fragmented_file_and_long_name(self):
        content = bytes(range(256)) * 5  # 1280 bytes: 3 clusters, last one partial
        im = Image()
        im.add_file(b"FACTOR~1   ", "factory_t31_ZMC6tiIDQN", content, [7, 3, 9])
        r, out = self.run_extract(im.build(), "factory_t31_ZMC6tiIDQN")
        self.assertEqual(r.returncode, 0, r.stderr)
        with open(out, "rb") as f:
            self.assertEqual(f.read(), content)
        # the 8.3 alias and any case work too
        r, out = self.run_extract(im.build(), "FACTORY_T31_ZMC6TIIDQN")
        self.assertEqual(r.returncode, 0, r.stderr)
        r, out = self.run_extract(im.build(), "factor~1")
        self.assertEqual(r.returncode, 0, r.stderr)

    def test_mbr(self):
        content = b"hello world" * 100
        im = Image(mbr=True)
        im.add_file(b"A       BIN", None, content, [4, 5, 6])
        r, out = self.run_extract(im.build(), "a.bin")
        self.assertEqual(r.returncode, 0, r.stderr)
        with open(out, "rb") as f:
            self.assertEqual(f.read(), content)

    def test_empty_file(self):
        im = Image()
        im.add_file(b"EMPTY   TXT", None, b"", [3], size=0)
        r, out = self.run_extract(im.build(), "empty.txt")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(os.path.getsize(out), 0)

    def test_not_found_leaves_nothing(self):
        im = Image()
        im.add_file(b"A       BIN", None, b"x" * 10, [3])
        r, out = self.run_extract(im.build(), "b.bin")
        self.assertEqual(r.returncode, 1)
        self.assertIn("not found", r.stderr)
        self.assertFalse(os.path.exists(out))
        self.assertFalse(os.path.exists(out + ".part"))

    def test_end_of_directory_stops_the_search(self):
        # the entry is placed after an end-of-directory marker: invalid
        im = Image()
        im.add_raw_dir(b"\x00" * 32)
        im.add_file(b"A       BIN", None, b"x" * 10, [3])
        r, out = self.run_extract(im.build(), "a.bin")
        self.assertEqual(r.returncode, 1)
        self.assertIn("not found", r.stderr)

    def test_looping_chain(self):
        im = Image()
        im.add_file(b"A       BIN", None, b"x" * 2000, [3, 4])
        im.link(4, 3)  # 4 -> 3 -> 4 ...
        r, out = self.run_extract(im.build(), "a.bin")
        self.assertEqual(r.returncode, 1)
        self.assertIn("loops", r.stderr)
        self.assertFalse(os.path.exists(out))

    def test_chain_ends_early(self):
        im = Image()
        im.add_file(b"A       BIN", None, b"x" * 2000, [3])  # size says 4 clusters
        r, out = self.run_extract(im.build(), "a.bin")
        self.assertEqual(r.returncode, 1)
        self.assertIn("ended", r.stderr)
        self.assertFalse(os.path.exists(out))
        self.assertFalse(os.path.exists(out + ".part"))

    def test_bad_cluster_and_out_of_range(self):
        for value, text in ((0x0FFFFFF7, "not a data cluster"), (9999999, "not a data cluster"), (1, "not a data cluster")):
            im = Image()
            im.add_file(b"A       BIN", None, b"x" * 1000, [3])
            im.link(3, value)
            r, out = self.run_extract(im.build(), "a.bin")
            self.assertEqual(r.returncode, 1, value)
            self.assertIn(text, r.stderr)

    def test_unrelated_long_name_before_entry(self):
        # a long name whose checksum does not match the 8.3 entry that
        # follows must not be attached to it
        im = Image()
        im.add_raw_dir(lfn_entries("wrong name.bin", b"WRONG   BIN"))
        im.add_file(b"A       BIN", None, b"y" * 10, [3])
        r, out = self.run_extract(im.build(), "wrong name.bin")
        self.assertEqual(r.returncode, 1)
        self.assertIn("not found", r.stderr)
        r, out = self.run_extract(im.build(), "a.bin")
        self.assertEqual(r.returncode, 0, r.stderr)

    def test_directory_entry_is_not_a_file(self):
        im = Image()
        im.add_raw_dir(short_entry(b"A       BIN", 0x10, 3, 0))
        r, out = self.run_extract(im.build(), "a.bin")
        self.assertEqual(r.returncode, 1)

    def test_not_fat32(self):
        img = bytearray(4096)
        img[510:512] = b"\x55\xaa"
        r, out = self.run_extract(bytes(img), "a.bin")
        self.assertEqual(r.returncode, 1)
        self.assertIn("no FAT32 partition", r.stderr)

    def test_empty_name(self):
        im = Image()
        r, out = self.run_extract(im.build(), "")
        self.assertEqual(r.returncode, 1)


if __name__ == "__main__":
    unittest.main()
