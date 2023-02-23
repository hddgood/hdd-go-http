package httpd

import (
	"fmt"
	"strings"
)

type MethodTree struct {
	method string //对应方法的前缀树 GET POST
	root   *Node
}

func (t *MethodTree) getHandler(r *Request) (HandlerFunc, bool) {
	parts := strings.Split(r.URL.Path[1:], "/")

	cur := t.root
	for i, part := range parts {
		var tmp *Node
		for _, node := range cur.children {
			// 如果是*通配符 就直接返回
			if node.isCatchAll {
				sb := strings.Builder{}
				for _, p := range parts[i:] {
					sb.WriteString("/")
					sb.WriteString(p)
				}
				r.queryString[node.path[1:]] = sb.String()
				return node.handler, true
			}

			if node.path == part {
				tmp = node
				break
			}

			if node.wildChild {
				tmp = node
			}
		}
		cur = tmp
		if tmp.wildChild {
			r.queryString[tmp.path[1:]] = part
		}

		if cur == nil {
			return nil, false
		}
	}

	return cur.handler, true
}

type Node struct {
	path       string      // 当前节点的路径
	wildChild  bool        //当前节点是否为参数节点
	handler    HandlerFunc //处理当前节点的函数
	children   []*Node     //孩子节点
	nType      nodeType    //当前节点类型
	isCatchAll bool        //是否为*节点
	fullPath   string      //当前的全部路径
}

type nodeType uint8

const (
	static nodeType = iota
	root
	param
	catchAll
)

func newRoot() *Node {
	return &Node{
		path:     "/",
		nType:    root,
		children: make([]*Node, 0),
		fullPath: "/",
	}
}

// 添加路由
func (n *Node) addPath(path string, handler HandlerFunc) {
	parts := strings.Split(path, "/")

	cur := n
	for i, part := range parts {
		//assert1(len(cur.children) > 0 && part[0] == '*', fmt.Sprintf("catch-all conflicts with existing handle for the path segment root in path /%s", path))
		//assert1(len(part) > 1 && part[0] == ':', fmt.Sprintf("wildcards must be named with a non-empty name in path /%s", path))
		//assert1(len(part) > 1 && part[0] == '*', fmt.Sprintf("wildcards must be named with a non-empty name in path /%s", path))

		if len(cur.children) > 0 && part[0] == '*' {
			panic(fmt.Sprintf("catch-all conflicts with existing handle for the path segment root in path /%s", path))
		}

		if len(part) == 1 && part[0] == ':' {
			panic(fmt.Sprintf("wildcards must be named with a non-empty name in path /%s", path))
		}

		if len(part) == 1 && part[0] == '*' {
			panic(fmt.Sprintf("wildcards must be named with a non-empty name in path /%s", path))
		}

		if part == "" {
			continue
		}

		if node, ok := matchPart(cur.children, part); ok {
			cur = node
		} else {
			insertNode(cur, parts[i:], handler)
			return
		}
	}

	panic(fmt.Sprintf("handlers are already registered for path /%s", path))
}

func insertNode(cur *Node, parts []string, handler HandlerFunc) {
	for i, part := range parts {
		newNode := new(Node)
		newNode.nType = static
		if part[0] == ':' {
			newNode.wildChild = true
			newNode.nType = param
		} else if part[0] == '*' {

			newNode.isCatchAll = true
			newNode.nType = catchAll
			newNode.fullPath = cur.fullPath + part
			newNode.path = part
			newNode.handler = handler
			cur.children = append(cur.children, newNode)
			if i != len(part)-1 {
				panic(fmt.Sprintf("catch-all routes are only allowed at the end of the path in path '%s'", newNode.fullPath))
			}
			return
		}

		newNode.path = part
		newNode.children = []*Node{}
		newNode.fullPath = cur.fullPath + part + "/"
		cur.children = append(cur.children, newNode)
		cur = newNode
	}
	cur.fullPath = strings.TrimRight(cur.fullPath, "/")
	cur.handler = handler
}

// 匹配路由树中的路径
// 不能同时有两个路径变量
// 不能有catchAll类型还有其他类型的
func matchPart(children []*Node, part string) (*Node, bool) {
	var n *Node
	wildChild := part[0] == ':'
	for _, node := range children {
		if node.isCatchAll {
			panic("catch-all conflicts with existing handle for the path segment root in path " + node.fullPath)
		}
		if wildChild && node.wildChild {
			panic("catch-all conflicts with existing handle for the path segment root in path " + node.fullPath)
		}

		if node.path == part {
			n = node
			return n, true
		}
	}

	return n, false
}