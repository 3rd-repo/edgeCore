package modbus

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/anviod/edgeCore/internal/model"
	"github.com/anviod/edgeCore/internal/pkg/dataformat"
)

func TestResolvePointFormat_Int16_Default(t *testing.T) {
	p := model.Point{
		DataType: "int16",
	}
	cfg := dataformat.ResolvePointFormat(p, "ABCD")
	if cfg.Type != dataformat.FormatSignedInt {
		t.Fatalf("expect FormatSignedInt, got %d", cfg.Type)
	}
	if cfg.Order != dataformat.OrderABCD {
		t.Fatalf("expect OrderABCD")
	}
}

func TestResolvePointFormat_Expr32_WithWordOrder(t *testing.T) {
	p := model.Point{
		DataType:    "int32",
		ReadFormula: "v>>1",
		WordOrder:   "CDAB",
	}
	cfg := dataformat.ResolvePointFormat(p, "ABCD")
	if cfg.Type != dataformat.FormatExpr32 {
		t.Fatalf("expect FormatExpr32, got %d", cfg.Type)
	}
	if cfg.Order != dataformat.OrderCDAB {
		t.Fatalf("expect OrderCDAB")
	}
	if cfg.ReadExpr != "v>>1" {
		t.Fatalf("unexpected ReadExpr %s", cfg.ReadExpr)
	}
}

