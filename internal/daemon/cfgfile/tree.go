package cfgfile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// YAML and JSON are both edited as a yaml.Node tree, which keeps key order
// (and, for YAML, comments).

// path splits "listeners[0].host" into listeners, 0, host. A * segment
// stands for every child of the node before it.
func path(key string) []string {
	key = strings.NewReplacer("[", ".", "]", "").Replace(key)
	var out []string
	for _, p := range strings.Split(key, ".") {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// typed is a value as Wings stores it in YAML and JSON: a whole number or a
// boolean becomes one, anything else stays a string.
func typed(v string) (tag, value string) {
	if _, err := strconv.Atoi(v); err == nil {
		return "!!int", v
	}
	if b, err := strconv.ParseBool(v); err == nil {
		return "!!bool", strconv.FormatBool(b)
	}
	return "!!str", v
}

func setScalar(n *yaml.Node, v string) {
	tag, val := typed(v)
	if n.Kind != yaml.ScalarNode || tag != "!!str" || n.Tag != "!!str" {
		n.Style = 0 // a quoted string keeps its quotes, anything else is written plainly
	}
	n.Kind, n.Tag, n.Value, n.Content = yaml.ScalarNode, tag, val, nil
}

// scalarText is a node's value as the if_value test compares it.
func scalarText(n *yaml.Node) (string, bool) {
	if n.Kind != yaml.ScalarNode {
		return "", false
	}
	return n.Value, true
}

// setPath applies s at the path below n.
func setPath(n *yaml.Node, segs []string, s proto.ConfigSet) {
	if len(segs) == 0 {
		return
	}
	seg, rest := segs[0], segs[1:]
	if seg == "*" {
		var kids []*yaml.Node
		switch n.Kind {
		case yaml.MappingNode:
			for i := 1; i < len(n.Content); i += 2 {
				kids = append(kids, n.Content[i])
			}
		case yaml.SequenceNode:
			kids = n.Content
		}
		for _, c := range kids {
			if len(rest) == 0 {
				setLeaf(c, s, true)
			} else {
				setPath(c, rest, s)
			}
		}
		return
	}
	child, added := childAt(n, seg, rest, s)
	if child == nil {
		return
	}
	if len(rest) > 0 {
		setPath(child, rest, s)
		return
	}
	setLeaf(child, s, !added)
}

// setLeaf applies s to a value; existed is false for one just added.
func setLeaf(n *yaml.Node, s proto.ConfigSet, existed bool) {
	cur, found := scalarText(n)
	if v, ok := change(s, cur, found && existed); ok {
		setScalar(n, v)
	}
}

func isIndex(seg string) bool {
	i, err := strconv.Atoi(seg)
	return err == nil && i >= 0
}

// newNode is an empty node for a missing key: a list when the path goes on
// with an index ("listeners[0]"), a mapping when it goes on with a key, or a
// value at the end of the path.
func newNode(rest []string) *yaml.Node {
	switch {
	case len(rest) == 0:
		return &yaml.Node{Kind: yaml.ScalarNode}
	case isIndex(rest[0]):
		return &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	}
	return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
}

// childAt finds (or, for an unconditional set, adds) the child seg of n.
// A list gets a new element only right after its last one.
func childAt(n *yaml.Node, seg string, rest []string, s proto.ConfigSet) (*yaml.Node, bool) {
	switch n.Kind {
	case yaml.MappingNode:
		var child *yaml.Node
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value == seg {
				child = n.Content[i+1]
			}
		}
		if child != nil {
			return child, false
		}
		if s.IfValue != nil {
			return nil, false // only existing values are changed
		}
		child = newNode(rest)
		n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: seg}, child)
		return child, true
	case yaml.SequenceNode:
		if !isIndex(seg) {
			return nil, false
		}
		i, _ := strconv.Atoi(seg)
		if i < len(n.Content) {
			return n.Content[i], false
		}
		if i > len(n.Content) || s.IfValue != nil {
			return nil, false
		}
		child := newNode(rest)
		n.Content = append(n.Content, child)
		return child, true
	}
	return nil, false
}

// emptyDoc is a document holding an empty mapping: a new or empty file.
func emptyDoc() *yaml.Node {
	return &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
}

