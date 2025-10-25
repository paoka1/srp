package common

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"strings"
)

type StatusCode uint8
type TypeCode uint8

const (
	CodeSuccess   StatusCode = 1 // 操作成功
	CodeForbidden StatusCode = 2 // 操作不允许
)

const (
	TypePing       TypeCode = 1 // srp-client 验证
	TypePong       TypeCode = 2 // srp-server 响应
	TypeNewConn    TypeCode = 3 // 新的 user 连接
	TypeAcceptConn TypeCode = 4 // 同意 user 连接
	TypeRejectConn TypeCode = 5 // 拒绝 user 连接
	TypeForwarding TypeCode = 6 // 数据转发
	TypeDisconnect TypeCode = 7 // 断开连接
)

var statusCodeMap = map[StatusCode]string{
	CodeSuccess:   "CodeSuccess",
	CodeForbidden: "CodeForbidden",
}

var typeCodeMap = map[TypeCode]string{
	TypePing:       "TypePing",
	TypePong:       "TypePong",
	TypeNewConn:    "TypeNewConn",
	TypeAcceptConn: "TypeAcceptConn",
	TypeRejectConn: "TypeRejectConn",
	TypeForwarding: "TypeForwarding",
	TypeDisconnect: "TypeDisconnect",
}

type Proto struct {
	Code       StatusCode
	Type       TypeCode
	CID        uint32 // User CID
	PayloadLen uint32 // Payload 长度
	Payload    []byte
}

func NewProto(scode StatusCode, tcode TypeCode, cid uint32, payload []byte) Proto {
	return Proto{
		Code:       scode,
		Type:       tcode,
		CID:        cid,
		PayloadLen: uint32(len(payload)),
		Payload:    payload,
	}
}

func (p *Proto) EncodeProto() ([]byte, error) {
	buf := new(bytes.Buffer)

	if err := binary.Write(buf, binary.BigEndian, p.Code); err != nil {
		return nil, fmt.Errorf("编码Code失败: %w", err)
	}

	if err := binary.Write(buf, binary.BigEndian, p.Type); err != nil {
		return nil, fmt.Errorf("编码Type失败: %w", err)
	}

	if err := binary.Write(buf, binary.BigEndian, p.CID); err != nil {
		return nil, fmt.Errorf("编码cid失败: %w", err)
	}

	if err := binary.Write(buf, binary.BigEndian, p.PayloadLen); err != nil {
		return nil, fmt.Errorf("编码PayloadLen失败: %w", err)
	}

	// []byte 不考虑大小端问题
	if p.PayloadLen > 0 {
		if err := binary.Write(buf, binary.BigEndian, p.Payload); err != nil {
			return nil, fmt.Errorf("编码Payload失败: %w", err)
		}
	}

	return buf.Bytes(), nil
}

func (p *Proto) DecodeProto(reader *bufio.Reader) error {
	// code 和 type 都是 uint8，直接读取 byte 即可
	codeByte, err := reader.ReadByte()
	if err != nil {
		return fmt.Errorf("解码Code失败: %w", err)
	}
	p.Code = StatusCode(codeByte)

	typeByte, err := reader.ReadByte()
	if err != nil {
		return fmt.Errorf("解码Type失败: %w", err)
	}
	p.Type = TypeCode(typeByte)

	cidBytes := make([]byte, 4)
	if _, err := io.ReadFull(reader, cidBytes); err != nil {
		return fmt.Errorf("解码cid失败: %w", err)
	}
	p.CID = binary.BigEndian.Uint32(cidBytes)

	lenBytes := make([]byte, 4)
	if _, err := io.ReadFull(reader, lenBytes); err != nil {
		return fmt.Errorf("解码PayloadLen失败: %w", err)
	}
	p.PayloadLen = binary.BigEndian.Uint32(lenBytes)

	if p.PayloadLen > 0 {
		p.Payload = make([]byte, p.PayloadLen)
		if _, err := io.ReadFull(reader, p.Payload); err != nil {
			return fmt.Errorf("解码Payload失败: %w", err)
		}
	} else {
		p.Payload = nil
	}

	return nil
}

func (p *Proto) String() string {
	return fmt.Sprintf(
		"\nData{\n"+
			"  Code:       %s\n"+
			"  Type:       %s\n"+
			"  CID:        %d\n"+
			"  PayloadLen: %d\n"+
			"  Payload:    %s\n"+
			"}",
		statusCodeToString(p.Code),
		typeCodeToString(p.Type),
		p.CID,
		p.PayloadLen,
		bytesToHexString(p.Payload),
	)
}

// 辅助函数：将 StatusCode 转换为可读字符串
func statusCodeToString(c StatusCode) string {
	if name, exists := statusCodeMap[c]; exists {
		return fmt.Sprintf("%s (%d)", name, c)
	}
	return fmt.Sprintf("UnknownStatusCode (%d)", c)
}

// 辅助函数：将 TypeCode 转换为可读字符串
func typeCodeToString(t TypeCode) string {
	if name, exists := typeCodeMap[t]; exists {
		return fmt.Sprintf("%s (%d)", name, t)
	}
	return fmt.Sprintf("UnknownTypeCode (%d)", t)
}

// 辅助函数：将字节切片转换为十六进制字符串
func bytesToHexString(b []byte) string {
	if len(b) == 0 {
		return "[]"
	}
	hexParts := make([]string, len(b))
	for i, v := range b {
		hexParts[i] = fmt.Sprintf("0x%02x", v)
	}
	return "[" + strings.Join(hexParts, ", ") + "]"
}
