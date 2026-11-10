#!/usr/bin/env python3
"""fatextract.py - copy one file out of a FAT32 image without mounting it.

    python3 -I firmware/fatextract.py <image> <name in root dir> <output>

Used by firmware/make-sdcard.sh to take the Wyze bootloader's installer
(factory_t31_ZMC6tiIDQN) out of thingino-installers' SD image, which is a
whole-disk image (MBR + one FAT32 partition) that would otherwise need root
(loop mount) or mtools. Only what that needs: MBR or bare FAT32, a file in
the root directory (8.3 or long name, matched without regard to case), any
size, following its cluster chain. The output is written to a temporary name
and renamed when complete, so a failure leaves nothing behind. No
dependencies outside the standard library. Tested by fatextract_test.py.
"""

import os
import struct
import sys

EOC = 0x0FFFFFF8  # end-of-chain markers are >= this
BAD = 0x0FFFFFF7


def fail(msg):
    print("fatextract: " + msg, file=sys.stderr)
    sys.exit(1)


class Volume:
    def __init__(self, f):
        self.f = f
        self.base = self.partition_offset()
        bpb = self.read_at(self.base, 512)
        if bpb[510:512] != b"\x55\xaa":
            fail("no boot signature in the FAT32 volume")
        self.bytes_per_sector = struct.unpack("<H", bpb[11:13])[0]
        self.sectors_per_cluster = bpb[13]
        reserved = struct.unpack("<H", bpb[14:16])[0]
        fats = bpb[16]
        total16 = struct.unpack("<H", bpb[19:21])[0]
        total32 = struct.unpack("<I", bpb[32:36])[0]
        fat16_size = struct.unpack("<H", bpb[22:24])[0]
        self.sectors_per_fat = struct.unpack("<I", bpb[36:40])[0]
        self.root_cluster = struct.unpack("<I", bpb[44:48])[0]
        if self.bytes_per_sector not in (512, 1024, 2048, 4096):
            fail("not a FAT32 BPB (bytes per sector %d)" % self.bytes_per_sector)
        if self.sectors_per_cluster == 0 or self.sectors_per_cluster & (self.sectors_per_cluster - 1):
            fail("not a FAT32 BPB (sectors per cluster %d)" % self.sectors_per_cluster)
        if fat16_size != 0 or self.sectors_per_fat == 0 or fats == 0 or reserved == 0:
            fail("not a FAT32 volume (FAT12/16 or broken BPB)")
        total = total32 or total16
        self.cluster_size = self.bytes_per_sector * self.sectors_per_cluster
        self.fat_offset = self.base + reserved * self.bytes_per_sector
        data_sector = reserved + fats * self.sectors_per_fat
        self.data_offset = self.base + data_sector * self.bytes_per_sector
        if total <= data_sector:
            fail("broken BPB: no data area")
        # the highest cluster number that exists on this volume
        self.last_cluster = 1 + (total - data_sector) // self.sectors_per_cluster
        if not 2 <= self.root_cluster <= self.last_cluster:
            fail("root directory cluster %d is out of range" % self.root_cluster)

    def read_at(self, offset, size):
        self.f.seek(offset)
        data = self.f.read(size)
        if len(data) != size:
            fail("short read at %d (truncated image?)" % offset)
        return data

    def partition_offset(self):
        """Byte offset of the FAT32 volume: 0 for a bare filesystem, else the
        first FAT32 (0x0b/0x0c) partition of the MBR."""
        sector = self.read_at(0, 512)
        if sector[510:512] != b"\x55\xaa":
            fail("no boot signature")
        # a bare FAT32 volume names itself in the BPB extension
        if sector[82:87] == b"FAT32":
            return 0
        for i in range(4):
            entry = sector[446 + 16 * i : 446 + 16 * (i + 1)]
            if entry[4] in (0x0B, 0x0C):
                (lba,) = struct.unpack("<I", entry[8:12])
                return lba * 512
        fail("no FAT32 partition in the MBR")

    def cluster_offset(self, n):
        return self.data_offset + (n - 2) * self.cluster_size

    def chain(self, first):
        """Cluster numbers of a chain, in order. Loops, bad clusters and
        numbers outside the volume are errors, not silent truncation."""
        n = first
        seen = set()
        while n < EOC:
            if n in seen:
                fail("cluster chain loops at %d" % n)
            if n == BAD or not 2 <= n <= self.last_cluster:
                fail("cluster chain points at %d, which is not a data cluster" % n)
            seen.add(n)
            yield n
            (n,) = struct.unpack("<I", self.read_at(self.fat_offset + n * 4, 4))
            n &= 0x0FFFFFFF


