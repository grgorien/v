package msgpack

import (
	"bytes"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestMarshalVectors(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want []byte
	}{
		{"nil", nil, []byte{0xc0}},
		{"true", true, []byte{0xc3}},
		{"zero", 0, []byte{0x00}},
		{"127", 127, []byte{0x7f}},
		{"128", 128, []byte{0xcc, 0x80}},
		{"255", 255, []byte{0xcc, 0xff}},
		{"256", 256, []byte{0xcd, 0x01, 0x00}},
		{"65535", 65535, []byte{0xcd, 0xff, 0xff}},
		{"65536", 65536, []byte{0xce, 0x00, 0x01, 0x00, 0x00}},
		{"-1", -1, []byte{0xff}},
		{"-32", -32, []byte{0xe0}},
		{"-33", -33, []byte{0xd0, 0xdf}},
		{"-128", -128, []byte{0xd0, 0x80}},
		{"-129", -129, []byte{0xd1, 0xff, 0x7f}},
		{"str31", strings.Repeat("a", 31), append([]byte{0xbf}, bytes.Repeat([]byte{'a'}, 31)...)},
		{"str32", strings.Repeat("a", 32), append([]byte{0xd9, 0x20}, bytes.Repeat([]byte{'a'}, 32)...)},
		{"empty array", []any{}, []byte{0x90}},
		{"mixed array", []any{0, 1, "x"}, []byte{0x93, 0x00, 0x01, 0xa1, 'x'}},
		{"map", map[string]any{"a": 1}, []byte{0x81, 0xa1, 'a', 0x01}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Marshal(c.in)
			if err != nil {
				t.Fatalf("Marshal error: %v", err)
			}
			if !bytes.Equal(got, c.want) {
				t.Fatalf("got % x want % x", got, c.want)
			}
		})
	}
}

func TestMarshalUnsupported(t *testing.T) {
	if _, err := Marshal(struct{}{}); err == nil {
		t.Fatal("expected error for struct")
	}
}

func TestDecodeFormats(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want any
	}{
		{"nil", []byte{0xc0}, nil},
		{"false", []byte{0xc2}, false},
		{"true", []byte{0xc3}, true},
		{"fixint", []byte{0x2a}, int64(42)},
		{"negfixint", []byte{0xff}, int64(-1)},
		{"uint8", []byte{0xcc, 0xff}, int64(255)},
		{"uint16", []byte{0xcd, 0x01, 0x00}, int64(256)},
		{"uint32", []byte{0xce, 0x00, 0x01, 0x00, 0x00}, int64(65536)},
		{"uint64 big", []byte{0xcf, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, uint64(math.MaxUint64)},
		{"int8", []byte{0xd0, 0x80}, int64(-128)},
		{"int16", []byte{0xd1, 0xff, 0x7f}, int64(-129)},
		{"int32", []byte{0xd2, 0xff, 0xff, 0xff, 0x7f}, int64(-129)},
		{"float32", []byte{0xca, 0x3f, 0x80, 0x00, 0x00}, float64(1)},
		{"float64", []byte{0xcb, 0x3f, 0xf0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, float64(1)},
		{"str8", append([]byte{0xd9, 0x03}, 'a', 'b', 'c'), "abc"},
		{"bin8", append([]byte{0xc4, 0x02}, 0x01, 0x02), []byte{0x01, 0x02}},
		{"ext", append([]byte{0xd4, 0x05}, 0x7f), Ext{Type: 5, Data: []byte{0x7f}}},
		{"array", []byte{0x93, 0x00, 0x01, 0xa1, 'x'}, []any{int64(0), int64(1), "x"}},
		{"map", []byte{0x81, 0xa1, 'a', 0x01}, map[string]any{"a": int64(1)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Unmarshal(c.in)
			if err != nil {
				t.Fatalf("Unmarshal error: %v", err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %#v want %#v", got, c.want)
			}
		})
	}
}

func TestStreamDecode(t *testing.T) {
	b := []byte{0x01, 0xa1, 'x'}
	d := NewDecoder(bytes.NewReader(b))
	v1, err := d.Decode()
	if err != nil {
		t.Fatal(err)
	}
	v2, err := d.Decode()
	if err != nil {
		t.Fatal(err)
	}
	if v1 != int64(1) || v2 != "x" {
		t.Fatalf("got %#v %#v", v1, v2)
	}
}

func TestTruncated(t *testing.T) {
	if _, err := Unmarshal([]byte{0xcd, 0x01}); err == nil {
		t.Fatal("expected error on truncated input")
	}
}

func TestUnknownFormat(t *testing.T) {
	if _, err := Unmarshal([]byte{0xc1}); err == nil {
		t.Fatal("expected error on 0xc1")
	}
}
