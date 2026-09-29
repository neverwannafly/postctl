package postmanlocal

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
)

// idbComparer only needs Chromium's comparator name to open the database;
// reads are full sequential scans, so byte order is sufficient.
type idbComparer struct{}

// Compare orders keys bytewise.
func (idbComparer) Compare(a, b []byte) int { return bytes.Compare(a, b) }

// Name matches Chromium's IndexedDB comparator so LevelDB accepts the files.
func (idbComparer) Name() string { return "idb_cmp1" }

// Separator is unused for read-only access.
func (idbComparer) Separator(dst, _, _ []byte) []byte { return nil }

// Successor is unused for read-only access.
func (idbComparer) Successor(dst, _ []byte) []byte { return nil }

type idbDatabase struct {
	ID     uint64
	Name   string
	Stores map[uint64]string
}

type idbReader struct {
	db        *leveldb.DB
	blobDir   string
	databases map[uint64]*idbDatabase
}

func openIndexedDB(path string) (*idbReader, error) {
	db, err := leveldb.OpenFile(path, &opt.Options{
		Comparer: idbComparer{},
		ReadOnly: true,
	})
	if err != nil {
		return nil, fmt.Errorf("open copied IndexedDB: %w", err)
	}
	reader := &idbReader{
		db:        db,
		blobDir:   strings.TrimSuffix(path, ".leveldb") + ".blob",
		databases: map[uint64]*idbDatabase{},
	}
	if err := reader.readMetadata(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return reader, nil
}

// Close releases the copied database.
func (r *idbReader) Close() error { return r.db.Close() }

func (r *idbReader) readMetadata() error {
	iter := r.db.NewIterator(nil, nil)
	defer iter.Release()
	for iter.Next() {
		key := append([]byte(nil), iter.Key()...)
		if len(key) >= 5 && bytes.Equal(key[:5], []byte{0, 0, 0, 0, 0xc9}) {
			name, err := decodeDatabaseName(key[5:])
			if err != nil {
				continue
			}
			id := decodeTruncatedInt(iter.Value())
			r.databases[id] = &idbDatabase{ID: id, Name: name, Stores: map[uint64]string{}}
		}
	}
	if err := iter.Error(); err != nil {
		return fmt.Errorf("iterate IndexedDB metadata: %w", err)
	}
	iter = r.db.NewIterator(nil, nil)
	defer iter.Release()
	for iter.Next() {
		dbID, storeID, indexID, prefixLen, ok := decodePrefix(iter.Key())
		if !ok || storeID != 0 || indexID != 0 || prefixLen >= len(iter.Key()) || iter.Key()[prefixLen] != 50 {
			continue
		}
		at := prefixLen + 1
		objectStoreID, err := readUvarint(iter.Key(), &at)
		if err != nil || at >= len(iter.Key()) || iter.Key()[at] != 0 {
			continue
		}
		database := r.databases[dbID]
		if database != nil {
			database.Stores[objectStoreID] = decodeUTF16BE(iter.Value())
		}
	}
	return iter.Error()
}

func (r *idbReader) records(databaseMatch func(string) bool, storeName string) ([]map[string]any, int, error) {
	var dbID, storeID uint64
	for id, database := range r.databases {
		if !databaseMatch(database.Name) {
			continue
		}
		for sid, name := range database.Stores {
			if name == storeName {
				dbID, storeID = id, sid
				break
			}
		}
	}
	if dbID == 0 || storeID == 0 {
		return nil, 0, nil
	}
	result := []map[string]any{}
	failures := 0
	iter := r.db.NewIterator(nil, nil)
	defer iter.Release()
	for iter.Next() {
		gotDB, gotStore, indexID, prefixLen, ok := decodePrefix(iter.Key())
		if !ok || gotDB != dbID || gotStore != storeID || indexID != 1 {
			continue
		}
		value, err := decodeIndexedDBValue(append([]byte(nil), iter.Value()...))
		var external externalValueError
		if errors.As(err, &external) {
			value, err = r.readExternalValue(dbID, storeID, iter.Key()[prefixLen:], external.index)
		}
		if err != nil {
			failures++
			continue
		}
		record, ok := value.(map[string]any)
		if !ok {
			failures++
			continue
		}
		result = append(result, record)
	}
	if err := iter.Error(); err != nil {
		return nil, failures, err
	}
	return result, failures, nil
}

func (r *idbReader) readExternalValue(database, store uint64, userKey []byte, wanted uint64) (any, error) {
	iter := r.db.NewIterator(nil, nil)
	defer iter.Release()
	for iter.Next() {
		gotDB, gotStore, indexID, prefixLen, ok := decodePrefix(iter.Key())
		if !ok || gotDB != database || gotStore != store || indexID != 3 ||
			!bytes.Equal(iter.Key()[prefixLen:], userKey) {
			continue
		}
		blobNumber, err := externalBlobNumber(iter.Value(), wanted)
		if err != nil {
			return nil, err
		}
		path := filepath.Join(
			r.blobDir,
			fmt.Sprintf("%x", database),
			fmt.Sprintf("%02x", blobNumber>>8),
			fmt.Sprintf("%x", blobNumber),
		)
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read copied IndexedDB blob: %w", err)
		}
		return decodeBlinkValue(raw)
	}
	if err := iter.Error(); err != nil {
		return nil, err
	}
	return nil, errors.New("IndexedDB external value metadata is missing")
}

