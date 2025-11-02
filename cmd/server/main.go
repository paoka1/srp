package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"srp/internal/common"
	"srp/internal/server"
	"srp/pkg/logger"
	"srp/pkg/utils"
	"sync"
)

func main() {
	logger.SetLogFormat()

	clientIP := flag.String("client-ip", "0.0.0.0", "srp-client连接的IP地址")
	clientPort := flag.Int("client-port", 6352, "srp-client连接的端口")
	userIP := flag.String("server-ip", "0.0.0.0", "用户访问被转发服务的IP地址")
	userPort := flag.Int("user-port", 9352, "用户访问被转发服务的端口")
	serverPassword := flag.String("server-pwd", common.DefaultServerPasswd, "srp-server连接密码")
	protocol := flag.String("protocol", "tcp", "用户和srp-server间的通信协议，支持："+utils.Protocols2String(common.Protocols))
	logLevel := flag.Int("log-level", 2, fmt.Sprintf("日志级别（1-%d）", logger.MaxLogLevel))
	flag.Parse()

	srpServer := server.Server{
		Config: server.Config{
			ClientIP:        *clientIP,
			UserIP:          *userIP,
			ClientPort:      *clientPort,
			UserPort:        *userPort,
			ServerPassword:  *serverPassword,
			ServiceProtocol: *protocol,
			LogLevel:        *logLevel,
		},
		ClientConn:      nil,
		CIDNext:         1,
		UserConnIDMap:   make(map[uint32]net.Conn),
		DataChan2User:   make(chan common.Proto, 100),
		DataChan2Client: make(chan common.Proto, 100),
		DataChan2Handle: make(chan common.Proto, 100),
		Mu:              &sync.Mutex{},
	}

	// 根据命令行参数 protocol 选择协议
	// 实现新的协议时，务必在此添加代码
	switch srpServer.ServiceProtocol {
	case "tcp":
		srpServer.HandleNewConn = srpServer.HandleUserConnTCP
		srpServer.AcceptUserConn = srpServer.AcceptUserConnTCP
	case "udp":
		srpServer.HandleNewConn = srpServer.HandleUserConnUDP
		srpServer.AcceptUserConn = srpServer.AcceptUserConnUDP
	default:
		log.Fatal("不支持的协议：" + srpServer.ServiceProtocol)
	}

	logger.LogWithLevel(srpServer.LogLevel, 1, fmt.Sprintf("srp-client连接地址：%s:%d", srpServer.ClientIP, srpServer.ClientPort))
	logger.LogWithLevel(srpServer.LogLevel, 1, fmt.Sprintf("用户访问地址：%s:%d", srpServer.UserIP, srpServer.UserPort))

	go srpServer.AcceptClient()
	defer srpServer.CloseClientConn()

	go srpServer.AcceptUserConn()

	// 通过 DataChan2Client 接收 user 消息，发送到 srp-client
	// 通过 DataChan2User 接收 srp-client 消息，根据 cid 发送到 user
	for {
		select {
		case data := <-srpServer.DataChan2Client:
			if srpServer.ClientConn == nil {
				continue
			}
			dataByte, err := data.EncodeProto()
			if err != nil {
				logger.LogWithLevel(srpServer.LogLevel, 2, "丢弃user发往srp-client的数据包，无法处理数据，"+err.Error())
				continue
			}
			if _, err := srpServer.ClientConn.Write(dataByte); err != nil {
				logger.LogWithLevel(srpServer.LogLevel, 2, "丢弃user发往srp-client的数据包，无法发送数据，"+err.Error())
				if errors.Is(err, io.EOF) {
					srpServer.CloseClientConn()
					srpServer.CloseAllUserConn()
				}
			}
			logger.LogWithLevel(srpServer.LogLevel, 2, fmt.Sprintf("转发数据到srp-client，有效载荷大小：%dbyte", data.PayloadLen))
			logger.LogWithLevel(srpServer.LogLevel, 3, "转发数据到srp-client：")
			logger.LogWithLevel(srpServer.LogLevel, 3, data.String())
		case data := <-srpServer.DataChan2User:
			if data.Type == common.TypeDisconnect {
				srpServer.CloseUserConn(data.CID)
				logger.LogWithLevel(srpServer.LogLevel, 2, fmt.Sprintf("关闭user(cid：%d)的连接", data.CID))
				continue
			}
			conn := srpServer.GetUserConn(data.CID)
			if conn == nil {
				logger.LogWithLevel(srpServer.LogLevel, 2, fmt.Sprintf("丢弃srp-client发往user的数据包，无效的cid：%d", data.CID))
				continue
			}
			if _, err := conn.Write(data.Payload); err != nil {
				logger.LogWithLevel(srpServer.LogLevel, 2, "丢弃srp-client发往user的数据包，无法发送数据，"+err.Error())
				continue
			}
			logger.LogWithLevel(srpServer.LogLevel, 2, fmt.Sprintf("转发数据到user，有效载荷大小：%dbyte", data.PayloadLen))
			logger.LogWithLevel(srpServer.LogLevel, 3, "转发数据到user：")
			logger.LogWithLevel(srpServer.LogLevel, 3, data.String())
		}
	}
}
