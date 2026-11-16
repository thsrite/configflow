package installer

import (
	"encoding/binary"
	"hash/crc32"
)

// zipEntry is one zipfile.ZipInfo(name) with create_system 3, the given
// Unix permission bits and ZIP_DEFLATED, as docker_ha_compose.generate_bundle
// writes it.
type zipEntry struct {
	name        string
	data        []byte
	permissions uint32
}

// pyZipArchive is the bytes of a zipfile.ZipFile(BytesIO(), 'w',
// ZIP_DEFLATED) after writestr(info, data) for each entry and close():
// CPython 3.11's local headers (date 1980-01-01 00:00:00, versions 20, no
// extra field, sizes and CRC rewritten after compression), zlib raw deflate
// data, central directory and end record without comment. Names are ASCII
// here; a non-ASCII name sets the UTF-8 flag like _encodeFilenameFlags.
func pyZipArchive(entries []zipEntry) []byte {
	const dosDate = (1980-1980)<<9 | 1<<5 | 1
	const dosTime = 0
	var out []byte
	type central struct {
		entry      zipEntry
		flags      uint16
		crc        uint32
		compressed int
		offset     int
	}
	var records []central
	le := binary.LittleEndian
	for _, entry := range entries {
		flags := uint16(0)
		for i := 0; i < len(entry.name); i++ {
			if entry.name[i] >= 0x80 {
				flags = 0x800
				break
			}
		}
		compressed := zlibDeflate(entry.data)
		crc := crc32.ChecksumIEEE(entry.data)
		offset := len(out)
		header := make([]byte, 30)
		copy(header, "PK\x03\x04")
		header[4], header[5] = 20, 0
		le.PutUint16(header[6:], flags)
		le.PutUint16(header[8:], 8)
		le.PutUint16(header[10:], dosTime)
		le.PutUint16(header[12:], dosDate)
		le.PutUint32(header[14:], crc)
		le.PutUint32(header[18:], uint32(len(compressed)))
		le.PutUint32(header[22:], uint32(len(entry.data)))
		le.PutUint16(header[26:], uint16(len(entry.name)))
		le.PutUint16(header[28:], 0)
		out = append(out, header...)
		out = append(out, entry.name...)
		out = append(out, compressed...)
		records = append(records, central{entry, flags, crc, len(compressed), offset})
	}
	start := len(out)
	for _, r := range records {
		header := make([]byte, 46)
		copy(header, "PK\x01\x02")
		header[4], header[5], header[6], header[7] = 20, 3, 20, 0
		le.PutUint16(header[8:], r.flags)
		le.PutUint16(header[10:], 8)
		le.PutUint16(header[12:], dosTime)
		le.PutUint16(header[14:], dosDate)
		le.PutUint32(header[16:], r.crc)
		le.PutUint32(header[20:], uint32(r.compressed))
		le.PutUint32(header[24:], uint32(len(r.entry.data)))
		le.PutUint16(header[28:], uint16(len(r.entry.name)))
		// extra, comment, disk start, internal attributes: 0
		le.PutUint32(header[38:], (0o100000|r.entry.permissions)<<16)
		le.PutUint32(header[42:], uint32(r.offset))
		out = append(out, header...)
		out = append(out, r.entry.name...)
	}
	end := make([]byte, 22)
	copy(end, "PK\x05\x06")
	le.PutUint16(end[8:], uint16(len(records)))
	le.PutUint16(end[10:], uint16(len(records)))
	le.PutUint32(end[12:], uint32(len(out)-start))
	le.PutUint32(end[16:], uint32(start))
	return append(out, end...)
}
