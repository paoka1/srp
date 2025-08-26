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
	isDebug := flag.Bool("debug", false, "开启调试模式")
	flag.Parse()

	srpClient := client.Client{
		Config: client.Config{
			*serverIP,
			*serverPort,
			*serviceIP,
			*servicePort,
			*serverPassword,
			*isDebug,
		},
		ServerConn: nil,
		UserUIDMap: make(map[uint32]net.Conn),
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
			log.Fatal("反序列化srp-server消息失败" + err.Error())
		}

		switch data.Type {
		case common.TypeNewUser:
			go srpClient.HandleNewUserConn(data)
		case common.TypeForwarding:
			if srpClient.UserUIDMap[data.UID] == nil {
				log.Println("收到uid：" + strconv.Itoa(int(data.UID)) + "的消息，无匹配uid")
				continue
			}
			if _, err := srpClient.UserUIDMap[data.UID].Write(data.Payload); err != nil {
				log.Println("收到uid：" + strconv.Itoa(int(data.UID)) + "的消息，无法转发到service")
			}
			if srpClient.IsDebug {
				log.Println("转发数据到srp-server：")
				log.Println(data.String())
			}
		case common.TypeDisconnection:
			if conn, ok := srpClient.UserUIDMap[data.UID]; ok {
				srpClient.RemoveUserConn(data.UID)
				conn.Close()
				log.Println(fmt.Sprintf("关闭uid：%d的连接", data.UID))
			}
		}
	}
}
