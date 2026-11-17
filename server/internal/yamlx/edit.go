package yamlx

import (
	"bytes"
	"fmt"
	"gopkg.in/yaml.v3"
	"strings"
)

// Edit applies a change to a parsed YAML tree, preserving comments and verifying
// the result through Parse and Equal. On any error it returns the original bytes.
// Edits that cannot retain the original layout are encoded as a whole document.
func Edit(src []byte, edit func(doc *Node) error) ([]byte, error) {
	doc, err := Parse(src)
	if err != nil {
		return src, err
	}
	before := captureEdit(doc)
	if doc.Line == 0 { // yaml.v3 reports comment-only input as an empty stream.
		for _, line := range strings.Split(string(src), "\n") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "\ufeff"))
			if strings.HasPrefix(line, "#") {
				before.comments = append(before.comments, line)
			}
		}
	}
	if err := edit(doc); err != nil {
		return src, err
	}
	if before.unchanged(doc, map[*Node]bool{}) {
		return src, nil
	}
	if err := validateNode(doc); err != nil {
		return src, fmt.Errorf("%w: validate edited document: %w", ErrUnsafeEdit, err)
	}
	if out, ok := spliceEdit(src, doc, before); ok {
		if err := verifyEdit(out, doc, before.comments); err != nil {
			return src, err
		}
		return out, nil
	}
	return rewriteEdit(src, doc, before.comments)
}

type editSnapshot struct {
	nodes    map[*Node]Node
	comments []string
}

func captureEdit(root *Node) editSnapshot {
	state := editSnapshot{nodes: make(map[*Node]Node)}
	walkEditNodes(root, func(n *Node) {
		value := *n
		value.Content = append([]*Node(nil), n.Content...)
		state.nodes[n] = value
		state.comments = append(state.comments, nodeCommentLines(n)...)
	})
	return state
}

func walkEditNodes(n *Node, visit func(*Node)) {
	seen := map[*Node]bool{}
	var walk func(*Node)
	walk = func(n *Node) {
		if n == nil || seen[n] {
			return
		}
		seen[n] = true
		visit(n)
		for _, child := range n.Content {
			walk(child)
		}
	}
	walk(n)
}

func (s editSnapshot) unchanged(n *Node, seen map[*Node]bool) bool {
	if n == nil {
		return false
	}
	if seen[n] {
		return true
	}
	seen[n] = true
	old, ok := s.nodes[n]
	if !ok || !sameEditMetadata(&old, n) || old.Value != n.Value || old.Tag != n.Tag || len(old.Content) != len(n.Content) {
		return false
	}
	for i, child := range n.Content {
		if old.Content[i] != child || !s.unchanged(child, seen) {
			return false
		}
	}
	return true
}

func sameEditMetadata(a, b *Node) bool {
	return a.Kind == b.Kind && a.Style == b.Style && a.Anchor == b.Anchor && a.Alias == b.Alias &&
		a.HeadComment == b.HeadComment && a.LineComment == b.LineComment && a.FootComment == b.FootComment &&
		a.Line == b.Line && a.Column == b.Column
}

func nodeCommentLines(n *Node) []string {
	var comments []string
	for _, text := range []string{n.HeadComment, n.LineComment, n.FootComment} {
		for _, line := range strings.Split(text, "\n") {
			if line = strings.TrimSpace(line); line != "" {
				comments = append(comments, line)
			}
		}
	}
	return comments
}

// keepEditComments retains notes orphaned by removed pairs or replaced trees.
// Their placement can change on the whole-document path; their text survives.
func keepEditComments(doc *Node, comments []string) {
	counts := map[string]int{}
	walkEditNodes(doc, func(n *Node) {
		for _, line := range nodeCommentLines(n) {
			counts[line]++
		}
	})
	var missing []string
	for _, line := range comments {
		if counts[line] > 0 {
			counts[line]--
		} else {
			missing = append(missing, line)
		}
	}
	if len(missing) > 0 {
		doc.HeadComment = joinComment(strings.Join(missing, "\n"), doc.HeadComment)
	}
}

func rewriteEdit(src []byte, doc *Node, comments []string) ([]byte, error) {
	if err := validateNode(doc); err != nil {
		return src, fmt.Errorf("%w: validate edited document: %w", ErrUnsafeEdit, err)
	}
	materializeEditAliases(doc)
	keepEditComments(doc, comments)
	out, err := Marshal(doc)
	if err != nil {
		return src, fmt.Errorf("%w: encode edited document: %w", ErrUnsafeEdit, err)
	}
	// yaml.v3 may attach an end-of-document comment to a block container's
	// LineComment, a position its emitter does not write. Check the emitted
	// tree and relocate only missing notes before the final verification.
	parsed, err := Parse(out)
	if err != nil || !Equal(parsed, doc) {
		if err != nil {
			return src, fmt.Errorf("%w: parse edited document: %w", ErrUnsafeEdit, err)
		}
		return src, fmt.Errorf("%w: edited document changed meaning", ErrUnsafeEdit)
	}
	if missing := missingEditComments(parsed, comments); len(missing) > 0 {
		doc.HeadComment = joinComment(strings.Join(missing, "\n"), doc.HeadComment)
		out, err = Marshal(doc)
		if err != nil {
			return src, fmt.Errorf("%w: encode retained comments: %w", ErrUnsafeEdit, err)
		}
	}
	if bytes.Contains(src, []byte("\r\n")) && !bytes.ContainsAny(bytes.ReplaceAll(src, []byte("\r\n"), nil), "\r\n") {
		out = bytes.ReplaceAll(out, []byte("\n"), []byte("\r\n"))
	}
	if err := verifyEdit(out, doc, comments); err != nil {
		return src, err
	}
	return out, nil
}

func verifyEdit(out []byte, want *Node, comments []string) error {
	parsed, err := Parse(out)
	if err != nil {
		return fmt.Errorf("%w: parse edited document: %w", ErrUnsafeEdit, err)
	}
	if !Equal(parsed, want) {
		return fmt.Errorf("%w: edited document changed meaning", ErrUnsafeEdit)
	}
	if len(missingEditComments(parsed, comments)) > 0 {
		return fmt.Errorf("%w: edited document lost a comment", ErrUnsafeEdit)
	}
	return nil
}

// A removed/replaced anchor definition may still have alias consumers. Preserve
// their original value before the emitter sees an undefined or forward alias.
func materializeEditAliases(doc *Node) {
	emitted := map[*Node]bool{}
	var walk func(*Node)
	walk = func(n *Node) {
		if n == nil {
			return
		}
		if n.Kind == yaml.AliasNode && !emitted[n.Alias] {
			copy := Clone(n)
			copy.HeadComment = joinComment(n.HeadComment, copy.HeadComment)
			copy.LineComment = joinComment(n.LineComment, copy.LineComment)
			copy.FootComment = joinComment(n.FootComment, copy.FootComment)
			*n = *copy
		}
		if emitted[n] {
			return
		}
		emitted[n] = true
		for _, child := range n.Content {
			walk(child)
		}
	}
	walk(doc)
}

func missingEditComments(doc *Node, comments []string) []string {
	counts := map[string]int{}
	walkEditNodes(doc, func(n *Node) {
		for _, line := range nodeCommentLines(n) {
			counts[line]++
		}
	})
	var missing []string
	for _, line := range comments {
		if counts[line] > 0 {
			counts[line]--
		} else {
			missing = append(missing, line)
		}
	}
	return missing
}
