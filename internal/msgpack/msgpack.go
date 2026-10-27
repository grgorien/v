// Implements a minimal subset of the MessagePack encoding,
// sufficient for Neovim's msgpack-rpc protocol.
package msgpack

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"sort"
)

// Ext is a MessagePack extension value.
type Ext struct {
	Type int8
	Data []byte
}

var errUnknownFormat = errors.New("msgpack: unknown format byte 0xc1")

// maxContainer bounds container sizes to avoid allocation bombs.
const maxContainer = 1 << 24

// Marshal encodes v into MessagePack.
func Marshal(v any) ([]byte, error) {
	var b []byte
	b, err := encode(b, v)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func encode(b []byte, v any) ([]byte, error) {
	if v == nil {
		return append(b, 0xc0), nil
	}
	switch x := v.(type) {
	case bool:
		if x {
			return append(b, 0xc3), nil
		}
		return append(b, 0xc2), nil
	case int:
		return encodeInt(b, int64(x))
	case int8:
		return encodeInt(b, int64(x))
	case int16:
		return encodeInt(b, int64(x))
	case int32:
		return encodeInt(b, int64(x))
	case int64:
		return encodeInt(b, x)
	case uint:
		return encodeUint(b, uint64(x))
	case uint8:
		return encodeUint(b, uint64(x))
	case uint16:
		return encodeUint(b, uint64(x))
	case uint32:
		return encodeUint(b, uint64(x))
	case uint64:
		return encodeUint(b, x)
	case float64:
		b = append(b, 0xcb)
		b = binary.BigEndian.AppendUint64(b, math.Float64bits(x))
		return b, nil
	case string:
		return encodeString(b, x)
	case []byte:
		return encodeBin(b, x)
	case []string:
		b = encodeArrayHeader(b, len(x))
		for _, s := range x {
			var err error
			b, err = encodeString(b, s)
			if err != nil {
				return nil, err
			}
		}
		return b, nil
	case []any:
		b = encodeArrayHeader(b, len(x))
		for _, e := range x {
			var err error
			b, err = encode(b, e)
			if err != nil {
				return nil, err
			}
		}
		return b, nil
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b = encodeMapHeader(b, len(keys))
		for _, k := range keys {
			var err error
			b, err = encodeString(b, k)
			if err != nil {
				return nil, err
			}
			b, err = encode(b, x[k])
			if err != nil {
				return nil, err
			}
		}
		return b, nil
	}
	return nil, fmt.Errorf("msgpack: unsupported type %s", reflect.TypeOf(v).String())
}

func encodeInt(b []byte, x int64) ([]byte, error) {
	if x >= 0 {
		return encodeUint(b, uint64(x))
	}
	switch {
	case x >= -32:
		return append(b, byte(int8(x))), nil
	case x >= math.MinInt8:
		return append(b, 0xd0, byte(int8(x))), nil
	case x >= math.MinInt16:
		b = append(b, 0xd1)
		b = binary.BigEndian.AppendUint16(b, uint16(int16(x)))
		return b, nil
	case x >= math.MinInt32:
		b = append(b, 0xd2)
		b = binary.BigEndian.AppendUint32(b, uint32(int32(x)))
		return b, nil
	default:
		b = append(b, 0xd3)
		b = binary.BigEndian.AppendUint64(b, uint64(x))
		return b, nil
	}
}

func encodeUint(b []byte, x uint64) ([]byte, error) {
	switch {
	case x <= 0x7f:
		return append(b, byte(x)), nil
	case x <= math.MaxUint8:
		return append(b, 0xcc, byte(x)), nil
	case x <= math.MaxUint16:
		b = append(b, 0xcd)
		b = binary.BigEndian.AppendUint16(b, uint16(x))
		return b, nil
	case x <= math.MaxUint32:
		b = append(b, 0xce)
		b = binary.BigEndian.AppendUint32(b, uint32(x))
		return b, nil
	default:
		b = append(b, 0xcf)
		b = binary.BigEndian.AppendUint64(b, x)
		return b, nil
	}
}

func encodeString(b []byte, s string) ([]byte, error) {
	n := len(s)
	switch {
	case n <= 31:
		b = append(b, 0xa0|byte(n))
	case n <= math.MaxUint8:
		b = append(b, 0xd9, byte(n))
	case n <= math.MaxUint16:
		b = append(b, 0xda)
		b = binary.BigEndian.AppendUint16(b, uint16(n))
	default:
		b = append(b, 0xdb)
		b = binary.BigEndian.AppendUint32(b, uint32(n))
	}
	return append(b, s...), nil
}

func encodeBin(b []byte, x []byte) ([]byte, error) {
	n := len(x)
	switch {
	case n <= math.MaxUint8:
		b = append(b, 0xc4, byte(n))
	case n <= math.MaxUint16:
		b = append(b, 0xc5)
		b = binary.BigEndian.AppendUint16(b, uint16(n))
	default:
		b = append(b, 0xc6)
		b = binary.BigEndian.AppendUint32(b, uint32(n))
	}
	return append(b, x...), nil
}

func encodeArrayHeader(b []byte, n int) []byte {
	switch {
	case n <= 15:
		return append(b, 0x90|byte(n))
	case n <= math.MaxUint16:
		b = append(b, 0xdc)
		return binary.BigEndian.AppendUint16(b, uint16(n))
	default:
		b = append(b, 0xdd)
		return binary.BigEndian.AppendUint32(b, uint32(n))
	}
}

func encodeMapHeader(b []byte, n int) []byte {
	switch {
	case n <= 15:
		return append(b, 0x80|byte(n))
	case n <= math.MaxUint16:
		b = append(b, 0xde)
		return binary.BigEndian.AppendUint16(b, uint16(n))
	default:
		b = append(b, 0xdf)
		return binary.BigEndian.AppendUint32(b, uint32(n))
	}
}

// Decoder decodes MessagePack values from a stream.
type Decoder struct {
	r *bufio.Reader
}

// NewDecoder returns a decoder reading from r.
func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{r: bufio.NewReader(r)}
}

