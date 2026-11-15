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
	if errors.Is(err, errMRSTruncated) && trailingGarbage(data) {
		err = errors.New("unknown frame prefix")
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

// trailingGarbage reports whether data is complete frames followed by one
// to three bytes that cannot begin a frame: zstd rejects such a fragment
// (unknown frame prefix) instead of waiting for more input.
func trailingGarbage(data []byte) bool {
	magics := [][]byte{{0x28, 0xb5, 0x2f, 0xfd}}
	for k := 1; k <= 3 && k < len(data); k++ {
		if _, _, err := decodeMRSStream(data[:len(data)-k]); err != nil {
			continue
		}
		tail := data[len(data)-k:]
		if bytes.HasPrefix(magics[0], tail) || (tail[0]&0xf0 == 0x50 && bytes.HasPrefix([]byte{tail[0], 0x2a, 0x4d, 0x18}, tail)) {
			return false
		}
		return true
	}
	return false
}
