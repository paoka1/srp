package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"srp/internal/common"
	"srp/internal/server"
	"srp/pkg/logger"
	"sync"
)

func main() {
	logger.SetLogFormat()

	clientPort := flag.Int("client-port", 6000, "srp-client连接的端口")
	userPort := flag.Int("user-port", 9000, "用户访问转发服务的端口")
	serverPassword := flag.String("server-pwd", common.DefaultServerPasswd, "访问转发服务的密码")
	logLevel := flag.Int("log-level", 1, fmt.Sprintf("日志级别（1-%d）", logger.MaxLogLevel))
	flag.Parse()

	srpServer := server.Server{
		Config: server.Config{
			ClientPort:     *clientPort,
			UserPort:       *userPort,
			ServerPassword: *serverPassword,
			LogLevel:       *logLevel,
		},
		ClientConn:      nil,
		CIDNext:         1,
		CIDMap:          make(map[uint32]net.Conn),
		DataChan2User:   make(chan common.Proto, 100),
		DataChan2Client: make(chan common.Proto, 100),
		DataChan2Handle: make(chan common.Proto, 100),
		Mu:              &sync.Mutex{},
	}

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
			if data.Type == common.TypeDisconnection {
				if conn, ok := srpServer.CIDMap[data.CID]; ok {
					srpServer.RemoveUserConn(data.CID)
					conn.Close()
					logger.LogWithLevel(srpServer.LogLevel, 2, fmt.Sprintf("关闭user(cid：%d)的连接", data.CID))
				}
				continue
			}

			conn := srpServer.CIDMap[data.CID]
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
