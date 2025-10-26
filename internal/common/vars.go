package common

const (
	DefaultServerPasswd = "default_password"
	MaxBufferSize       = 1500 // 大约等于以太网 MTU
)

var (
	// Protocols 支持的协议类型
	Protocols = []string{
		"tcp",
	}
)
