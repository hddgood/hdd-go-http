package httpd

import "net"

// 处理器
type Handler interface {
	ServeHttp(w ResponseWriter, r *Request)
}

type HandlerFunc func(ResponseWriter, *Request)

type ServeMux struct {
	m map[string]HandlerFunc //利用map存取路由
}

func NewServeMux() *ServeMux {
	return &ServeMux{
		m: make(map[string]HandlerFunc),
	}
}

func (sm *ServeMux) HandleFunc(pattern string, cb HandlerFunc) {
	if sm.m == nil {
		sm.m = make(map[string]HandlerFunc)
	}
	sm.m[pattern] = cb
}

func (sm *ServeMux) Handle(pattern string, handler Handler) {
	if sm.m == nil {
		sm.m = make(map[string]HandlerFunc)
	}
	sm.m[pattern] = handler.ServeHttp
}

func (sm *ServeMux) ServeHttp(w ResponseWriter, r *Request) {
	// 查看路由表项是否存在对应entry
	handler, ok := sm.m[r.URL.Path]
	if !ok {
		if len(r.URL.Path) > 1 && r.URL.Path[len(r.URL.Path)-1] == '/' {
			handler, ok = sm.m[r.URL.Path[:len(r.URL.Path)-1]]
		}
		if !ok {
			w.WriteHeader(StatusNotFound)
			return
		}
	}
	handler(w, r)
}

// 对应的一个服务 监听一个地址（Addr） 对应的回调函数（Handler）
type Server struct {
	Addr    string
	Handler Handler
}

// 监听地址函数
func (s *Server) ListenAndServer() error {
	// tcp监听
	listen, err := net.Listen("tcp", s.Addr)

	if err != nil {
		return err
	}

	// 重复循环监听端口 有tcp连接的请求就建立tcp连接 并为每个连接开启一个协程
	for {
		rwc, err := listen.Accept()
		if err != nil {
			continue
		}
		c := newConn(rwc, s)
		go c.serve() // 为每一个连接开启一个go程
	}
}
