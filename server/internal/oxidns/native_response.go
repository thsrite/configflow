package oxidns

import "encoding/json"

// MarshalJSON emits the public reference location shape, including null tags
// for dynamic/include references and array paths for exact form-field jumps.
func (r Reference) MarshalJSON() ([]byte, error) {
	var tag *string
	if r.Tag != "" || r.tagSet {
		value := r.Tag
		tag = &value
	}
	return json.Marshal(struct {
		Path   Path    `json:"path"`
		Tag    *string `json:"tag"`
		Reason string  `json:"reason"`
	}{r.Path, tag, r.Reason})
}
