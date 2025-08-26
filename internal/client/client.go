package client

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"srp/internal/common"
	"strconv"
	"sync"
)

type Config struct {
	ServerIP       string // srp server ip
	ServerPort     int    // srp server port
	ServiceIP      string // service ip
	ServicePort    int    // service port
	ServerPassword string
	IsDebug        bool
}

type Client struct {
	Config
	ServerConn net.Conn
	UserUIDMap map[uint32]net.Conn
	Mu         *sync.Mutex
}

func (n *Client) AddUserConn(uid uint32, conn net.Conn) {
	n.Mu.Lock()
	defer n.Mu.Unlock()
	n.UserUIDMap[uid] = conn
}

func (n *Client) RemoveUserConn(uid uint32) {
	n.Mu.Lock()
	defer n.Mu.Unlock()
	delete(n.UserUIDMap, uid)
}

func (n *Client) CloseAllUserConn() {
	n.Mu.Lock()
	defer n.Mu.Unlock()
	for _, c := range n.UserUIDMap {
		c.Close()
	}
	n.UserUIDMap = make(map[uint32]net.Conn)
}

func (n *Client) EstablishConn() {
	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", n.ServerIP, n.ServerPort))
	if err != nil {
		log.Fatal(err.Error())
	}

	// 发送密码
	data := common.NewProto(common.CodeSuccess, common.TypePing, 0, []byte(n.ServerPassword))
	dataByte, err := data.EncodeProto()
	if err != nil {
		log.Fatal("与srp-server建立连接失败，无法序列化数据：" + err.Error())
	}

	_, err = conn.Write(dataByte)
	if err != nil {
		log.Fatal("与srp-server建立连接失败：" + err.Error())
	}
	log.Println("已向srp-server发送验证信息，等待响应")

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

	n.ServerConn = conn
	log.Println("成功与srp-server建立连接")
}

func (n *Client) HandleNewUserConn(data common.Proto) {
	var isReject bool

	// 获得 UID
	uid := data.UID
	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", n.ServiceIP, n.ServicePort))
	if err != nil {
		log.Println(fmt.Sprintf("拒绝user(uid：%d)加入，无法创建本地套接字", uid))
		data = common.NewProto(common.CodeForbidden, common.TypeRejectUser, uid, []byte{})
		isReject = true
	} else {
		data = common.NewProto(common.CodeSuccess, common.TypeAcceptUser, uid, []byte{})
		isReject = false
	}

	dataByte, err := data.EncodeProto()
	if err != nil {
		log.Println(fmt.Sprintf("处理user(uid：%d)加入时无法序列化数据", uid))
		conn.Close()
		return
	}

	if _, err = n.ServerConn.Write(dataByte); err != nil {
		log.Println("无法向srp-server发送处理user conn的数据")
		return
	}

	if isReject {
		conn.Close()
		return
	}

	tcpConn := conn.(*net.TCPConn)
	tcpConn.SetKeepAlive(true)

	n.AddUserConn(uid, conn)
	log.Println(fmt.Sprintf("建立连接(uid：%d)：%s->%s", uid, conn.LocalAddr().String(), conn.RemoteAddr().String()))

	// 阻塞在获取 service 消息处
	// 获得消息后立刻包装发送
	for {
		dataByte = make([]byte, common.MaxBufferSize)
		byteLen, err := conn.Read(dataByte)
		if err != nil {
			if err == io.EOF {
				log.Println(fmt.Sprintf("和uid(%d)的本地连接(%s)的连接断开", uid, conn.LocalAddr().String()))
			} else {
				log.Println(fmt.Sprintf("和uid(%d)的本地连接(%s)的连接断开，%s", uid, conn.LocalAddr().String(), err.Error()))
			}
			data = common.NewProto(common.CodeSuccess, common.TypeDisconnection, uid, []byte{})
			if _, ok := n.UserUIDMap[data.UID]; ok {
				dataByteEncoded, _ := data.EncodeProto()
				n.ServerConn.Write(dataByteEncoded)
				n.RemoveUserConn(uid)
				conn.Close()
			}
			return
		}

		// 只传输读取的所有数据，而不是原来的 dataByte
		dataByte = dataByte[:byteLen]
		data = common.NewProto(common.CodeSuccess, common.TypeForwarding, uid, dataByte)
		dataByteEncoded, err := data.EncodeProto()
		if err != nil {
			log.Println("序列化uid：" + strconv.Itoa(int(uid)) + "的本地连接" + conn.LocalAddr().String() + "的消息失败" + err.Error())
			n.RemoveUserConn(uid)
			conn.Close()
			return
		}

		if _, err = n.ServerConn.Write(dataByteEncoded); err != nil {
			log.Println("发送uid：" + strconv.Itoa(int(uid)) + "的本地连接" + conn.LocalAddr().String() + "的消息失败" + err.Error())
			n.RemoveUserConn(uid)
			conn.Close()
			return
		}
	}
}
