package oxidns

import (
	"bytes"
	"encoding/json"

	"configflow/internal/yamlx"
)

// NativeDraft owns the prepared old/new document pair. Preparation precedes
// decoding the optional edit intents, preserving the public validation order.
type NativeDraft struct{ edit *nativeEdit }

// PrepareNativeDraft validates the old and candidate documents without saving.
func PrepareNativeDraft(settings *Node, text string, p *Profile, baseURL string) (*NativeDraft, error) {
	edit, err := prepareNativeEdit(settings, text, p, baseURL)
	if err != nil {
		return nil, err
	}
	return &NativeDraft{edit: edit}, nil
}

// Apply consumes a prepared candidate using typed rename/standard-entry intent.
func (d *NativeDraft) Apply(options NativeApplyOptions) (NativeApplied, error) {
	if d == nil || d.edit == nil {
		return NativeApplied{}, errMalformed
	}
	return d.edit.apply(d.edit.input, options)
}

// DecodeNativeOptions decodes the JSON edit intents into an ordered command.
// Missing/null values are absent. JSON object ordering and duplicate-name
// handling follow the shared native FromJSON boundary.
func DecodeNativeOptions(renames, standard json.RawMessage) (NativeApplyOptions, error) {
	options := NativeApplyOptions{}
	if len(renames) > 0 && !bytes.Equal(bytes.TrimSpace(renames), []byte("null")) {
		value, err := yamlx.FromJSON(renames)
		if err != nil || !isMap(value) {
			return options, fail(messageNativeRenamesShape)
		}
		valid := true
		pairs(value, func(from string, to *Node) bool {
			if !isStr(to) {
				valid = false
				return false
			}
			options.Renames = append(options.Renames, NativeRename{From: from, To: to.Value})
			return true
		})
		if !valid {
			return NativeApplyOptions{}, fail(messageNativeRenamesShape)
		}
	}
	if len(standard) > 0 && !bytes.Equal(bytes.TrimSpace(standard), []byte("null")) {
		value, err := yamlx.FromJSON(standard)
		if err != nil || !isSeq(value) {
			return NativeApplyOptions{}, fail(messageNativeStandardList)
		}
		for _, tag := range value.Content {
			if !isStr(tag) {
				return NativeApplyOptions{}, fail(messageNativeStandardList)
			}
			options.StandardAdd = append(options.StandardAdd, tag.Value)
		}
	}
	return options, nil
}
