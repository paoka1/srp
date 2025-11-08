package server

import (
	"fmt"
	"net"
	"os"
	"sync"
	"time"
)

// UDPWrapper 包装 UDP 连接以实现 net.Conn。
// ReadC 模拟 TCP 中的 Read，在超过 UDPTimeOut 的时间未通信后，ReadC 关闭，
// 阻塞在 Read 的函数会立即返回错误。由 AcceptUserConnUDP 向该 chan 写入数据。
// Write 会向 ClientAddr 写入指定的数据
type UDPWrapper struct {
	Conn       *net.UDPConn
	ClientAddr *net.UDPAddr // 远程地址

	ReadC chan []byte   // 从该 UDP 连接中读取数据
	Sigc  chan struct{} // signal cancel: deadline 取消信号

	Deadline time.Time
}

// 实现 net.Conn 的 Read
func (u *UDPWrapper) Read(b []byte) (int, error) {
	if u.ReadC == nil {
		return 0, fmt.Errorf("read chan not set")
	}

	timer := time.NewTimer(time.Until(u.Deadline))
	defer timer.Stop()

	for {
		select {
		case data, ok := <-u.ReadC:
			if !ok {
				return 0, fmt.Errorf("read chan closed")
			}
			copy(b, data)
			return len(data), nil
		case <-u.Sigc:
			if !timer.Stop() {
				<-timer.C
			}
			// 重置 deadline，Sigc 信号由 Write 发出
			timer.Reset(time.Until(u.Deadline))
			continue
		case <-time.After(time.Until(u.Deadline)):
			return 0, fmt.Errorf("read/write timeout")
		}
	}
}

// 实现 net.Conn 的 Write
func (u *UDPWrapper) Write(b []byte) (int, error) {
	defer func() {
		u.Sigc <- struct{}{}
	}()
	if u.ClientAddr == nil {
		return 0, fmt.Errorf("remote address not set")
	}
	return u.Conn.WriteToUDP(b, u.ClientAddr)
}

func (u *UDPWrapper) Close() error {
	if u.ReadC == nil {
		return fmt.Errorf("read chan not set")
	}
	close(u.ReadC)
	u.ReadC = nil
	return nil
}

func (u *UDPWrapper) LocalAddr() net.Addr {
	return u.Conn.LocalAddr()
}

func (u *UDPWrapper) RemoteAddr() net.Addr {
	return u.ClientAddr
}

func (u *UDPWrapper) SetDeadline(t time.Time) error {
	if u.ReadC == nil {
		return fmt.Errorf("read chan not set: %w", os.ErrDeadlineExceeded)
	}
	u.Deadline = t
	return nil
}

func (u *UDPWrapper) SetReadDeadline(t time.Time) error {
	return fmt.Errorf("not implemented")
}

func (u *UDPWrapper) SetWriteDeadline(t time.Time) error {
	return fmt.Errorf("not implemented")
}

// UDPConn 主要用途为：加快从远程地址到 UDP 连接的索引速度
type UDPConn struct {
	AddrConnMap map[string]*UDPWrapper // 远程地址到 UDP 连接的映射

	RWMu *sync.RWMutex // 该锁应为对 Server 或 Client 中锁的引用
}

func (u *UDPConn) AddConn(remoteAddr *net.UDPAddr, conn *UDPWrapper) {
	u.RWMu.Lock()
	defer u.RWMu.Unlock()
	u.AddrConnMap[remoteAddr.String()] = conn
}

func (u *UDPConn) DelConn(remoteAddr *net.UDPAddr) {
	u.RWMu.Lock()
	defer u.RWMu.Unlock()
	delete(u.AddrConnMap, remoteAddr.String())
}

func (u *UDPConn) GetConn(remoteAddr *net.UDPAddr) *UDPWrapper {
	u.RWMu.RLock()
	defer u.RWMu.RUnlock()
	return u.AddrConnMap[remoteAddr.String()]
}
