package client

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"srp/internal/common"
	"srp/pkg/logger"
	"strconv"
	"sync"
)

type Config struct {
	ServerIP       string // srp server ip
	ServerPort     int    // srp server port
	ServiceIP      string // service ip
	ServicePort    int    // service port
	ServerPassword string
	LogLevel       int
}

type Client struct {
	Config
	ServerConn net.Conn
	CIDMap     map[uint32]net.Conn
	Mu         *sync.Mutex
}

func (c *Client) AddUserConn(cid uint32, conn net.Conn) {
	c.Mu.Lock()
	defer c.Mu.Unlock()
	c.CIDMap[cid] = conn
}

func (c *Client) RemoveUserConn(cid uint32) {
	c.Mu.Lock()
	defer c.Mu.Unlock()
	delete(c.CIDMap, cid)
}

func (c *Client) CloseAllUserConn() {
	c.Mu.Lock()
	defer c.Mu.Unlock()
	for _, m := range c.CIDMap {
		m.Close()
	}
	c.CIDMap = make(map[uint32]net.Conn)
}

func (c *Client) EstablishConn() {
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

	// 接收响应
	reader := bufio.NewReader(conn)
	if err = data.DecodeProto(reader); err != nil {
		conn.Close()
		log.Fatal("与srp-server建立连接失败：" + err.Error())
	}

	if data.Code != common.CodeSuccess {
		conn.Close()
		log.Fatal("与srp-server建立连接失败：连接密码错误")
	}

	tcpConn := conn.(*net.TCPConn)
	tcpConn.SetKeepAlive(true)

	c.ServerConn = conn
	logger.LogWithLevel(c.LogLevel, 1, "成功与srp-server建立连接")
}

func (c *Client) HandleNewUserConn(data common.Proto) {
	var isReject bool

	// 获得 CID
	cid := data.CID
	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", c.ServiceIP, c.ServicePort))
	if err != nil {
		logger.LogWithLevel(c.LogLevel, 2, fmt.Sprintf("拒绝连接(cid：%d)，无法创建本地套接字，%s", cid, err.Error()))
		data = common.NewProto(common.CodeForbidden, common.TypeRejectConn, cid, []byte{})
		isReject = true
	} else {
		data = common.NewProto(common.CodeSuccess, common.TypeAcceptConn, cid, []byte{})
		isReject = false
	}

	dataByte, err := data.EncodeProto()
	if err != nil {
		logger.LogWithLevel(c.LogLevel, 2, fmt.Sprintf("建立连接(cid：%d)加入时无法处理数据，%s", cid, err.Error()))
		conn.Close()
		return
	}

	if _, err = c.ServerConn.Write(dataByte); err != nil {
		logger.LogWithLevel(c.LogLevel, 2, "无法向srp-server发送处理连接的数据，"+err.Error())
		return
	}

	if isReject {
		conn.Close()
		return
	}

	tcpConn := conn.(*net.TCPConn)
	tcpConn.SetKeepAlive(true)

	c.AddUserConn(cid, conn)
	logger.LogWithLevel(c.LogLevel, 2, fmt.Sprintf("建立连接(cid：%d)：%s->%s", cid, conn.LocalAddr().String(), conn.RemoteAddr().String()))

	// 阻塞在获取 service 消息处
	// 获得消息后立刻包装发送
	for {
		dataByte = make([]byte, common.MaxBufferSize)
		byteLen, err := conn.Read(dataByte)
		if err != nil {
			if err == io.EOF {
				logger.LogWithLevel(c.LogLevel, 2, fmt.Sprintf("和连接(cid：%d)的本地连接(%s)的连接断开，%s", cid, conn.LocalAddr().String(), err.Error()))
			} else {
				logger.LogWithLevel(c.LogLevel, 2, fmt.Sprintf("和连接(cid：%d)的本地连接(%s)的连接断开，%s", cid, conn.LocalAddr().String(), err.Error()))
			}
			data = common.NewProto(common.CodeSuccess, common.TypeDisconnect, cid, []byte{})
			if _, ok := c.CIDMap[data.CID]; ok {
				dataByteEncoded, _ := data.EncodeProto()
				c.ServerConn.Write(dataByteEncoded)
				c.RemoveUserConn(cid)
				conn.Close()
			}
			return
		}

		// 只传输读取的所有数据，而不是原来的 dataByte
		dataByte = dataByte[:byteLen]
		data = common.NewProto(common.CodeSuccess, common.TypeForwarding, cid, dataByte)
		dataByteEncoded, err := data.EncodeProto()
		if err != nil {
			logger.LogWithLevel(c.LogLevel, 2, "处理cid："+strconv.Itoa(int(cid))+"的本地连接"+conn.LocalAddr().String()+"的消息失败，"+err.Error())
			c.RemoveUserConn(cid)
			conn.Close()
			return
		}

		if _, err = c.ServerConn.Write(dataByteEncoded); err != nil {
			logger.LogWithLevel(c.LogLevel, 2, "发送cid："+strconv.Itoa(int(cid))+"的本地连接"+conn.LocalAddr().String()+"的消息失败，"+err.Error())
			c.RemoveUserConn(cid)
			conn.Close()
			return
		}
	}
}
