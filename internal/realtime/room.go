package realtime

import (
	"context"
	"sort"
	"sync"

	"github.com/Tomori77/z-coc/internal/store"
	"github.com/coder/websocket"
)

// client 是一条活跃的 WebSocket 连接。发送走带缓冲的 send 通道，由写泵串行写出，
// 避免多 goroutine 并发写同一连接。
type client struct {
	ws        *websocket.Conn
	roomID    string
	userID    string
	hostToken string
	roomInfo  *store.Room // hello 时抓取的房间快照（主机鉴权用）
	send      chan []byte
	cancel    context.CancelFunc
	closed    chan struct{}
	closeOnce sync.Once
}

// enqueue 非阻塞投递一帧；缓冲满（慢消费者）直接断开连接。
func (c *client) enqueue(data []byte) bool {
	select {
	case <-c.closed:
		return false
	default:
	}
	select {
	case c.send <- data:
		return true
	case <-c.closed:
		return false
	default:
		c.close()
		return false
	}
}

// close 幂等地取消上下文、关闭底层连接，从而结束读写泵。
func (c *client) close() {
	c.closeOnce.Do(func() {
		close(c.closed)
		c.cancel()
		_ = c.ws.CloseNow()
	})
}

// room 是一个房间在本实例内的连接集合；members 记录曾出现过的成员，
// 用于在成员断开后仍能广播 online=false 的 presence。
type room struct {
	id      string
	mu      sync.Mutex
	clients map[*client]struct{}
	members map[string]struct{}
}

func newRoom(id string) *room {
	return &room{
		id:      id,
		clients: make(map[*client]struct{}),
		members: make(map[string]struct{}),
	}
}

func (r *room) add(c *client) {
	r.mu.Lock()
	r.clients[c] = struct{}{}
	r.members[c.userID] = struct{}{}
	r.mu.Unlock()
}

// remove 移除连接并报告房间是否已空。
func (r *room) remove(c *client) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.clients, c)
	return len(r.clients) == 0
}

// broadcast 向房间内所有连接投递一帧（快照后逐个投递，避免持锁发送）。
func (r *room) broadcast(data []byte) {
	r.mu.Lock()
	targets := make([]*client, 0, len(r.clients))
	for c := range r.clients {
		targets = append(targets, c)
	}
	r.mu.Unlock()
	for _, c := range targets {
		c.enqueue(data)
	}
}

// presence 由内存连接集派生在线状态：成员曾有连接但当前无活跃连接即 online=false。
func (r *room) presence() []Member {
	r.mu.Lock()
	online := make(map[string]bool, len(r.clients))
	for c := range r.clients {
		online[c.userID] = true
	}
	members := make([]Member, 0, len(r.members))
	for userID := range r.members {
		members = append(members, Member{UserID: userID, Online: online[userID]})
	}
	r.mu.Unlock()
	sort.Slice(members, func(i, j int) bool { return members[i].UserID < members[j].UserID })
	return members
}

// closeAll 在 Hub 关闭时断开房间内所有连接。
func (r *room) closeAll() {
	r.mu.Lock()
	targets := make([]*client, 0, len(r.clients))
	for c := range r.clients {
		targets = append(targets, c)
	}
	r.mu.Unlock()
	for _, c := range targets {
		c.close()
	}
}
