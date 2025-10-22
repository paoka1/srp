package server

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"srp/internal/common"
	"sync"
	"time"
)

type Config struct {
	ClientPort     int // srp client port
	UserPort       int // user port
	ServerPassword string
	IsDebug        bool
}

type Server struct {
	Config

	UIDNext    uint32
	ClientConn net.Conn
	UserUIDMap map[uint32]net.Conn

	DataChan2User   chan common.Proto // data channel to user
	DataChan2Client chan common.Proto // data channel to client
	DataChan2Handle chan common.Proto // data channel to handle

	Mu *sync.Mutex
}

func (s *Server) AddUserConn(uid uint32, conn net.Conn) {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	s.UserUIDMap[uid] = conn
}

func (s *Server) RemoveUserConn(uid uint32) {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	delete(s.UserUIDMap, uid)
}

func (s *Server) CloseClientConn() {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	if s.ClientConn != nil {
		s.ClientConn.Close()
		s.ClientConn = nil
	}
}

func (s *Server) NewClientConn(conn net.Conn) error {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	if s.ClientConn == nil {
		s.ClientConn = conn
		return nil
	} else {
		return errors.New("存在已连接的srp-client，拒绝" + conn.RemoteAddr().String() + "的连接")
	}
}

func (s *Server) GetNextUID() uint32 {
	s.Mu.Lock()
	defer func() {
		s.UIDNext++
		s.Mu.Unlock()
	}()
	return s.UIDNext
}

func (s *Server) CloseAllUserConn() {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	for _, c := range s.UserUIDMap {
		c.Close()
	}
	s.UserUIDMap = make(map[uint32]net.Conn)
}

func (s *Server) AcceptClient() {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", s.ClientPort))
	if err != nil {
		log.Fatal("无法创建tcp监听，" + err.Error())
	}

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Println("拒绝srp-client：" + conn.RemoteAddr().String() + "的连接，" + err.Error())
			continue
		} else {
			log.Println("开始处理srp-client：" + conn.RemoteAddr().String() + "的连接")
		}
		go s.HandleClient(conn)
	}
}

// HandleClient 完成 srp-client 的认证和处理
func (s *Server) HandleClient(conn net.Conn) {
	// 设置期限
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))

	// 已存在连接，断开连接请求
	if s.ClientConn != nil {
		conn.Close()
		log.Println("存在已连接的srp-client，拒绝：" + conn.RemoteAddr().String() + "的连接")
		return
	}

	// 完成验证
	data := common.Proto{}
	reader := bufio.NewReader(conn)
	if err := data.DecodeProto(reader); err != nil {
		conn.Close()
		log.Println("拒绝srp-client：" + conn.RemoteAddr().String() + "的连接，" + err.Error())
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
		log.Println("拒绝srp-client:" + conn.RemoteAddr().String() + "的连接，无法处理数据，" + err.Error())
		return
	}

	if _, err = conn.Write(dataByte); err != nil {
		conn.Close()
		log.Println("拒绝srp-client:" + conn.RemoteAddr().String() + "的连接，无法发送数据，" + err.Error())
		return
	}

	if authFailed {
		conn.Close()
		log.Println("拒绝srp-client:" + conn.RemoteAddr().String() + "的连接，密码错误")
		return
	}

	s.ClientConn = conn
	// 无限时长
	conn.SetReadDeadline(time.Time{})
	log.Println("成功建立与srp-client:" + conn.RemoteAddr().String() + "的连接")

	// 接收来自 srp-client 的消息，放到 DataChan2User
	for {
		if err = data.DecodeProto(reader); err != nil {
			if errors.Is(err, io.EOF) {
				log.Println("srp-client：" + conn.RemoteAddr().String() + "断开连接，" + err.Error())
			} else {
				log.Println("读取srp-client：" + conn.RemoteAddr().String() + "的数据失败，" + err.Error())
			}
			s.CloseClientConn()
			s.CloseAllUserConn()
			return
		}
		if data.Type == common.TypeAcceptUser || data.Type == common.TypeRejectUser {
			s.DataChan2Handle <- data
		} else {
			s.DataChan2User <- data
		}
	}
}

func (s *Server) AcceptUserConn() {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", s.UserPort))
	if err != nil {
		log.Fatal("无法监听tcp连接，" + err.Error())
	}
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Println("拒绝user：" + conn.RemoteAddr().String() + " 的连接，" + err.Error())
			continue
		}
		go s.HandleUserConn(conn)
	}
}

// HandleUserConn 完成连接创建和接收数据
func (s *Server) HandleUserConn(conn net.Conn) {
	if s.ClientConn == nil {
		conn.Close()
		log.Println("srp-client连接为空，拒绝user：" + conn.RemoteAddr().String() + "的连接")
		return
	}

	// 完成注册
	uid := s.GetNextUID()
	data := common.NewProto(common.CodeSuccess, common.TypeNewUser, uid, nil)
	dataByte, err := data.EncodeProto()
	if err != nil {
		log.Println("拒绝user：" + conn.RemoteAddr().String() + "的连接，" + err.Error())
		conn.Close()
		return
	}

	if _, err = s.ClientConn.Write(dataByte); err != nil {
		log.Println("拒绝user：" + conn.RemoteAddr().String() + "的连接，" + err.Error())
		conn.Close()
		return
	}
	log.Println(fmt.Sprintf("已向srp-client发送user(%s)的连接申请", conn.RemoteAddr().String()))

	// 验证 TypeAcceptUser
	for {
		data = <-s.DataChan2Handle
		if data.UID != uid {
			// 不是自己的就放进去
			s.DataChan2Handle <- data
			continue
		}
		break
	}

	if data.Code != common.CodeSuccess || data.Type != common.TypeAcceptUser {
		log.Println("拒绝user：" + conn.RemoteAddr().String() + "的连接，srp-client拒绝连接")
		conn.Close()
		return
	}

	tcpConn := conn.(*net.TCPConn)
	tcpConn.SetKeepAlive(true)

	s.AddUserConn(uid, conn)
	log.Println(fmt.Sprintf("建立连接(uid：%d)：%s->%s", uid, conn.LocalAddr().String(), conn.RemoteAddr().String()))

	// 读取消息，放到 DataChan2Client
	for {
		dataByte = make([]byte, common.MaxBufferSize)
		byteLen, err := conn.Read(dataByte)
		if err != nil {
			if err == io.EOF {
				log.Println("与user：" + conn.RemoteAddr().String() + "连接失效，" + err.Error())
			} else {
				log.Println("user：" + conn.RemoteAddr().String() + "断开连接，" + err.Error())
			}
			if _, ok := s.UserUIDMap[data.UID]; ok {
				data = common.NewProto(common.CodeSuccess, common.TypeDisconnection, uid, []byte{})
				s.DataChan2Client <- data
				s.RemoveUserConn(uid)
			}
			return
		}

		// 只传输读取的所有数据，而不是原来的 dataByte
		dataByte = dataByte[:byteLen]
		// 打上 UID 标签
		data = common.NewProto(common.CodeSuccess, common.TypeForwarding, uid, dataByte)
		s.DataChan2Client <- data
	}
}
