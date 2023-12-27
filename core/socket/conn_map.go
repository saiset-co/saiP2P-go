package socket

import (
	"bytes"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

type ConnMap struct {
	mu             sync.Mutex
	connections    map[int64]*SocketConn
	nextConnNumber *atomic.Int64

	lastUsed int
	nums     []int64
}

func (m *ConnMap) Update(num int64, fn func(c *SocketConn)) {
	m.mu.Lock()
	defer m.mu.Unlock()

	fn(m.connections[num])
}

func (m *ConnMap) Conn(num int64) net.Conn {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, ok := m.connections[num]
	if !ok {
		return nil
	}

	conn := m.connections[num].conn

	return conn
}

func (m *ConnMap) ConnExist(num int64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, ok := m.connections[num]
	return ok
}

func (m *ConnMap) ReadBuffer(num int64) *bytes.Buffer {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.connections[num].readBuffer
}

func (m *ConnMap) Status(num int64) Status {
	m.mu.Lock()
	defer m.mu.Unlock()

	status := m.connections[num].status
	return status
}

func (m *ConnMap) Connections() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	l := len(m.connections)

	return l
}

//func (m *ConnMap) ForEach(func(s *SocketConn)) {
//	m.mu.Lock()
//	defer m.mu.Unlock()
//
//	if len(m.nums) == 0 {
//		return 0
//	}
//	m.lastUsed++
//	if m.lastUsed >= len(m.nums) {
//		m.lastUsed = 0
//	}
//	return m.nums[m.lastUsed]
//}

func (m *ConnMap) Next() int64 {

	for {

		m.mu.Lock()
		if len(m.nums) == 0 {
			m.mu.Unlock()
			time.Sleep(time.Millisecond * 100)
			continue
		}

		m.lastUsed++
		if m.lastUsed >= len(m.nums) {
			m.lastUsed = 0
		}

		v := m.nums[m.lastUsed]

		c, ok := m.connections[v]
		if !ok {
			m.lastUsed = 0
			m.mu.Unlock()
			continue
		}

		if c.status == StatusOK {
			m.mu.Unlock()
			return v
		}

		m.mu.Unlock()
		time.Sleep(time.Second)
	}
}

func (m *ConnMap) Send(num int64, msg SocketMessage) {
	m.mu.Lock()

	_, ok := m.connections[num]
	if !ok {
		m.mu.Unlock()
		return
	}

	if m.connections[num].status == StatusClosing {
		m.mu.Unlock()
		return
	}
	out := m.connections[num].out
	m.mu.Unlock()

	out <- msg
}

func (m *ConnMap) AddConn(c *SocketConn) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()

	num := m.nextConnNumber.Load()
	m.connections[num] = c
	m.nextConnNumber.Add(1)
	m.nums = append(m.nums, num)

	return num
}

func (m *ConnMap) ReadErrorCount(num int64) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.connections[num].readingErrorCount
}

func (m *ConnMap) DropConn(num int64) {

	m.mu.Lock()
	defer m.mu.Unlock()

	_, ok := m.connections[num]
	if !ok {
		return
	}

	_ = m.connections[num].conn.Close()
	m.nextConnNumber.Add(-1)

	resolvedNums := []int64{}
	for _, n := range m.nums {
		if n == num {
			continue
		}
		resolvedNums = append(resolvedNums, n)
	}

	m.connections[num].readBuffer.Reset()
	close(m.connections[num].out)
	m.nums = resolvedNums
	m.lastUsed = 0
	delete(m.connections, num)
}
