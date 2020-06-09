package proto

import (
	"fmt"
	"reflect"
	"sync/atomic"
	"unsafe"
)

func Size(v interface{}) int {
	t, p := inspect(v)
	c := cachedCodecOf(t)
	return c.size(p, inline)
}

func Marshal(v interface{}) ([]byte, error) {
	t, p := inspect(v)
	c := cachedCodecOf(t)
	b := make([]byte, c.size(p, inline))
	_, err := c.encode(b, p, inline)
	if err != nil {
		return nil, fmt.Errorf("proto.Marshal(%T): %w", v, err)
	}
	return b, nil
}

func MarshalTo(b []byte, v interface{}) (int, error) {
	t, p := inspect(v)
	c := cachedCodecOf(t)
	n, err := c.encode(b, p, inline)
	if err != nil {
		err = fmt.Errorf("proto.MarshalTo: %w", err)
	}
	return n, err
}

func Unmarshal(b []byte, v interface{}) error {
	if len(b) == 0 {
		return nil
	}

	t, p := inspect(v)
	t = t.Elem() // Unmarshal must be passed a pointer
	c := cachedCodecOf(t)

	n, err := c.decode(b, p, noflags)
	if err != nil {
		return err
	}
	if n < len(b) {
		return fmt.Errorf("proto.Unmarshal(%T): read=%d < buffer=%d", v, n, len(b))
	}
	return nil
}

type flags uintptr

const (
	noflags  flags = 0
	inline   flags = 1 << 0
	wantzero flags = 1 << 1
)

func (f flags) has(x flags) bool {
	return (f & x) != 0
}

func (f flags) with(x flags) flags {
	return f | x
}

func (f flags) without(x flags) flags {
	return f & ^x
}

type iface struct {
	typ unsafe.Pointer
	ptr unsafe.Pointer
}

func inspect(v interface{}) (reflect.Type, unsafe.Pointer) {
	return reflect.TypeOf(v), pointer(v)
}

func pointer(v interface{}) unsafe.Pointer {
	return (*iface)(unsafe.Pointer(&v)).ptr
}

func inlined(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Ptr:
		return true
	case reflect.Map:
		return true
	case reflect.Struct:
		return t.NumField() == 1 && inlined(t.Field(0).Type)
	default:
		return false
	}
}

type fieldNumber int

type wireType int

const (
	varint  wireType = 0
	fixed64 wireType = 1
	varlen  wireType = 2
	fixed32 wireType = 5
)

func (wt wireType) String() string {
	switch wt {
	case varint:
		return "varint"
	case varlen:
		return "varlen"
	case fixed32:
		return "fixed32"
	case fixed64:
		return "fixed64"
	default:
		return "unknown"
	}
}

type codec struct {
	wire   wireType
	size   sizeFunc
	encode encodeFunc
	decode decodeFunc
}

var codecCache atomic.Value // map[unsafe.Pointer]*codec

func loadCachedCodec(t reflect.Type) (*codec, map[unsafe.Pointer]*codec) {
	cache, _ := codecCache.Load().(map[unsafe.Pointer]*codec)
	return cache[pointer(t)], cache
}

func storeCachedCodec(t reflect.Type, oldCache map[unsafe.Pointer]*codec, newCodec *codec) {
	newCache := make(map[unsafe.Pointer]*codec, len(oldCache)+1)
	for p, c := range oldCache {
		newCache[p] = c
	}
	newCache[pointer(t)] = newCodec
	codecCache.Store(newCache)
}

func cachedCodecOf(t reflect.Type) *codec {
	c, m := loadCachedCodec(t)
	if c != nil {
		return c
	}
	c = codecOf(t, make(map[reflect.Type]*codec))
	storeCachedCodec(t, m, c)
	return c
}

func codecOf(t reflect.Type, seen map[reflect.Type]*codec) *codec {
	if c := seen[t]; c != nil {
		return c
	}

	switch t.Kind() {
	case reflect.Bool:
		return &boolCodec

	case reflect.Int:
		return &intCodec

	case reflect.Int32:
		return &int32Codec

	case reflect.Int64:
		return &int64Codec

	case reflect.Uint:
		return &uintCodec

	case reflect.Uint32:
		return &uint32Codec

	case reflect.Uint64:
		return &uint64Codec

	case reflect.Float32:
		return &float32Codec

	case reflect.Float64:
		return &float64Codec

	case reflect.String:
		return &stringCodec

	case reflect.Array:
		elem := t.Elem()
		switch elem.Kind() {
		case reflect.Uint8:
			return byteArrayCodecOf(t, seen)
		}

	case reflect.Slice:
		elem := t.Elem()
		switch elem.Kind() {
		case reflect.Uint8:
			return &bytesCodec
		}

	case reflect.Struct:
		return structCodecOf(t, seen)

	case reflect.Ptr:
		return pointerCodecOf(t, seen)
	}
	panic("unsupported type: " + t.String())
}