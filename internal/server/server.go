package server

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"srp/internal/common"
	"srp/internal/server/wrappers"
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

	BufferPool sync.Pool // 缓冲区复用
	RWMu       *sync.RWMutex

	// 处理 SRP 客户端与服务之间连接的函数
	// 在运行时动态根据命令行参数被赋值
	AcceptUserConn func()
	HandleNewConn  func(values ...interface{})
}

func (s *Server) AddUserConn(cid uint32, conn net.Conn) {
	s.RWMu.Lock()
	defer s.RWMu.Unlock()
	s.UserConnIDMap[cid] = conn
}

func (s *Server) GetUserConn(cid uint32) net.Conn {
	s.RWMu.RLock()
	defer s.RWMu.RUnlock()
	return s.UserConnIDMap[cid]
}

func (s *Server) CloseUserConn(cid uint32) {
	s.RWMu.Lock()
	defer s.RWMu.Unlock()
	if conn, ok := s.UserConnIDMap[cid]; ok {
		conn.Close()
		delete(s.UserConnIDMap, cid)
	}
}

func (s *Server) CloseClientConn() {
	s.RWMu.Lock()
	defer s.RWMu.Unlock()
	if s.ClientConn != nil {
		s.ClientConn.Close()
		s.ClientConn = nil
	}
}

func (s *Server) GetNextCID() uint32 {
	s.RWMu.Lock()
	defer s.RWMu.Unlock()
	cid := s.CIDNext
	s.CIDNext++
	return cid
}

func (s *Server) CloseAllUserConn() {
	s.RWMu.Lock()
	defer s.RWMu.Unlock()
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
			logger.LogWithLevel(s.LogLevel, 2, fmt.Sprintf("拒绝srp-client：%s的连接，%s", conn.RemoteAddr(), err))
			continue
		}
		logger.LogWithLevel(s.LogLevel, 1, fmt.Sprintf("开始处理srp-client：%s的连接", conn.RemoteAddr()))
		s.HandleClient(conn)
	}
}

// HandleClient 完成 srp-client 的认证和处理
func (s *Server) HandleClient(conn net.Conn) {
	// 初始化，设置期限，开始进行验证
	data := common.Proto{}
	reader := bufio.NewReader(conn)
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := data.DecodeProto(reader); err != nil {
		conn.Close()
		logger.LogWithLevel(s.LogLevel, 2, fmt.Sprintf("拒绝srp-client：%s的连接，%s", conn.RemoteAddr(), err))
		return
	}

	if data.Type != common.TypePing || string(data.Payload) != s.ServerPassword {
		data = common.NewProto(common.CodeForbidden, common.TypePong, 0, []byte("连接失败，密码错误"))
		logger.LogWithLevel(s.LogLevel, 1, fmt.Sprintf("拒绝srp-client：%s的连接，密码错误", conn.RemoteAddr()))
	} else {
		data = common.NewProto(common.CodeSuccess, common.TypePong, 0, []byte("连接成功"))
	}

	// 发送 pong
	dataByte, err := data.EncodeProto()
	if err != nil {
		conn.Close()
		logger.LogWithLevel(s.LogLevel, 2, fmt.Sprintf("拒绝srp-client：%s的连接，无法处理数据，%s", conn.RemoteAddr(), err))
		return
	}
	if _, err = conn.Write(dataByte); err != nil {
		conn.Close()
		logger.LogWithLevel(s.LogLevel, 2, fmt.Sprintf("拒绝srp-client：%s的连接，无法发送数据，%s", conn.RemoteAddr(), err))
		return
	}

	// 在连接密码不正确或其他情况时，断开连接
	if data.Code == common.CodeForbidden {
		conn.Close()
		return
	}

	// 记录连接
	s.ClientConn = conn
	conn.SetReadDeadline(time.Time{})
	logger.LogWithLevel(s.LogLevel, 1, fmt.Sprintf("成功建立与srp-client：%s的连接", conn.RemoteAddr()))

	// 接收来自 srp-client 的消息，分类处理
	for {
		if err = data.DecodeProto(reader); err != nil {
			logger.LogWithLevel(s.LogLevel, 2, fmt.Sprintf("与srp-client：%s的连接断开，%s", conn.RemoteAddr(), err))
			s.CloseClientConn()
			s.CloseAllUserConn()
			for {
				select {
				case <-s.DataChan2Client:
				case <-s.DataChan2User:
				default:
					return
				}
			}
		}
		if data.Type == common.TypeAcceptConn || data.Type == common.TypeRejectConn {
			conn := s.GetUserConn(data.CID)
			if conn == nil {
				logger.LogWithLevel(s.LogLevel, 2, fmt.Sprintf("无效的cid：%d", data.CID))
				continue
			}
			// 根据不同的连接类型，向该连接的握手 chan 发送数据
			switch c := conn.(type) {
			case *wrappers.TCPWrapper:
				c.HandshakeRespC <- data
			case *wrappers.UDPWrapper:
				c.HandshakeRespC <- data
			default:
				logger.LogWithLevel(s.LogLevel, 2, fmt.Sprintf("未知的数据格式（cid：%d）：%T", data.CID, c))
			}
		} else {
			s.DataChan2User <- data
		}
	}
}

