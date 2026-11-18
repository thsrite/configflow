package yamlx

import (
	"bytes"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
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
	before.comments = sourceEditComments(src, doc, before.comments)
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
		// The planner proves original comment bytes survive. yaml.v3 may no
		// longer attach orphan notes to a Node, so verify only meaning here.
		// A layout the planner did not foresee falls back to the verified
		// whole-document path (D3) instead of refusing a valid edit.
		if err := verifyEdit(out, doc, nil); err == nil {
			return out, nil
		}
	}
	return rewriteEdit(src, doc, before)
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

func rewriteEdit(src []byte, doc *Node, before editSnapshot) ([]byte, error) {
	comments := before.comments
	if err := validateNode(doc); err != nil {
		return src, fmt.Errorf("%w: validate edited document: %w", ErrUnsafeEdit, err)
	}
	// Verification compares with the document the caller produced, not with
	// the tree after the preparation steps below rearranged it.
	want := copyGraph(doc, map[*Node]*Node{})
	MaterializeAliases(doc)
	placeRemovedComments(doc, before)
	keepEditComments(doc, comments)
	out, err := Marshal(doc)
	if err != nil {
		return src, fmt.Errorf("%w: encode edited document: %w", ErrUnsafeEdit, err)
	}
	// yaml.v3 may attach an end-of-document comment to a block container's
	// LineComment, a position its emitter does not write. Check the emitted
	// tree and relocate only missing notes before the final verification.
	parsed, err := Parse(out)
	if err != nil || !Equal(parsed, want) {
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
	if err := verifyEdit(out, want, comments); err != nil {
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

// MaterializeAliases repairs anchors displaced by edits in a validated graph,
// in place. The first remaining consumer becomes the definition; later consumers
// stay aliases. Anchor names are made unique when an edit copied a definition.
// Nothing is expanded, preserving alias chains and their budget (N7-2 F1).
func MaterializeAliases(doc *Node) {
	emitted := map[*Node]bool{}
	definitions := map[*Node]*Node{}
	names := map[*Node]string{}
	used := map[string]bool{}
	name := func(n *Node) string {
		if s := names[n]; s != "" {
			return s
		}
		s := n.Anchor
		if s == "" {
			s = "value"
		}
		base := s
		for i := 1; used[s]; i++ {
			s = fmt.Sprintf("%s_%d", base, i)
		}
		used[s] = true
		names[n] = s
		return s
	}
	var walk func(*Node)
	walk = func(n *Node) {
		if n == nil || emitted[n] {
			return
		}
		if n.Kind == yaml.AliasNode && n.Alias != nil {
			target := n.Alias
			anchor := name(target)
			if emitted[target] {
				n.Value = anchor
				n.Alias = definitions[target]
				return
			}
			definition := *target
			definition.Anchor = anchor
			definition.HeadComment = joinComment(n.HeadComment, target.HeadComment)
			definition.LineComment = joinComment(n.LineComment, target.LineComment)
			definition.FootComment = joinComment(n.FootComment, target.FootComment)
			if definition.Kind != yaml.ScalarNode && definition.Style&yaml.FlowStyle == 0 {
				// yaml.v3 does not write a block collection's line comment.
				definition.HeadComment = joinComment(definition.HeadComment, definition.LineComment)
				definition.LineComment = ""
			}
			*n = definition
			emitted[target] = true
			definitions[target] = n
			names[n] = anchor
		} else if n.Anchor != "" {
			n.Anchor = name(n)
		}
		emitted[n] = true
		definitions[n] = n
		for _, child := range n.Content {
			walk(child)
		}
	}
	walk(doc)
}

// placeRemovedComments keeps the notes of removed pairs next to the mapping
// they left: before a sequence item (where the Python editor put them), or
// before the following key of any other mapping. Without a following key they
// become the mapping's own head comment.
func placeRemovedComments(doc *Node, before editSnapshot) {
	live := map[*Node]bool{}
	walkEditNodes(doc, func(n *Node) { live[n] = true })
	seen := map[*Node]bool{}
	var walk func(n *Node, item bool)
	walk = func(n *Node, item bool) {
		if n == nil || seen[n] {
			return
		}
		seen[n] = true
		if old, ok := before.nodes[n]; ok && n.Kind == yaml.MappingNode && old.Kind == yaml.MappingNode {
			var notes []string
			var next *Node
			for i := 0; i+1 < len(old.Content); i += 2 {
				if live[old.Content[i]] {
					if len(notes) > 0 && next == nil {
						next = old.Content[i]
					}
					continue
				}
				for _, root := range old.Content[i : i+2] {
					walkEditNodes(root, func(r *Node) {
						if !live[r] {
							notes = append(notes, nodeCommentLines(r)...)
						}
					})
				}
			}
			if text := strings.Join(notes, "\n"); text != "" {
				switch {
				case next != nil && !item:
					next.HeadComment = joinComment(text, next.HeadComment)
				default:
					n.HeadComment = joinComment(n.HeadComment, text)
				}
			}
		}
		for _, child := range n.Content {
			walk(child, n.Kind == yaml.SequenceNode)
		}
	}
	walk(doc, false)
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
