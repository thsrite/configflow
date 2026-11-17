// Package yamlx reads and writes schema-free YAML documents on yaml.v3.
// Parse, Decode, Encode and Marshal use Node trees and native typed values;
// FromJSON and ToJSON bridge ordered dynamic JSON documents. The package does
// no I/O and knows no application document schema.
//
// All writing uses Marshal: two-space indentation, LF line endings, no wrapping.
// Callers build Nodes to select flow styles or retain anchors and annotations.
// Dynamic readers validate duplicate keys, tag/shape policies and alias budgets
// before navigating user documents. Edit verifies every edited output before it
// can replace its source.
//
// During the remaining N7-3d migration, the old Load APIs still build legacy
// jsonx values with Python loader semantics. Their implementation and old-only
// types are removed when the final reader consumers migrate. No old writer API
// remains; generation boundaries share the temporary internal/yamlbridge.
package yamlx
