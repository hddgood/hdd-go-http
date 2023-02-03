package httpd

import (
	"bytes"
	"io"
	"io/ioutil"
	"strings"
)

type Part struct {
	// 存储当前part的首部
	Header Header
	mr     *MultipartReader
	// Part的name
	formName string
	// 当该part传输文件时，fileName不为空
	fileName string
	// part是否关闭
	closed bool
	// 替补Reader
	substituteReader io.Reader
	// 是否已经解析过formName以及fileName
	parsed bool
}

func (p *Part) Read(b []byte) (n int, err error) {
	// part已经关闭后，直接返回io.EOF错误
	if p.closed {
		return 0, io.EOF
	}

	// 不为nil时，优先让substituteReader读取
	if p.substituteReader != nil {
		return p.substituteReader.Read(b)
	}

	bufr := p.mr.bufr
	var peek []byte
	//如果已经出现EOF错误，说明Body没数据了，这时只需要关心bufr还剩余已缓存的数据
	if p.mr.occurEofErr {
		peek, _ = bufr.Peek(bufr.Buffered()) // 将最后缓存数据取出
	} else {
		// Body还有数据,强制读取一次缓存区大小的数据
		peek, err = bufr.Peek(bufSize)
		// 如果有EOF错误则表示已经读取完了
		if err == io.EOF {
			p.mr.occurEofErr = true
			return p.Read(b)
		}
		if err != nil {
			return 0, err
		}
	}

	//在peek出的数据中找boundary
	index := bytes.Index(peek, p.mr.dashBoundary)
	//两种情况：
	//1.即||前的条件，index!=-1代表在peek出的数据中找到分隔符，也就代表顺利找到了该part的Read指针终点，
	//	给该part限制读取长度即可。
	//2.即||后的条件，在前文的multipart报文，是需要boudary来标识报文结尾，然后已经出现EOF错误,
	//  即在没有多余报文的情况下，还没有发现结尾标识，说明客户端没有将报文发送完整，就关闭了链接，
	//  这时让substituteReader = io.LimitReader(-1)，逻辑上等价于eofReader即可
	if index != -1 || (index == -1 && p.mr.occurEofErr) {
		p.substituteReader = io.LimitReader(bufr, int64(index))
		return p.substituteReader.Read(b)
	}
	//以下则是在peek出的数据中没有找到分隔符的情况，说明peek出的数据属于当前的part
	//见上文讲解，不能一次把所有的bufSize都当作消息主体读出，还需要减去分隔符的最长子串的长度。
	maxRead := bufSize - len(p.mr.crlfDashBoundary) + 1
	if maxRead > len(b) {
		maxRead = len(b)
	}
	return bufr.Read(b[:maxRead])
}

// 直接利用了解析http报文首部的函数readHeader，很简单
func (p *Part) readHeader() (err error) {
	p.Header, err = readHeader(p.mr.bufr)
	return err
}

// 将当前part剩余的数据消费掉，防止其报文残存在Reader上影响下一个part
func (p *Part) Close() error {
	if p.closed {
		return nil
	}
	_, err := io.Copy(ioutil.Discard, p)
	p.closed = true //标记状态为关闭
	return err
}

// 获取FormName
func (p *Part) FormName() string {
	if !p.parsed {
		p.parseFormData()
	}
	return p.formName
}

// 获取FileName
func (p *Part) FileName() string {
	if !p.parsed {
		p.parseFormData()
	}
	return p.fileName
}

func (p *Part) parseFormData() {
	p.parsed = true
	cd := p.Header.Get("Content-Disposition")
	ss := strings.Split(cd, ";")
	if len(ss) == 1 || strings.ToLower(ss[0]) != "form-data" {
		return
	}
	for _, s := range ss {
		key, value := getKV(s)
		switch key {
		case "name":
			p.formName = value
		case "filename":
			p.fileName = value
		}
	}
}

func getKV(s string) (key string, value string) {
	ss := strings.Split(s, "=")
	if len(ss) != 2 {
		return
	}
	return strings.TrimSpace(ss[0]), strings.Trim(ss[1], `"`)
}
