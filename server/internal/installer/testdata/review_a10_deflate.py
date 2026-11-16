"""A10 review: reference digests for the zlib raw deflate / zipfile port.

Usage: python review_a10_deflate.py DIR

DIR holds the inputs written by TestReviewDeflateAndZipMatchPython:
  deflate/NNNNNN.bin  one input each
  zips.json           [[{"name": str, "file": str, "perm": int}, ...], ...]
Prints one JSON line per case: {"kind", "id", "sha256", "size"} where the
digest is of zlib.compressobj(Z_DEFAULT_COMPRESSION, DEFLATED, -15)
compress()+flush() (deflate) or of the bytes zipfile.ZipFile(BytesIO, 'w',
ZIP_DEFLATED) writes for ZipInfo(name) with create_system 3 and
external_attr (0o100000 | perm) << 16 (zip), exactly as
backend/agents/docker_ha_compose.generate_bundle builds the bundle.

A10_ZLIB_LIBS="1.2.13=/path/libz.so:1.3.1=/path/libz.so" additionally
compresses every deflate input with those libz builds (ctypes, same
parameters, compress with Z_NO_FLUSH then Z_FINISH like CPython) and adds
{"alt": {version: sha256}} so the port can be checked against the zlib
versions production images link.
"""
import ctypes
import hashlib
import io
import json
import os
import sys
import warnings
import zipfile
import zlib


class ZStream(ctypes.Structure):
    _fields_ = [('next_in', ctypes.c_void_p), ('avail_in', ctypes.c_uint), ('total_in', ctypes.c_ulong),
                ('next_out', ctypes.c_void_p), ('avail_out', ctypes.c_uint), ('total_out', ctypes.c_ulong),
                ('msg', ctypes.c_char_p), ('state', ctypes.c_void_p),
                ('zalloc', ctypes.c_void_p), ('zfree', ctypes.c_void_p), ('opaque', ctypes.c_void_p),
                ('data_type', ctypes.c_int), ('adler', ctypes.c_ulong), ('reserved', ctypes.c_ulong)]


def alt_compressor(path):
    lib = ctypes.CDLL(path)
    version = lib.zlibVersion
    version.restype = ctypes.c_char_p
    ver = version()

    def compress(data):
        stream = ZStream()
        rc = lib.deflateInit2_(ctypes.byref(stream), -1, 8, -15, 8, 0, ver, ctypes.sizeof(ZStream))
        assert rc == 0, rc
        source = ctypes.create_string_buffer(data, len(data))
        capacity = len(data) + len(data) // 8 + 1024
        target = ctypes.create_string_buffer(capacity)
        stream.next_in = ctypes.cast(source, ctypes.c_void_p)
        stream.avail_in = len(data)
        stream.next_out = ctypes.cast(target, ctypes.c_void_p)
        stream.avail_out = capacity
        assert lib.deflate(ctypes.byref(stream), 0) in (0, -5)
        assert lib.deflate(ctypes.byref(stream), 4) == 1
        out = target.raw[:stream.total_out]
        lib.deflateEnd(ctypes.byref(stream))
        return out
    return ver.decode(), compress


def main():
    warnings.simplefilter("ignore")  # duplicate member names are deliberate test cases
    root = sys.argv[1]
    alts = []
    for item in filter(None, os.environ.get('A10_ZLIB_LIBS', '').split(':')):
        label, path = item.split('=', 1)
        alts.append((label,) + alt_compressor(path))
    names = sorted(os.listdir(os.path.join(root, 'deflate')))
    for name in names:
        data = open(os.path.join(root, 'deflate', name), 'rb').read()
        compressor = zlib.compressobj(zlib.Z_DEFAULT_COMPRESSION, zlib.DEFLATED, -15)
        out = compressor.compress(data) + compressor.flush()
        record = {'kind': 'deflate', 'id': int(name.split('.')[0]), 'sha256': hashlib.sha256(out).hexdigest(), 'size': len(out)}
        if alts:
            record['alt'] = {label + ' (' + ver + ')': hashlib.sha256(fn(data)).hexdigest() for label, ver, fn in alts}
        print(json.dumps(record))
    for index, case in enumerate(json.load(open(os.path.join(root, 'zips.json'), encoding='utf-8'))):
        archive = io.BytesIO()
        with zipfile.ZipFile(archive, 'w', zipfile.ZIP_DEFLATED) as output:
            for entry in case:
                info = zipfile.ZipInfo(entry['name'])
                info.create_system = 3
                info.external_attr = (0o100000 | entry['perm']) << 16
                info.compress_type = zipfile.ZIP_DEFLATED
                output.writestr(info, open(os.path.join(root, entry['file']), 'rb').read())
        value = archive.getvalue()
        print(json.dumps({'kind': 'zip', 'id': index, 'sha256': hashlib.sha256(value).hexdigest(), 'size': len(value)}))


main()
