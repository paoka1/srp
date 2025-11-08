package wrappers

import (
	"net"
	"srp/internal/common"
)

// TCPWrapper 包装 net.Conn，添加 HandshakeRespC 字段
type TCPWrapper struct {
	net.Conn

	HandshakeRespC chan common.Proto // handshake response chan：srp-client 响应的握手信息
}
