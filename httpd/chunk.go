package httpd

import (
	"bufio"
	"errors"
	"io"
)

// 处理chunk编码传输的报文
type chunkReader struct {
	//当前正在处理的块中还剩多少字节未读
	n    int
	bufr *bufio.Reader
	//利用done来记录报文主体是否读取完毕
	done bool
	crlf [2]byte //用来读取\r\n
}

// 实现io.Reader 接口
func (c *chunkReader) Read(p []byte) (n int, err error) {

	// 如果读完了就不读了
	if c.done {
		return 0, io.EOF
	}

	// 当前块读完了 读取下一个块
	if c.n == 0 {
		c.n, err = c.getChunkSize()
		if err != nil {
			return 0, err
		}
	}

	// 如果还是0 说明报文已经读取完毕了
	if c.n == 0 {
		c.done = true
		// 再次去除最后的 "\r\n"
		err = c.discardCRLF()
		return
	}

	// 如果当前块剩余的数据大于欲读取的长度
	if len(p) <= c.n {
		n, err = c.bufr.Read(p)
		c.n -= n
		return n, err
	}

	//如果当前块剩余的数据不够欲读取的长度，将剩余的数据全部取出返回
	n, _ = io.ReadFull(c.bufr, p[:c.n])
	c.n = 0

	//记得把每个chunkData后的\r\n消费掉
	if err = c.discardCRLF(); err != nil {
		return
	}
	return
}

// 去除数据中的 "\r\n"
func (c *chunkReader) discardCRLF() (err error) {
	if _, err = io.ReadFull(c.bufr, c.crlf[:]); err == nil {
		if c.crlf[0] != '\r' || c.crlf[1] != '\n' {
			return errors.New("unsupported encoding format of chunk")
		}
	}
	return
}

// 获取下一个chunk块的大小
func (c *chunkReader) getChunkSize() (chunkSize int, err error) {
	line, err := readLine(c.bufr)
	if err != nil {
		return
	}
	//将16进制换算成10进制
	for i := 0; i < len(line); i++ {
		switch {
		case 'a' <= line[i] && line[i] <= 'f':
			chunkSize = chunkSize*16 + int(line[i]-'a') + 10
		case 'A' <= line[i] && line[i] <= 'F':
			chunkSize = chunkSize*16 + int(line[i]-'A') + 10
		case '0' <= line[i] && line[i] <= '9':
			chunkSize = chunkSize*16 + int(line[i]-'0')
		default:
			return 0, errors.New("illegal hex number")
		}
	}
	return
}
