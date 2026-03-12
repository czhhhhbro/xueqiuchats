package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// 消息结构
type Message struct {
	Name    string `json:"name"`
	Content string `json:"content"` // 文本内容 或 图片的base64
	Time    int64  `json:"time"`
	Type    string `json:"type"` // "text" 或 "image"
}

// 登录请求
type LoginRequest struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

// 发送消息请求
type SendRequest struct {
	Content string `json:"content"`
	Type    string `json:"type"`
}

// 通用响应
type Response struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

var (
	// 内存存储
	messages   = make([]Message, 0, 100) // 保留最近100条
	messagesMu sync.RWMutex

	// session存储  sessionID -> userName
	sessions   = make(map[string]string)
	sessionsMu sync.RWMutex

	// 硬编码密码（仅演示）
	validPassword = "123456"
)

func main() {
	// 静态文件
	http.HandleFunc("/", serveStatic)
	http.HandleFunc("/icon.png", serveIcon)

	// API
	http.HandleFunc("/login", loginHandler)
	http.HandleFunc("/send", authMiddleware(sendHandler))
	http.HandleFunc("/messages", messagesHandler)

	log.Println("服务器启动于 :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

// 静态文件处理
func serveStatic(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" {
		http.ServeFile(w, r, "index.html")
		return
	}
	http.NotFound(w, r)
}

func serveIcon(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "icon.png")
}

// 登录
func loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	if req.Password != validPassword {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(Response{Success: false, Message: "密码错误"})
		return
	}

	// 生成sessionID
	sessionID := generateSessionID()

	// 存储session
	sessionsMu.Lock()
	sessions[sessionID] = req.Name
	sessionsMu.Unlock()

	// 设置cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		MaxAge:   3600 * 24, // 24小时
	})

	json.NewEncoder(w).Encode(Response{Success: true})
}

// 生成随机sessionID
func generateSessionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// 回退：使用时间戳+随机数（简单方案）
		return hex.EncodeToString([]byte(strconv.FormatInt(time.Now().UnixNano(), 10)))
	}
	return hex.EncodeToString(b)
}

// 认证中间件
func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session")
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		sessionsMu.RLock()
		name, ok := sessions[cookie.Value]
		sessionsMu.RUnlock()
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// 将用户名存入请求上下文（通过自定义方式，这里简单使用闭包变量传递）
		// 采用将用户名放入请求头的方式（不优雅但简单）
		r.Header.Set("X-User-Name", name)
		next.ServeHTTP(w, r)
	}
}

// 发送消息
func sendHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name := r.Header.Get("X-User-Name")
	if name == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req SendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// 内容不能为空
	if req.Content == "" {
		json.NewEncoder(w).Encode(Response{Success: false, Message: "内容不能为空"})
		return
	}

	// 构建消息
	msg := Message{
		Name:    name,
		Content: req.Content,
		Time:    time.Now().UnixNano() / 1e6, // 毫秒时间戳
		Type:    req.Type,
	}

	// 存储，保留最近100条
	messagesMu.Lock()
	messages = append(messages, msg)
	if len(messages) > 100 {
		messages = messages[1:]
	}
	messagesMu.Unlock()

	json.NewEncoder(w).Encode(Response{Success: true})
}

// 获取新消息（轮询）
func messagesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sinceStr := r.URL.Query().Get("since")
	since := int64(0)
	if sinceStr != "" {
		since, _ = strconv.ParseInt(sinceStr, 10, 64)
	}

	messagesMu.RLock()
	// 找出大于since的消息
	var newMessages []Message
	for _, msg := range messages {
		if msg.Time > since {
			newMessages = append(newMessages, msg)
		}
	}
	messagesMu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(newMessages)
}
