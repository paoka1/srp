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
	"sync"
)

func main() {
	logger.SetLogFormat()

	clientPort := flag.Int("client-port", 6000, "srp-client连接的端口")
	userPort := flag.Int("user-port", 9000, "用户访问转发服务的端口")
	serverPassword := flag.String("server-pwd", common.DefaultServerPasswd, "访问转发服务的密码")
	isDebug := flag.Bool("debug", false, "开启调试模式")
	flag.Parse()

	srpServer := server.Server{
		Config: server.Config{
			ClientPort:     *clientPort,
			UserPort:       *userPort,
			ServerPassword: *serverPassword,
			IsDebug:        *isDebug,
		},
		ClientConn:      nil,
		UIDNext:         1,
		UserUIDMap:      make(map[uint32]net.Conn),
		DataChan2User:   make(chan common.Proto, 100),
		DataChan2Client: make(chan common.Proto, 100),
		DataChan2Handle: make(chan common.Proto, 100),
		Mu:              &sync.Mutex{},
	}

	go srpServer.AcceptClient()
	defer srpServer.CloseClientConn()

	go srpServer.AcceptUserConn()

	// 通过 DataChan2Client 接收 user 消息，发送到 srp-client
	// 通过 DataChan2User 接收 srp-client 消息，根据 uid 发送到 user
	for {
		select {
		case data := <-srpServer.DataChan2Client:
			if srpServer.ClientConn == nil {
				continue
			}
			dataByte, err := data.EncodeProto()
			if err != nil {
				log.Println("丢弃user发往srp-client的数据包，无法序列化数据")
				continue
			}
			if _, err := srpServer.ClientConn.Write(dataByte); err != nil {
				log.Println("丢弃user发往srp-client的数据包，无法发送数据")
				if errors.Is(err, io.EOF) {
					srpServer.CloseClientConn()
					srpServer.CloseAllUserConn()
				}
			}
			if srpServer.IsDebug {
				log.Println("转发数据到srp-client：")
				log.Println(data.String())
			}
		case data := <-srpServer.DataChan2User:
			if data.Type == common.TypeDisconnection {
				if conn, ok := srpServer.UserUIDMap[data.UID]; ok {
					srpServer.RemoveUserConn(data.UID)
					conn.Close()
					log.Println(fmt.Sprintf("关闭user(uid：%d)的连接", data.UID))
				}
				continue
			}

			conn := srpServer.UserUIDMap[data.UID]
			if conn == nil {
				log.Println(fmt.Sprintf("丢弃srp-client发往user的数据包，无效的uid：%d", data.UID))
				continue
			}
			if _, err := conn.Write(data.Payload); err != nil {
				log.Println("丢弃srp-client发往user的数据包，无法发送数据")
				continue
			}
			if srpServer.IsDebug {
				log.Println("转发数据到user：")
				log.Println(data.String())
			}
		}
	}
}
