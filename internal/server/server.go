package server

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"srp/internal/common"
	"srp/pkg/logger"
	"sync"
	"time"
)

type Config struct {
	ClientIP string // srp-client ip
	UserIP   string // user ip

	ClientPort int // srp-client port
	UserPort   int // user port

	ServerPassword  string
	ServiceProtocol string // 和服务通信的协议，也为和 srp-server 通信的协议

	LogLevel int
}

type Server struct {
	Config

	CIDNext       uint32
	ClientConn    net.Conn
	UserConnIDMap map[uint32]net.Conn // map of User Connection ID to Connection

	DataChan2User   chan common.Proto // data channel to user
	DataChan2Client chan common.Proto // data channel to client
	DataChan2Handle chan common.Proto // data channel to handle

	Mu *sync.Mutex

	// 处理 SRP 客户端与服务之间连接的函数
	// 在运行时动态根据命令行参数被赋值
	HandleNewConn  func(values ...interface{})
	AcceptUserConn func()
}

func (s *Server) AddUserConn(cid uint32, conn net.Conn) {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	s.UserConnIDMap[cid] = conn
}

func (s *Server) GetUserConn(cid uint32) net.Conn {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	return s.UserConnIDMap[cid]
}

func (s *Server) CloseUserConn(cid uint32) {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	if conn, ok := s.UserConnIDMap[cid]; ok {
		conn.Close()
		delete(s.UserConnIDMap, cid)
	}
}

func (s *Server) CloseClientConn() {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	if s.ClientConn != nil {
		s.ClientConn.Close()
		s.ClientConn = nil
	}
}

func (s *Server) GetNextCID() uint32 {
	s.Mu.Lock()
	defer func() {
		s.CIDNext++
		s.Mu.Unlock()
	}()
	return s.CIDNext
}

func (s *Server) CloseAllUserConn() {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	for _, c := range s.UserConnIDMap {
		c.Close()
	}
	s.UserConnIDMap = make(map[uint32]net.Conn)
}

func (s *Server) AcceptClient() {
	listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", s.ClientIP, s.ClientPort))
	if err != nil {
		log.Fatal("无法创建tcp监听，" + err.Error())
	}

	for {
		conn, err := listener.Accept()
		if err != nil {
			logger.LogWithLevel(s.LogLevel, 2, fmt.Sprintf("拒绝srp-client：%s的连接，%s", conn.RemoteAddr(), err.Error()))
			continue
		} else {
			logger.LogWithLevel(s.LogLevel, 1, fmt.Sprintf("开始处理srp-client：%s的连接", conn.RemoteAddr()))
		}
		s.HandleClient(conn)
	}
}

// HandleClient 完成 srp-client 的认证和处理
func (s *Server) HandleClient(conn net.Conn) {
	// 设置期限
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))

	// 完成验证
	data := common.Proto{}
	reader := bufio.NewReader(conn)
	if err := data.DecodeProto(reader); err != nil {
		conn.Close()
		logger.LogWithLevel(s.LogLevel, 2, fmt.Sprintf("拒绝srp-client：%s的连接，%s", conn.RemoteAddr(), err.Error()))
		return
	}

	authFailed := data.Type != common.TypePing || string(data.Payload) != s.ServerPassword
	if authFailed {
		data = common.NewProto(common.CodeForbidden, common.TypePong, 0, []byte("reject"))
	} else {
		data = common.NewProto(common.CodeSuccess, common.TypePong, 0, []byte("accept"))
	}

	// 发送 pong
	dataByte, err := data.EncodeProto()
	if err != nil {
		conn.Close()
		logger.LogWithLevel(s.LogLevel, 2, fmt.Sprintf("拒绝srp-client：%s的连接，无法处理数据，%s", conn.RemoteAddr(), err.Error()))
		return
	}

	if _, err = conn.Write(dataByte); err != nil {
		conn.Close()
		logger.LogWithLevel(s.LogLevel, 2, fmt.Sprintf("拒绝srp-client：%s的连接，无法发送数据，%s", conn.RemoteAddr(), err.Error()))
		return
	}

	if authFailed {
		conn.Close()
		logger.LogWithLevel(s.LogLevel, 1, fmt.Sprintf("拒绝srp-client：%s的连接，密码错误", conn.RemoteAddr()))
		return
	}

	s.ClientConn = conn
	// 无限时长
	conn.SetReadDeadline(time.Time{})
	logger.LogWithLevel(s.LogLevel, 1, fmt.Sprintf("成功建立与srp-client：%s的连接", conn.RemoteAddr()))

	// 接收来自 srp-client 的消息，放到 DataChan2User
	for {
		if err = data.DecodeProto(reader); err != nil {
			logger.LogWithLevel(s.LogLevel, 2, fmt.Sprintf("与srp-client：%s的连接断开，%s", conn.RemoteAddr(), err.Error()))
			s.CloseClientConn()
			s.CloseAllUserConn()
			return
		}
		if data.Type == common.TypeAcceptConn || data.Type == common.TypeRejectConn {
			s.DataChan2Handle <- data
		} else {
			s.DataChan2User <- data
		}
	}
}