def short_name(entry):
    base = entry[0:8].decode("ascii", "replace").rstrip()
    ext = entry[8:11].decode("ascii", "replace").rstrip()
    if entry[0] == 0x05:  # 0xE5 as a first character, escaped
        base = "\xe5" + base[1:]
    return base + ("." + ext if ext else "")


def short_checksum(entry):
    s = 0
    for b in entry[0:11]:
        s = (((s & 1) << 7) + (s >> 1) + b) & 0xFF
    return s


def find_in_root(vol, want):
    """(first cluster, size) of the file called want in the root directory,
    or None. A long (VFAT) name comes in pieces of 13 UTF-16 characters, in
    entries with attribute 0x0f right before the 8.3 entry they belong to,
    last piece first (flagged 0x40); the pieces are kept only when they are
    complete and their checksum matches the 8.3 entry."""
    want = want.upper()
    pieces = {}
    expect_sum = None
    for c in vol.chain(vol.root_cluster):
        data = vol.read_at(vol.cluster_offset(c), vol.cluster_size)
        for i in range(0, vol.cluster_size, 32):
            e = data[i : i + 32]
            if e[0] == 0x00:
                return None  # end of the directory: nothing valid follows
            if e[0] == 0xE5:
                pieces = {}
                continue
            if e[11] == 0x0F:
                if e[0] & 0x40:  # the last piece starts a new long name
                    pieces = {}
                    expect_sum = e[13]
                if e[13] != expect_sum:
                    pieces = {}
                    continue
                seq = e[0] & 0x3F
                pieces[seq] = e[1:11] + e[14:26] + e[28:32]
                continue
            long_name = ""
            if pieces and sorted(pieces) == list(range(1, len(pieces) + 1)) and expect_sum == short_checksum(e):
                raw = b"".join(pieces[k] for k in sorted(pieces))
                long_name = raw.decode("utf-16-le", "replace").split("\x00", 1)[0].rstrip("￿")
            pieces = {}
            expect_sum = None
            if e[11] & 0x08 or e[11] & 0x10:  # volume label, directory
                continue
            if want in (short_name(e).upper(), long_name.upper()):
                hi = struct.unpack("<H", e[20:22])[0]
                lo = struct.unpack("<H", e[26:28])[0]
                size = struct.unpack("<I", e[28:32])[0]
                return (hi << 16) | lo, size
    return None


def extract(image, want, out_path):
    if not want:
        fail("the name to look for is empty")
    with open(image, "rb") as f:
        vol = Volume(f)
        found = find_in_root(vol, want)
        if not found:
            fail("%s not found in the root directory of %s" % (want, image))
        first, size = found
        tmp = out_path + ".part"
        left = size
        with open(tmp, "wb") as out:
            if size > 0:
                for c in vol.chain(first):
                    if left <= 0:
                        break
                    n = min(vol.cluster_size, left)
                    out.write(vol.read_at(vol.cluster_offset(c), n))
                    left -= n
        if left != 0:
            os.remove(tmp)
            fail("cluster chain ended %d bytes early" % left)
        os.replace(tmp, out_path)
        return size


def main():
    if len(sys.argv) != 4:
        print(__doc__.strip(), file=sys.stderr)
        sys.exit(2)
    image, want, out_path = sys.argv[1:]
    size = extract(image, want, out_path)
    print("%s: %d bytes -> %s" % (want, size, out_path))


if __name__ == "__main__":
    main()