// parseYAML reads a YAML file of one document. A file with several is
// refused: writing back only the first would delete the rest.
func parseYAML(data []byte) (*yaml.Node, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return emptyDoc(), nil
		}
		return nil, err
	}
	var more yaml.Node
	if err := dec.Decode(&more); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, err
		}
		return nil, errors.New("the file has more than one YAML document, which cannot be edited")
	}
	if len(doc.Content) == 0 {
		return emptyDoc(), nil
	}
	return &doc, nil
}

// parseJSON reads JSON with Go's JSON decoder (YAML's rejects some valid
// JSON, such as \uD83D\uDE00 escapes) into a node tree in key order.
func parseJSON(data []byte) (*yaml.Node, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return emptyDoc(), nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	root, err := jsonNode(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("the file has more than one JSON value")
	}
	return &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}}, nil
}

func jsonNode(dec *json.Decoder) (*yaml.Node, error) {
	t, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch v := t.(type) {
	case json.Delim:
		n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		if v == '[' {
			n = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		}
		for dec.More() {
			if n.Kind == yaml.MappingNode {
				k, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, _ := k.(string)
				n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key})
			}
			c, err := jsonNode(dec)
			if err != nil {
				return nil, err
			}
			n.Content = append(n.Content, c)
		}
		if _, err := dec.Token(); err != nil { // the closing } or ]
			return nil, err
		}
		return n, nil
	case string:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}, nil
	case json.Number:
		tag := "!!float"
		if _, err := v.Int64(); err == nil {
			tag = "!!int"
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: v.String()}, nil
	case bool:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(v)}, nil
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, nil
}

func editTree(doc *yaml.Node, sets []proto.ConfigSet) {
	for _, s := range sets {
		setPath(doc.Content[0], path(s.Key), s)
	}
}

// indent guesses the indentation a file uses, from its first indented line.
func indent(data []byte, def int) int {
	for _, l := range bytes.Split(data, []byte("\n")) {
		t := bytes.TrimLeft(l, " ")
		if n := len(l) - len(t); n > 0 && len(t) > 0 && t[0] != '#' {
			return min(n, 8)
		}
	}
	return def
}

func editYAML(data []byte, sets []proto.ConfigSet) ([]byte, error) {
	doc, err := parseYAML(data)
	if err != nil {
		return nil, err
	}
	editTree(doc, sets)
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(indent(data, 2))
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func editJSON(data []byte, sets []proto.ConfigSet) ([]byte, error) {
	doc, err := parseJSON(data)
	if err != nil {
		return nil, err
	}
	editTree(doc, sets)
	var b bytes.Buffer
	if err := writeJSON(&b, doc.Content[0], strings.Repeat(" ", indent(data, 4)), ""); err != nil {
		return nil, err
	}
	b.WriteByte('\n')
	return b.Bytes(), nil
}

// writeJSON writes a node parsed from JSON (or added by setPath) back as
// JSON, in its original key order.
func writeJSON(b *bytes.Buffer, n *yaml.Node, step, pad string) error {
	switch n.Kind {
	case yaml.MappingNode, yaml.SequenceNode:
		start, end, step2 := "{", "}", 2
		if n.Kind == yaml.SequenceNode {
			start, end, step2 = "[", "]", 1
		}
		if len(n.Content) == 0 {
			b.WriteString(start + end)
			return nil
		}
		b.WriteString(start + "\n")
		for i := 0; i < len(n.Content); i += step2 {
			b.WriteString(pad + step)
			if step2 == 2 {
				if err := writeString(b, n.Content[i].Value); err != nil {
					return err
				}
				b.WriteString(": ")
			}
			if err := writeJSON(b, n.Content[i+step2-1], step, pad+step); err != nil {
				return err
			}
			if i+step2 < len(n.Content) {
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
		b.WriteString(pad + end)
		return nil
	case yaml.ScalarNode:
		switch n.ShortTag() {
		case "!!int", "!!float":
			if !json.Valid([]byte(n.Value)) {
				return writeString(b, n.Value)
			}
			b.WriteString(n.Value)
		case "!!bool":
			b.WriteString(n.Value)
		case "!!null":
			b.WriteString("null")
		default:
			return writeString(b, n.Value)
		}
		return nil
	case yaml.AliasNode:
		return errors.New("YAML aliases cannot be written as JSON")
	}
	return fmt.Errorf("unexpected node kind %d", n.Kind)
}

func writeString(b *bytes.Buffer, s string) error {
	enc := json.NewEncoder(b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return err
	}
	b.Truncate(b.Len() - 1) // Encode ends with a newline
	return nil
}
