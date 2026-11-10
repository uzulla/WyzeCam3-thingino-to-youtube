#!/usr/bin/env python3
"""fatextract.py - copy one file out of a FAT32 image without mounting it.

    python3 -I firmware/fatextract.py <image> <name in root dir> <output>

Used by firmware/make-sdcard.sh to take the Wyze bootloader's installer
(factory_t31_ZMC6tiIDQN) out of thingino-installers' SD image, which is a
whole-disk image (MBR + one FAT32 partition) that would otherwise need root
(loop mount) or mtools. Only what that needs: MBR or bare FAT32, 8.3 names in
the root directory, a file of any size following its cluster chain. No
dependencies outside the standard library.
"""

import struct
import sys


def fail(msg):
    print("fatextract: " + msg, file=sys.stderr)
    sys.exit(1)


def read_at(f, offset, size):
    f.seek(offset)
    data = f.read(size)
    if len(data) != size:
        fail("short read at %d" % offset)
    return data


def partition_offset(f):
    """Byte offset of the FAT32 volume: 0 for a bare filesystem, else the
    first FAT32 (0x0b/0x0c) partition of the MBR."""
    sector = read_at(f, 0, 512)
    if sector[510:512] != b"\x55\xaa":
        fail("no boot signature")
    # a bare FAT volume has a jump at 0 and 'FAT32' in the BPB extension
    if sector[82:87] == b"FAT32":
        return 0
    for i in range(4):
        entry = sector[446 + 16 * i : 446 + 16 * (i + 1)]
        ptype = entry[4]
        if ptype in (0x0B, 0x0C):
            (lba,) = struct.unpack("<I", entry[8:12])
            return lba * 512
    fail("no FAT32 partition in the MBR")


def short_name(entry):
    base = entry[0:8].decode("ascii", "replace").rstrip()
    ext = entry[8:11].decode("ascii", "replace").rstrip()
    if entry[0] == 0x05:  # 0xE5 as a first character, escaped
        base = "\xe5" + base[1:]
    return base + ("." + ext if ext else "")


def main():
    if len(sys.argv) != 4:
        print(__doc__.strip(), file=sys.stderr)
        sys.exit(2)
    image, want, out_path = sys.argv[1:]
    with open(image, "rb") as f:
        base = partition_offset(f)
        bpb = read_at(f, base, 512)
        bytes_per_sector = struct.unpack("<H", bpb[11:13])[0]
        sectors_per_cluster = bpb[13]
        reserved = struct.unpack("<H", bpb[14:16])[0]
        fats = bpb[16]
        sectors_per_fat = struct.unpack("<I", bpb[36:40])[0]
        root_cluster = struct.unpack("<I", bpb[44:48])[0]
        if bytes_per_sector == 0 or sectors_per_cluster == 0 or sectors_per_fat == 0:
            fail("not a FAT32 BPB")
        cluster_size = bytes_per_sector * sectors_per_cluster
        fat_offset = base + reserved * bytes_per_sector
        data_offset = fat_offset + fats * sectors_per_fat * bytes_per_sector

        def cluster_offset(n):
            return data_offset + (n - 2) * cluster_size

        def chain(first):
            n = first
            seen = 0
            while 2 <= n < 0x0FFFFFF8:
                yield n
                (n,) = struct.unpack("<I", read_at(f, fat_offset + n * 4, 4))
                n &= 0x0FFFFFFF
                seen += 1
                if seen > 1 << 24:
                    fail("cluster chain loops")

        # root directory: walk its clusters, 32-byte entries. A long name
        # (VFAT) comes in pieces of 13 UTF-16 characters, in entries with
        # attribute 0x0f right before the 8.3 entry they belong to, last piece
        # first; it is assembled and matched case-insensitively, as is the 8.3
        # name (stored in upper case)
        found = None
        pieces = {}
        for c in chain(root_cluster):
            data = read_at(f, cluster_offset(c), cluster_size)
            for i in range(0, cluster_size, 32):
                e = data[i : i + 32]
                if e[0] == 0x00:
                    break
                if e[0] == 0xE5:
                    pieces = {}
                    continue
                if e[11] == 0x0F:
                    seq = e[0] & 0x1F
                    raw = e[1:11] + e[14:26] + e[28:32]
                    pieces[seq] = raw.decode("utf-16-le", "replace")
                    continue
                long_name = ""
                if pieces and sorted(pieces) == list(range(1, len(pieces) + 1)):
                    long_name = "".join(pieces[k] for k in sorted(pieces))
                    long_name = long_name.split("\x00", 1)[0].rstrip("\uffff")
                pieces = {}
                if e[11] & 0x08:  # volume label
                    continue
                names = (short_name(e).upper(), long_name.upper())
                if want.upper() in names and not e[11] & 0x10:
                    hi = struct.unpack("<H", e[20:22])[0]
                    lo = struct.unpack("<H", e[26:28])[0]
                    size = struct.unpack("<I", e[28:32])[0]
                    found = ((hi << 16) | lo, size)
                    break
            if found:
                break
        if not found:
            fail("%s not found in the root directory of %s" % (want, image))
        first, size = found
        left = size
        with open(out_path, "wb") as out:
            for c in chain(first):
                if left <= 0:
                    break
                n = min(cluster_size, left)
                out.write(read_at(f, cluster_offset(c), n))
                left -= n
        if left != 0:
            fail("cluster chain ended %d bytes early" % left)
        print("%s: %d bytes -> %s" % (want, size, out_path))


if __name__ == "__main__":
    main()
