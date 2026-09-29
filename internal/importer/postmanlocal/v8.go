package postmanlocal

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"time"
	"unicode/utf16"

	"github.com/golang/snappy"
)

type externalValueError struct {
	index uint64
}

func (e externalValueError) Error() string { return "IndexedDB value is stored in a blob" }

type v8Decoder struct {
	data    []byte
	at      int
	version uint64
	objects []any
}

// decodeIndexedDBValue removes Chromium's IndexedDB and Blink envelopes.
func decodeIndexedDBValue(data []byte) (any, error) {
	at := 0
	if _, err := readUvarint(data, &at); err != nil { // IndexedDB value version.
		return nil, err
	}
	if at >= len(data) || data[at] != 0xff {
		return nil, errors.New("missing Blink envelope")
	}
	at++
	blinkVersion, err := readUvarint(data, &at)
	if err != nil {
		return nil, err
	}
	if blinkVersion == 0x11 {
		if at >= len(data) {
			return nil, errors.New("truncated Blink processing envelope")
		}
		switch data[at] {
		case 1:
			at++
			if _, err := readUvarint(data, &at); err != nil { // Serialized size.
				return nil, err
			}
			index, err := readUvarint(data, &at)
			if err != nil {
				return nil, err
			}
			return nil, externalValueError{index: index}
		case 2:
			unpacked, err := snappy.Decode(nil, data[at+1:])
			if err != nil {
				return nil, fmt.Errorf("decode Snappy value: %w", err)
			}
			return decodeBlinkValue(unpacked)
		default:
			return nil, errors.New("unknown Blink processing envelope")
		}
	}
	if blinkVersion >= 21 {
		if len(data)-at < 13 || data[at] != 0xfe {
			return nil, errors.New("invalid Blink trailer")
		}
		at += 13
	}
	return decodeV8(data[at:])
}

func decodeBlinkValue(data []byte) (any, error) {
	at := 0
	if at >= len(data) || data[at] != 0xff {
		return nil, errors.New("missing Blink envelope")
	}
	at++
	version, err := readUvarint(data, &at)
	if err != nil {
		return nil, err
	}
	if version >= 21 {
		if len(data)-at < 13 || data[at] != 0xfe {
			return nil, errors.New("invalid Blink trailer")
		}
		at += 13
	}
	return decodeV8(data[at:])
}

func decodeV8(data []byte) (any, error) {
	d := &v8Decoder{data: data}
	if d.byte() != 0xff {
		return nil, errors.New("missing V8 version tag")
	}
	version, err := d.uvarint()
	if err != nil {
		return nil, err
	}
	d.version = version
	return d.value()
}

func (d *v8Decoder) value() (any, error) {
	tag, err := d.tag()
	if err != nil {
		return nil, err
	}
	switch tag {
	case '-', '_': // Hole and undefined are represented as nil in JSON.
		return nil, nil
	case '0':
		return nil, nil
	case 'T':
		return true, nil
	case 'F':
		return false, nil
	case 'I':
		n, err := d.uvarint()
		return int64(n>>1) ^ -int64(n&1), err
	case 'U':
		return d.uvarint()
	case 'N':
		return d.float64()
	case 'Z':
		return d.bigint()
	case 'S':
		return d.stringBytes("UTF-8")
	case '"':
		return d.stringBytes("Latin-1")
	case 'c':
		return d.twoByteString()
	case '^':
		id, err := d.uvarint()
		if err != nil || id >= uint64(len(d.objects)) {
			return nil, errors.New("invalid V8 object reference")
		}
		return d.objects[id], nil
	case 'o':
		return d.object()
	case 'A':
		return d.denseArray()
	case 'a':
		return d.sparseArray()
	case 'D':
		n, err := d.float64()
		if err != nil {
			return nil, err
		}
		value := time.UnixMilli(int64(n)).UTC()
		d.objects = append(d.objects, value)
		return value, nil
	case ';':
		return d.jsMap()
	case '\'':
		return d.jsSet()
	case 'B':
		n, err := d.uvarint()
		if err != nil {
			return nil, err
		}
		value, err := d.take(int(n))
		if err == nil {
			d.objects = append(d.objects, value)
		}
		return value, err
	case 'y':
		d.objects = append(d.objects, true)
		return true, nil
	case 'x':
		d.objects = append(d.objects, false)
		return false, nil
	case 'n':
		value, err := d.float64()
		if err == nil {
			d.objects = append(d.objects, value)
		}
		return value, err
	case 's':
		value, err := d.value()
		if err == nil {
			d.objects = append(d.objects, value)
		}
		return value, err
	default:
		return nil, fmt.Errorf("unsupported V8 tag 0x%02x", tag)
	}
}

func (d *v8Decoder) object() (any, error) {
	result := map[string]any{}
	d.objects = append(d.objects, result)
	count := uint64(0)
	for {
		tag, err := d.peekTag()
		if err != nil {
			return nil, err
		}
		if tag == '{' {
			_, _ = d.tag()
			break
		}
		key, err := d.value()
		if err != nil {
			return nil, err
		}
		value, err := d.value()
		if err != nil {
			return nil, err
		}
		result[jsonKey(key)] = value
		count++
	}
	expected, err := d.uvarint()
	if err != nil {
		return nil, err
	}
	if expected != count {
		return nil, errors.New("V8 object property count mismatch")
	}
	return result, nil
}

