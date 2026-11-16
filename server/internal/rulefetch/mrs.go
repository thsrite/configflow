package rulefetch

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/klauspost/compress/zstd"

	"configflow/internal/jsonx"
)

const (
	mrsWindow   = 128 * 1024
	mrsMaxTotal = 256 * 1024 * 1024
)

// validateRemoteMRS is _validate_remote_mrs: check the MRS v1 envelope
// (magic, behavior, signed count, extra length) inside a complete zstd
// stream (concatenated frames, checksums verified, 128 KiB window) without
// decoding the rule tree.
func validateRemoteMRS(content any, rule *jsonx.Object) error {
	fail := func(message string) error {
		return NewRemoteRuleError(rule, "mrs", []any{remoteProblem(0, "", message)})
	}
	data, ok := content.([]byte)
	if !ok {
		return fail("MRS 必须按原始二进制读取，不能作为文本校验。")
	}
	header, total, err := decodeMRSStream(data)
	switch mrsFraming(data) {
	case framingInvalid:
		if !errors.Is(err, errMRSTooLarge) {
			err = errors.New("invalid zstd framing")
		}
	case framingComplete:
		if errors.Is(err, errMRSTruncated) {
			// Every frame is complete, so libzstd rejected the content.
			err = errors.New("corrupt zstd frame")
		}
	}
	switch {
	case errors.Is(err, errMRSTooLarge):
		return fail("MRS 解压后超过 256 MiB 校验上限。")
	case errors.Is(err, errMRSTruncated):
		return fail("MRS 的 Zstandard 压缩流不完整或已截断。")
	case err != nil:
		return fail("MRS 的 Zstandard 压缩流无效或校验失败；请检查订阅链接和文件格式。")
	}
	if len(header) != 21 || !bytes.Equal(header[:4], []byte("MRS\x01")) {
		return fail("不是有效 MRS v1 文件：文件头缺失或版本不受支持。")
	}
	actual := map[byte]string{0: "domain", 1: "ipcidr"}[header[4]]
	if actual == "" {
		return fail(fmt.Sprintf("MRS 文件包含未知规则类型标记 %d。", header[4]))
	}
	declared := rule.GetOr("behavior", "domain")
	if declared != actual {
		return fail(fmt.Sprintf("MRS 文件实际类型为 %s，与声明的 %s 不匹配。", actual, jsonx.Str(declared)))
	}
	count := int64(binary.BigEndian.Uint64(header[5:13]))
	extra := int64(binary.BigEndian.Uint64(header[13:21]))
	if count < 0 {
		return fail("MRS 规则数量不能为负数。")
	}
	if extra < 0 || extra > total-21 {
		return fail("MRS 扩展字段长度无效或内容已截断。")
	}
	if total-21-extra == 0 {
		return fail("MRS 规则正文缺失。")
	}
	return nil
}

var (
	errMRSTooLarge  = errors.New("mrs too large")
	errMRSTruncated = errors.New("mrs truncated")
)

// decodeMRSStream decodes every zstd frame of data, keeping the first 21
// decoded bytes and counting the rest. A stream that ends inside a frame
// (or is empty) is errMRSTruncated, like python-zstandard's decompressobj
// whose eof stays false.
func decodeMRSStream(data []byte) ([]byte, int64, error) {
	if len(data) == 0 {
		return nil, 0, errMRSTruncated
	}
	decoder, err := zstd.NewReader(bytes.NewReader(data), zstd.WithDecoderMaxWindow(mrsWindow),
		zstd.WithDecoderConcurrency(1), zstd.WithDecoderLowmem(true))
	if err != nil {
		return nil, 0, err
	}
	defer decoder.Close()
	header := make([]byte, 0, 21)
	var total int64
	buffer := make([]byte, 64*1024)
	for {
		n, err := decoder.Read(buffer)
		if n > 0 {
			total += int64(n)
			if total > mrsMaxTotal {
				return nil, 0, errMRSTooLarge
			}
			if need := 21 - len(header); need > 0 {
				header = append(header, buffer[:min(need, n)]...)
			}
		}
		if err == io.EOF {
			return header, total, nil
		}
		if err != nil {
			if errors.Is(err, io.ErrUnexpectedEOF) {
				return nil, 0, errMRSTruncated
			}
			return nil, 0, err
		}
	}
}