// SendDataToClient 向 srp-client 发送数据
func (s *Server) SendDataToClient(p common.Proto) error {
	if s.ClientConn == nil {
		return fmt.Errorf("未建立和srp-client的连接")
	}
	dataByte, err := p.EncodeProto()
	if err != nil {
		return err
	}
	_, err = s.ClientConn.Write(dataByte)
	return err
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
			logger.LogWithLevel(s.LogLevel, 2, fmt.Sprintf("拒绝user：%s的连接，%s", conn.RemoteAddr(), err))
			continue
		}
		go s.HandleNewConn(conn)
	}
}

// AcceptUserConnUDP 监听和接受 UDP 连接
func (s *Server) AcceptUserConnUDP() {
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", s.UserIP, s.UserPort))
	if err != nil {
		log.Fatal("无法解析udp地址：" + addr.String() + "，" + err.Error())
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Fatal("无法监听udp地址：" + addr.String() + "，" + err.Error())
	}
	defer conn.Close()

	// 初始化
	buffer := make([]byte, common.MaxBufferSize)
	udpConn := &wrappers.UDPConn{
		AddrConnMap: make(map[string]*wrappers.UDPWrapper),
		RWMu:        s.RWMu,
	}

	for {
		n, clientAddr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			logger.LogWithLevel(s.LogLevel, 2, "读取udp数据失败："+err.Error())
			continue
		}
		// 拷贝数据，避免被下一次循环覆盖
		data := make([]byte, n)
		copy(data, buffer[:n])
		// 查看该远程地址是否已经建立映射
		if c := udpConn.GetConn(clientAddr); c != nil {
			c.ReadC <- data
			continue
		}
		go s.HandleUserConnUDP(udpConn, conn, clientAddr, data)
	}
}

// HandleUserConnTCP 完成 TCP 连接创建和接收数据
func (s *Server) HandleUserConnTCP(values ...interface{}) {
	conn, _ := values[0].(net.Conn)
	if s.ClientConn == nil {
		logger.LogWithLevel(s.LogLevel, 2, fmt.Sprintf("拒绝user：%s的连接，serp-client未连接", conn.RemoteAddr()))
		conn.Close()
		return
	}

	// 获取该连接的 connection id，并初始化
	cid := s.GetNextCID()
	tcpWrapper := &wrappers.TCPWrapper{
		Conn:           conn,
		HandshakeRespC: make(chan common.Proto),
	}
	s.AddUserConn(cid, tcpWrapper)
	defer s.CloseUserConn(cid)

	err := s.SendDataToClient(common.NewProto(common.CodeSuccess, common.TypeNewConn, cid, nil))
	if err != nil {
		logger.LogWithLevel(s.LogLevel, 2, fmt.Sprintf("拒绝user：%s的连接，无法向srp-client发送数据，%s", conn.RemoteAddr(), err))
		return
	}
	logger.LogWithLevel(s.LogLevel, 2, fmt.Sprintf("已向srp-client发送user(%s)的连接申请", conn.RemoteAddr()))

	// 验证 TypeAcceptConn
	data := <-tcpWrapper.HandshakeRespC
	if data.Code != common.CodeSuccess || data.Type != common.TypeAcceptConn {
		logger.LogWithLevel(s.LogLevel, 2, fmt.Sprintf("拒绝user：%s的连接，srp-client拒绝连接：%s", conn.RemoteAddr(), data.Payload))
		return
	}

	(conn.(*net.TCPConn)).SetKeepAlive(true)
	logger.LogWithLevel(s.LogLevel, 2, fmt.Sprintf("建立连接(cid：%d)：%s->%s", cid, conn.LocalAddr(), conn.RemoteAddr()))

	buffer := s.BufferPool.Get().([]byte)
	defer s.BufferPool.Put(buffer)
	// 读取消息，放到 DataChan2Client
	for {
		n, err := conn.Read(buffer)
		if err != nil {
			logger.LogWithLevel(s.LogLevel, 2, fmt.Sprintf("与user：%s的断开连接，%s", conn.RemoteAddr(), err))
			s.DataChan2Client <- common.NewProto(common.CodeSuccess, common.TypeDisconnect, cid, []byte(err.Error()))
			return
		}
		// 打上 CID 标签，只传输读取的所有数据，而不是原来的 buffer
		// 重新申请内存来拷贝 buffer 也许在某些情况下会造成 GC 性能问题
		// 但其可以保留现有的代码结构，同时发挥缓冲区复用的优势
		data := make([]byte, n)
		copy(data, buffer[:n])
		s.DataChan2Client <- common.NewProto(common.CodeSuccess, common.TypeForwarding, cid, data)
	}
}

