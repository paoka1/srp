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
	"srp/pkg/utils"
	"sync"
)

func main() {
	logger.SetLogFormat()

	serverIP := flag.String("server-ip", "127.0.0.1", "srp-server的IP地址")
	serverPort := flag.Int("server-port", 6352, "srp-server监听的端口")
	serviceIP := flag.String("service-ip", "127.0.0.1", "被转发服务的IP地址")
	servicePort := flag.Int("service-port", 80, "被转发服务的端口")
	serverPassword := flag.String("server-pwd", common.DefaultServerPasswd, "连接srp-server的密码")
	protocol := flag.String("protocol", "tcp", "srp-client和被转发服务的通信协议，支持："+utils.Protocols2String(common.Protocols))
	logLevel := flag.Int("log-level", 2, fmt.Sprintf("日志级别（1-%d）", logger.MaxLogLevel))
	flag.Parse()

	srpClient := client.Client{
		Config: client.Config{
			ServerIP:       *serverIP,
			ServerPort:     *serverPort,
			ServiceIP:      *serviceIP,
			ServicePort:    *servicePort,
			ServerPassword: *serverPassword,
			ServerProtocol: *protocol,
			LogLevel:       *logLevel,
		},
		ServerConn:    nil,
		UserConnIDMap: make(map[uint32]net.Conn),
		Mu:            &sync.Mutex{},
	}

	// 根据命令行参数 protocol 选择协议
	// 实现新的协议时，务必在此添加代码
	switch srpClient.ServerProtocol {
	case "tcp":
		srpClient.HandleServerData = srpClient.HandleServerDataTCP
	case "udp":
		srpClient.HandleServerData = srpClient.HandleServerDataUDP
		srpClient.UDPConn = common.UDPConn{
			AddrConnMap: make(map[string]*common.UDPWrapper),
			Mu:          srpClient.Mu,
		}
	default:
		log.Fatal("不支持的协议：" + srpClient.ServerProtocol)
	}
	logger.LogWithLevel(srpClient.LogLevel, 1, fmt.Sprintf("被转发服务地址: %s:%d", srpClient.ServiceIP, srpClient.ServicePort))
	logger.LogWithLevel(srpClient.LogLevel, 1, fmt.Sprintf("srp-server地址: %s:%d", srpClient.ServerIP, srpClient.ServerPort))

	srpClient.EstablishServerConn()
	defer func() {
		if srpClient.ServerConn != nil {
			srpClient.ServerConn.Close()
		}
	}()

	// 阻塞在接收 srp-server 的消息处
	// 1.取出消息，若为新的用户连接，调用则向服务发送数据并处理连接
	// 2.若为转发的流量，则转发到对应的连接
	data := common.Proto{}
	reader := bufio.NewReader(srpClient.ServerConn)
	for {
		if err := data.DecodeProto(reader); err != nil {
			srpClient.CloseAllServiceConn()
			log.Fatal("无法处理srp-server的数据：" + err.Error())
		}

		switch data.Type {
		case common.TypeNewConn:
			go srpClient.HandleServerData(data)
		case common.TypeForwarding:
			conn := srpClient.GetUserConn(data.CID)
			if conn == nil {
				logger.LogWithLevel(srpClient.LogLevel, 2, fmt.Sprintf("无匹配的cid：%d", data.CID))
				continue
			}
			if _, err := conn.Write(data.Payload); err != nil {
				logger.LogWithLevel(srpClient.LogLevel, 2, fmt.Sprintf("无法转发cid：%d的数据到服务：%s", data.CID, err.Error()))
			}
			logger.LogWithLevel(srpClient.LogLevel, 2, fmt.Sprintf("转发数据到服务，有效载荷大小：%dbyte", data.PayloadLen))
			logger.LogWithLevel(srpClient.LogLevel, 3, "转发数据到srp-server：")
			logger.LogWithLevel(srpClient.LogLevel, 3, data.String())
		case common.TypeDisconnect:
			srpClient.CloseUserConn(data.CID)
			logger.LogWithLevel(srpClient.LogLevel, 2, fmt.Sprintf("关闭cid：%d的连接", data.CID))
		}
	}
}