const (
	framingComplete = iota
	framingTruncated
	framingInvalid
)

// mrsFraming walks the zstd frames the way libzstd 1.5.7's streaming decoder
// (python-zstandard decompressobj, max_window_size=128 KiB) sees their
// structure: an unknown frame prefix (also 1–3 trailing bytes that cannot
// start a frame), a reserved header bit, a dictionary id (no dictionary is
// loaded), a window above 128 KiB, a reserved block type or a block larger
// than the frame's block size limit fail as soon as they are read;
// a frame cut short anywhere is incomplete. Block contents are left to the
// decoder.
func mrsFraming(data []byte) int {
	zstdMagic := []byte{0x28, 0xb5, 0x2f, 0xfd}
	i := 0
	if len(data) == 0 {
		return framingTruncated
	}
	for i < len(data) {
		rest := data[i:]
		if len(rest) < 4 {
			skippablePrefix := rest[0]&0xf0 == 0x50 && bytes.HasPrefix([]byte{rest[0], 0x2a, 0x4d, 0x18}, rest)
			if bytes.HasPrefix(zstdMagic, rest) || skippablePrefix {
				return framingTruncated
			}
			return framingInvalid
		}
		if rest[0]&0xf0 == 0x50 && bytes.Equal(rest[1:4], []byte{0x2a, 0x4d, 0x18}) {
			if len(rest) < 8 {
				return framingTruncated
			}
			size := int(binary.LittleEndian.Uint32(rest[4:8]))
			if len(rest)-8 < size {
				return framingTruncated
			}
			i += 8 + size
			continue
		}
		if !bytes.Equal(rest[:4], zstdMagic) {
			return framingInvalid
		}
		if len(rest) < 5 {
			return framingTruncated
		}
		fhd := rest[4]
		single := fhd&0x20 != 0
		checksum := fhd&0x04 != 0
		dictSize := []int{0, 1, 2, 4}[fhd&3]
		fcsSize := []int{0, 2, 4, 8}[fhd>>6]
		if fcsSize == 0 && single {
			fcsSize = 1
		}
		headerSize := 5 + dictSize + fcsSize
		if !single {
			headerSize++
		}
		if len(rest) < headerSize {
			return framingTruncated
		}
		if fhd&0x08 != 0 {
			return framingInvalid
		}
		pos := 5
		var window uint64
		if !single {
			wd := rest[pos]
			pos++
			windowLog := 10 + uint(wd>>3)
			if windowLog > 31 {
				return framingInvalid
			}
			base := uint64(1) << windowLog
			window = base + base/8*uint64(wd&7)
		}
		var dictID uint64
		for k := 0; k < dictSize; k++ {
			dictID |= uint64(rest[pos+k]) << (8 * k)
		}
		pos += dictSize
		var fcs uint64
		for k := 0; k < fcsSize; k++ {
			fcs |= uint64(rest[pos+k]) << (8 * k)
		}
		if fcsSize == 2 {
			fcs += 256
		}
		pos += fcsSize
		if single {
			window = fcs
		}
		if dictID != 0 {
			return framingInvalid
		}
		if max(window, 1024) > mrsWindow {
			return framingInvalid
		}
		blockMax := min(window, 128*1024)
		for {
			if len(rest)-pos < 3 {
				return framingTruncated
			}
			header := uint32(rest[pos]) | uint32(rest[pos+1])<<8 | uint32(rest[pos+2])<<16
			pos += 3
			last := header&1 != 0
			kind := (header >> 1) & 3
			size := int(header >> 3)
			switch kind {
			case 3:
				return framingInvalid
			case 1: // RLE: one byte (checked against the limit), size is the regenerated size
				if blockMax < 1 {
					return framingInvalid
				}
				if len(rest)-pos < 1 {
					return framingTruncated
				}
				pos++
			default:
				if uint64(size) > blockMax {
					return framingInvalid
				}
				if len(rest)-pos < size {
					return framingTruncated
				}
				pos += size
			}
			if last {
				break
			}
		}
		if checksum {
			if len(rest)-pos < 4 {
				return framingTruncated
			}
			pos += 4
		}
		i += pos
	}
	return framingComplete
}