// HandleUserConnUDP 完成 UDP 连接创建和接收数据
func (s *Server) HandleUserConnUDP(values ...interface{}) {
	udpConn, _ := values[0].(*wrappers.UDPConn)
	conn, _ := values[1].(*net.UDPConn)
	clientAddr, _ := values[2].(*net.UDPAddr)
	data0, _ := values[3].([]byte)

	if s.ClientConn == nil {
		logger.LogWithLevel(s.LogLevel, 2, fmt.Sprintf("拒绝user：%s的连接，serp-client未连接", conn.RemoteAddr()))
		return
	}

	// 初始化 UDPWrapper，获取 connection id
	udpWrapper := &wrappers.UDPWrapper{
		Conn:           conn,
		ClientAddr:     clientAddr,
		ReadC:          make(chan []byte, 100),
		Sigc:           make(chan struct{}),
		HandshakeRespC: make(chan common.Proto),
	}
	cid := s.GetNextCID()
	err := s.SendDataToClient(common.NewProto(common.CodeSuccess, common.TypeNewConn, cid, nil))
	if err != nil {
		logger.LogWithLevel(s.LogLevel, 2, fmt.Sprintf("拒绝user：%s的连接，无法向srp-client发送数据，%s", clientAddr, err))
		return
	}

	// 记录映射
	udpConn.AddConn(clientAddr, udpWrapper)
	defer udpConn.DelConn(clientAddr)
	s.AddUserConn(cid, udpWrapper)
	defer s.CloseUserConn(cid)

	// 写入第一次传输的数据，验证 TypeAcceptConn
	udpWrapper.ReadC <- data0
	data := <-udpWrapper.HandshakeRespC
	if data.Type == common.TypeDisconnect {
		logger.LogWithLevel(s.LogLevel, 2, fmt.Sprintf("无法建立UDP连接，srp-server：%s", data.Payload))
		return
	}

	// 设置 deadline
	udpWrapper.SetDeadline(time.Now().Add(common.UDPTimeOut))
	logger.LogWithLevel(s.LogLevel, 2, fmt.Sprintf("建立连接(cid：%d)：%s->srp-server", cid, clientAddr))

	buffer := s.BufferPool.Get().([]byte)
	defer s.BufferPool.Put(buffer)
	for {
		n, err := udpWrapper.Read(buffer)
		if err != nil {
			logger.LogWithLevel(s.LogLevel, 2, fmt.Sprintf("与user：%s的断开连接，%s", clientAddr, err))
			s.DataChan2Client <- common.NewProto(common.CodeSuccess, common.TypeDisconnect, cid, []byte(err.Error()))
			return
		}
		data := make([]byte, n)
		copy(data, buffer[:n])
		s.DataChan2Client <- common.NewProto(common.CodeSuccess, common.TypeForwarding, cid, data)
	}
}
