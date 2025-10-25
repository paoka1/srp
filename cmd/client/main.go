package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net"
	"srp/internal/client"
	"srp/internal/common"
	"srp/pkg/logger"
	"strconv"
	"sync"
)

func main() {
	logger.SetLogFormat()

	serverIP := flag.String("server-ip", "127.0.0.1", "srp-server的IP地址")
	serverPort := flag.Int("server-port", 6000, "srp-server监听的端口")
	serviceIP := flag.String("service-ip", "127.0.0.1", "被转发服务的IP地址")
	servicePort := flag.Int("service-port", 3000, "被转发服务的端口")
	serverPassword := flag.String("server-pwd", common.DefaultServerPasswd, "连接srp-server的密码")
	logLevel := flag.Int("log-level", 1, fmt.Sprintf("日志级别（1-%d）", logger.MaxLogLevel))
	flag.Parse()

	srpClient := client.Client{
		Config: client.Config{
			ServerIP:       *serverIP,
			ServerPort:     *serverPort,
			ServiceIP:      *serviceIP,
			ServicePort:    *servicePort,
			ServerPassword: *serverPassword,
			LogLevel:       *logLevel,
		},
		ServerConn: nil,
		CIDMap:     make(map[uint32]net.Conn),
		Mu:         &sync.Mutex{},
	}

	srpClient.EstablishConn()
	defer func() {
		if srpClient.ServerConn != nil {
			srpClient.ServerConn.Close()
		}
	}()

	// 阻塞在接收 srp-server 的消息处
	// 1.取出消息，若为新的 user conn，调用 HandleNewUserConn
	// 2.若为转发的流量，则转发到对应的 user conn
	data := common.Proto{}
	reader := bufio.NewReader(srpClient.ServerConn)
	for {
		if err := data.DecodeProto(reader); err != nil {
			srpClient.CloseAllUserConn()
			log.Fatal("处理srp-server的数据失败：" + err.Error())
		}

		switch data.Type {
		case common.TypeNewConn:
			go srpClient.HandleNewUserConn(data)
		case common.TypeForwarding:
			if srpClient.CIDMap[data.CID] == nil {
				logger.LogWithLevel(srpClient.LogLevel, 2, "收到cid："+strconv.Itoa(int(data.CID))+"的数据，无匹配的cid")
				continue
			}
			if _, err := srpClient.CIDMap[data.CID].Write(data.Payload); err != nil {
				logger.LogWithLevel(srpClient.LogLevel, 2, "收到ucid："+strconv.Itoa(int(data.CID))+"的数据，无法转发到service，"+err.Error())
			}

			logger.LogWithLevel(srpClient.LogLevel, 2, fmt.Sprintf("转发数据到srp-server，有效载荷大小：%dbyte", data.PayloadLen))
			logger.LogWithLevel(srpClient.LogLevel, 3, "转发数据到srp-server：")
			logger.LogWithLevel(srpClient.LogLevel, 3, data.String())
		case common.TypeDisconnect:
			if conn, ok := srpClient.CIDMap[data.CID]; ok {
				srpClient.RemoveUserConn(data.CID)
				conn.Close()
				logger.LogWithLevel(srpClient.LogLevel, 2, fmt.Sprintf("关闭cid：%d的连接", data.CID))
			}
		}
	}
}
