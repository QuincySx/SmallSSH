package tasks

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// unwrapWOFF exposes the design's WOFF 1.0 font tables as an SFNT font for
// Core Text. The native app uses the same glyphs as SmallPen.
func unwrapWOFF(source []byte) ([]byte, error) {
	be := binary.BigEndian
	const header, entry = 44, 20
	if len(source) < header || string(source[:4]) != "wOFF" || int(be.Uint32(source[8:])) != len(source) {
		return nil, errors.New("invalid WOFF header")
	}
	count, size := int(be.Uint16(source[12:])), int(be.Uint32(source[16:]))
	dirEnd := 12 + 16*count
	if count == 0 || header+entry*count > len(source) || size < dirEnd || size > 16<<20 {
		return nil, errors.New("invalid WOFF table directory")
	}
	font := make([]byte, size)
	copy(font[:4], source[4:8])
	be.PutUint16(font[4:], uint16(count))
	power, selector := 1, 0
	for power*2 <= count {
		power *= 2
		selector++
	}
	be.PutUint16(font[6:], uint16(16*power))
	be.PutUint16(font[8:], uint16(selector))
	be.PutUint16(font[10:], uint16(16*(count-power)))
	dst := dirEnd
	for index := range count {
		table := source[header+entry*index:]
		offset, compressed, length := int(be.Uint32(table[4:])), int(be.Uint32(table[8:])), int(be.Uint32(table[12:]))
		if offset < header+entry*count || offset > len(source) || compressed > len(source)-offset || compressed > length || length > len(font)-dst {
			return nil, fmt.Errorf("invalid WOFF table %q", table[:4])
		}
		record := font[12+16*index:]
		copy(record[:4], table[:4])
		copy(record[4:8], table[16:20])
		be.PutUint32(record[8:], uint32(dst))
		be.PutUint32(record[12:], uint32(length))
		raw := source[offset : offset+compressed]
		if compressed == length {
			copy(font[dst:dst+length], raw)
		} else {
			reader, err := zlib.NewReader(bytes.NewReader(raw))
			if err != nil {
				return nil, err
			}
			decoded, err := io.ReadAll(io.LimitReader(reader, int64(length)+1))
			closeErr := reader.Close()
			if err != nil || closeErr != nil || len(decoded) != length {
				return nil, errors.Join(err, closeErr, fmt.Errorf("invalid compressed WOFF table %q", table[:4]))
			}
			copy(font[dst:dst+length], decoded)
		}
		dst += (length + 3) &^ 3
	}
	if dst != size {
		return nil, errors.New("invalid WOFF font size")
	}
	return font, nil
}