// AcceptUserConnTCP 监听和接受 TCP 连接
func (s *Server) AcceptUserConnTCP() {
	listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", s.UserIP, s.UserPort))
	if err != nil {
		log.Fatal("无法监听tcp连接，" + err.Error())
	}
	for {
		conn, err := listener.Accept()
		if err != nil {
			logger.LogWithLevel(s.LogLevel, 2, fmt.Sprintf("拒绝user：%s的连接，%s", conn.RemoteAddr(), err.Error()))
			continue
		}
		go s.HandleNewConn(conn)
	}
}

// HandleUserConnTCP 完成 TCP 连接创建和接收数据
func (s *Server) HandleUserConnTCP(values ...interface{}) {
	conn, _ := values[0].(net.Conn)
	if s.ClientConn == nil {
		conn.Close()
		logger.LogWithLevel(s.LogLevel, 2, fmt.Sprintf("拒绝user：%s的连接，serp-client未连接", conn.RemoteAddr()))
		return
	}

	// 完成注册
	cid := s.GetNextCID()
	data := common.NewProto(common.CodeSuccess, common.TypeNewConn, cid, nil)
	dataByte, err := data.EncodeProto()
	if err != nil {
		logger.LogWithLevel(s.LogLevel, 2, fmt.Sprintf("拒绝user：%s的连接，%s", conn.RemoteAddr(), err.Error()))
		conn.Close()
		return
	}

	if _, err = s.ClientConn.Write(dataByte); err != nil {
		logger.LogWithLevel(s.LogLevel, 2, fmt.Sprintf("拒绝user：%s的连接，%s", conn.RemoteAddr(), err.Error()))
		conn.Close()
		return
	}
	logger.LogWithLevel(s.LogLevel, 2, fmt.Sprintf("已向srp-client发送user(%s)的连接申请", conn.RemoteAddr().String()))

	// 验证 TypeAcceptConn
	for {
		data = <-s.DataChan2Handle
		if data.CID != cid {
			// 不是自己的就放进去
			s.DataChan2Handle <- data
			continue
		}
		break
	}

	if data.Code != common.CodeSuccess || data.Type != common.TypeAcceptConn {
		logger.LogWithLevel(s.LogLevel, 2, fmt.Sprintf("拒绝user：%s的连接，srp-client拒绝连接", conn.RemoteAddr()))
		conn.Close()
		return
	}

	tcpConn := conn.(*net.TCPConn)
	tcpConn.SetKeepAlive(true)

	s.AddUserConn(cid, conn)
	logger.LogWithLevel(s.LogLevel, 2, fmt.Sprintf("建立连接(cid：%d)：%s->%s", cid, conn.LocalAddr().String(), conn.RemoteAddr().String()))

	// 读取消息，放到 DataChan2Client
	for {
		dataByte = make([]byte, common.MaxBufferSize)
		byteLen, err := conn.Read(dataByte)
		if err != nil {
			logger.LogWithLevel(s.LogLevel, 2, fmt.Sprintf("与user：%s的断开连接，%s", conn.RemoteAddr(), err.Error()))
			data = common.NewProto(common.CodeSuccess, common.TypeDisconnect, cid, []byte{})
			s.DataChan2Client <- data
			s.CloseUserConn(cid)
			return
		}

		// 只传输读取的所有数据，而不是原来的 dataByte
		dataByte = dataByte[:byteLen]
		// 打上 CID 标签
		data = common.NewProto(common.CodeSuccess, common.TypeForwarding, cid, dataByte)
		s.DataChan2Client <- data
	}
}

// AcceptUserConnUDP 监听和接受 UDP 连接
func (s *Server) AcceptUserConnUDP() {
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", s.UserIP, s.UserPort))
	if err != nil {
		log.Fatal("无法解析udp地址：" + addr.String() + err.Error())
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Fatal("无法监听udp地址：" + addr.String() + err.Error())
	}
	defer conn.Close()

	dataByte := make([]byte, common.MaxBufferSize)
	for {
		n, clientAddr, err := conn.ReadFromUDP(dataByte)
		if err != nil {
			logger.LogWithLevel(s.LogLevel, 2, "读取udp数据失败："+err.Error())
			continue
		}
		// 拷贝数据，避免被下一次循环覆盖
		data := make([]byte, n)
		copy(data, dataByte[:n])
		go s.HandleUserConnUDP(conn, clientAddr, data)
	}
}

// HandleUserConnUDP 完成 UDP 连接创建和接收数据
func (s *Server) HandleUserConnUDP(values ...interface{}) {

}