// Unmarshal decodes a single value from b.
func Unmarshal(b []byte) (any, error) {
	d := NewDecoder(bytesReader(b))
	return d.Decode()
}

type sliceReader struct {
	b []byte
	i int
}

func (s *sliceReader) Read(p []byte) (int, error) {
	if s.i >= len(s.b) {
		return 0, io.EOF
	}
	n := copy(p, s.b[s.i:])
	s.i += n
	return n, nil
}

func bytesReader(b []byte) io.Reader { return &sliceReader{b: b} }

// Decode decodes the next value.
func (d *Decoder) Decode() (any, error) {
	c, err := d.r.ReadByte()
	if err != nil {
		return nil, err
	}
	return d.decodeWithFormat(c)
}

func (d *Decoder) readFull(p []byte) error {
	_, err := io.ReadFull(d.r, p)
	return err
}

func (d *Decoder) readByte() (byte, error) {
	return d.r.ReadByte()
}

func (d *Decoder) readUint16() (uint16, error) {
	var p [2]byte
	if err := d.readFull(p[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(p[:]), nil
}

func (d *Decoder) readUint32() (uint32, error) {
	var p [4]byte
	if err := d.readFull(p[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(p[:]), nil
}

func (d *Decoder) readUint64() (uint64, error) {
	var p [8]byte
	if err := d.readFull(p[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint64(p[:]), nil
}

func (d *Decoder) readStr(n int) (string, error) {
	if n == 0 {
		return "", nil
	}
	p := make([]byte, n)
	if err := d.readFull(p); err != nil {
		return "", err
	}
	return string(p), nil
}

func (d *Decoder) readBin(n int) ([]byte, error) {
	if n == 0 {
		return []byte{}, nil
	}
	p := make([]byte, n)
	if err := d.readFull(p); err != nil {
		return nil, err
	}
	return p, nil
}

func (d *Decoder) readArray(n int) ([]any, error) {
	if n > maxContainer {
		return nil, fmt.Errorf("msgpack: array too large (%d)", n)
	}
	out := make([]any, n)
	for i := 0; i < n; i++ {
		v, err := d.Decode()
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

func (d *Decoder) readMap(n int) (map[string]any, error) {
	if n > maxContainer {
		return nil, fmt.Errorf("msgpack: map too large (%d)", n)
	}
	out := make(map[string]any, n)
	// can use range over int
	for i := 0; i < n; i++ {
		k, err := d.Decode()
		if err != nil {
			return nil, err
		}
		v, err := d.Decode()
		if err != nil {
			return nil, err
		}
		var key string
		if s, ok := k.(string); ok {
			key = s
		} else {
			key = fmt.Sprint(k)
		}
		out[key] = v
	}
	return out, nil
}

func (d *Decoder) readExt(n int) (Ext, error) {
	t, err := d.readByte()
	if err != nil {
		return Ext{}, err
	}
	var data []byte
	if n > 0 {
		data = make([]byte, n)
		if err := d.readFull(data); err != nil {
			return Ext{}, err
		}
	}
	return Ext{Type: int8(t), Data: data}, nil
}

func (d *Decoder) decodeWithFormat(c byte) (any, error) {
	switch {
	case c <= 0x7f:
		return int64(c), nil
	case c >= 0xe0:
		return int64(int8(c)), nil
	case c >= 0x80 && c <= 0x8f:
		return d.readMap(int(c & 0x0f))
	case c >= 0x90 && c <= 0x9f:
		return d.readArray(int(c & 0x0f))
	case c >= 0xa0 && c <= 0xbf:
		return d.readStr(int(c & 0x1f))
	}
	switch c {
	case 0xc0:
		return nil, nil
	case 0xc1:
		return nil, errUnknownFormat
	case 0xc2:
		return false, nil
	case 0xc3:
		return true, nil
	case 0xc4:
		n, err := d.readByte()
		if err != nil {
			return nil, err
		}
		return d.readBin(int(n))
	case 0xc5:
		n, err := d.readUint16()
		if err != nil {
			return nil, err
		}
		return d.readBin(int(n))
	case 0xc6:
		n, err := d.readUint32()
		if err != nil {
			return nil, err
		}
		return d.readBin(int(n))
	case 0xc7:
		n, err := d.readByte()
		if err != nil {
			return nil, err
		}
		return d.readExt(int(n))
	case 0xc8:
		n, err := d.readUint16()
		if err != nil {
			return nil, err
		}
		return d.readExt(int(n))
	case 0xc9:
		n, err := d.readUint32()
		if err != nil {
			return nil, err
		}
		return d.readExt(int(n))
	case 0xca:
		u, err := d.readUint32()
		if err != nil {
			return nil, err
		}
		return float64(math.Float32frombits(u)), nil
	case 0xcb:
		u, err := d.readUint64()
		if err != nil {
			return nil, err
		}
		return math.Float64frombits(u), nil
	case 0xcc:
		n, err := d.readByte()
		if err != nil {
			return nil, err
		}
		return int64(n), nil
	case 0xcd:
		n, err := d.readUint16()
		if err != nil {
			return nil, err
		}
		return int64(n), nil
	case 0xce:
		n, err := d.readUint32()
		if err != nil {
			return nil, err
		}
		return int64(n), nil
	case 0xcf:
		n, err := d.readUint64()
		if err != nil {
			return nil, err
		}
		if n > math.MaxInt64 {
			return n, nil
		}
		return int64(n), nil
	case 0xd0:
		n, err := d.readByte()
		if err != nil {
			return nil, err
		}
		return int64(int8(n)), nil
	case 0xd1:
		n, err := d.readUint16()
		if err != nil {
			return nil, err
		}
		return int64(int16(n)), nil
	case 0xd2:
		n, err := d.readUint32()
		if err != nil {
			return nil, err
		}
		return int64(int32(n)), nil
	case 0xd3:
		n, err := d.readUint64()
		if err != nil {
			return nil, err
		}
		return int64(n), nil
	case 0xd4:
		return d.readExt(1)
	case 0xd5:
		return d.readExt(2)
	case 0xd6:
		return d.readExt(4)
	case 0xd7:
		return d.readExt(8)
	case 0xd8:
		return d.readExt(16)
	case 0xd9:
		n, err := d.readByte()
		if err != nil {
			return nil, err
		}
		return d.readStr(int(n))
	case 0xda:
		n, err := d.readUint16()
		if err != nil {
			return nil, err
		}
		return d.readStr(int(n))
	case 0xdb:
		n, err := d.readUint32()
		if err != nil {
			return nil, err
		}
		return d.readStr(int(n))
	case 0xdc:
		n, err := d.readUint16()
		if err != nil {
			return nil, err
		}
		return d.readArray(int(n))
	case 0xdd:
		n, err := d.readUint32()
		if err != nil {
			return nil, err
		}
		return d.readArray(int(n))
	case 0xde:
		n, err := d.readUint16()
		if err != nil {
			return nil, err
		}
		return d.readMap(int(n))
	case 0xdf:
		n, err := d.readUint32()
		if err != nil {
			return nil, err
		}
		return d.readMap(int(n))
	}
	return nil, fmt.Errorf("msgpack: unsupported format byte 0x%02x", c)
}
