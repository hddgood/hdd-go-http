package httpd

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
)

// 每个连接的服务 以及底层的tcp连接
type conn struct {
	svr  *Server
	rwc  net.Conn
	lr   *io.LimitedReader
	bufr *bufio.Reader //bufr是对lr的封装
	bufw *bufio.Writer // 使用带缓存的写入
}

func newConn(rwc net.Conn, svr *Server) *conn {
	lr := &io.LimitedReader{R: rwc, N: 1 << 20}
	return &conn{
		svr:  svr,
		rwc:  rwc,
		lr:   lr,
		bufr: bufio.NewReaderSize(rwc, 4<<10),
		bufw: bufio.NewWriterSize(rwc, 4<<10),
	}
}

func (c *conn) serve() {
	defer func() {
		if err := recover(); err != nil {
			log.Printf("panic recoverred,err:%v\n", err)
		}
		c.close()
	}()

	// 使用for 循环支持一个长连接 不断的读取请求
	for {
		// 读取request请求
		req, err := c.readRequest()

		// 处理请求的错误
		if err != nil {
			handleErr(err, c)
			return
		}

		// 设置响应
		res := c.setUpResponse(req)

		// 直接使用回调函数
		c.svr.Handler.ServeHttp(res, req)

		// 结束请求的操作
		if err = req.finishRequest(res); err != nil {
			return
		}

		//如果此次请求已经结束，就直接关闭连接
		if res.closeAfterReply {
			return
		}
	}
}

func handleErr(err error, c *conn) {
	fmt.Println(err)
}

func (c *conn) close() { c.rwc.Close() }

func (c *conn) readRequest() (r *Request, err error) {
	r, err = readRequest(c)
	// 解析表单的类型
	r.parseContentType()
	return

}

func (c *conn) setUpResponse(req *Request) *response {
	return setupResponse(c, req)
}