func (d *v8Decoder) denseArray() (any, error) {
	length, err := d.uvarint()
	if err != nil {
		return nil, err
	}
	result := make([]any, int(length))
	d.objects = append(d.objects, result)
	for i := range result {
		result[i], err = d.value()
		if err != nil {
			return nil, err
		}
	}
	for {
		tag, err := d.peekTag()
		if err != nil {
			return nil, err
		}
		if tag == '$' {
			_, _ = d.tag()
			break
		}
		if _, err = d.value(); err != nil {
			return nil, err
		}
		if _, err = d.value(); err != nil {
			return nil, err
		}
	}
	propertyCount, err := d.uvarint()
	if err != nil {
		return nil, err
	}
	finalLength, err := d.uvarint()
	if err != nil || finalLength != length || propertyCount != 0 {
		return nil, errors.New("unsupported dense array properties")
	}
	return result, nil
}

func (d *v8Decoder) sparseArray() (any, error) {
	length, err := d.uvarint()
	if err != nil {
		return nil, err
	}
	result := make([]any, int(length))
	d.objects = append(d.objects, result)
	count := uint64(0)
	for {
		tag, err := d.peekTag()
		if err != nil {
			return nil, err
		}
		if tag == '@' {
			_, _ = d.tag()
			break
		}
		key, err := d.value()
		if err != nil {
			return nil, err
		}
		value, err := d.value()
		if err != nil {
			return nil, err
		}
		index, err := strconv.Atoi(jsonKey(key))
		if err == nil && index >= 0 && index < len(result) {
			result[index] = value
		}
		count++
	}
	expected, err := d.uvarint()
	if err != nil {
		return nil, err
	}
	finalLength, err := d.uvarint()
	if err != nil || expected != count || finalLength != length {
		return nil, errors.New("V8 sparse array metadata mismatch")
	}
	return result, nil
}

func (d *v8Decoder) jsMap() (any, error) {
	result := map[string]any{}
	d.objects = append(d.objects, result)
	count := uint64(0)
	for {
		tag, err := d.peekTag()
		if err != nil {
			return nil, err
		}
		if tag == ':' {
			_, _ = d.tag()
			break
		}
		key, err := d.value()
		if err != nil {
			return nil, err
		}
		value, err := d.value()
		if err != nil {
			return nil, err
		}
		result[jsonKey(key)] = value
		count += 2
	}
	expected, err := d.uvarint()
	if err != nil || expected != count {
		return nil, errors.New("V8 map count mismatch")
	}
	return result, nil
}

func (d *v8Decoder) jsSet() (any, error) {
	result := []any{}
	d.objects = append(d.objects, &result)
	for {
		tag, err := d.peekTag()
		if err != nil {
			return nil, err
		}
		if tag == ',' {
			_, _ = d.tag()
			break
		}
		value, err := d.value()
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	expected, err := d.uvarint()
	if err != nil || expected != uint64(len(result)) {
		return nil, errors.New("V8 set count mismatch")
	}
	return result, nil
}

func (d *v8Decoder) bigint() (any, error) {
	flags, err := d.uvarint()
	if err != nil {
		return nil, err
	}
	size := int(flags >> 4)
	raw, err := d.take(size * 8)
	if err != nil {
		return nil, err
	}
	reversed := append([]byte(nil), raw...)
	for left, right := 0, len(reversed)-1; left < right; left, right = left+1, right-1 {
		reversed[left], reversed[right] = reversed[right], reversed[left]
	}
	n := new(big.Int).SetBytes(reversed)
	if flags&1 != 0 {
		n.Neg(n)
	}
	return n.String(), nil
}

func (d *v8Decoder) stringBytes(encoding string) (any, error) {
	length, err := d.uvarint()
	if err != nil {
		return nil, err
	}
	raw, err := d.take(int(length))
	if err != nil {
		return nil, err
	}
	if encoding == "Latin-1" {
		runes := make([]rune, len(raw))
		for i, b := range raw {
			runes[i] = rune(b)
		}
		return string(runes), nil
	}
	return string(raw), nil
}

func (d *v8Decoder) twoByteString() (any, error) {
	length, err := d.uvarint()
	if err != nil {
		return nil, err
	}
	raw, err := d.take(int(length))
	if err != nil || len(raw)%2 != 0 {
		return nil, errors.New("invalid V8 two-byte string")
	}
	units := make([]uint16, len(raw)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(raw[i*2:])
	}
	return string(utf16.Decode(units)), nil
}

func (d *v8Decoder) float64() (float64, error) {
	raw, err := d.take(8)
	if err != nil {
		return 0, err
	}
	return math.Float64frombits(binary.LittleEndian.Uint64(raw)), nil
}

func (d *v8Decoder) tag() (byte, error) {
	for d.at < len(d.data) && d.data[d.at] == 0 {
		d.at++
	}
	if d.at >= len(d.data) {
		return 0, errors.New("truncated V8 value")
	}
	tag := d.data[d.at]
	d.at++
	return tag, nil
}

func (d *v8Decoder) peekTag() (byte, error) {
	saved := d.at
	tag, err := d.tag()
	d.at = saved
	return tag, err
}

func (d *v8Decoder) uvarint() (uint64, error) {
	return readUvarint(d.data, &d.at)
}

func (d *v8Decoder) byte() byte {
	if d.at >= len(d.data) {
		return 0
	}
	value := d.data[d.at]
	d.at++
	return value
}

func (d *v8Decoder) take(n int) ([]byte, error) {
	if n < 0 || n > len(d.data)-d.at {
		return nil, errors.New("truncated V8 value")
	}
	value := append([]byte(nil), d.data[d.at:d.at+n]...)
	d.at += n
	return value, nil
}

func readUvarint(data []byte, at *int) (uint64, error) {
	if *at < 0 || *at >= len(data) {
		return 0, errors.New("invalid varint")
	}
	value, n := binary.Uvarint(data[*at:])
	if n <= 0 {
		return 0, errors.New("invalid varint")
	}
	*at += n
	return value, nil
}

func jsonKey(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprint(value)
}
