package httpd

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
)

const bufSize = 4096 // 滑动窗口大小

type MultipartReader struct {
	// bufr是对Body的封装，方便我们预查看Body上的数据，从而确定part之间边界
	// 每个part共享这个bufr，但只有Body的读取指针指向哪个part的报文，
	// 哪个part才能在bufr上读取数据，此时其他part是无效的
	bufr *bufio.Reader
	// 记录bufr的读取过程中是否出现io.EOF错误，如果发生了这个错误，
	// 说明Body数据消费完毕，表单报文也消费完，不需要再产生下一个part
	occurEofErr          bool
	crlfDashBoundaryDash []byte  //\r\n--boundary--
	crlfDashBoundary     []byte  //\r\n--boundary，分隔符
	dashBoundary         []byte  //--boundary
	dashBoundaryDash     []byte  //--boundary--  报文结尾标识
	curPart              *Part   //当前解析到了哪个part
	crlf                 [2]byte //用于消费掉\r\n
}

// 获取一个新的MultipartReader 传入的r将是Request的Body，boundary会在http首部解析时就得到
func NewMultipartReader(r io.Reader, boundary string) *MultipartReader {
	b := []byte("\n\n--" + boundary + "--")
	return &MultipartReader{
		bufr:                 bufio.NewReaderSize(r, bufSize), //将io.Reader封装成bufio.Reader
		crlfDashBoundaryDash: b,
		crlfDashBoundary:     b[:len(b)-2],
		dashBoundary:         b[2 : len(b)-2],
		dashBoundaryDash:     b[2:],
		curPart:              nil,
	}
}

// 获取下一个Part
func (m *MultipartReader) NextPart() (p *Part, err error) {
	// 上一个Part还没有消费完
	if m.curPart != nil {
		// 将当前的Part关闭掉，即消费掉当前part数据，好让body的读取指针指向下一个part
		if err = m.curPart.Close(); err != nil {
			return
		}
		// 消费掉最后的"\r\n"
		//if err = m.discardCRLF(); err != nil {
		//	return
		//}
	}

	// 下一行就应该是boundary分割
	line, err := m.readLine()
	if err != nil {
		return
	}

	// 到multipart报文的结尾了，直接返回
	if bytes.Equal(line, m.dashBoundaryDash) {
		return nil, io.EOF
	}

	// 如果不是分割符 说明报文格式错误
	if !bytes.Equal(line, m.dashBoundary) {
		err = fmt.Errorf("want delimiter %s, but got %s", m.dashBoundary, line)
		return
	}

	p = &Part{}
	p.mr = m
	// 前文讲到要将part的首部信息预解析，好让part指向消息主体
	if err = p.readHeader(); err != nil {
		return
	}
	m.curPart = p
	return
}

// 消费掉\r\n
func (mr *MultipartReader) discardCRLF() (err error) {
	if _, err = io.ReadFull(mr.bufr, mr.crlf[:]); err == nil {
		if mr.crlf[0] != '\r' && mr.crlf[1] != '\n' {
			err = fmt.Errorf("expect crlf, but got %s", mr.crlf)
		}
	}
	return
}

func (m *MultipartReader) readLine() ([]byte, error) {
	return readLine(m.bufr)
}

func (m *MultipartReader) ReadForm() (mf *MultipartForm, err error) {
	mf = &MultipartForm{
		Value: make(map[string]string),
		File:  make(map[string]*FileHeader),
	}
	//非文件部分在内存中存取的最大量10MB,超出返回错误
	var nonFileMaxMemory int64 = 10 << 20
	//文件在内存中存取的最大量30MB,超出部分存储到硬盘
	var fileMaxMemory int64 = 30 << 20

	var part *Part

	for {
		part, err = m.NextPart()
		// 如果读取到结尾了 就推出循环
		if err == io.EOF {
			break
		}
		// 出现其他错误就返回
		if err != nil {
			return
		}
		//读取的表单名为空 并且文件名也为空
		if part.FormName() == "" && part.FileName() == "" {
			continue
		}
		//缓冲区
		var buff bytes.Buffer
		var n int64
		//普通表单项处理
		if part.FileName() == "" {
			//copy的字节数未nonFileMaxMemory+1，好判断是否超过了内存大小限制
			//如果err==io.EOF，则代表文本数据大小<nonFileMaxMemory+1，并未超过最大限制
			n, err = io.CopyN(&buff, part, nonFileMaxMemory+1)
			if err != nil && err != io.EOF {
				return
			}
			nonFileMaxMemory -= n
			if nonFileMaxMemory < 0 {
				return nil, errors.New("multipart: message too large")
			}
			mf.Value[part.FormName()] = buff.String()
			continue
		}
		//文件表单项处理
		n, err = io.CopyN(&buff, part, fileMaxMemory+1)
		if err != nil && err != io.EOF {
			return
		}
		fh := &FileHeader{
			Filename: part.FileName(),
			Header:   part.Header,
		}
		//未达到了内存限制
		if fileMaxMemory >= n {
			fileMaxMemory -= n
			fh.Size = int(n)
			fh.content = buff.Bytes()
			mf.File[part.FormName()] = fh
			continue
		}
		//达到了内存的限制，要将文件存储磁盘当中
		var file *os.File
		file, err = os.CreateTemp("", "multipart-")
		if err != nil {
			return
		}
		//将缓冲区中的内容以及part剩余的部分写入到文件中
		n, err = io.Copy(file, io.MultiReader(&buff, part))
		if err1 := file.Close(); err1 != nil {
			err = err1
		}
		if err != nil {
			os.Remove(file.Name())
			return
		}
		fh.Size = int(n)
		fh.tmpFile = file.Name()

		mf_, ok := mf.File[part.FormName()]
		if ok {
			os.Remove(mf_.tmpFile)
		}
		mf.File[part.FormName()] = fh
	}
	return mf, nil
}

type MultipartForm struct {
	Value map[string]string
	File  map[string]*FileHeader
}

// 删除暂时存储在磁盘上的文件
func (mf *MultipartForm) RemoveAll() {
	for _, fh := range mf.File {
		if fh == nil || fh.tmpFile == "" {
			continue
		}
		os.Remove(fh.tmpFile)
	}
}

type FileHeader struct {
	Filename string
	Header   Header
	Size     int
	//如果当前缓存量未超过这个值，我们将这些数据存到content这个字节切片里去。
	content []byte
	//如果超过这个最大值，我们则将客户端上传文件的数据暂时存储到硬盘中去，待用户需要时再读取出来。tmpFile是这个暂时文件的路径。
	tmpFile string
}

// 读取对应的文件
func (fh *FileHeader) Open() (io.ReadCloser, error) {
	if fh.inDisk() {
		return os.Open(fh.tmpFile)
	}
	b := bytes.NewReader(fh.content)
	return ioutil.NopCloser(b), nil
}

// 判断文件是否存在在磁盘上
func (fh *FileHeader) inDisk() bool {
	return fh.tmpFile != ""
}

// 将文件保存到指定的位置去
func (fh *FileHeader) Save(dest string) (err error) {
	rc, err := fh.Open()
	if err != nil {
		return
	}
	defer rc.Close()
	file, err := os.Create(dest)
	if err != nil {
		return
	}
	defer file.Close()
	_, err = io.Copy(file, rc)
	if err != nil {
		os.Remove(dest)
	}
	return
}
