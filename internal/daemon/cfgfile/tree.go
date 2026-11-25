package cfgfile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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
		switch n.Kind {
		case yaml.MappingNode:
			for i := 1; i < len(n.Content); i += 2 {
				setPath(n.Content[i], rest, s)
			}
		case yaml.SequenceNode:
			for _, c := range n.Content {
				setPath(c, rest, s)
			}
		}
		return
	}
	var child *yaml.Node
	added := false
	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value == seg {
				child = n.Content[i+1]
			}
		}
		if child == nil {
			if s.IfValue != "" {
				return // only existing values are changed
			}
			child = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			if len(rest) == 0 {
				child = &yaml.Node{Kind: yaml.ScalarNode}
			}
			n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: seg}, child)
			added = true
		}
	case yaml.SequenceNode:
		i, err := strconv.Atoi(seg)
		if err != nil || i < 0 || i >= len(n.Content) {
			return
		}
		child = n.Content[i]
	default:
		return
	}
	if len(rest) > 0 {
		setPath(child, rest, s)
		return
	}
	cur, found := scalarText(child)
	if v, ok := change(s, cur, found && !added); ok {
		setScalar(child, v)
	}
}

// parseTree reads YAML or JSON; an empty file is an empty mapping.
func parseTree(data []byte) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if doc.Kind == 0 || len(doc.Content) == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	}
	return &doc, nil
}

func editTree(data []byte, sets []proto.ConfigSet) (*yaml.Node, error) {
	doc, err := parseTree(data)
	if err != nil {
		return nil, err
	}
	for _, s := range sets {
		setPath(doc.Content[0], path(s.Key), s)
	}
	return doc, nil
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
	doc, err := editTree(data, sets)
	if err != nil {
		return nil, err
	}
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
	doc, err := editTree(data, sets)
	if err != nil {
		return nil, err
	}
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
