// Package client provides definitions for log clients and search structures.
package client

import "github.com/estran-studio/logviewer/pkg/ty"

// FieldRemapping handles remapping of field names.
type FieldRemapping struct{}

// RemapFieldSet remaps a set of fields.
func (m FieldRemapping) RemapFieldSet(fields ty.UniSet[string]) ty.UniSet[string] {

	return fields
}

// RemapField remaps a single field.
func (m FieldRemapping) RemapField(field ty.MI) ty.MI {

	return field
}