func TestPointDecoder_Decode_WithDataformat_Int16(t *testing.T) {
	d := NewPointDecoder("ABCD", 0, 0)
	d.EnableDataformatDecoder(true)

	point := model.Point{
		ID:       "p1",
		DataType: "int16",
		Scale:    0,
		Offset:   0,
	}
	raw := []byte{0x01, 0x00}

	val, quality, err := d.Decode(point, raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if quality != "Good" {
		t.Fatalf("expect Good, got %s", quality)
	}
	if v, ok := val.(int16); !ok || v != 256 {
		t.Fatalf("expect int16 256, got %#v", val)
	}
}

func TestPointDecoder_Decode_WithDataformat_Float32Expr(t *testing.T) {
	d := NewPointDecoder("ABCD", 0, 0)
	d.EnableDataformatDecoder(true)

	point := model.Point{
		ID:          "p2",
		DataType:    "int32",
		ReadFormula: "v>>2",
	}
	raw := []byte{0x00, 0x00, 0x01, 0x00}

	val, _, err := d.Decode(point, raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v, ok := val.(int32); !ok || v != 64 {
		t.Fatalf("expect int32 64, got %#v", val)
	}
}

func TestPointDecoder_Decode_WithDataformat_ParseTypeTakesPrecedence(t *testing.T) {
	d := NewPointDecoder("ABCD", 0, 0)
	d.EnableDataformatDecoder(true)

	point := model.Point{DataType: "int16", ParseType: "INT32"}
	val, _, err := d.Decode(point, []byte{0x00, 0x00, 0x01, 0x08})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v, ok := val.(int32); !ok || v != 264 {
		t.Fatalf("expect int32 264, got %#v", val)
	}
}

func TestPointDecoder_Decode_WithDataformat_Float32Order(t *testing.T) {
	d := NewPointDecoder("CDAB", 0, 0)
	d.EnableDataformatDecoder(true)

	point := model.Point{
		ID:       "p3",
		DataType: "float32",
	}

	f := float32(123.456)
	bits := math.Float32bits(f)
	var base [4]byte
	binary.BigEndian.PutUint32(base[:], bits)
	raw := []byte{base[2], base[3], base[0], base[1]}

	val, _, err := d.Decode(point, raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	v, ok := val.(float32)
	if !ok {
		t.Fatalf("expect float32, got %#v", val)
	}
	if diff := math.Abs(float64(f) - float64(v)); diff > 1e-3 {
		t.Fatalf("diff too large: %v", diff)
	}
}

func TestPointDecoder_Decode_WithDataformat_Int32_CDAB(t *testing.T) {
	d := NewPointDecoder("ABCD", 0, 0)
	d.EnableDataformatDecoder(true)

	point := model.Point{
		ID:        "p4",
		DataType:  "int32",
		WordOrder: "CDAB",
	}

	raw := []byte{0x33, 0x44, 0x11, 0x22}

	val, quality, err := d.Decode(point, raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if quality != "Good" {
		t.Fatalf("expect Good, got %s", quality)
	}
	v, ok := val.(int32)
	if !ok {
		t.Fatalf("expect int32, got %#v", val)
	}
	if v != int32(0x11223344) {
		t.Fatalf("expect 0x11223344, got %#x", uint32(v))
	}
}

func TestPointDecoder_Encode_WithWriteFormula(t *testing.T) {
	d := NewPointDecoder("ABCD", 0, 0)

	point := model.Point{
		ID:           "wf-point",
		DataType:     "uint16",
		Scale:        1,
		Offset:       0,
		WriteFormula: "v*10",
	}

	regs, err := d.Encode(point, 6.0)
	if err != nil {
		t.Fatalf("encode error: %v", err)
	}
	if len(regs) != 1 {
		t.Fatalf("expect 1 reg, got %d", len(regs))
	}
	if regs[0] != 60 {
		t.Fatalf("expect 60, got %d", regs[0])
	}
}

func TestResolvePointFormat_Swap16_OnlyFromPointWordOrder(t *testing.T) {
	// 点位自身 DCBA → Swap16
	cfg := dataformat.ResolvePointFormat(model.Point{DataType: "int16", WordOrder: "DCBA"}, "ABCD")
	if !cfg.Swap16 {
		t.Fatalf("expect Swap16=true for point WordOrder=DCBA")
	}
	// 点位自身 BADC → Swap16
	cfg = dataformat.ResolvePointFormat(model.Point{DataType: "uint16", WordOrder: "BADC"}, "ABCD")
	if !cfg.Swap16 {
		t.Fatalf("expect Swap16=true for point WordOrder=BADC")
	}
	// 点位未配置 word order，仅设备默认 DCBA → 不触发 Swap16（存量通道回归保护）
	cfg = dataformat.ResolvePointFormat(model.Point{DataType: "int16"}, "DCBA")
	if cfg.Swap16 {
		t.Fatalf("expect Swap16=false when only device default is DCBA")
	}
	// 点位 AB/CDAB → 不交换
	cfg = dataformat.ResolvePointFormat(model.Point{DataType: "int16", WordOrder: "CDAB"}, "ABCD")
	if cfg.Swap16 {
		t.Fatalf("expect Swap16=false for CDAB")
	}
}

func TestPointDecoder_Decode_WithDataformat_Int16_BA(t *testing.T) {
	d := NewPointDecoder("ABCD", 0, 0)
	d.EnableDataformatDecoder(true)

	// UI 的 Signed_BA 落盘：parse_type=INT16, word_order=DCBA
	point := model.Point{ID: "p-ba", DataType: "int16", ParseType: "INT16", WordOrder: "DCBA"}
	raw := []byte{0x01, 0x00} // 线上 [01 00]，BA 语义 → 0x0001 = 1

	val, quality, err := d.Decode(point, raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if quality != "Good" {
		t.Fatalf("expect Good, got %s", quality)
	}
	if v, ok := val.(int16); !ok || v != 1 {
		t.Fatalf("expect int16 1, got %#v", val)
	}
}

func TestPointDecoder_Decode_WithDataformat_Int16_BA_DeviceDefaultNoSwap(t *testing.T) {
	// 设备默认 DCBA、点位未配置 word order → 2 字节仍按大端解析（回归保护）
	d := NewPointDecoder("DCBA", 0, 0)
	d.EnableDataformatDecoder(true)

	point := model.Point{ID: "p-default", DataType: "int16"}
	val, _, err := d.Decode(point, []byte{0x01, 0x00})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v, ok := val.(int16); !ok || v != 256 {
		t.Fatalf("expect int16 256 (big-endian), got %#v", val)
	}
}

func TestPointDecoder_DecodeRaw_Uint16_BA(t *testing.T) {
	// 传统 decodeRaw 路径（未启用 dataformat 解码器）也要尊重点位 word order
	d := NewPointDecoder("ABCD", 0, 0)

	point := model.Point{ID: "p-raw", DataType: "uint16", WordOrder: "DCBA"}
	val, err := d.decodeRaw(point, []byte{0x01, 0x00})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v, ok := val.(uint16); !ok || v != 1 {
		t.Fatalf("expect uint16 1, got %#v", val)
	}

	// AB 点位不受影响
	pointAB := model.Point{ID: "p-raw-ab", DataType: "uint16", WordOrder: "ABCD"}
	val, err = d.decodeRaw(pointAB, []byte{0x01, 0x00})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v, ok := val.(uint16); !ok || v != 256 {
		t.Fatalf("expect uint16 256, got %#v", val)
	}
}

func TestPointDecoder_Encode_Uint16_BA(t *testing.T) {
	d := NewPointDecoder("ABCD", 0, 0)

	// 写 256（0x0100）到 BA 点位 → 寄存器应为 0x0001
	point := model.Point{ID: "p-w-ba", DataType: "uint16", WordOrder: "DCBA"}
	regs, err := d.Encode(point, 256)
	if err != nil {
		t.Fatalf("encode error: %v", err)
	}
	if len(regs) != 1 || regs[0] != 1 {
		t.Fatalf("expect reg 1, got %#v", regs)
	}

	// AB 点位写 256 → 寄存器 0x0100 = 256
	pointAB := model.Point{ID: "p-w-ab", DataType: "uint16", WordOrder: "ABCD"}
	regs, err = d.Encode(pointAB, 256)
	if err != nil {
		t.Fatalf("encode error: %v", err)
	}
	if len(regs) != 1 || regs[0] != 256 {
		t.Fatalf("expect reg 256, got %#v", regs)
	}
}
