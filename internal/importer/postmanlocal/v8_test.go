package postmanlocal

import (
	"encoding/binary"
	"math"
	"reflect"
	"testing"

	"github.com/golang/snappy"
)

func TestDecodeV8ObjectAndArray(t *testing.T) {
	data := []byte{0xff, 15, 'o'}
	data = append(data, v8String("name")...)
	data = append(data, v8String("synthetic")...)
	data = append(data, v8String("items")...)
	data = append(data, 'A', 3, 'T', '0', 'I', 84, '$', 0, 3)
	data = append(data, '{', 2)

	got, err := decodeV8(data)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"name":  "synthetic",
		"items": []any{true, nil, int64(42)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decoded value mismatch: %#v", got)
	}
}

func TestDecodeV8NumbersAndTwoByteString(t *testing.T) {
	number := []byte{0xff, 15, 'N'}
	number = binary.LittleEndian.AppendUint64(number, math.Float64bits(12.5))
	got, err := decodeV8(number)
	if err != nil || got != 12.5 {
		t.Fatalf("decode number: got %#v, err %v", got, err)
	}

	twoByte := []byte{0xff, 15, 'c', 4, 'G', 0, 'o', 0}
	got, err = decodeV8(twoByte)
	if err != nil || got != "Go" {
		t.Fatalf("decode two-byte string: got %#v, err %v", got, err)
	}
}

func TestDecodeSnappyIndexedDBEnvelope(t *testing.T) {
	v8 := append([]byte{0xff, 15}, v8String("compressed")...)
	blink := append([]byte{0xff, 20}, v8...)
	compressed := snappy.Encode(nil, blink)
	record := append([]byte{1, 0xff, 0x11, 2}, compressed...)

	got, err := decodeIndexedDBValue(record)
	if err != nil || got != "compressed" {
		t.Fatalf("decode compressed record: got %#v, err %v", got, err)
	}
}

func TestDecodePrefix(t *testing.T) {
	database, store, index, length, ok := decodePrefix([]byte{0x20, 0x34, 0x12, 0x07, 0x01, 0xaa})
	if !ok || database != 0x1234 || store != 7 || index != 1 || length != 5 {
		t.Fatalf("unexpected prefix: db=%d store=%d index=%d length=%d ok=%v", database, store, index, length, ok)
	}
}

func TestExternalBlobNumber(t *testing.T) {
	metadata := []byte{0, 42, 3, 0, 'a', 0, 'p', 0, 'i', 9}
	number, err := externalBlobNumber(metadata, 0)
	if err != nil {
		t.Fatal(err)
	}
	if number != 42 {
		t.Fatalf("blob number = %d", number)
	}
}

func v8String(value string) []byte {
	result := []byte{'S'}
	result = binary.AppendUvarint(result, uint64(len(value)))
	return append(result, value...)
}
