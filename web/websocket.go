package web

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"quantmesh/storage"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // 允許所有来源（生產环境应該限制）
	},
}

// WebSocketHub WebSocket 中心
type WebSocketHub struct {
	clients    map[*websocket.Conn]bool
	broadcast  chan []byte
	register   chan *websocket.Conn
	unregister chan *websocket.Conn
	mu         sync.RWMutex
}

var (
	hub          *WebSocketHub
	logStorage   *storage.LogStorage
	logStorageMu sync.RWMutex
)

func init() {
	hub = &WebSocketHub{
		clients:    make(map[*websocket.Conn]bool),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *websocket.Conn),
		unregister: make(chan *websocket.Conn),
	}
	go hub.Run()
}

// SetLogStorage 設置日志存儲（用於實時推送）
func SetLogStorage(ls *storage.LogStorage) {
	logStorageMu.Lock()
	defer logStorageMu.Unlock()
	logStorage = ls
}

// Run 运行 WebSocket 中心
func (h *WebSocketHub) Run() {
	for {
		select {
		case conn := <-h.register:
			h.mu.Lock()
			h.clients[conn] = true
			h.mu.Unlock()

		case conn := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[conn]; ok {
				delete(h.clients, conn)
				conn.Close()
			}
			h.mu.Unlock()

		case message := <-h.broadcast:
			h.mu.RLock()
			for conn := range h.clients {
				if err := conn.WriteMessage(websocket.TextMessage, message); err != nil {
					h.mu.RUnlock()
					h.mu.Lock()
					delete(h.clients, conn)
					conn.Close()
					h.mu.Unlock()
					h.mu.RLock()
				}
			}
			h.mu.RUnlock()
		}
	}
}

// BroadcastStatus 广播状態
func BroadcastStatus(status *SystemStatus) {
	if hub == nil {
		return
	}
	data, err := json.Marshal(status)
	if err != nil {
		return
	}
	select {
	case hub.broadcast <- data:
	default:
		// Channel 满了，丢弃消息
	}
}

func handleWebSocket(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}

	hub.register <- conn

	// 检查是否订阅日志
	subscribeLogs := c.Query("subscribe_logs") == "true"

	var logCh chan *storage.LogRecord
	if subscribeLogs {
		logStorageMu.RLock()
		ls := logStorage
		logStorageMu.RUnlock()

		if ls != nil {
			logCh = ls.Subscribe()
			defer ls.Unsubscribe(logCh)
		}
	}

	// 啟动日志推送协程
	if logCh != nil {
		go func() {
			for {
				select {
				case logRecord, ok := <-logCh:
					if !ok {
						return
					}
					// 推送日志
					message := map[string]interface{}{
						"type": "log",
						"data": map[string]interface{}{
							"id":        logRecord.ID,
							"timestamp": logRecord.Timestamp,
							"level":     logRecord.Level,
							"message":   logRecord.Message,
						},
					}
					data, err := json.Marshal(message)
					if err != nil {
						continue
					}
					if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
						return
					}
				}
			}
		}()
	}

	// 啟动心跳协程
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				// 发送 ping 消息
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					close(done)
					return
				}
			case <-done:
				return
			}
		}
	}()

	// 設置 pong 处理器
	conn.SetPongHandler(func(string) error {
		// 收到 pong 响应，重置读取超時
		conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		return nil
	})

	// 設置初始读取超時
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))

	// 保持连接
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			close(done)
			hub.unregister <- conn
			break
		}
	}
}
