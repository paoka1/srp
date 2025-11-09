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

	BufferPool sync.Pool // 缓冲区复用
	RWMu       *sync.RWMutex

	// 处理 SRP 客户端与服务之间连接的函数
	// 在运行时动态根据命令行参数被赋值
	HandleServerData func(data common.Proto)
}

func (c *Client) AddUserConn(cid uint32, conn net.Conn) {
	c.RWMu.Lock()
	defer c.RWMu.Unlock()
	c.UserConnIDMap[cid] = conn
}

func (c *Client) GetUserConn(cid uint32) net.Conn {
	c.RWMu.RLock()
	defer c.RWMu.RUnlock()
	return c.UserConnIDMap[cid]
}

func (c *Client) CloseUserConn(cid uint32) {
	c.RWMu.Lock()
	defer c.RWMu.Unlock()
	if conn, ok := c.UserConnIDMap[cid]; ok {
		conn.Close()
		delete(c.UserConnIDMap, cid)
	}
}

func (c *Client) CloseAllServiceConn() {
	c.RWMu.Lock()
	defer c.RWMu.Unlock()
	for _, m := range c.UserConnIDMap {
		m.Close()
	}
	c.UserConnIDMap = make(map[uint32]net.Conn)
}

func (c *Client) CloseServerConn() {
	c.RWMu.Lock()
	defer c.RWMu.Unlock()
	if c.ServerConn != nil {
		c.ServerConn.Close()
	}
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
	if _, err = conn.Write(dataByte); err != nil {
		conn.Close()
		log.Fatal("与srp-server建立连接失败：" + err.Error())
	}
	logger.LogWithLevel(c.LogLevel, 2, "已向srp-server发送验证信息，等待响应")

	// 在 srp-server 在处理连接或已存在连接时，主动退出
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
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
		log.Fatal("与srp-server建立连接失败，%s" + string(data.Payload))
	}

	// 添加连接
	(conn.(*net.TCPConn)).SetKeepAlive(true)
	c.ServerConn = conn
	logger.LogWithLevel(c.LogLevel, 1, "成功与srp-server建立连接")
}

// SendDataToServer 向 srp-server 发送数据
func (c *Client) SendDataToServer(p common.Proto) error {
	if c.ServerConn == nil {
		return fmt.Errorf("未建立和srp-server的连接")
	}
	dataByte, err := p.EncodeProto()
	if err != nil {
		return err
	}
	_, err = c.ServerConn.Write(dataByte)
	return err
}

// HandleServerDataTCP 处理 TCP 数据
func (c *Client) HandleServerDataTCP(data common.Proto) {
	cid := data.CID
	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", c.ServiceIP, c.ServicePort))
	if err != nil {
		logger.LogWithLevel(c.LogLevel, 2, fmt.Sprintf("拒绝用户连接(cid：%d)，无法和服务建立连接：%s", cid, err))
		dataErr := common.NewProto(common.CodeForbidden, common.TypeRejectConn, cid, []byte(err.Error()))
		if err := c.SendDataToServer(dataErr); err != nil {
			logger.LogWithLevel(c.LogLevel, 2, fmt.Sprintf("无法向srp-server发送数据，%s", err))
		}
		return
	} else {
		dataOk := common.NewProto(common.CodeSuccess, common.TypeAcceptConn, cid, []byte{})
		if err := c.SendDataToServer(dataOk); err != nil {
			conn.Close()
			logger.LogWithLevel(c.LogLevel, 2, fmt.Sprintf("无法向srp-server发送数据，%s", err))
			return
		}
	}

	// 完成注册
	(conn.(*net.TCPConn)).SetKeepAlive(true)
	c.AddUserConn(cid, conn)
	defer c.CloseUserConn(cid)
	logger.LogWithLevel(c.LogLevel, 2, fmt.Sprintf("建立连接(cid：%d)：%s->%s", cid, conn.LocalAddr(), conn.RemoteAddr()))

	buffer := c.BufferPool.Get().([]byte)
	defer c.BufferPool.Put(buffer)
	// 阻塞在获取 service 消息处，获得消息后立刻包装发送
	for {
		n, err := conn.Read(buffer)
		if err != nil {
			logger.LogWithLevel(c.LogLevel, 2, fmt.Sprintf("用户连接(cid：%d)的服务连接断开，%s", cid, err))
			c.SendDataToServer(common.NewProto(common.CodeSuccess, common.TypeDisconnect, cid, []byte(err.Error())))
			return
		}
		// 只传输读取的所有数据，而不是原来的 buffer
		if err = c.SendDataToServer(common.NewProto(common.CodeSuccess, common.TypeForwarding, cid, buffer[:n])); err != nil {
			logger.LogWithLevel(c.LogLevel, 2, fmt.Sprintf("无法向srp-server发送用户(cid:%d)的数据，%s", cid, err))
		}
	}
}

// HandleServerDataUDP 处理 UDP 数据
func (c *Client) HandleServerDataUDP(data common.Proto) {
	// 初始化
	cid := data.CID
	clientAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", c.ServiceIP, c.ServicePort))
	if err != nil {
		dataErr := common.NewProto(common.CodeSuccess, common.TypeDisconnect, cid, []byte(err.Error()))
		if err := c.SendDataToServer(dataErr); err != nil {
			logger.LogWithLevel(c.LogLevel, 2, fmt.Sprintf("无法向srp-server发送数据，%s", err))
			return
		}
		logger.LogWithLevel(c.LogLevel, 2, fmt.Sprintf("无法向服务发起UDP连接，%s", err))
		return
	}

	conn, err := net.DialUDP("udp", nil, clientAddr)
	if err != nil {
		dataErr := common.NewProto(common.CodeSuccess, common.TypeDisconnect, cid, []byte(err.Error()))
		if err := c.SendDataToServer(dataErr); err != nil {
			logger.LogWithLevel(c.LogLevel, 2, fmt.Sprintf("无法向srp-server发送数据，%s", err))
			return
		}
		logger.LogWithLevel(c.LogLevel, 2, fmt.Sprintf("无法向服务发起UDP连接，%s", err))
		return
	}

	// 设置映射存活时限
	conn.SetDeadline(time.Now().Add(common.UDPTimeOut))
	// 发送连接请求响应
	err = c.SendDataToServer(common.NewProto(common.CodeSuccess, common.TypeAcceptConn, cid, []byte{}))
	if err != nil {
		logger.LogWithLevel(c.LogLevel, 2, fmt.Sprintf("无法向srp-server发送数据：%s", err))
		conn.Close()
		return
	}

	// 记录映射
	c.AddUserConn(cid, conn)
	defer c.CloseUserConn(cid)
	logger.LogWithLevel(c.LogLevel, 2, fmt.Sprintf("建立连接(cid：%d)：srp-client->%s", cid, conn.RemoteAddr()))

	buffer := c.BufferPool.Get().([]byte)
	defer c.BufferPool.Put(buffer)
	for {
		n, err := conn.Read(buffer)
		if err != nil {
			logger.LogWithLevel(c.LogLevel, 2, fmt.Sprintf("用户连接(cid：%d)的服务连接断开，%s", cid, err))
			c.SendDataToServer(common.NewProto(common.CodeSuccess, common.TypeDisconnect, cid, []byte(err.Error())))
			return
		}
		// 只传输读取的所有数据，而不是原来的 buffer
		if err = c.SendDataToServer(common.NewProto(common.CodeSuccess, common.TypeForwarding, cid, buffer[:n])); err != nil {
			logger.LogWithLevel(c.LogLevel, 2, fmt.Sprintf("无法向srp-server发送数据：%s", err))
			continue
		}
	}
}