func externalBlobNumber(data []byte, wanted uint64) (uint64, error) {
	at := 0
	for index := uint64(0); at < len(data); index++ {
		objectType := data[at]
		at++
		if objectType > 1 {
			return 0, errors.New("unsupported IndexedDB external object type")
		}
		blobNumber, err := readUvarint(data, &at)
		if err != nil {
			return 0, err
		}
		mimeLength, err := readUvarint(data, &at)
		if err != nil || int(mimeLength)*2 > len(data)-at {
			return 0, errors.New("invalid IndexedDB blob metadata")
		}
		at += int(mimeLength) * 2
		if _, err := readUvarint(data, &at); err != nil { // Data size.
			return 0, err
		}
		if objectType == 1 {
			nameLength, err := readUvarint(data, &at)
			if err != nil || int(nameLength)*2 > len(data)-at {
				return 0, errors.New("invalid IndexedDB file metadata")
			}
			at += int(nameLength) * 2
			if _, err := readUvarint(data, &at); err != nil { // Last modified.
				return 0, err
			}
		}
		if index == wanted {
			return blobNumber, nil
		}
	}
	return 0, errors.New("IndexedDB external value index is missing")
}

func findLevelDBs(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var result []string
	for _, entry := range entries {
		if entry.IsDir() && strings.HasSuffix(entry.Name(), ".indexeddb.leveldb") {
			result = append(result, filepath.Join(root, entry.Name()))
		}
	}
	return result, nil
}

func decodePrefix(key []byte) (database, store, index uint64, length int, ok bool) {
	if len(key) < 4 {
		return 0, 0, 0, 0, false
	}
	dbLen := int((key[0]>>5)&7) + 1
	storeLen := int((key[0]>>2)&7) + 1
	indexLen := int(key[0]&3) + 1
	length = 1 + dbLen + storeLen + indexLen
	if len(key) < length {
		return 0, 0, 0, 0, false
	}
	at := 1
	database = littleEndianInt(key[at : at+dbLen])
	at += dbLen
	store = littleEndianInt(key[at : at+storeLen])
	at += storeLen
	index = littleEndianInt(key[at : at+indexLen])
	return database, store, index, length, true
}

func littleEndianInt(raw []byte) uint64 {
	var value uint64
	for i, b := range raw {
		value |= uint64(b) << (8 * i)
	}
	return value
}

func decodeTruncatedInt(raw []byte) uint64 { return littleEndianInt(raw) }

func decodeDatabaseName(raw []byte) (string, error) {
	at := 0
	originLength, err := readUvarint(raw, &at)
	if err != nil {
		return "", err
	}
	originBytes := int(originLength) * 2
	if originBytes > len(raw)-at {
		return "", errors.New("invalid IndexedDB origin")
	}
	at += originBytes
	nameLength, err := readUvarint(raw, &at)
	if err != nil {
		return "", err
	}
	nameBytes := int(nameLength) * 2
	if nameBytes > len(raw)-at {
		return "", errors.New("invalid IndexedDB database name")
	}
	return decodeUTF16BE(raw[at : at+nameBytes]), nil
}

func decodeUTF16BE(raw []byte) string {
	if len(raw)%2 != 0 {
		return ""
	}
	units := make([]uint16, len(raw)/2)
	for i := range units {
		units[i] = binary.BigEndian.Uint16(raw[i*2:])
	}
	return string(utf16.Decode(units))
}
