package main

// Маленькое дерево XML поверх encoding/xml (для чтения docx/odt).

import (
	"bytes"
	"encoding/xml"
	"io"
	"strings"
)

type Node struct {
	NS, Name string
	Attr     []xml.Attr
	Kids     []*Node
	Text     string // для текстовых узлов Name == ""
	Parent   *Node
}

func ParseXML(data []byte) (*Node, error) {
	d := xml.NewDecoder(bytes.NewReader(data))
	d.Strict = false
	d.CharsetReader = func(label string, in io.Reader) (io.Reader, error) { return in, nil }
	root := &Node{Name: "#root"}
	cur := root
	for {
		t, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return root, err
		}
		switch v := t.(type) {
		case xml.StartElement:
			n := &Node{NS: v.Name.Space, Name: v.Name.Local, Attr: append([]xml.Attr{}, v.Attr...), Parent: cur}
			cur.Kids = append(cur.Kids, n)
			cur = n
		case xml.EndElement:
			if cur.Parent != nil {
				cur = cur.Parent
			}
		case xml.CharData:
			cur.Kids = append(cur.Kids, &Node{Text: string(v), Parent: cur})
		}
	}
	return root, nil
}

// Root — первый элемент документа.
func (n *Node) Root() *Node {
	for _, k := range n.Kids {
		if k.Name != "" {
			return k
		}
	}
	return nil
}

func (n *Node) Elems() []*Node {
	if n == nil {
		return nil
	}
	var out []*Node
	for _, k := range n.Kids {
		if k.Name != "" {
			out = append(out, k)
		}
	}
	return out
}

func (n *Node) Kid(name string) *Node {
	if n == nil {
		return nil
	}
	for _, k := range n.Kids {
		if k.Name == name {
			return k
		}
	}
	return nil
}

func (n *Node) KidsNamed(name string) []*Node {
	if n == nil {
		return nil
	}
	var out []*Node
	for _, k := range n.Kids {
		if k.Name == name {
			out = append(out, k)
		}
	}
	return out
}

func (n *Node) Find(name string) []*Node {
	var out []*Node
	var walk func(*Node)
	walk = func(x *Node) {
		for _, k := range x.Kids {
			if k.Name == name {
				out = append(out, k)
			}
			walk(k)
		}
	}
	if n != nil {
		walk(n)
	}
	return out
}

func (n *Node) First(name string) *Node {
	if r := n.Find(name); len(r) > 0 {
		return r[0]
	}
	return nil
}

// A — атрибут по локальному имени (любое пространство имён). ok=false, если нет.
func (n *Node) A(name string) (string, bool) {
	if n == nil {
		return "", false
	}
	for _, a := range n.Attr {
		if a.Name.Local == name {
			return a.Value, true
		}
	}
	return "", false
}

func (n *Node) Av(name string) string { v, _ := n.A(name); return v }

// AttrNS — атрибут с учётом пространства имён (нужно, когда локальные имена совпадают).
func (n *Node) AttrNS(ns, name string) string {
	if n == nil {
		return ""
	}
	for _, a := range n.Attr {
		if a.Name.Local == name && a.Name.Space == ns {
			return a.Value
		}
	}
	return ""
}

func (n *Node) TextContent() string {
	if n == nil {
		return ""
	}
	if n.Name == "" {
		return n.Text
	}
	var sb strings.Builder
	for _, k := range n.Kids {
		sb.WriteString(k.TextContent())
	}
	return sb.String()
}

func xesc(s string) string {
	var sb strings.Builder
	for _, r := range s {
		switch {
		case r == '&':
			sb.WriteString("&amp;")
		case r == '<':
			sb.WriteString("&lt;")
		case r == '>':
			sb.WriteString("&gt;")
		case r == '"':
			sb.WriteString("&quot;")
		case r < 0x20 && r != '\t' && r != '\n' && r != '\r':
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}
