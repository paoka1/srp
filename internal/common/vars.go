package common

import "time"

const (
	DefaultServerPasswd = "default_password"
	MaxBufferSize       = 1500 // 大约等于以太网 MTU
	UDPTimeOut          = 3 * time.Minute
)

var (
	// Protocols 支持的协议类型
	Protocols = []string{
		"tcp",
		"udp",
	}
)
