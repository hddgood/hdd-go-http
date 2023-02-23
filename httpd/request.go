package httpd

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/url"
	"strconv"
	"strings"
)

type eofReader struct{}

// 实现了io.Reader接口
func (er *eofReader) Read([]byte) (n int, err error) {
	return 0, io.EOF
}

type Request struct {
	//请求方法，如GET、POST、PUT
	Method string
	//URL
	URL *url.URL
	//协议以及版本
	Proto string
	//首部字段
	Header Header
	//用于读取报文主体
	Body io.Reader
	// 客户端地址
	RemoteAddr string
	//字符串形式的url
	RequestURI string
	//产生此request的http连接
	conn *conn
	//存储cookie	私有化
	cookies map[string]string
	//存储queryString	私有化
	queryString map[string]string
	//body的类型
	contentType string
	//表单边界的标志
	boundary string
	//post请求的表单
	postForm map[string]string
	//存储传输的文件信息
	multipartForm *MultipartForm
	//是否已经解析过表单
	haveParsedForm bool
	//解析表单出错
	parseFormErr error
}

func readRequest(c *conn) (r *Request, err error) {
	r = new(Request)
	r.conn = c
	r.RemoteAddr = c.rwc.RemoteAddr().String()

	//读出第一行,如：Get /index?name=gu HTTP/1.1
	line, err := readLine(c.bufr)
	if err != nil {
		return
	}

	// 按空格分割就得到了三个属性
	_, err = fmt.Sscanf(string(line), "%s%s%s", &r.Method, &r.RequestURI, &r.Proto)
	if err != nil {
		return
	}

	// 将字符串形式的URI变成url.URL形式
	r.URL, err = url.ParseRequestURI(r.RequestURI)
	if err != nil {
		return
	}

	//解析queryString
	r.parseQuery()

	//读header
	r.Header, err = readHeader(c.bufr)
	if err != nil {
		return
	}
	const noLimit = (1 << 63) - 1
	r.conn.lr.N = noLimit //Body的读取无需进行读取字节数限制

	//设置body
	r.setupBody()
	return r, nil
}

// 避免首部行超过缓存 所以将读取首部行的进行封装
func readLine(bufr *bufio.Reader) ([]byte, error) {
	// prefix 为bool类型 代表这一行是否超出了缓存 如果为true的话 表示缓存已满，一行未完全读取
	p, prefix, err := bufr.ReadLine()
	if err != nil {
		return p, err
	}
	var l []byte
	// 使用for 循环一直读取 直到prefix为false
	for prefix {
		l, prefix, err = bufr.ReadLine()
		if err != nil {
			break
		}
		p = append(p, l...)
	}
	return p, err
}

// 解析queryString
func (r *Request) parseQuery() {
	r.queryString = parseQuery(r.URL.RawQuery)
}

// 先以&符为分隔得到一个个k-v对，然后以=符为分割分别得到key以及value，存入map即可。
func parseQuery(query string) map[string]string {
	parts := strings.Split(query, "&")
	queries := make(map[string]string, len(parts))
	for _, part := range parts {
		index := strings.IndexByte(part, '=')
		if index == -1 || index == len(part)-1 {
			continue
		}
		queries[strings.TrimSpace(part[:index])] = strings.TrimSpace(part[index+1:])
	}
	return queries
}

// 读取报文的头部
func readHeader(bufr *bufio.Reader) (Header, error) {
	header := Header{}

	for {
		line, err := readLine(bufr)

		if err != nil {
			return nil, err
		}

		// 如果读到/r/n/r/n，代表报文首部的结束
		if len(line) == 0 {
			break
		}
		index := bytes.IndexByte(line, ':')
		if index == -1 {
			return header, errors.New("unsupported protocol")
		}
		if index == len(line)-1 {
			continue
		}

		k, v := string(line[:index]), strings.TrimSpace(string(line[index+1:]))
		header[k] = append(header[k], v)
	}

	return header, nil
}

// 查询queryString 将queryString设为只读的
func (r *Request) Query(name string) string {
	return r.queryString[name]
}

// 查询cookie Cookie也是只读的 并且使用懒加载的方式
func (r *Request) Cookie(name string) string {
	if r.cookies == nil {
		r.parseCookies()
	}

	return r.cookies[name]
}

// 解析cookie
func (r *Request) parseCookies() {
	if r.cookies != nil {
		return
	}
	r.cookies = make(map[string]string)
	// 从Header中读取cookie
	rawCookies, ok := r.Header["Cookie"]

	if !ok {
		return
	}

	for _, line := range rawCookies {
		//example(line): uuid=12314753; tid=1BDB9E9; HOME=1(见上述的http请求报文)
		kvs := strings.Split(strings.TrimSpace(line), ";")
		if len(kvs) == 1 && kvs[0] == "" {
			continue
		}
		for i := 0; i < len(kvs); i++ {
			//example(kvs[i]): uuid=12314753
			index := strings.IndexByte(kvs[i], '=')
			if index == -1 {
				continue
			}
			r.cookies[strings.TrimSpace(kvs[i][:index])] = strings.TrimSpace(kvs[i][index+1:])
		}
	}
	return
}

func (r *Request) setupBody() {
	if r.Method != "POST" && r.Method != "PUT" && r.Method != "GET" {
		r.Body = &eofReader{} //POST和PUT和GET以外的方法不允许设置报文主体
	} else if cl := r.Header.Get("Content-Length"); cl != "" {
		//如果设置了Content-Length
		contentLength, err := strconv.ParseInt(cl, 10, 64)
		if err != nil {
			r.Body = &eofReader{}
			return
		}
		// 允许Body最多读取contentLength的数据
		r.Body = io.LimitReader(r.conn.bufr, contentLength)
		r.fixExpectContinueReader()
	} else if r.chunked() {
		// 将读取报文设置为chunkReader
		r.Body = &chunkReader{bufr: r.conn.bufr}
		r.fixExpectContinueReader()
	} else {
		r.Body = &eofReader{}
	}
}

