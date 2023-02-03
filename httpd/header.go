package httpd

type Header map[string][]string

func (h Header) Add(key, value string) {
	h[key] = append(h[key], value)
}

// 插入键值对
func (h Header) Set(key, value string) {
	h[key] = []string{value}
}

// 获取值，key不存在则返回空
func (h Header) Get(key string) string {
	if value, ok := h[key]; ok && len(value) > 0 {
		return value[0]
	} else {
		return ""
	}
}

// 删除键
func (h Header) Del(key string) {
	delete(h, key)
}
