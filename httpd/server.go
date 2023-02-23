package httpd

import (
	"log"
	"net"
)

// 处理器
type Handler interface {
	ServeHttp(w ResponseWriter, r *Request)
}

type HandlerFunc func(ResponseWriter, *Request)

type Engine struct {
	trees []MethodTree
}

func New() *Engine {
	e := &Engine{
		trees: make([]MethodTree, 0, 9),
	}
	return e
}

// 添加路由
func (e *Engine) HandlerFunc(method string, path string, handler HandlerFunc) {
	// 错误判断
	assert1(path[0] == '/', "path must begin with '/'")
	assert1(method != "", "HTTP method can not be empty")

	tree := e.get(method)
	if tree == nil {
		tree = new(MethodTree)
		tree.method = method
		tree.root = newRoot()
		e.trees = append(e.trees, *tree)
	}
	tree.root.addPath(path[1:], handler)
}

func (e Engine) get(method string) *MethodTree {
	for _, v := range e.trees {
		if v.method == method {
			return &v
		}
	}
	return nil
}

func (e *Engine) ServeHttp(w ResponseWriter, r *Request) {
	// 检查路由书树中是否有对应路由
	method := r.Method
	mt := e.get(method)
	if mt == nil {
		log.Println("method not fount")
		w.WriteHeader(StatusNotFound)
		return
	}
	handler, ok := mt.getHandler(r)
	if ok {
		handler(w, r)
	} else {
		w.WriteHeader(StatusNotFound)
	}
}

// 对应的一个服务 监听一个地址（Addr） 对应的回调函数（Handler）
type Server struct {
	Addr    string
	Handler Handler
}

// 监听地址函数
func (s *Server) ListenAndServer() error {
	log.Println("start server success ...")
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