// 检查此报文的body是否使用chunk编码方式传输
func (r *Request) chunked() bool {
	chunk := r.Header.Get("Transfer-Encoding")
	return chunk == "chunked"
}

/*
有些客户端在发送完http首部之后，发送body数据前，会先通过发送Expect: 100-continue查询服务端是否希望接受body数据，
服务端只有回复了HTTP/1.1 100 Continue客户端才会再次发送body。
*/
type expectContinueReader struct {
	// 是否已经发送过100 continue
	wroteContinue bool
	r             io.Reader
	w             *bufio.Writer
}

func (er *expectContinueReader) Read(p []byte) (n int, err error) {
	//第一次读取前发送100 continue
	if !er.wroteContinue {
		er.w.WriteString("HTTP/1.1 100 Continue\r\n\r\n")
		er.w.Flush()
		er.wroteContinue = true
	}
	return er.r.Read(p)
}

// 判断客户端是否发送的是Expect: 100-continue请求
func (r *Request) fixExpectContinueReader() {
	if r.Header.Get("Expect") != "100-continue" {
		return
	}
	r.Body = &expectContinueReader{
		r: r.Body,
		w: r.conn.bufw,
	}
}

// 防止处理此次请求并未读取报文主体的情况
// 对响应做一些处理
func (r *Request) finishRequest(resp *response) (err error) {

	// 删除所有在磁盘上的临时文件
	if r.multipartForm != nil {
		r.multipartForm.RemoveAll()
	}
	//告诉chunkWriter handler已经结束
	resp.handlerDone = true

	//触发chunkWriter的Write方法，Write方法通过handlerDone来决定是用chunk还是Content-Length
	if err = resp.bufw.Flush(); err != nil {
		return
	}

	//如果是使用chunk编码，还需要将结束标识符传输
	if resp.chunking {
		_, err = resp.c.bufw.WriteString("0\r\n\r\n")
		if err != nil {
			return
		}
	}

	//如果用户的handler中未Write任何数据，我们手动触发(*chunkWriter).writeHeader
	if !resp.cw.wrote {
		resp.header.Set("Content-Length", "0")
		if err = resp.cw.writeHeader(); err != nil {
			return
		}
	}

	//将缓存中的剩余的数据发送到rwc中
	if err = r.conn.bufw.Flush(); err != nil {
		return
	}
	//消费掉剩余的数据
	_, err = io.Copy(ioutil.Discard, r.Body)

	return err
}

// 解析ContentType 并把boundary解析出来
func (r *Request) parseContentType() {
	ct := r.Header.Get("Content-Type")
	//Content-Type: multipart/form-data; boundary=------974767299852498929531610575
	//Content-Type: multipart/form-data; boundary=""------974767299852498929531610575"
	//Content-Type: application/x-www-form-urlencoded
	index := strings.IndexByte(ct, ';')
	if index == -1 {
		r.contentType = ct
		return
	}
	if index == len(ct)-1 {
		return
	}
	ss := strings.Split(ct[index+1:], "=")
	if len(ss) < 2 || strings.TrimSpace(ss[0]) != "boundary" {
		return
	}
	// 将解析到的CT和boundary保存在Request中
	r.contentType, r.boundary = ct[:index], strings.Trim(ss[1], `"`)
	return
}

// 得到一个MultipartReader
func (r *Request) MultipartReader() (*MultipartReader, error) {
	if r.boundary == "" {
		return nil, errors.New("no boundary detected")
	}
	return NewMultipartReader(r.Body, r.boundary), nil
}

// 通过formname获取指定文件
func (r *Request) FormFile(name string) (fh *FileHeader, err error) {
	mf, err := r.MultipartForm()
	if err != nil {
		return
	}
	fh, ok := mf.File[name]
	if !ok {
		return nil, errors.New("http: missing multipart file")
	}
	return
}

// 获取普通文本信息
func (r *Request) PostForm(name string) string {
	if !r.haveParsedForm {
		r.parseFormErr = r.parseForm()
	}
	if r.parseFormErr != nil || r.postForm == nil {
		return ""
	}
	return r.postForm[name]
}

// 获取文件信息
func (r *Request) MultipartForm() (*MultipartForm, error) {
	if !r.haveParsedForm {
		if err := r.parseForm(); err != nil {
			r.parseFormErr = err
			return nil, err
		}
	}
	return r.multipartForm, r.parseFormErr
}

// 解析表单操作
func (r *Request) parseForm() error {
	if r.Method != "POST" && r.Method != "PUT" && r.Method != "GET" {
		return errors.New("missing form body")
	}
	r.haveParsedForm = true
	switch r.contentType {
	case "application/x-www-form-urlencoded":
		return r.parsePostForm()
	case "multipart/form-data":
		return r.parseMultipartForm()
	default:
		return errors.New("unsupported form type")
	}
}

// 解析"application/x-www-form-urlencoded"的表单
func (r *Request) parsePostForm() error {
	data, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return err
	}
	r.postForm = parseQuery(string(data))
	return nil
}

// 解析"multipart/form-data"类型的表单 包括文字与文件的解析
func (r *Request) parseMultipartForm() error {
	mr, err := r.MultipartReader()
	if err != nil {
		return err
	}
	r.multipartForm, err = mr.ReadForm()
	//让PostForm方法也可以访问multipart表单的文本数据
	r.postForm = r.multipartForm.Value
	return err
}
