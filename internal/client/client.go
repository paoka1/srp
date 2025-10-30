package client

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"net"
	"srp/internal/common"
	"srp/pkg/logger"
	"sync"
	"time"
)

type Config struct {
	ServerIP   string // srp-server ip
	ServerPort int    // srp-server port

	ServiceIP   string // service ip
	ServicePort int    // service port

	ServerPassword string
	ServerProtocol string // 用户和 srp-server 通信的协议，也为 srp-client 和 service 的通信协议

	LogLevel int
}

type Client struct {
	Config

	ServerConn    net.Conn
	UserConnIDMap map[uint32]net.Conn // map of User Connection ID to Connection

	Mu *sync.Mutex

	// 处理 SRP 客户端与服务之间连接的函数
	// 在运行时动态根据命令行参数被赋值
	HandleServerData func(data common.Proto)
}

func (c *Client) AddUserConn(cid uint32, conn net.Conn) {
	c.Mu.Lock()
	defer c.Mu.Unlock()
	c.UserConnIDMap[cid] = conn
}

func (c *Client) GetUserConn(cid uint32) net.Conn {
	c.Mu.Lock()
	defer c.Mu.Unlock()
	return c.UserConnIDMap[cid]
}

func (c *Client) CloseUserConn(cid uint32) {
	c.Mu.Lock()
	defer c.Mu.Unlock()
	if conn, ok := c.UserConnIDMap[cid]; ok {
		conn.Close()
		delete(c.UserConnIDMap, cid)
	}
}

func (c *Client) CloseAllServiceConn() {
	c.Mu.Lock()
	defer c.Mu.Unlock()
	for _, m := range c.UserConnIDMap {
		m.Close()
	}
	c.UserConnIDMap = make(map[uint32]net.Conn)
}

func (c *Client) EstablishServerConn() {
	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", c.ServerIP, c.ServerPort))
	if err != nil {
		log.Fatal("与srp-server建立连接失败，" + err.Error())
	}

	// 发送密码
	data := common.NewProto(common.CodeSuccess, common.TypePing, 0, []byte(c.ServerPassword))
	dataByte, err := data.EncodeProto()
	if err != nil {
		log.Fatal("与srp-server建立连接失败，无法构造数据：" + err.Error())
	}

	_, err = conn.Write(dataByte)
	if err != nil {
		log.Fatal("与srp-server建立连接失败：" + err.Error())
	}
	logger.LogWithLevel(c.LogLevel, 2, "已向srp-server发送验证信息，等待响应")

	// 在 srp-server 在处理连接或已存在连接时，主动退出
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	// 接收响应
	reader := bufio.NewReader(conn)
	if err = data.DecodeProto(reader); err != nil {
		conn.Close()
		// 判断是否超时
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			log.Fatal("连接超时，请检查必要信息，在稍后重试")
		} else {
			log.Fatal("与srp-server建立连接失败：" + err.Error())
		}
	}

	// 取消过期时长
	conn.SetReadDeadline(time.Time{})

	if data.Code != common.CodeSuccess {
		conn.Close()
		log.Fatal("与srp-server建立连接失败：连接密码错误")
	}

	tcpConn := conn.(*net.TCPConn)
	tcpConn.SetKeepAlive(true)

	c.ServerConn = conn
	logger.LogWithLevel(c.LogLevel, 1, "成功与srp-server建立连接")
}

// HandleServerDataTCP 处理 TCP 数据
func (c *Client) HandleServerDataTCP(data common.Proto) {
	var isReject bool

	// 获得 CID
	cid := data.CID
	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", c.ServiceIP, c.ServicePort))
	if err != nil {
		logger.LogWithLevel(c.LogLevel, 2, fmt.Sprintf(fmt.Sprintf("拒绝用户连接(cid：%d)，无法和服务建立连接：%s", cid, err.Error())))
		data = common.NewProto(common.CodeForbidden, common.TypeRejectConn, cid, []byte{})
		isReject = true
	} else {
		data = common.NewProto(common.CodeSuccess, common.TypeAcceptConn, cid, []byte{})
		isReject = false
	}

	dataByte, err := data.EncodeProto()
	if err != nil {
		logger.LogWithLevel(c.LogLevel, 2, fmt.Sprintf("无法处理用户连接(cid: %d)，构造数据失败：%s", cid, err.Error()))
		conn.Close()
		return
	}

	if _, err = c.ServerConn.Write(dataByte); err != nil {
		logger.LogWithLevel(c.LogLevel, 2, fmt.Sprintf("无法处理用户连接(cid：%d)，向srp-server发送数据失败：%s", cid, err.Error()))
		return
	}

	if isReject {
		conn.Close()
		return
	}

	tcpConn := conn.(*net.TCPConn)
	tcpConn.SetKeepAlive(true)

	c.AddUserConn(cid, conn)
	logger.LogWithLevel(c.LogLevel, 2, fmt.Sprintf("建立连接(cid：%d)：%s->%s", cid, conn.LocalAddr(), conn.RemoteAddr()))

	// 阻塞在获取 service 消息处
	// 获得消息后立刻包装发送
	for {
		dataByte = make([]byte, common.MaxBufferSize)
		byteLen, err := conn.Read(dataByte)
		if err != nil {
			logger.LogWithLevel(c.LogLevel, 2, fmt.Sprintf("用户连接(cid：%d)的服务连接断开，%s", cid, err.Error()))
			data = common.NewProto(common.CodeSuccess, common.TypeDisconnect, cid, []byte{})
			dataByteEncoded, _ := data.EncodeProto()
			c.ServerConn.Write(dataByteEncoded)
			c.CloseUserConn(cid)
			return
		}

		// 只传输读取的所有数据，而不是原来的 dataByte
		dataByte = dataByte[:byteLen]
		data = common.NewProto(common.CodeSuccess, common.TypeForwarding, cid, dataByte)
		dataByteEncoded, err := data.EncodeProto()
		if err != nil {
			logger.LogWithLevel(c.LogLevel, 2, fmt.Sprintf("无法处理用户连接(cid：%d)的服务响应数据：%s", cid, err.Error()))
			continue
		}

		if _, err = c.ServerConn.Write(dataByteEncoded); err != nil {
			logger.LogWithLevel(c.LogLevel, 2, fmt.Sprintf("无法向srp-server发送用户连接(cid: %d)的服务响应数据：%s", cid, err.Error()))
			continue
		}
	}
}

// HandleServerDataUDP 处理 UDP 数据
func (c *Client) HandleServerDataUDP(data common.Proto) {

}
