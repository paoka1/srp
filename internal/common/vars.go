package common

import "time"

const (
	DefaultServerPasswd = "default_password"
	MaxBufferSize       = 65507
	UDPTimeOut          = 3 * time.Minute
)

var (
	Version = "v1.0.1"

	// Protocols 支持的协议类型
	Protocols = []string{
		"tcp",
		"udp",
	}
)
