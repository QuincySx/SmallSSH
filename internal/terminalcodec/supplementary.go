package terminalcodec

import (
	"errors"
	"unicode/utf8"
)

// GB18030 maps U+10000..U+10FFFF linearly at pointers 189000..1237575.
// Older system iconv implementations omit this range. Handle it consistently
// while retaining the OS tables for BMP characters. See the Encoding Standard:
// https://encoding.spec.whatwg.org/#index-gb18030-ranges-code-point
func convertSupplementary(src []byte, from, to string, system func([]byte) ([]byte, error)) ([]byte, error) {
	if from != "gb18030" && to != "gb18030" {
		return system(src)
	}
	var out []byte
	start := 0
	flush := func(end int) error {
		if start == end {
			return nil
		}
		b, e := system(src[start:end])
		out = append(out, b...)
		return e
	}
	for i := 0; i < len(src); {
		if from == "utf-8" {
			value, width := utf8.DecodeRune(src[i:])
			if value >= 0x10000 {
				if e := flush(i); e != nil {
					return nil, e
				}
				pointer := int(value) - 0x10000 + 189000
				out = append(out, byte(pointer/12600+0x81), byte(pointer/1260%10+0x30), byte(pointer/10%126+0x81), byte(pointer%10+0x30))
				i += width
				start = i
				continue
			}
			i += width
			continue
		}
		width := 1
		if src[i] >= 0x81 {
			width = 2
			if i+1 < len(src) && src[i+1] >= '0' && src[i+1] <= '9' {
				width = 4
			}
		}
		if width == 4 && i+4 <= len(src) && src[i] >= 0x81 && src[i] <= 0xfe && src[i+2] >= 0x81 && src[i+2] <= 0xfe && src[i+3] >= '0' && src[i+3] <= '9' {
			pointer := (int(src[i])-0x81)*12600 + (int(src[i+1])-0x30)*1260 + (int(src[i+2])-0x81)*10 + int(src[i+3]) - 0x30
			if pointer >= 189000 {
				if pointer > 1237575 {
					return nil, errors.New("GB18030 字符超出 Unicode 范围")
				}
				if e := flush(i); e != nil {
					return nil, e
				}
				out = utf8.AppendRune(out, rune(pointer-189000+0x10000))
				i += 4
				start = i
				continue
			}
		}
		i += width
	}
	if e := flush(len(src)); e != nil {
		return nil, e
	}
	return out, nil
}
